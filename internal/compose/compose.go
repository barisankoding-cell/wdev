package compose

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"

	"gopkg.in/yaml.v3"
)

var (
	projectNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	profileNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	composeFileRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}\.(ya?ml)$`)

	sensitivePortLabels = map[int]string{
		22:    "SSH",
		2375:  "Docker Daemon TCP",
		2376:  "Docker Daemon TLS",
		3306:  "MySQL/MariaDB",
		5432:  "PostgreSQL",
		6379:  "Redis/Valkey",
		8081:  "PowerDNS API",
		8082:  "Edge Agent",
		9000:  "MinIO/FastCGI/Portainer",
		9200:  "Elasticsearch",
		11211: "Memcached",
		27017: "MongoDB",
	}

	composeFilenames = []string{
		"compose.yaml",
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
	}
)

// CommandRunner abstraksi eksekusi CLI untuk memudahkan pengujian unit dan fallback sudo.
type CommandRunner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
	Stream(ctx context.Context, dir string, cb func(string), args ...string) error
	LookPath(bin string) (string, error)
}

type defaultRunner struct {
	sudoPassFn func() string
}

func (r *defaultRunner) LookPath(bin string) (string, error) {
	if p, err := exec.LookPath(bin); err == nil {
		return p, nil
	}
	for _, candidate := range []string{"/usr/bin/" + bin, "/usr/local/bin/" + bin} {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func isDockerPermissionErr(out []byte, err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(low, "permission denied") &&
		(strings.Contains(low, "docker.sock") || strings.Contains(low, "got permission denied while trying to connect"))
}

func (r *defaultRunner) buildCmd(ctx context.Context, useSudo bool, dir string, args []string) (*exec.Cmd, bool) {
	if !useSudo || os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, "docker", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		return cmd, false
	}
	if exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil {
		sudoArgs := append([]string{"-n", "docker"}, args...)
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
		if dir != "" {
			cmd.Dir = dir
		}
		return cmd, false
	}
	if r.sudoPassFn != nil {
		if pass := r.sudoPassFn(); pass != "" {
			sudoArgs := append([]string{"-S", "-p", "", "docker"}, args...)
			cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
			cmd.Stdin = strings.NewReader(pass + "\n")
			if dir != "" {
				cmd.Dir = dir
			}
			return cmd, true
		}
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd, false
}

func (r *defaultRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd, _ := r.buildCmd(ctx, false, dir, args)
	out, err := cmd.CombinedOutput()
	if isDockerPermissionErr(out, err) && os.Geteuid() != 0 {
		cmdSudo, hasPass := r.buildCmd(ctx, true, dir, args)
		if hasPass || exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil {
			out2, err2 := cmdSudo.CombinedOutput()
			if err2 == nil {
				return out2, nil
			}
			return out2, err2
		}
	}
	return out, err
}

func (r *defaultRunner) Stream(ctx context.Context, dir string, cb func(string), args ...string) error {
	useSudo := false
	if os.Geteuid() != 0 {
		// Cek cepat hanya bila perlu eskalasi izin socket Docker agar password sudo tidak terpakai sia-sia.
		if out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); isDockerPermissionErr(out, err) {
			useSudo = true
		}
	}
	cmd, _ := r.buildCmd(ctx, useSudo, dir, args)
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return err
	}
	_ = pw.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if cb != nil {
				cb(sc.Text())
			}
		}
	}()

	waitErr := cmd.Wait()
	_ = pr.Close()
	wg.Wait()
	return waitErr
}

// Manager mengelola proyek Docker Compose v5 dan siklus hidup container.
type Manager struct {
	cfg        *config.Config
	store      projectRegistry
	runner     CommandRunner
	sudoPassFn func() string
}

// New membuat manajer Docker Compose dengan akses ke config, SQLite store, dan penyedia password sudo opsional.
func New(cfg *config.Config, s *store.Store, sudoPassFn func() string) *Manager {
	var reg projectRegistry
	if s != nil {
		reg = s
	}
	return &Manager{
		cfg:        cfg,
		store:      reg,
		runner:     &defaultRunner{sudoPassFn: sudoPassFn},
		sudoPassFn: sudoPassFn,
	}
}

// NewWithDeps membuat Manager dengan dependensi custom (untuk pengujian unit).
func NewWithDeps(cfg *config.Config, reg projectRegistry, runner CommandRunner) *Manager {
	return &Manager{
		cfg:    cfg,
		store:  reg,
		runner: runner,
	}
}

// ValidateProjectName memastikan nama proyek Compose aman dan sesuai standar Docker Compose.
func ValidateProjectName(name string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if !projectNameRe.MatchString(clean) {
		return "", fmt.Errorf("nama proyek tidak valid (gunakan huruf kecil a-z, angka 0-9, '-' atau '_', maksimal 63 karakter)")
	}
	return clean, nil
}

// ValidateServiceName memastikan nama service/job valid dan bebas injeksi argumen.
func ValidateServiceName(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if !serviceNameRe.MatchString(clean) {
		return "", fmt.Errorf("nama service tidak valid: %q", name)
	}
	return clean, nil
}

// ValidateComposeFilename memastikan nama file Compose hanya berupa nama file YAML relatif yang aman.
func ValidateComposeFilename(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "compose.yaml", nil
	}
	if strings.Contains(clean, "/") || strings.Contains(clean, "\\") || strings.Contains(clean, "..") {
		return "", fmt.Errorf("nama file compose tidak boleh mengandung path direktori")
	}
	if !composeFileRe.MatchString(clean) {
		return "", fmt.Errorf("nama file compose harus berakhiran .yaml atau .yml yang valid")
	}
	return clean, nil
}

// ValidateProjectDir memastikan direktori proyek berada di lokasi aman (webroot, home, /srv, /opt, /mnt, /media).
func (m *Manager) ValidateProjectDir(dirPath string) (string, error) {
	raw := strings.TrimSpace(dirPath)
	if raw == "" {
		return "", fmt.Errorf("direktori proyek wajib diisi")
	}
	if strings.ContainsAny(raw, "\x00\r\n;\"'`$\\") {
		return "", fmt.Errorf("direktori proyek mengandung karakter terlarang")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("direktori proyek harus berupa path absolut")
	}
	clean := filepath.Clean(raw)
	if clean == "/" {
		return "", fmt.Errorf("direktori root '/' tidak diizinkan sebagai proyek Compose")
	}
	allowed := []string{"/var/www", "/home", "/srv", "/opt", "/mnt", "/media"}
	if m != nil && m.cfg != nil && strings.TrimSpace(m.cfg.WebRootBase) != "" {
		allowed = append(allowed, filepath.Clean(m.cfg.WebRootBase))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		allowed = append(allowed, filepath.Clean(home))
	}
	ok := false
	for _, base := range allowed {
		if clean == base || strings.HasPrefix(clean, base+"/") {
			ok = true
			break
		}
	}
	if !ok {
		return "", fmt.Errorf("direktori proyek harus berada di bawah /var/www, /home, /srv, /opt, /mnt, atau /media")
	}
	return clean, nil
}

