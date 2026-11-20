package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"wdev/internal/version"
)

type SystemStats struct {
	Hostname     string            `json:"hostname"`
	OSName       string            `json:"os_name"`
	Kernel       string            `json:"kernel"`
	UptimeString string            `json:"uptime_string"`
	LoadAvg      string            `json:"load_avg"`
	MemTotalMB   int64             `json:"mem_total_mb"`
	MemUsedMB    int64             `json:"mem_used_mb"`
	MemPercent   int               `json:"mem_percent"`
	DiskTotalGB  int64             `json:"disk_total_gb"`
	DiskUsedGB   int64             `json:"disk_used_gb"`
	DiskPercent  int               `json:"disk_percent"`
	LanIP        string            `json:"lan_ip"`
	NetbirdIP    string            `json:"netbird_ip"`
	Services     []ServiceStatus   `json:"services"`
	Runtimes     map[string]string `json:"runtimes"`
}

type ServiceStatus struct {
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	Category         string `json:"category"`
	Active           bool   `json:"active"`
	StatusText       string `json:"status_text"`
	Version          string `json:"version,omitempty"`
	Port             string `json:"port,omitempty"`
	IsRemote         bool   `json:"is_remote,omitempty"`
	Installed        bool   `json:"installed"`
	LatestVersion    string `json:"latest_version,omitempty"`
	HasUpdate        bool   `json:"has_update"`
	InstallComponent string `json:"install_component,omitempty"`
}

type serviceVersionCache struct {
	mu        sync.RWMutex
	checkedAt time.Time
	latest    map[string]string
}

var globalServiceCache = &serviceVersionCache{
	latest: make(map[string]string),
}

func init() {
	go RefreshLatestVersions(false)
}

func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "rust-")
	v = strings.TrimPrefix(v, "go")
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "version ")
	return v
}

func compareVersions(v1, v2 string) int {
	v1Clean := cleanVersion(v1)
	v2Clean := cleanVersion(v2)
	if v1Clean == v2Clean {
		return 0
	}
	parts1 := strings.Split(strings.Split(v1Clean, "-")[0], ".")
	parts2 := strings.Split(strings.Split(v2Clean, "-")[0], ".")
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			n1, _ = strconv.Atoi(strings.TrimRight(parts1[i], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"))
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(strings.TrimRight(parts2[i], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"))
		}
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}
	return 0
}

func hasNewerVersion(installed, latest string) bool {
	instClean := cleanVersion(installed)
	latClean := cleanVersion(latest)
	if instClean == "" || latClean == "" || latClean == "-" {
		return false
	}
	if instClean == latClean {
		return false
	}
	if strings.HasPrefix(instClean, latClean) || strings.Contains(instClean, latClean) {
		return false
	}
	return compareVersions(instClean, latClean) < 0
}

func Collect() (*SystemStats, error) {
	hostname, _ := os.Hostname()

	osName := getOSName()
	kernel := getKernel()
	uptimeStr := getUptime()
	loadAvg := getLoadAvg()
	memTotal, memUsed, memPct := getMemInfo()
	diskTotal, diskUsed, diskPct := getDiskInfo("/")
	lanIP, netbirdIP := getNetworkIPs()
	services := getServiceStatuses()
	runtimes := getRuntimeVersions()

	return &SystemStats{
		Hostname:     hostname,
		OSName:       osName,
		Kernel:       kernel,
		UptimeString: uptimeStr,
		LoadAvg:      loadAvg,
		MemTotalMB:   memTotal,
		MemUsedMB:    memUsed,
		MemPercent:   memPct,
		DiskTotalGB:  diskTotal,
		DiskUsedGB:   diskUsed,
		DiskPercent:  diskPct,
		LanIP:        lanIP,
		NetbirdIP:    netbirdIP,
		Services:     services,
		Runtimes:     runtimes,
	}, nil
}

func getOSName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return "Linux"
}

func getKernel() string {
	out, err := exec.Command("uname", "-r").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func getUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "N/A"
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return "N/A"
	}
	secondsFloat, _ := strconv.ParseFloat(parts[0], 64)
	totalSec := int64(secondsFloat)
	days := totalSec / 86400
	hours := (totalSec % 86400) / 3600
	minutes := (totalSec % 3600) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func getLoadAvg() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "N/A"
	}
	parts := strings.Fields(string(data))
	if len(parts) >= 3 {
		return fmt.Sprintf("%s, %s, %s", parts[0], parts[1], parts[2])
	}
	return "N/A"
}