func (m *Manager) resolveComposeFile(dirPath, preferred string) string {
	if preferred != "" {
		if clean, err := ValidateComposeFilename(preferred); err == nil {
			cand := filepath.Join(dirPath, clean)
			if fi, statErr := os.Stat(cand); statErr == nil && !fi.IsDir() {
				return clean
			}
		}
	}
	for _, candName := range composeFilenames {
		cand := filepath.Join(dirPath, candName)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return candName
		}
	}
	if preferred != "" {
		if clean, err := ValidateComposeFilename(preferred); err == nil {
			return clean
		}
	}
	return "compose.yaml"
}

// GetEngineInfo mendeteksi biner docker, plugin docker compose (v5), serta konektivitas daemon Docker.
func (m *Manager) GetEngineInfo(ctx context.Context) EngineInfo {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var info EngineInfo
	if _, err := m.runner.LookPath("docker"); err != nil {
		return info
	}
	info.DockerInstalled = true

	if out, err := m.runner.Run(probeCtx, "", "version", "--format", "{{.Client.Version}}"); err == nil {
		ver := strings.TrimSpace(firstNonEmptyLine(string(out)))
		if ver != "" {
			info.DockerVersion = strings.TrimPrefix(ver, "v")
		}
	} else if out, err2 := m.runner.Run(probeCtx, "", "--version"); err2 == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			info.DockerVersion = strings.TrimSuffix(strings.TrimPrefix(fields[2], "v"), ",")
		}
	}

	if out, err := m.runner.Run(probeCtx, "", "compose", "version", "--short"); err == nil {
		ver := strings.TrimSpace(firstNonEmptyLine(string(out)))
		if ver != "" {
			info.ComposeInstalled = true
			info.ComposeVersion = strings.TrimPrefix(ver, "v")
			info.ComposeMajor = parseMajorVersion(info.ComposeVersion)
			info.IsComposeV5 = info.ComposeMajor >= 5
		}
	} else if out, err2 := m.runner.Run(probeCtx, "", "compose", "version"); err2 == nil {
		for _, tok := range strings.Fields(string(out)) {
			clean := strings.TrimPrefix(strings.TrimSpace(tok), "v")
			if major := parseMajorVersion(clean); major > 0 && strings.Contains(clean, ".") {
				info.ComposeInstalled = true
				info.ComposeVersion = clean
				info.ComposeMajor = major
				info.IsComposeV5 = major >= 5
				break
			}
		}
	}

	out, err := m.runner.Run(probeCtx, "", "info", "--format", "{{.ServerVersion}}|{{.DockerRootDir}}")
	if err == nil {
		line := strings.TrimSpace(firstNonEmptyLine(string(out)))
		parts := strings.SplitN(line, "|", 2)
		if len(parts) >= 1 && parts[0] != "" {
			info.DaemonRunning = true
			if info.DockerVersion == "" {
				info.DockerVersion = strings.TrimPrefix(strings.TrimSpace(parts[0]), "v")
			}
		}
		if len(parts) == 2 {
			info.DockerRootDir = strings.TrimSpace(parts[1])
		}
	} else {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if isDockerPermissionErr(out, err) {
			info.NeedsSudo = true
			info.DaemonError = "Memerlukan izin akses socket Docker (sudo atau grup docker)."
		} else {
			info.DaemonError = msg
		}
	}

	if info.DaemonRunning {
		if apiOut, apiErr := m.runner.Run(probeCtx, "", "version", "--format", "{{.Server.APIVersion}}"); apiErr == nil {
			info.APIVersion = strings.TrimSpace(firstNonEmptyLine(string(apiOut)))
		}
	}

	return info
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t != "" {
			return t
		}
	}
	return ""
}

func parseMajorVersion(v string) int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return 0
	}
	head := strings.SplitN(v, ".", 2)[0]
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0
	}
	return n
}

type composeLSEntry struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

func parseComposeLSOutput(raw []byte) []composeLSEntry {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var list []composeLSEntry
		if err := json.Unmarshal(trimmed, &list); err == nil {
			return list
		}
	}
	var out []composeLSEntry
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		l := bytes.TrimSpace(line)
		if len(l) == 0 {
			continue
		}
		var item composeLSEntry
		if err := json.Unmarshal(l, &item); err == nil && item.Name != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseComposeStatusSummary(rawStatus string) (normalized string, runningCount int, totalFromStatus int) {
	low := strings.ToLower(strings.TrimSpace(rawStatus))
	if low == "" {
		return "stopped", 0, 0
	}
	// Format umum: "running(3)", "running(2), exited(1)", "exited(2)", "created(1)"
	rePart := regexp.MustCompile(`([a-z]+)\((\d+)\)`)
	matches := rePart.FindAllStringSubmatch(low, -1)
	if len(matches) == 0 {
		if strings.Contains(low, "running") {
			return "running", 1, 1
		}
		if strings.Contains(low, "exited") || strings.Contains(low, "dead") {
			return "exited", 0, 1
		}
		return "stopped", 0, 0
	}
	other := 0
	for _, m := range matches {
		state := m[1]
		cnt, _ := strconv.Atoi(m[2])
		totalFromStatus += cnt
		if state == "running" {
			runningCount += cnt
		} else {
			other += cnt
		}
	}
	if runningCount > 0 && other == 0 {
		return "running", runningCount, totalFromStatus
	}
	if runningCount > 0 && other > 0 {
		return "partial", runningCount, totalFromStatus
	}
	if strings.Contains(low, "exited") || strings.Contains(low, "dead") {
		return "exited", 0, totalFromStatus
	}
	return "stopped", 0, totalFromStatus
}

// ParseComposeYAML mengurai definisi services, jobs, ports, profiles, dan healthcheck dari YAML Compose.
func ParseComposeYAML(yamlContent []byte) (services []ServiceSpec, availableProfiles []string, hasJobs bool, err error) {
	var root struct {
		Services map[string]yaml.Node `yaml:"services"`
		Jobs     map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(yamlContent, &root); err != nil {
		return nil, nil, false, fmt.Errorf("sintaks YAML tidak valid: %w", err)
	}

	profileSet := make(map[string]bool)
	parseServiceNode := func(name string, node yaml.Node, isJob bool) ServiceSpec {
		var raw struct {
			Image         string   `yaml:"image"`
			Build         yaml.Node `yaml:"build"`
			ContainerName string   `yaml:"container_name"`
			Restart       string   `yaml:"restart"`
			Ports         []yaml.Node `yaml:"ports"`
			Profiles      []string `yaml:"profiles"`
			DependsOn     yaml.Node `yaml:"depends_on"`
			Volumes       []yaml.Node `yaml:"volumes"`
			Healthcheck   yaml.Node `yaml:"healthcheck"`
			Triggers      struct {
				Schedule []struct {
					Cron string `yaml:"cron"`
				} `yaml:"schedule"`
			} `yaml:"triggers"`
		}
		_ = node.Decode(&raw)

		buildStr := ""
		if raw.Build.Kind == yaml.ScalarNode {
			buildStr = strings.TrimSpace(raw.Build.Value)
		} else if raw.Build.Kind == yaml.MappingNode {
			var b struct {
				Context    string `yaml:"context"`
				Dockerfile string `yaml:"dockerfile"`
			}
			_ = raw.Build.Decode(&b)
			if b.Context != "" {
				buildStr = b.Context
			} else if b.Dockerfile != "" {
				buildStr = b.Dockerfile
			} else {
				buildStr = "custom build"
			}
		}

		var deps []string
		if raw.DependsOn.Kind == yaml.SequenceNode {
			_ = raw.DependsOn.Decode(&deps)
		} else if raw.DependsOn.Kind == yaml.MappingNode {
			var depMap map[string]any
			if derr := raw.DependsOn.Decode(&depMap); derr == nil {
				for k := range depMap {
					deps = append(deps, k)
				}
				sort.Strings(deps)
			}
		}

		var ports []PortBinding
		for _, pn := range raw.Ports {
			ports = append(ports, parseYAMLPortNode(name, pn)...)
		}

		for _, prof := range raw.Profiles {
			p := strings.TrimSpace(prof)
			if p != "" {
				profileSet[p] = true
			}
		}

		hasHC := false
		if raw.Healthcheck.Kind == yaml.MappingNode {
			var hc struct {
				Disable bool `yaml:"disable"`
			}
			_ = raw.Healthcheck.Decode(&hc)
			hasHC = !hc.Disable
		}

		cronExpr := ""
		if len(raw.Triggers.Schedule) > 0 {
			cronExpr = strings.TrimSpace(raw.Triggers.Schedule[0].Cron)
		}

		return ServiceSpec{
			Name:           name,
			Image:          strings.TrimSpace(raw.Image),
			Build:          buildStr,
			ContainerName:  strings.TrimSpace(raw.ContainerName),
			Restart:        strings.TrimSpace(raw.Restart),
			Ports:          ports,
			Profiles:       raw.Profiles,
			DependsOn:      deps,
			VolumesCount:   len(raw.Volumes),
			HasHealthcheck: hasHC,
			IsJob:          isJob,
			ScheduleCron:   cronExpr,
		}
	}

	names := make([]string, 0, len(root.Services))
	for name := range root.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		services = append(services, parseServiceNode(name, root.Services[name], false))
	}

	if len(root.Jobs) > 0 {
		hasJobs = true
		jobNames := make([]string, 0, len(root.Jobs))
		for name := range root.Jobs {
			jobNames = append(jobNames, name)
		}
		sort.Strings(jobNames)
		for _, name := range jobNames {
			services = append(services, parseServiceNode(name, root.Jobs[name], true))
		}
	}

	for prof := range profileSet {
		availableProfiles = append(availableProfiles, prof)
	}
	sort.Strings(availableProfiles)
	return services, availableProfiles, hasJobs, nil
}

func parseYAMLPortNode(service string, node yaml.Node) []PortBinding {
	if node.Kind == yaml.ScalarNode {
		if pb, ok := parsePortSpecString(service, node.Value); ok {
			return []PortBinding{pb}
		}
		return nil
	}
	if node.Kind == yaml.MappingNode {
		var longForm struct {
			HostIP    string `yaml:"host_ip"`
			Published string `yaml:"published"`
			Target    int    `yaml:"target"`
			Protocol  string `yaml:"protocol"`
		}
		if err := node.Decode(&longForm); err == nil && longForm.Target > 0 {
			hostPort, _ := strconv.Atoi(strings.Split(strings.TrimSpace(longForm.Published), "-")[0])
			hostIP := strings.TrimSpace(longForm.HostIP)
			if hostIP == "" && hostPort > 0 {
				hostIP = "0.0.0.0"
			}
			proto := strings.ToLower(strings.TrimSpace(longForm.Protocol))
			if proto == "" {
				proto = "tcp"
			}
			return []PortBinding{buildPortBinding(service, hostIP, hostPort, longForm.Target, proto)}
		}
	}
	return nil
}

func splitPortSpecTokens(s string) []string {
	var parts []string
	var cur strings.Builder
	braceDepth := 0
	bracketDepth := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '{':
			braceDepth++
			cur.WriteByte(ch)
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
			cur.WriteByte(ch)
		case '[':
			bracketDepth++
			cur.WriteByte(ch)
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			cur.WriteByte(ch)
		case ':':
			if braceDepth == 0 && bracketDepth == 0 {
				parts = append(parts, cur.String())
				cur.Reset()
				continue
			}
			cur.WriteByte(ch)
		default:
			cur.WriteByte(ch)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

func parsePortSpecString(service, spec string) (PortBinding, bool) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return PortBinding{}, false
	}
	proto := "tcp"
	if idx := strings.LastIndex(s, "/"); idx > 0 {
		p := strings.ToLower(strings.TrimSpace(s[idx+1:]))
		if p == "tcp" || p == "udp" {
			proto = p
			s = s[:idx]
		}
	}
	// Format umum: containerPort | hostPort:containerPort | hostIP:hostPort:containerPort
	var hostIP string
	var hostPort, containerPort int

	parts := splitPortSpecTokens(s)
	switch len(parts) {
	case 1:
		containerPort = parsePortNum(parts[0])
	case 2:
		hostPort = parsePortNum(parts[0])
		containerPort = parsePortNum(parts[1])
		if hostPort > 0 {
			hostIP = "0.0.0.0"
		}
	case 3:
		hostIP = strings.Trim(strings.TrimSpace(parts[0]), "[]")
		hostPort = parsePortNum(parts[1])
		containerPort = parsePortNum(parts[2])
		if hostIP == "" && hostPort > 0 {
			hostIP = "0.0.0.0"
		}
	default:
		return PortBinding{}, false
	}
	if containerPort <= 0 {
		return PortBinding{}, false
	}
	return buildPortBinding(service, hostIP, hostPort, containerPort, proto), true
}