func getMemInfo() (totalMB int64, usedMB int64, percent int) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}

	var totalKB, availKB int64
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			switch fields[0] {
			case "MemTotal:":
				totalKB, _ = strconv.ParseInt(fields[1], 10, 64)
			case "MemAvailable:":
				availKB, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}

	if totalKB > 0 {
		usedKB := totalKB - availKB
		totalMB = totalKB / 1024
		usedMB = usedKB / 1024
		percent = int((float64(usedKB) / float64(totalKB)) * 100)
	}
	return
}

func getDiskInfo(path string) (totalGB int64, usedGB int64, percent int) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes

	totalGB = int64(totalBytes / (1024 * 1024 * 1024))
	usedGB = int64(usedBytes / (1024 * 1024 * 1024))
	if totalBytes > 0 {
		percent = int((float64(usedBytes) / float64(totalBytes)) * 100)
	}
	return
}

func getNetworkIPs() (lanIP string, netbirdIP string) {
	lanIP = "127.0.0.1"
	netbirdIP = "-"

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ipnet, ok := a.(*net.IPNet)
				if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
					continue
				}
				ipStr := ipnet.IP.String()
				if iface.Name == "wt0" {
					netbirdIP = ipStr
				} else if !strings.HasPrefix(iface.Name, "docker") && !strings.HasPrefix(iface.Name, "br-") && lanIP == "127.0.0.1" {
					lanIP = ipStr
				}
			}
		}
	}
	return
}

func isSystemdServiceActive(serviceName string) bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", serviceName)
	return cmd.Run() == nil
}

func IsSystemdServiceActive(serviceName string) bool {
	return isSystemdServiceActive(serviceName)
}

func GetServiceStatus(serviceName string) ServiceStatus {
	stats, err := Collect()
	if err == nil {
		for _, s := range stats.Services {
			if s.Name == serviceName {
				return s
			}
		}
	}
	active := isSystemdServiceActive(serviceName)
	return ServiceStatus{
		Name:       serviceName,
		Active:     active,
		StatusText: statusText(active),
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func getAptCandidates(ctx context.Context, pkgs ...string) map[string]string {
	res := make(map[string]string)
	for _, p := range pkgs {
		res[p] = "-"
	}
	if len(pkgs) == 0 {
		return res
	}

	args := append([]string{"policy"}, pkgs...)
	out, err := exec.CommandContext(ctx, "apt-cache", args...).CombinedOutput()
	if err != nil {
		return res
	}

	lines := strings.Split(string(out), "\n")
	currentPkg := ""
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
			pkg := strings.TrimSuffix(trimmed, ":")
			if _, ok := res[pkg]; ok {
				currentPkg = pkg
			} else {
				currentPkg = ""
			}
			continue
		}
		if currentPkg != "" && strings.HasPrefix(trimmed, "Candidate:") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				cand := parts[1]
				cand = strings.Split(cand, "+")[0]
				cand = strings.Split(cand, "~")[0]
				cand = strings.Split(cand, "-")[0]
				cand = strings.TrimPrefix(cand, "5:")
				res[currentPkg] = cand
			}
			currentPkg = ""
		}
	}
	return res
}