func parsePortNum(raw string) int {
	clean := strings.TrimSpace(raw)
	// Strip ${VAR:-default} -> check default if numeric
	if strings.HasPrefix(clean, "${") && strings.HasSuffix(clean, "}") {
		inner := clean[2 : len(clean)-1]
		if idx := strings.Index(inner, ":-"); idx >= 0 {
			clean = inner[idx+2:]
		} else if idx := strings.Index(inner, "-"); idx >= 0 {
			clean = inner[idx+1:]
		}
	}
	if dash := strings.Index(clean, "-"); dash > 0 {
		clean = clean[:dash]
	}
	n, err := strconv.Atoi(strings.TrimSpace(clean))
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

func buildPortBinding(service, hostIP string, hostPort, containerPort int, proto string) PortBinding {
	if proto == "" {
		proto = "tcp"
	}
	exposedPublic := hostPort > 0 && (hostIP == "" || hostIP == "0.0.0.0" || hostIP == "::")
	_, sensitive := sensitivePortLabels[containerPort]
	if !sensitive && hostPort > 0 {
		_, sensitive = sensitivePortLabels[hostPort]
	}
	summary := fmt.Sprintf("%d/%s", containerPort, proto)
	if hostPort > 0 {
		if hostIP != "" {
			summary = fmt.Sprintf("%s:%d->%d/%s", hostIP, hostPort, containerPort, proto)
		} else {
			summary = fmt.Sprintf("%d->%d/%s", hostPort, containerPort, proto)
		}
	}
	return PortBinding{
		Service:       service,
		HostIP:        hostIP,
		HostPort:      hostPort,
		ContainerPort: containerPort,
		Protocol:      proto,
		ExposedPublic: exposedPublic,
		SensitivePort: sensitive,
		Summary:       summary,
	}
}

func analyzeSecurityWarnings(ports []PortBinding) []string {
	var warnings []string
	seen := make(map[string]bool)
	for _, p := range ports {
		if p.HostPort <= 0 || !p.ExposedPublic || !p.SensitivePort {
			continue
		}
		label := sensitivePortLabels[p.ContainerPort]
		if label == "" {
			label = sensitivePortLabels[p.HostPort]
		}
		if label == "" {
			label = "Layanan Internal"
		}
		msg := fmt.Sprintf("Service %q mempublikasikan port %d (%s) ke 0.0.0.0 (melewati UFW). Gunakan 127.0.0.1:%d:%d bila hanya untuk lokal/proxy.", p.Service, p.HostPort, label, p.HostPort, p.ContainerPort)
		if !seen[msg] {
			seen[msg] = true
			warnings = append(warnings, msg)
		}
	}
	return warnings
}

// ListProjects menggabungkan proyek Compose terdaftar di SQLite dan yang terdeteksi dari `docker compose ls --all`.
func (m *Manager) ListProjects(ctx context.Context) ([]Project, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var regRecords []store.ComposeProjectRecord
	if m.store != nil {
		var err error
		regRecords, err = m.store.ListComposeProjects()
		if err != nil {
			return nil, err
		}
	}

	var sites []store.SiteRecord
	if m.store != nil {
		sites, _ = m.store.ListSites()
	}
	sitesByPort := make(map[int][]LinkedVHost)
	for _, s := range sites {
		if strings.EqualFold(s.Framework, "proxy") && s.ProxyPort > 0 {
			sitesByPort[s.ProxyPort] = append(sitesByPort[s.ProxyPort], LinkedVHost{
				Domain:     s.Domain,
				ProxyPort:  s.ProxyPort,
				SSLEnabled: s.SSLEnabled,
				ExposedTo:  s.ExposedTo,
			})
		}
	}

	discoveredMap := make(map[string]composeLSEntry)
	if out, err := m.runner.Run(probeCtx, "", "compose", "ls", "--all", "--format", "json"); err == nil {
		for _, item := range parseComposeLSOutput(out) {
			if item.Name != "" {
				discoveredMap[strings.ToLower(item.Name)] = item
			}
		}
	}

	byName := make(map[string]*Project)
	for _, rec := range regRecords {
		key := strings.ToLower(rec.Name)
		compFile := m.resolveComposeFile(rec.DirPath, rec.ComposeFile)
		var profs []string
		for _, p := range strings.Split(rec.Profiles, ",") {
			if clean := strings.TrimSpace(p); clean != "" {
				profs = append(profs, clean)
			}
		}
		p := &Project{
			Name:           rec.Name,
			DirPath:        rec.DirPath,
			ComposeFile:    compFile,
			Description:    rec.Description,
			Status:         "stopped",
			Source:         "registered",
			Registered:     true,
			ActiveProfiles: profs,
		}
		if !rec.CreatedAt.IsZero() {
			p.CreatedAt = rec.CreatedAt.Format(time.RFC3339)
		}
		if !rec.UpdatedAt.IsZero() {
			p.UpdatedAt = rec.UpdatedAt.Format(time.RFC3339)
		}
		if disc, ok := discoveredMap[key]; ok {
			p.Source = "both"
			p.RawStatus = disc.Status
			if disc.ConfigFiles != "" {
				for _, cf := range strings.Split(disc.ConfigFiles, ",") {
					if c := strings.TrimSpace(cf); c != "" {
						p.ConfigFiles = append(p.ConfigFiles, c)
					}
				}
			}
			st, runCnt, totCnt := parseComposeStatusSummary(disc.Status)
			p.Status = st
			p.RunningCount = runCnt
			if totCnt > 0 {
				p.ServicesCount = totCnt
			}
			delete(discoveredMap, key)
		}
		byName[key] = p
	}

	for key, disc := range discoveredMap {
		var configFiles []string
		dirPath := ""
		compFile := "compose.yaml"
		for _, cf := range strings.Split(disc.ConfigFiles, ",") {
			if c := strings.TrimSpace(cf); c != "" {
				configFiles = append(configFiles, c)
				if dirPath == "" {
					dirPath = filepath.Dir(c)
					compFile = filepath.Base(c)
				}
			}
		}
		st, runCnt, totCnt := parseComposeStatusSummary(disc.Status)
		byName[key] = &Project{
			Name:          disc.Name,
			DirPath:       dirPath,
			ComposeFile:   compFile,
			ConfigFiles:   configFiles,
			Status:        st,
			RawStatus:     disc.Status,
			Source:        "discovered",
			Registered:    false,
			RunningCount:  runCnt,
			ServicesCount: totCnt,
		}
	}

	projects := make([]Project, 0, len(byName))
	for _, p := range byName {
		m.enrichProjectFromDisk(p, sitesByPort)
		projects = append(projects, *p)
	}

	sort.Slice(projects, func(i, j int) bool {
		// Running projects first, then alphabetical
		rank := func(s string) int {
			switch s {
			case "running":
				return 0
			case "partial":
				return 1
			case "exited":
				return 2
			case "stopped":
				return 3
			default:
				return 4
			}
		}
		if rank(projects[i].Status) != rank(projects[j].Status) {
			return rank(projects[i].Status) < rank(projects[j].Status)
		}
		return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name)
	})

	return projects, nil
}

func (m *Manager) enrichProjectFromDisk(p *Project, sitesByPort map[int][]LinkedVHost) {
	if p.DirPath == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(p.DirPath, ".env")); err == nil {
		p.HasEnvFile = true
	}
	fullComposePath := filepath.Join(p.DirPath, p.ComposeFile)
	if len(p.ConfigFiles) > 0 && p.ComposeFile == "" {
		fullComposePath = p.ConfigFiles[0]
	}
	data, err := os.ReadFile(fullComposePath)
	if err != nil {
		if p.Status == "stopped" {
			p.Status = "missing"
		}
		return
	}
	p.FileExists = true

	services, availProfiles, hasJobs, perr := ParseComposeYAML(data)
	if perr != nil {
		p.ParseError = perr.Error()
		return
	}
	p.DefinedServices = services
	p.AvailableProfiles = availProfiles
	p.HasJobs = hasJobs
	if len(services) > p.ServicesCount {
		p.ServicesCount = len(services)
	}

	var allPorts []PortBinding
	linkedSeen := make(map[string]bool)
	for _, svc := range services {
		for _, pb := range svc.Ports {
			allPorts = append(allPorts, pb)
			if pb.HostPort > 0 && sitesByPort != nil {
				for _, vh := range sitesByPort[pb.HostPort] {
					if !linkedSeen[vh.Domain] {
						linkedSeen[vh.Domain] = true
						p.LinkedVHosts = append(p.LinkedVHosts, vh)
					}
				}
			}
		}
	}
	p.ExposedPorts = allPorts
	p.SecurityWarnings = analyzeSecurityWarnings(allPorts)
}

// GetProject mengambil detail lengkap satu proyek Compose termasuk daftar container & statistik CPU/RAM.
func (m *Manager) GetProject(ctx context.Context, name string) (*Project, error) {
	cleanName, err := ValidateProjectName(name)
	if err != nil {
		return nil, err
	}
	projects, err := m.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var found *Project
	for i := range projects {
		if strings.EqualFold(projects[i].Name, cleanName) {
			cp := projects[i]
			found = &cp
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("proyek Compose %q tidak ditemukan", cleanName)
	}

	containers, _ := m.inspectProjectContainers(ctx, found)
	found.Containers = containers
	if len(containers) > 0 {
		running := 0
		for _, c := range containers {
			if strings.EqualFold(c.State, "running") {
				running++
			}
		}
		found.RunningCount = running
		if len(containers) > found.ServicesCount {
			found.ServicesCount = len(containers)
		}
		if m.store != nil {
			if sites, serr := m.store.ListSites(); serr == nil {
				linkedSeen := make(map[string]bool)
				for _, vh := range found.LinkedVHosts {
					linkedSeen[vh.Domain] = true
				}
				for _, c := range containers {
					for _, pb := range c.Ports {
						if pb.HostPort <= 0 {
							continue
						}
						for _, s := range sites {
							if strings.EqualFold(s.Framework, "proxy") && s.ProxyPort == pb.HostPort && !linkedSeen[s.Domain] {
								linkedSeen[s.Domain] = true
								found.LinkedVHosts = append(found.LinkedVHosts, LinkedVHost{
									Domain:     s.Domain,
									ProxyPort:  s.ProxyPort,
									SSLEnabled: s.SSLEnabled,
									ExposedTo:  s.ExposedTo,
								})
							}
						}
					}
				}
			}
		}
	}
	return found, nil
}

func (m *Manager) composeBaseArgs(p *Project, profiles []string) []string {
	var args []string
	args = append(args, "compose", "-p", p.Name)
	if p.DirPath != "" && p.ComposeFile != "" {
		candidate := filepath.Join(p.DirPath, p.ComposeFile)
		if _, err := os.Stat(candidate); err == nil {
			args = append(args, "-f", candidate)
		}
	} else if len(p.ConfigFiles) > 0 {
		for _, cf := range p.ConfigFiles {
			if _, err := os.Stat(cf); err == nil {
				args = append(args, "-f", cf)
			}
		}
	}
	for _, prof := range profiles {
		if clean := strings.TrimSpace(prof); clean != "" && profileNameRe.MatchString(clean) {
			args = append(args, "--profile", clean)
		}
	}
	return args
}

type composePSRaw struct {
	ID         string `json:"ID"`
	Name       string `json:"Name"`
	Service    string `json:"Service"`
	Image      string `json:"Image"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	Health     string `json:"Health"`
	ExitCode   int    `json:"ExitCode"`
	Command    string `json:"Command"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

type dockerStatsRaw struct {
	Container string `json:"Container"`
	Name      string `json:"Name"`
	ID        string `json:"ID"`
	CPUPerc   string `json:"CPUPerc"`
	MemUsage  string `json:"MemUsage"`
	MemPerc   string `json:"MemPerc"`
	NetIO     string `json:"NetIO"`
	BlockIO   string `json:"BlockIO"`
	PIDs      string `json:"PIDs"`
}

func parseComposePSOutput(raw []byte) []composePSRaw {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var list []composePSRaw
		if err := json.Unmarshal(trimmed, &list); err == nil {
			return list
		}
	}
	var out []composePSRaw
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		l := bytes.TrimSpace(line)
		if len(l) == 0 {
			continue
		}
		var item composePSRaw
		if err := json.Unmarshal(l, &item); err == nil && (item.Name != "" || item.ID != "") {
			out = append(out, item)
		}
	}
	return out
}

func (m *Manager) inspectProjectContainers(ctx context.Context, p *Project) ([]ContainerStatus, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	args := append(m.composeBaseArgs(p, p.AvailableProfiles), "ps", "--all", "--format", "json")
	out, err := m.runner.Run(probeCtx, p.DirPath, args...)
	if err != nil {
		return nil, err
	}
	psItems := parseComposePSOutput(out)
	if len(psItems) == 0 {
		return nil, nil
	}

	var runningNames []string
	for _, item := range psItems {
		if strings.EqualFold(item.State, "running") && item.Name != "" {
			runningNames = append(runningNames, item.Name)
		}
	}

	statsMap := make(map[string]dockerStatsRaw)
	if len(runningNames) > 0 {
		statArgs := append([]string{"stats", "--no-stream", "--format", "json"}, runningNames...)
		if stOut, stErr := m.runner.Run(probeCtx, "", statArgs...); stErr == nil {
			for _, line := range bytes.Split(bytes.TrimSpace(stOut), []byte("\n")) {
				l := bytes.TrimSpace(line)
				if len(l) == 0 {
					continue
				}
				var sr dockerStatsRaw
				if json.Unmarshal(l, &sr) == nil {
					if sr.Name != "" {
						statsMap[sr.Name] = sr
					}
					if sr.ID != "" {
						statsMap[sr.ID] = sr
					}
				}
			}
		}
	}

	var result []ContainerStatus
	for _, item := range psItems {
		var ports []PortBinding
		seenPort := make(map[string]bool)
		for _, pub := range item.Publishers {
			if pub.TargetPort <= 0 {
				continue
			}
			pb := buildPortBinding(item.Service, pub.URL, pub.PublishedPort, pub.TargetPort, pub.Protocol)
			if !seenPort[pb.Summary] {
				seenPort[pb.Summary] = true
				ports = append(ports, pb)
			}
		}
		cs := ContainerStatus{
			ID:       item.ID,
			Name:     item.Name,
			Service:  item.Service,
			Image:    item.Image,
			State:    strings.ToLower(item.State),
			Status:   item.Status,
			Health:   strings.ToLower(item.Health),
			ExitCode: item.ExitCode,
			Command:  item.Command,
			Ports:    ports,
		}
		if sr, ok := statsMap[item.Name]; ok {
			cs.CPUPerc = sr.CPUPerc
			cs.MemUsage = sr.MemUsage
			cs.MemPerc = sr.MemPerc
			cs.NetIO = sr.NetIO
			cs.BlockIO = sr.BlockIO
			cs.PIDs = sr.PIDs
		}
		result = append(result, cs)
	}
	return result, nil
}