func fetchCoreLatestVersions(ctx context.Context) map[string]string {
	latest := make(map[string]string)
	client := &http.Client{Timeout: 3 * time.Second}

	var wg sync.WaitGroup
	var mu sync.Mutex

	setVal := func(key, val string) {
		if val == "" || val == "-" {
			return
		}
		mu.Lock()
		latest[key] = val
		mu.Unlock()
	}

	// 1. APT / DNF candidates
	wg.Add(1)
	go func() {
		defer wg.Done()
		pgMajor := GetActivePostgresMajor()
		pgPkg := fmt.Sprintf("postgresql-%s", pgMajor)
		if _, err := exec.LookPath("apt-cache"); err == nil {
			cands := getAptCandidates(ctx, "nginx", "php8.4", pgPkg, "redis", "redis-server", "netbird")
			if c, ok := cands["nginx"]; ok && c != "-" {
				setVal("nginx", c)
			}
			if c, ok := cands["php8.4"]; ok && c != "-" {
				setVal("php-fpm", c)
			}
			if c, ok := cands[pgPkg]; ok && c != "-" {
				setVal("postgresql", c)
			}
			if c, ok := cands["redis"]; ok && c != "-" {
				setVal("redis", c)
			} else if c, ok := cands["redis-server"]; ok && c != "-" {
				setVal("redis", c)
			}
			if c, ok := cands["netbird"]; ok && c != "-" {
				setVal("netbird", c)
			}
		} else if dnfBin, err := exec.LookPath("dnf"); err == nil {
			queryDnf := func(pkgs ...string) string {
				best := ""
				for _, p := range pkgs {
					var out []byte
					var qErr error
					for _, extraArgs := range [][]string{{"-q", "-C", "repoquery"}, {"-q", "repoquery"}} {
						args := append(extraArgs, "--latest-limit=1", "--qf", "%{version}\n", p)
						cmd := exec.CommandContext(ctx, dnfBin, args...)
						cmd.Stdin = strings.NewReader("n\n")
						out, qErr = cmd.CombinedOutput()
						if qErr == nil && strings.TrimSpace(string(out)) != "" {
							break
						}
					}
					if qErr != nil {
						continue
					}
					for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
						l = strings.TrimSpace(l)
						if l == "" || strings.Contains(l, " ") || !strings.ContainsAny(l, "0123456789") || strings.Contains(l, ":") {
							continue
						}
						if best == "" || compareVersions(best, l) < 0 {
							best = l
						}
					}
					if best != "" {
						return best
					}
				}
				return "-"
			}
			setVal("nginx", queryDnf("nginx"))
			setVal("php-fpm", queryDnf("php"))
			setVal("postgresql", queryDnf(fmt.Sprintf("postgresql%s-server", pgMajor), "postgresql-server"))
			setVal("redis", queryDnf("redis"))
			setVal("netbird", queryDnf("netbird"))
		}
	}()

	// 2. Cloudflared (GitHub release)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/cloudflare/cloudflared/releases/latest", nil)
		if err == nil {
			req.Header.Set("User-Agent", "wdev-control-plane")
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var rel struct {
					TagName string `json:"tag_name"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil && rel.TagName != "" {
					setVal("cloudflared", strings.TrimPrefix(rel.TagName, "v"))
				}
			}
		}
	}()

	setVal("pdns", "4.9.18")
	setVal("stalwart", "1.1.1")
	setVal("wdev", version.Version)

	wg.Wait()
	return latest
}

// RefreshLatestVersions memperbarui cache versi rilis terbaru untuk layanan inti.
func RefreshLatestVersions(force bool) {
	globalServiceCache.mu.Lock()
	if !force && !globalServiceCache.checkedAt.IsZero() && time.Since(globalServiceCache.checkedAt) < 15*time.Minute {
		globalServiceCache.mu.Unlock()
		return
	}
	globalServiceCache.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	latest := fetchCoreLatestVersions(ctx)

	globalServiceCache.mu.Lock()
	for k, v := range latest {
		globalServiceCache.latest[k] = v
	}
	globalServiceCache.checkedAt = time.Now()
	globalServiceCache.mu.Unlock()
}

func getServiceStatuses() []ServiceStatus {
	var list []ServiceStatus

	globalServiceCache.mu.RLock()
	cachedLatest := make(map[string]string)
	for k, v := range globalServiceCache.latest {
		cachedLatest[k] = v
	}
	cacheChecked := !globalServiceCache.checkedAt.IsZero()
	globalServiceCache.mu.RUnlock()

	if !cacheChecked {
		go RefreshLatestVersions(false)
	}

	// 1. Nginx
	nginxActive := isSystemdServiceActive("nginx")
	nginxVer := getNginxVersion()
	nginxInstalled := nginxVer != "" || fileExists("/usr/sbin/nginx") || fileExists("/usr/bin/nginx")
	nginxLatest := cachedLatest["nginx"]
	if nginxLatest == "" && nginxVer != "" {
		nginxLatest = cleanVersion(nginxVer)
	}
	list = append(list, ServiceStatus{
		Name:             "nginx",
		DisplayName:      "Nginx Web Server",
		Category:         "Web Stack",
		Active:           nginxActive,
		StatusText:       statusText(nginxActive),
		Version:          nginxVer,
		Port:             "80, 443",
		Installed:        nginxInstalled,
		LatestVersion:    nginxLatest,
		HasUpdate:        nginxInstalled && hasNewerVersion(nginxVer, nginxLatest),
		InstallComponent: "nginx",
	})

	// 2. PHP-FPM (check multiple versions + Fedora/RHEL php-fpm.service)
	phpActive := false
	phpVerActive := ""
	for _, v := range []string{"8.4", "8.3", "8.2", "8.1"} {
		if isSystemdServiceActive("php" + v + "-fpm") {
			phpActive = true
			phpVerActive = v
			break
		}
	}
	if !phpActive && isSystemdServiceActive("php-fpm") {
		phpActive = true
		if out := runCmdStdout("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;"); out != "" {
			phpVerActive = parsePHPMajorMinor(out)
		}
	}
	phpDisplayName := "PHP-FPM"
	if phpVerActive != "" {
		phpDisplayName = fmt.Sprintf("PHP-FPM (%s)", phpVerActive)
	}
	phpVer := getPHPFPMVersion(phpVerActive)
	phpInstalled := phpVer != "" || fileExists("/usr/sbin/php-fpm8.4") || fileExists("/usr/sbin/php-fpm") || fileExists("/usr/bin/php")
	phpLatest := cachedLatest["php-fpm"]
	if phpLatest == "" && phpVer != "" {
		phpLatest = cleanVersion(phpVer)
	}
	list = append(list, ServiceStatus{
		Name:             "php-fpm",
		DisplayName:      phpDisplayName,
		Category:         "Web Stack",
		Active:           phpActive,
		StatusText:       statusText(phpActive),
		Version:          phpVer,
		Port:             "Unix Socket",
		Installed:        phpInstalled,
		LatestVersion:    phpLatest,
		HasUpdate:        phpInstalled && hasNewerVersion(phpVer, phpLatest),
		InstallComponent: "php",
	})

	// 3. PostgreSQL
	pgActive := isSystemdServiceActive("postgresql")
	pgInstalled := fileExists("/usr/bin/psql") || fileExists("/usr/bin/postgres") || dirExists("/usr/lib/postgresql")
	for _, v := range supportedPostgresMajors {
		if !pgActive && isSystemdServiceActive("postgresql-"+v) {
			pgActive = true
		}
		if !pgInstalled && dirExists("/usr/pgsql-"+v) {
			pgInstalled = true
		}
	}
	pgVer := getPostgresVersion()
	if pgVer != "" {
		pgInstalled = true
	}
	pgLatest := cachedLatest["postgresql"]
	if pgLatest == "" && pgVer != "" {
		pgLatest = cleanVersion(pgVer)
	}
	list = append(list, ServiceStatus{
		Name:             "postgresql",
		DisplayName:      "PostgreSQL Database",
		Category:         "Database",
		Active:           pgActive,
		StatusText:       statusText(pgActive),
		Version:          pgVer,
		Port:             "5432",
		Installed:        pgInstalled,
		LatestVersion:    pgLatest,
		HasUpdate:        pgInstalled && hasNewerVersion(pgVer, pgLatest),
		InstallComponent: "postgres",
	})

	// 4. Redis / Valkey
	redisActive := isSystemdServiceActive("redis-server") || isSystemdServiceActive("valkey-server") || isSystemdServiceActive("valkey") || isSystemdServiceActive("redis")
	redisVer := getRedisVersion()
	redisInstalled := redisVer != "" || fileExists("/usr/bin/redis-server") || fileExists("/usr/bin/valkey-server") || fileExists("/usr/bin/valkey-cli")
	redisLatest := cachedLatest["redis"]
	if redisLatest == "" && redisVer != "" {
		redisLatest = cleanVersion(redisVer)
	}
	list = append(list, ServiceStatus{
		Name:             "redis",
		DisplayName:      "Redis / Valkey Cache",
		Category:         "Database",
		Active:           redisActive,
		StatusText:       statusText(redisActive),
		Version:          redisVer,
		Port:             "6379",
		Installed:        redisInstalled,
		LatestVersion:    redisLatest,
		HasUpdate:        redisInstalled && hasNewerVersion(redisVer, redisLatest),
		InstallComponent: "redis",
	})

	// 5. Cloudflare Tunnel
	cfActive := isSystemdServiceActive("cloudflared")
	if !cfActive {
		_ = exec.Command("pgrep", "cloudflared").Run()
		cfActive = (exec.Command("pgrep", "cloudflared").Run() == nil)
	}
	cfVer := getCloudflaredVersion()
	cfInstalled := cfVer != "" || fileExists("/usr/local/bin/cloudflared") || fileExists("/usr/bin/cloudflared")
	cfLatest := cachedLatest["cloudflared"]
	if cfLatest == "" && cfVer != "" {
		cfLatest = cleanVersion(cfVer)
	}
	list = append(list, ServiceStatus{
		Name:             "cloudflared",
		DisplayName:      "Cloudflare Tunnel",
		Category:         "Network / Ingress",
		Active:           cfActive,
		StatusText:       statusText(cfActive),
		Version:          cfVer,
		Installed:        cfInstalled,
		LatestVersion:    cfLatest,
		HasUpdate:        cfInstalled && hasNewerVersion(cfVer, cfLatest),
		InstallComponent: "cloudflare",
	})

	// 6. Netbird
	nbActive := isSystemdServiceActive("netbird")
	nbVer := getNetbirdVersion()
	nbInstalled := nbVer != "" || fileExists("/usr/bin/netbird") || fileExists("/usr/local/bin/netbird")
	nbLatest := cachedLatest["netbird"]
	if nbLatest == "" && nbVer != "" {
		nbLatest = cleanVersion(nbVer)
	}
	list = append(list, ServiceStatus{
		Name:             "netbird",
		DisplayName:      "NetBird Mesh VPN",
		Category:         "Network / Ingress",
		Active:           nbActive,
		StatusText:       statusText(nbActive),
		Version:          nbVer,
		Port:             "wt0",
		Installed:        nbInstalled,
		LatestVersion:    nbLatest,
		HasUpdate:        nbInstalled && hasNewerVersion(nbVer, nbLatest),
		InstallComponent: "pdns",
	})

	// 7. PowerDNS (Local systemd or VPS Edge Gateway)
	pdnsStatus := getPDNSServiceStatus()
	pdnsVer := pdnsStatus.Version
	pdnsInstalled := pdnsStatus.IsRemote || pdnsVer != "" || fileExists("/usr/sbin/pdns_server")
	pdnsLatest := cachedLatest["pdns"]
	if pdnsLatest == "" && pdnsVer != "" {
		pdnsLatest = cleanVersion(pdnsVer)
	}
	pdnsStatus.Installed = pdnsInstalled
	pdnsStatus.LatestVersion = pdnsLatest
	pdnsStatus.HasUpdate = false
	pdnsStatus.InstallComponent = "pdns"
	list = append(list, pdnsStatus)

	// 8. Stalwart Mail Server
	stalwartActive := isSystemdServiceActive("stalwart")
	stalwartVer := getStalwartVersion()
	stalwartInstalled := stalwartVer != "" || fileExists("/usr/local/bin/stalwart") || fileExists("/etc/systemd/system/stalwart.service")
	stalwartLatest := cachedLatest["stalwart"]
	if stalwartLatest == "" && stalwartVer != "" {
		stalwartLatest = cleanVersion(stalwartVer)
	}
	list = append(list, ServiceStatus{
		Name:             "stalwart",
		DisplayName:      "Stalwart Mail Server",
		Category:         "DNS & Mail",
		Active:           stalwartActive,
		StatusText:       statusText(stalwartActive),
		Version:          stalwartVer,
		Port:             "25, 465, 993, 8080",
		Installed:        stalwartInstalled,
		LatestVersion:    stalwartLatest,
		HasUpdate:        false,
		InstallComponent: "mail",
	})

	// 9. wdev Control Plane Service
	wdevActive := isSystemdServiceActive("wdev")
	if !wdevActive {
		_ = exec.Command("pgrep", "-f", "wdev serve").Run()
		wdevActive = (exec.Command("pgrep", "-f", "wdev serve").Run() == nil)
	}
	list = append(list, ServiceStatus{
		Name:             "wdev",
		DisplayName:      "wdev Control Plane",
		Category:         "Core Service",
		Active:           wdevActive,
		StatusText:       statusText(wdevActive),
		Version:          "2.0.0",
		Port:             "3200",
		Installed:        true,
		LatestVersion:    "2.0.0",
		HasUpdate:        false,
		InstallComponent: "self-check",
	})

	return list
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func runCmdVersion(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func runCmdStdout(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func getNginxVersion() string {
	out := runCmdVersion("nginx", "-v")
	if out == "" {
		out = runCmdVersion("/usr/sbin/nginx", "-v")
	}
	if out == "" {
		return ""
	}
	if idx := strings.Index(out, "nginx/"); idx != -1 {
		ver := out[idx+len("nginx/"):]
		if end := strings.IndexAny(ver, " \t\n\r"); end != -1 {
			ver = ver[:end]
		}
		ver = strings.TrimSpace(ver)
		if ver != "" {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			return ver
		}
	}
	return ""
}

func parsePHPMajorMinor(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		parts := strings.Split(line, ".")
		if len(parts) == 2 && isDigits(parts[0]) && isDigits(parts[1]) {
			return line
		}
	}
	return ""
}

func parsePHPVersionFromOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "PHP" && isSemverLike(fields[1]) {
			return "v" + strings.TrimPrefix(fields[1], "v")
		}
		if len(fields) == 1 && isSemverLike(fields[0]) {
			return "v" + strings.TrimPrefix(fields[0], "v")
		}
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isSemverLike(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	base := strings.SplitN(s, "-", 2)[0]
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return false
	}
	return isDigits(parts[0]) && isDigits(parts[1])
}

func getPHPFPMVersion(activeVer string) string {
	candidates := [][]string{}
	if activeVer != "" {
		candidates = append(candidates,
			[]string{"php-fpm" + activeVer, "-v"},
			[]string{"/usr/sbin/php-fpm" + activeVer, "-v"},
			[]string{"php" + activeVer + "-fpm", "-v"},
			[]string{"/usr/sbin/php" + activeVer + "-fpm", "-v"},
		)
	}
	candidates = append(candidates,
		[]string{"php-fpm", "-v"},
		[]string{"/usr/sbin/php-fpm", "-v"},
	)
	for _, c := range candidates {
		if out := runCmdStdout(c[0], c[1:]...); out != "" {
			if ver := parsePHPVersionFromOutput(out); ver != "" {
				return ver
			}
		}
	}
	if out := runCmdStdout("php", "-r", "echo PHP_VERSION;"); out != "" {
		if ver := parsePHPVersionFromOutput(out); ver != "" {
			return ver
		}
	}
	if activeVer != "" {
		return "v" + activeVer
	}
	return ""
}

var supportedPostgresMajors = []string{"19", "18", "17", "16", "15", "14"}

func parsePostgresMajorFromVersion(out string) string {
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) < 3 {
		return ""
	}
	ver := strings.TrimPrefix(parts[2], "v")
	major := strings.SplitN(ver, ".", 2)[0]
	for _, r := range major {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return major
}

// GetActivePostgresMajor mengembalikan versi mayor cluster postgresql yang aktif (default "16").
func GetActivePostgresMajor() string {
	// 1. Debian/Ubuntu (pg_lsclusters)
	if out, err := exec.Command("pg_lsclusters", "-h").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[2] == "5432" && fields[3] == "online" {
				return fields[0]
			}
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 1 && fields[0] != "" {
				return fields[0]
			}
		}
	}

	// 2. Fedora/RHEL PGDG systemd units (postgresql-19 .. postgresql-14)
	for _, v := range supportedPostgresMajors {
		if isSystemdServiceActive("postgresql-" + v) {
			return v
		}
	}

	// 3. Fedora/RHEL distro unit (postgresql.service)
	if isSystemdServiceActive("postgresql") {
		if out := runCmdVersion("/usr/bin/postgres", "--version"); out != "" {
			if m := parsePostgresMajorFromVersion(out); m != "" {
				return m
			}
		}
	}

	// 4. Biner PGDG terpasang di /usr/pgsql-<major>
	for _, v := range supportedPostgresMajors {
		if fileExists(fmt.Sprintf("/usr/pgsql-%s/bin/postgres", v)) {
			return v
		}
	}

	// 5. Fallback dari postgres / psql di PATH
	if out := runCmdVersion("/usr/bin/postgres", "--version"); out != "" {
		if m := parsePostgresMajorFromVersion(out); m != "" {
			return m
		}
	}
	if out := runCmdVersion("psql", "--version"); out != "" {
		if m := parsePostgresMajorFromVersion(out); m != "" {
			return m
		}
	}
	return "16"
}

func getPostgresVersion() string {
	major := GetActivePostgresMajor()
	candidates := []string{
		fmt.Sprintf("/usr/lib/postgresql/%s/bin/postgres", major),
		fmt.Sprintf("/usr/pgsql-%s/bin/postgres", major),
		"/usr/bin/postgres",
		fmt.Sprintf("/usr/lib/postgresql/%s/bin/psql", major),
		fmt.Sprintf("/usr/pgsql-%s/bin/psql", major),
		"psql",
		"/usr/lib/postgresql/16/bin/psql",
	}
	var out string
	for _, bin := range candidates {
		if out = runCmdVersion(bin, "--version"); out != "" {
			break
		}
	}
	if out == "" {
		return ""
	}
	parts := strings.Fields(out)
	if len(parts) >= 3 {
		ver := parts[2]
		if !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		return ver
	}
	return ""
}

func getRedisVersion() string {
	out := runCmdVersion("redis-server", "--version")
	if out == "" {
		out = runCmdVersion("valkey-server", "--version")
	}
	if out == "" {
		return ""
	}
	if idx := strings.Index(out, "v="); idx != -1 {
		ver := out[idx+2:]
		if end := strings.IndexAny(ver, " \t\n\r"); end != -1 {
			ver = ver[:end]
		}
		ver = strings.TrimSpace(ver)
		if ver != "" {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			return ver
		}
	}
	return ""
}

func getCloudflaredVersion() string {
	out := runCmdVersion("cloudflared", "--version")
	if out == "" {
		out = runCmdVersion("/usr/local/bin/cloudflared", "--version")
	}
	if out == "" {
		return ""
	}
	parts := strings.Fields(out)
	if len(parts) >= 3 {
		ver := parts[2]
		if !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		return ver
	}
	return ""
}

func getNetbirdVersion() string {
	out := runCmdVersion("netbird", "version")
	if out == "" {
		out = runCmdVersion("/usr/bin/netbird", "version")
	}
	if out == "" {
		return ""
	}
	ver := strings.TrimSpace(out)
	if ver != "" {
		if !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		return ver
	}
	return ""
}

func getStalwartVersion() string {
	out := runCmdVersion("/usr/local/bin/stalwart", "-V")
	if out == "" {
		out = runCmdVersion("stalwart", "-V")
	}
	if out != "" {
		ver := strings.TrimSpace(out)
		if ver != "" {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			return ver
		}
	}
	out = runCmdVersion("stalwart-cli", "--version")
	if out != "" {
		parts := strings.Fields(out)
		if len(parts) >= 2 {
			ver := parts[1]
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			return ver
		}
	}
	return ""
}

func getLocalPDNSVersion() string {
	out := runCmdVersion("pdns_server", "--version")
	if out == "" {
		out = runCmdVersion("/usr/sbin/pdns_server", "--version")
	}
	if out == "" {
		return ""
	}
	parts := strings.Fields(out)
	for i, p := range parts {
		if (p == "Server" || p == "Authoritative") && i+1 < len(parts) {
			ver := parts[i+1]
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			return ver
		}
	}
	return ""
}

func loadEdgeDNSConf() (vpsIP, apiKey, port string) {
	paths := []string{"/etc/wdev/edge-dns.conf"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "wdev", "edge-dns.conf"))
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		port = "8081"
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			switch k {
			case "VPS_NETBIRD_IP":
				vpsIP = v
			case "API_KEY":
				apiKey = v
			case "PDNS_PORT":
				port = v
			}
		}
		if vpsIP != "" {
			return
		}
	}
	return
}

func getPDNSServiceStatus() ServiceStatus {
	// 1. Check local systemd service if installed locally
	if isSystemdServiceActive("pdns") {
		return ServiceStatus{
			Name:        "pdns",
			DisplayName: "PowerDNS Authoritative",
			Category:    "DNS & Mail",
			Active:      true,
			StatusText:  "Running (Local)",
			Version:     getLocalPDNSVersion(),
			Port:        "53, 8081",
			IsRemote:    false,
		}
	}

	// 2. Check Edge VPS Gateway via NetBird
	vpsIP, apiKey, port := loadEdgeDNSConf()
	if vpsIP != "" {
		if port == "" {
			port = "8081"
		}
		client := http.Client{Timeout: 1200 * time.Millisecond}
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%s/api/v1/servers/localhost", vpsIP, port), nil)
		if err == nil {
			req.Header.Set("X-API-Key", apiKey)
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var serverInfo struct {
						Version string `json:"version"`
					}
					_ = json.NewDecoder(resp.Body).Decode(&serverInfo)
					verStr := strings.TrimSpace(serverInfo.Version)
					if verStr != "" && !strings.HasPrefix(verStr, "v") {
						verStr = "v" + verStr
					}
					return ServiceStatus{
						Name:        "pdns",
						DisplayName: "PowerDNS Authoritative (VPS Edge)",
						Category:    "DNS & Mail",
						Active:      true,
						StatusText:  "Online",
						Version:     verStr,
						Port:        fmt.Sprintf("NetBird %s:%s", vpsIP, port),
						IsRemote:    true,
					}
				}
			}
		}
		return ServiceStatus{
			Name:        "pdns",
			DisplayName: "PowerDNS Authoritative (VPS Edge)",
			Category:    "DNS & Mail",
			Active:      false,
			StatusText:  "Unreachable (VPS Offline)",
			Port:        fmt.Sprintf("NetBird %s:%s", vpsIP, port),
			IsRemote:    true,
		}
	}

	return ServiceStatus{
		Name:        "pdns",
		DisplayName: "PowerDNS Authoritative",
		Category:    "DNS & Mail",
		Active:      false,
		StatusText:  "Not Installed / Unconfigured",
		Version:     getLocalPDNSVersion(),
		Port:        "53, 8081",
		IsRemote:    false,
	}
}

func statusText(active bool) string {
	if active {
		return "Running"
	}
	return "Stopped"
}

func getRuntimeVersions() map[string]string {
	res := make(map[string]string)

	// Node.js
	if out, err := runToolVersion("node", "-v"); err == nil {
		res["node"] = out
	} else {
		res["node"] = "Not Installed"
	}

	// PHP
	if out, err := runToolVersion("php", "-r", "echo PHP_VERSION;"); err == nil {
		res["php"] = "v" + out
	} else {
		res["php"] = "Not Installed"
	}

	// Go
	if out, err := runToolVersion("go", "version"); err == nil {
		if parts := strings.Fields(out); len(parts) >= 3 {
			res["go"] = parts[2]
		}
	} else {
		res["go"] = "Not Installed"
	}

	// Rust: rustc adalah shim rustup, butuh RUSTUP_HOME agar toolchain ketemu.
	if out, err := runToolVersion("rustc", "--version"); err == nil {
		if parts := strings.Fields(out); len(parts) >= 2 {
			res["rust"] = parts[1]
		}
	} else {
		res["rust"] = "Not Installed"
	}

	// Composer
	if out, err := runToolVersion("composer", "--version"); err == nil {
		if parts := strings.Fields(out); len(parts) >= 3 {
			res["composer"] = parts[2]
		}
	} else {
		res["composer"] = "Not Installed"
	}

	// PostgreSQL
	pgMajor := GetActivePostgresMajor()
	pgPsqlOut, pgPsqlErr := runToolVersion("psql", "--version")
	if pgPsqlErr != nil {
		pgdgPsql := fmt.Sprintf("/usr/pgsql-%s/bin/psql", pgMajor)
		if out := runCmdVersion(pgdgPsql, "--version"); out != "" {
			pgPsqlOut, pgPsqlErr = out, nil
		}
	}
	if pgPsqlErr == nil {
		if parts := strings.Fields(pgPsqlOut); len(parts) >= 3 {
			res["postgresql"] = parts[2]
		}
	} else {
		res["postgresql"] = "Not Installed"
	}

	// Flutter (opsional; hanya bila terpasang)
	if out, err := runToolVersion("flutter", "--version"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Flutter ") {
				if parts := strings.Fields(line); len(parts) >= 2 {
					res["flutter"] = parts[1]
				}
				break
			}
		}
	}

	return res
}

// ControlService executes systemctl start/stop/restart
func ControlService(serviceName, action string) error {
	switch action {
	case "start", "stop", "restart", "reload":
		// Allowed actions
	default:
		return fmt.Errorf("aksi tidak valid: %s", action)
	}

	// Map service alias
	actualService := serviceName
	switch serviceName {
	case "php-fpm":
		if _, err := os.Stat("/usr/lib/systemd/system/php-fpm.service"); err == nil {
			actualService = "php-fpm"
			break
		}
		// Find active or latest php on Debian/Ubuntu
		for _, v := range []string{"8.4", "8.3", "8.2", "8.1"} {
			if _, err := os.Stat(filepath.Join("/etc/systemd/system/multi-user.target.wants", "php"+v+"-fpm.service")); err == nil {
				actualService = "php" + v + "-fpm"
				break
			}
			if _, err := os.Stat("/lib/systemd/system/php" + v + "-fpm.service"); err == nil {
				actualService = "php" + v + "-fpm"
				break
			}
		}
	case "postgresql":
		actualService = resolvePostgresSystemdUnit()
	case "redis":
		if _, err := os.Stat("/usr/lib/systemd/system/valkey.service"); err == nil {
			actualService = "valkey"
		} else if _, err := os.Stat("/lib/systemd/system/valkey-server.service"); err == nil {
			actualService = "valkey-server"
		} else if _, err := os.Stat("/lib/systemd/system/redis-server.service"); err == nil {
			actualService = "redis-server"
		} else {
			actualService = "redis"
		}
	}

	if serviceName == "pdns" && !isSystemdServiceActive("pdns") {
		vpsIP, apiKey, port := loadEdgeDNSConf()
		if vpsIP != "" {
			client := http.Client{Timeout: 2 * time.Second}
			req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%s/api/v1/servers/localhost", vpsIP, port), nil)
			if err == nil {
				req.Header.Set("X-API-Key", apiKey)
				resp, err := client.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					_ = resp.Body.Close()
					return nil
				}
			}
			return fmt.Errorf("gagal terhubung ke PowerDNS di VPS Edge (%s:%s)", vpsIP, port)
		}
	}

	cmd := exec.Command("systemctl", action, actualService)
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "systemctl", action, actualService)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s gagal: %s", action, actualService, string(out))
	}
	return nil
}

// resolvePostgresSystemdUnit menentukan nama unit systemd PostgreSQL yang tepat:
// memprioritaskan unit PGDG yang aktif (postgresql-16..19), lalu postgresql.service
// bawaan distro, lalu unit terpasang yang cocok dengan versi mayor aktif.
func resolvePostgresSystemdUnit() string {
	return pickPostgresSystemdUnit(
		isSystemdServiceActive,
		func(unit string) bool {
			return fileExists("/usr/lib/systemd/system/"+unit+".service") ||
				fileExists("/lib/systemd/system/"+unit+".service")
		},
		GetActivePostgresMajor(),
	)
}

func pickPostgresSystemdUnit(isActive, unitExists func(string) bool, activeMajor string) string {
	for _, v := range supportedPostgresMajors {
		unit := "postgresql-" + v
		if isActive(unit) {
			return unit
		}
	}
	if isActive("postgresql") {
		return "postgresql"
	}
	if activeMajor != "" {
		unit := "postgresql-" + activeMajor
		if unitExists(unit) {
			return unit
		}
	}
	for _, v := range supportedPostgresMajors {
		unit := "postgresql-" + v
		if unitExists(unit) {
			return unit
		}
	}
	return "postgresql"
}