// CreateOrRegisterProject mendaftarkan direktori Compose yang sudah ada atau membuat stack baru dari template/YAML.
func (m *Manager) CreateOrRegisterProject(ctx context.Context, opts CreateProjectOptions) (*Project, error) {
	cleanName, err := ValidateProjectName(opts.Name)
	if err != nil {
		return nil, err
	}
	cleanDir, err := m.ValidateProjectDir(opts.DirPath)
	if err != nil {
		return nil, err
	}
	compFile, err := ValidateComposeFilename(opts.ComposeFile)
	if err != nil {
		return nil, err
	}

	var validProfiles []string
	for _, p := range opts.Profiles {
		cleanP := strings.TrimSpace(p)
		if cleanP == "" {
			continue
		}
		if !profileNameRe.MatchString(cleanP) {
			return nil, fmt.Errorf("nama profile tidak valid: %q", p)
		}
		validProfiles = append(validProfiles, cleanP)
	}

	if err := os.MkdirAll(cleanDir, 0755); err != nil {
		return nil, fmt.Errorf("gagal membuat direktori proyek %s: %w", cleanDir, err)
	}

	targetComposePath := filepath.Join(cleanDir, compFile)
	hasCustomCompose := strings.TrimSpace(opts.ComposeContent) != ""
	var chosenTemplate *TemplateSpec
	if !hasCustomCompose && strings.TrimSpace(opts.Template) != "" {
		for _, tpl := range StarterTemplates() {
			if tpl.ID == strings.TrimSpace(opts.Template) {
				tCopy := tpl
				chosenTemplate = &tCopy
				break
			}
		}
		if chosenTemplate == nil {
			return nil, fmt.Errorf("template Compose tidak dikenal: %q", opts.Template)
		}
	}

	if hasCustomCompose || chosenTemplate != nil {
		content := opts.ComposeContent
		envContent := opts.EnvContent
		if !hasCustomCompose && chosenTemplate != nil {
			content = strings.ReplaceAll(chosenTemplate.ComposeContent, "{{PROJECT_NAME}}", cleanName)
			if strings.TrimSpace(envContent) == "" {
				envContent = strings.ReplaceAll(chosenTemplate.EnvContent, "{{PROJECT_NAME}}", cleanName)
			}
		}

		if _, _, _, yErr := ParseComposeYAML([]byte(content)); yErr != nil {
			return nil, yErr
		}

		if _, statErr := os.Stat(targetComposePath); statErr == nil && !opts.Overwrite {
			return nil, fmt.Errorf("berkas %s sudah ada di %s (aktifkan timpa berkas atau kosongkan template untuk mendaftarkan folder yang ada)", compFile, cleanDir)
		}
		if err := os.WriteFile(targetComposePath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("gagal menulis %s: %w", targetComposePath, err)
		}
		if strings.TrimSpace(envContent) != "" {
			envPath := filepath.Join(cleanDir, ".env")
			if _, statErr := os.Stat(envPath); statErr != nil || opts.Overwrite {
				if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
					return nil, fmt.Errorf("gagal menulis .env: %w", err)
				}
			}
		}
	} else {
		// Mode registrasi direktori yang sudah ada: cari file compose di dalam folder
		resolved := m.resolveComposeFile(cleanDir, compFile)
		if _, statErr := os.Stat(filepath.Join(cleanDir, resolved)); statErr != nil {
			return nil, fmt.Errorf("tidak ditemukan berkas Compose (compose.yaml / docker-compose.yml) di %s", cleanDir)
		}
		compFile = resolved
	}

	if m.store != nil {
		err := m.store.SaveComposeProject(store.ComposeProjectRecord{
			Name:        cleanName,
			DirPath:     cleanDir,
			ComposeFile: compFile,
			Description: strings.TrimSpace(opts.Description),
			Profiles:    strings.Join(validProfiles, ","),
		})
		if err != nil {
			return nil, fmt.Errorf("gagal menyimpan proyek Compose ke database: %w", err)
		}
	}

	return m.GetProject(ctx, cleanName)
}

// DeleteProject menghapus registrasi proyek dari wdev (dan opsional menjalankan compose down terlebih dahulu).
func (m *Manager) DeleteProject(ctx context.Context, name string, runDown bool) error {
	cleanName, err := ValidateProjectName(name)
	if err != nil {
		return err
	}
	if runDown {
		if proj, gerr := m.GetProject(ctx, cleanName); gerr == nil && proj != nil {
			args := append(m.composeBaseArgs(proj, proj.ActiveProfiles), "down")
			if out, derr := m.runner.Run(ctx, proj.DirPath, args...); derr != nil {
				return fmt.Errorf("gagal menghentikan stack sebelum hapus: %s (%w)", strings.TrimSpace(string(out)), derr)
			}
		}
	}
	if m.store != nil {
		return m.store.DeleteComposeProject(cleanName)
	}
	return nil
}

// GetProjectFiles membaca isi file Compose YAML dan .env dari proyek.
func (m *Manager) GetProjectFiles(ctx context.Context, name string) (*ProjectFiles, error) {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return nil, err
	}
	if proj.DirPath == "" {
		return nil, fmt.Errorf("direktori proyek %q tidak diketahui", proj.Name)
	}
	compFile := m.resolveComposeFile(proj.DirPath, proj.ComposeFile)
	compPath := filepath.Join(proj.DirPath, compFile)
	compBytes, err := os.ReadFile(compPath)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca %s: %w", compPath, err)
	}

	envPath := filepath.Join(proj.DirPath, ".env")
	var envContent string
	hasEnv := false
	if envBytes, err := os.ReadFile(envPath); err == nil {
		hasEnv = true
		envContent = string(envBytes)
	}

	return &ProjectFiles{
		ProjectName:    proj.Name,
		DirPath:        proj.DirPath,
		ComposeFile:    compFile,
		ComposeContent: string(compBytes),
		EnvContent:     envContent,
		HasEnvFile:     hasEnv,
		EnvKeys:        extractEnvKeys(envContent),
	}, nil
}

func extractEnvKeys(envContent string) []string {
	var keys []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(envContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		if k, _, ok := strings.Cut(trimmed, "="); ok {
			key := strings.TrimSpace(k)
			if key != "" && !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// SaveProjectFiles memvalidasi sintaks YAML + `docker compose config --quiet` lalu menyimpan compose.yaml & .env.
func (m *Manager) SaveProjectFiles(ctx context.Context, name, composeContent, envContent string, updateEnv bool) (*ProjectFiles, error) {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return nil, err
	}
	if proj.DirPath == "" {
		return nil, fmt.Errorf("direktori proyek %q tidak diketahui", proj.Name)
	}
	if _, _, _, yErr := ParseComposeYAML([]byte(composeContent)); yErr != nil {
		return nil, yErr
	}

	compFile := m.resolveComposeFile(proj.DirPath, proj.ComposeFile)
	compPath := filepath.Join(proj.DirPath, compFile)
	envPath := filepath.Join(proj.DirPath, ".env")

	prevCompose, compReadErr := os.ReadFile(compPath)
	prevEnv, envReadErr := os.ReadFile(envPath)

	if err := os.WriteFile(compPath, []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("gagal menulis %s: %w", compPath, err)
	}
	if updateEnv {
		if strings.TrimSpace(envContent) == "" && envReadErr != nil {
			// Jangan buat .env kosong bila sebelumnya tidak ada
		} else if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
			if compReadErr == nil {
				_ = os.WriteFile(compPath, prevCompose, 0644)
			}
			return nil, fmt.Errorf("gagal menulis .env: %w", err)
		}
	}

	// Jika docker compose tersedia, lakukan validasi `docker compose config --quiet` dan rollback bila gagal
	if _, lpErr := m.runner.LookPath("docker"); lpErr == nil {
		valCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		args := append(m.composeBaseArgs(proj, proj.AvailableProfiles), "config", "--quiet")
		if out, valErr := m.runner.Run(valCtx, proj.DirPath, args...); valErr != nil {
			if compReadErr == nil {
				_ = os.WriteFile(compPath, prevCompose, 0644)
			}
			if updateEnv {
				if envReadErr == nil {
					_ = os.WriteFile(envPath, prevEnv, 0600)
				} else {
					_ = os.Remove(envPath)
				}
			}
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = valErr.Error()
			}
			return nil, fmt.Errorf("validasi `docker compose config` gagal (perubahan dibatalkan): %s", msg)
		}
	}

	return m.GetProjectFiles(ctx, proj.Name)
}

// RenderResolvedConfig menjalankan `docker compose config --format json` (Compose v5 spec resolver).
func (m *Manager) RenderResolvedConfig(ctx context.Context, name string) (json.RawMessage, error) {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return nil, err
	}
	cfgCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	args := append(m.composeBaseArgs(proj, proj.ActiveProfiles), "config", "--format", "json")
	out, err := m.runner.Run(cfgCtx, proj.DirPath, args...)
	if err != nil {
		return nil, fmt.Errorf("gagal merender konfigurasi Compose: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	trimmed := bytes.TrimSpace(out)
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("output config bukan JSON valid")
	}
	return json.RawMessage(trimmed), nil
}

// BuildComposeArgs menyusun argumen `docker compose` yang aman untuk eksekusi aksi stack.
func (m *Manager) BuildComposeArgs(p *Project, opts ActionOptions) ([]string, error) {
	action := strings.ToLower(strings.TrimSpace(opts.Action))
	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = p.ActiveProfiles
	}
	base := m.composeBaseArgs(p, profiles)

	var svc string
	if strings.TrimSpace(opts.Service) != "" {
		cleanSvc, err := ValidateServiceName(opts.Service)
		if err != nil {
			return nil, err
		}
		svc = cleanSvc
	}

	switch action {
	case "up":
		args := append(base, "up", "-d")
		if opts.Build {
			args = append(args, "--build")
		}
		if opts.ForceRecreate {
			args = append(args, "--force-recreate")
		}
		if opts.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
		if svc != "" {
			args = append(args, svc)
		}
		return args, nil
	case "down":
		args := append(base, "down")
		if opts.RemoveVolumes {
			args = append(args, "--volumes")
		}
		if opts.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
		return args, nil
	case "start":
		args := append(base, "start")
		if svc != "" {
			args = append(args, svc)
		}
		return args, nil
	case "stop":
		args := append(base, "stop")
		if svc != "" {
			args = append(args, svc)
		}
		return args, nil
	case "restart":
		args := append(base, "restart")
		if svc != "" {
			args = append(args, svc)
		}
		return args, nil
	case "pull":
		args := append(base, "pull")
		if svc != "" {
			args = append(args, svc)
		}
		return args, nil
	case "build":
		args := append(base, "build")
		if opts.NoCache {
			args = append(args, "--no-cache")
		}
		if svc != "" {
			args = append(args, svc)
		}
		return args, nil
	case "run-job":
		if svc == "" {
			return nil, fmt.Errorf("nama job atau service wajib diisi untuk run-job")
		}
		return append(base, "run", "--rm", svc), nil
	default:
		return nil, fmt.Errorf("aksi Compose tidak valid: %q", opts.Action)
	}
}

// RunActionTask menjalankan aksi lifecycle Compose secara streaming ke log callback.
func (m *Manager) RunActionTask(ctx context.Context, name string, opts ActionOptions, cb func(string)) error {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return err
	}
	args, err := m.BuildComposeArgs(proj, opts)
	if err != nil {
		return err
	}
	if cb != nil {
		cb(fmt.Sprintf("==> Proyek: %s (%s)", proj.Name, proj.DirPath))
		cb("$ docker " + strings.Join(args, " "))
	}

	if err := m.runner.Stream(ctx, proj.DirPath, cb, args...); err != nil {
		return fmt.Errorf("docker compose %s gagal: %w", opts.Action, err)
	}

	// Simpan pilihan profile terbaru bila dijalankan dengan profile eksplisit on `up`
	if strings.EqualFold(opts.Action, "up") && opts.Profiles != nil && m.store != nil && proj.Registered {
		if rec, gerr := m.store.GetComposeProject(proj.Name); gerr == nil && rec != nil {
			rec.Profiles = strings.Join(opts.Profiles, ",")
			_ = m.store.SaveComposeProject(*rec)
		}
	}

	if cb != nil {
		cb(fmt.Sprintf("✓ Aksi docker compose %s selesai untuk proyek %s.", opts.Action, proj.Name))
	}
	return nil
}

// GetLogs mengambil log container milik proyek Compose.
func (m *Manager) GetLogs(ctx context.Context, name, service string, tail int) (string, error) {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return "", err
	}
	if tail <= 0 || tail > 2000 {
		tail = 200
	}
	args := append(m.composeBaseArgs(proj, proj.ActiveProfiles), "logs", "--no-color", "--timestamps", "--tail", strconv.Itoa(tail))
	if strings.TrimSpace(service) != "" {
		svc, serr := ValidateServiceName(service)
		if serr != nil {
			return "", serr
		}
		args = append(args, svc)
	}
	logCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := m.runner.Run(logCtx, proj.DirPath, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gagal mengambil log Compose: %s", msg)
	}
	return string(out), nil
}

// StreamLogs mengalirkan `docker compose logs -f` secara real-time melalui callback.
func (m *Manager) StreamLogs(ctx context.Context, name, service string, tail int, cb func(string)) error {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return err
	}
	if tail <= 0 || tail > 1000 {
		tail = 100
	}
	args := append(m.composeBaseArgs(proj, proj.ActiveProfiles), "logs", "-f", "--no-color", "--timestamps", "--tail", strconv.Itoa(tail))
	if strings.TrimSpace(service) != "" {
		svc, serr := ValidateServiceName(service)
		if serr != nil {
			return serr
		}
		args = append(args, svc)
	}
	return m.runner.Stream(ctx, proj.DirPath, cb, args...)
}

// BuildExecCommand menyusun command `docker compose exec` yang tervalidasi untuk sesi PTY terminal.
func (m *Manager) BuildExecCommand(ctx context.Context, name, service, shell string) (dir string, args []string, display string, err error) {
	proj, err := m.GetProject(ctx, name)
	if err != nil {
		return "", nil, "", err
	}
	svc, err := ValidateServiceName(service)
	if err != nil {
		return "", nil, "", err
	}
	sh := strings.TrimSpace(shell)
	switch sh {
	case "", "sh", "/bin/sh":
		sh = "/bin/sh"
	case "bash", "/bin/bash":
		sh = "/bin/bash"
	case "ash", "/bin/ash":
		sh = "/bin/ash"
	default:
		return "", nil, "", fmt.Errorf("shell container %q tidak diizinkan (gunakan sh, bash, atau ash)", shell)
	}

	base := m.composeBaseArgs(proj, proj.ActiveProfiles)
	execArgs := append(base, "exec", svc, sh)
	return proj.DirPath, execArgs, fmt.Sprintf("%s/%s (%s)", proj.Name, svc, sh), nil
}

// GetDiskUsage membaca penggunaan disk Docker (`docker system df --format json`).
func (m *Manager) GetDiskUsage(ctx context.Context) (*DiskUsageSummary, error) {
	dfCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := m.runner.Run(dfCtx, "", "system", "df", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("gagal memeriksa penggunaan disk Docker: %s", strings.TrimSpace(string(out)))
	}
	var items []DiskUsageItem
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) > 0 {
		if trimmed[0] == '[' {
			_ = json.Unmarshal(trimmed, &items)
		} else {
			for _, line := range bytes.Split(trimmed, []byte("\n")) {
				l := bytes.TrimSpace(line)
				if len(l) == 0 {
					continue
				}
				var item DiskUsageItem
				if json.Unmarshal(l, &item) == nil && item.Type != "" {
					items = append(items, item)
				}
			}
		}
	}
	return &DiskUsageSummary{Items: items}, nil
}

// Prune menjalankan pembersihan resource Docker yang tidak terpakai (`containers`, `images`, `builder`, `networks`, `volumes`).
func (m *Manager) Prune(ctx context.Context, target string, cb func(string)) error {
	var args []string
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "containers":
		args = []string{"container", "prune", "-f"}
	case "images":
		args = []string{"image", "prune", "-f"}
	case "images-all":
		args = []string{"image", "prune", "-a", "-f"}
	case "builder", "build-cache":
		args = []string{"builder", "prune", "-f"}
	case "networks":
		args = []string{"network", "prune", "-f"}
	case "volumes":
		args = []string{"volume", "prune", "-f"}
	case "system":
		args = []string{"system", "prune", "-f"}
	default:
		return fmt.Errorf("target prune tidak valid: %q", target)
	}
	if cb != nil {
		cb("$ docker " + strings.Join(args, " "))
	}
	if err := m.runner.Stream(ctx, "", cb, args...); err != nil {
		return fmt.Errorf("docker %s gagal: %w", strings.Join(args, " "), err)
	}
	if cb != nil {
		cb("✓ Pembersihan Docker selesai.")
	}
	return nil
}

// StarterTemplates menyediakan template YAML Compose Specification v5 modern (tanpa key `version:` usang).
func StarterTemplates() []TemplateSpec {
	return []TemplateSpec{
		{
			ID:          "nginx-static",
			Name:        "Web App / Nginx Static",
			Category:    "Web",
			Description: "Layanan web Nginx ringan terikat ke 127.0.0.1 dengan healthcheck dan volume HTML.",
			DefaultPort: 8080,
			ComposeContent: `name: {{PROJECT_NAME}}

services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "127.0.0.1:${APP_PORT:-8080}:80"
    volumes:
      - ./html:/usr/share/nginx/html:ro
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1/ >/dev/null || exit 1"]
      interval: 15s
      timeout: 5s
      retries: 3
`,
			EnvContent: `APP_PORT=8080
`,
		},
		{
			ID:          "postgres-redis",
			Name:        "PostgreSQL + Redis Stack",
			Category:    "Database",
			Description: "Database PostgreSQL 17 & Redis 7 terisolasi pada loopback 127.0.0.1 lengkap dengan healthcheck.",
			DefaultPort: 5432,
			ComposeContent: `name: {{PROJECT_NAME}}

services:
  postgres:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${POSTGRES_DB:-app}
      POSTGRES_USER: ${POSTGRES_USER:-app}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
    ports:
      - "127.0.0.1:${POSTGRES_PORT:-5432}:5432"
    volumes:
      - pg_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-app} -d ${POSTGRES_DB:-app}"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes"]
    ports:
      - "127.0.0.1:${REDIS_PORT:-6379}:6379"
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  pg_data:
  redis_data:
`,
			EnvContent: `POSTGRES_DB=app
POSTGRES_USER=app
POSTGRES_PASSWORD=changeme_strong_password
POSTGRES_PORT=5432
REDIS_PORT=6379
`,
		},
		{
			ID:          "app-postgres-redis",
			Name:        "App Backend + Postgres + Redis",
			Category:    "Fullstack",
			Description: "Pola aplikasi Node/Go dengan depends_on service_healthy ke PostgreSQL & Redis.",
			DefaultPort: 3000,
			ComposeContent: `name: {{PROJECT_NAME}}

services:
  app:
    image: node:22-alpine
    working_dir: /app
    command: ["sh", "-c", "node server.js"]
    restart: unless-stopped
    ports:
      - "127.0.0.1:${APP_PORT:-3000}:3000"
    environment:
      DATABASE_URL: postgresql://${POSTGRES_USER:-app}:${POSTGRES_PASSWORD:-changeme}@db:5432/${POSTGRES_DB:-app}
      REDIS_URL: redis://cache:6379
    volumes:
      - ./app:/app
    depends_on:
      db:
        condition: service_healthy
      cache:
        condition: service_healthy

  db:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${POSTGRES_DB:-app}
      POSTGRES_USER: ${POSTGRES_USER:-app}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
    volumes:
      - db_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-app} -d ${POSTGRES_DB:-app}"]
      interval: 10s
      timeout: 5s
      retries: 5

  cache:
    image: redis:7-alpine
    restart: unless-stopped
    volumes:
      - cache_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  db_data:
  cache_data:
`,
			EnvContent: `APP_PORT=3000
POSTGRES_DB=app
POSTGRES_USER=app
POSTGRES_PASSWORD=changeme_strong_password
`,
		},
		{
			ID:          "wordpress",
			Name:        "WordPress + MariaDB",
			Category:    "CMS",
			Description: "Stack WordPress siap diekspos ke Nginx VHost SSL wdev melalui port lokal 127.0.0.1.",
			DefaultPort: 8085,
			ComposeContent: `name: {{PROJECT_NAME}}

services:
  wordpress:
    image: wordpress:php8.3-apache
    restart: unless-stopped
    ports:
      - "127.0.0.1:${WP_PORT:-8085}:80"
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_USER: ${MYSQL_USER:-wpuser}
      WORDPRESS_DB_PASSWORD: ${MYSQL_PASSWORD:-wppass}
      WORDPRESS_DB_NAME: ${MYSQL_DATABASE:-wordpress}
    volumes:
      - wp_content:/var/www/html
    depends_on:
      db:
        condition: service_healthy

  db:
    image: mariadb:11
    restart: unless-stopped
    environment:
      MYSQL_DATABASE: ${MYSQL_DATABASE:-wordpress}
      MYSQL_USER: ${MYSQL_USER:-wpuser}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-wppass}
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-rootpass}
    volumes:
      - db_data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  wp_content:
  db_data:
`,
			EnvContent: `WP_PORT=8085
MYSQL_DATABASE=wordpress
MYSQL_USER=wpuser
MYSQL_PASSWORD=changeme_wp_password
MYSQL_ROOT_PASSWORD=changeme_root_password
`,
		},
		{
			ID:          "blank",
			Name:        "Compose Kosong (Custom)",
			Category:    "Custom",
			Description: "Kerangka Compose Specification v5 minimalis untuk ditulis atau disesuaikan sendiri.",
			DefaultPort: 8000,
			ComposeContent: `name: {{PROJECT_NAME}}

services:
  app:
    image: alpine:latest
    command: ["sleep", "infinity"]
    restart: unless-stopped
    # Contoh ekspos port aman untuk Reverse Proxy wdev:
    # ports:
    #   - "127.0.0.1:8000:8000"
`,
			EnvContent: `# Environment variables untuk proyek {{PROJECT_NAME}}
`,
		},
	}
}

// Keep io imported cleanly if needed in future or verify compile
var _ = errors.Is
var _ = io.EOF
