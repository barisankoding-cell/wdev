package switcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type RuntimeInfo struct {
	Name              string   `json:"name"`
	ActiveVersion     string   `json:"active_version"`
	AvailableVersions []string `json:"available_versions"`
}

type Manager struct{}

func New() *Manager {
	return &Manager{}
}

func (m *Manager) GetAll() map[string]RuntimeInfo {
	return map[string]RuntimeInfo{
		"node": m.GetNodeInfo(),
		"php":  m.GetPHPInfo(),
		"pg":   m.GetPGInfo(),
		"java": m.GetJavaInfo(),
	}
}

// Node.js
func (m *Manager) GetNodeInfo() RuntimeInfo {
	active := "None"
	if out, err := exec.Command("node", "-v").Output(); err == nil {
		active = strings.TrimSpace(string(out))
	}

	var available []string
	nodeBase := "/usr/local/lib/nodejs"
	if entries, err := os.ReadDir(nodeBase); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				name := e.Name()
				if strings.HasPrefix(name, "node-v") {
					parts := strings.Split(name, "-")
					if len(parts) >= 2 {
						available = append(available, parts[1])
					} else {
						available = append(available, name)
					}
				}
			}
		}
	}
	if len(available) == 0 {
		available = []string{"20", "22", "24"}
	}

	return RuntimeInfo{
		Name:              "Node.js",
		ActiveVersion:     active,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchNode(ver string) error {
	nodeBase := "/usr/local/lib/nodejs"
	entries, err := os.ReadDir(nodeBase)
	if err != nil {
		return fmt.Errorf("direktori %s tidak ditemukan: %w", nodeBase, err)
	}

	var targetDir string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, "v"+ver+".") || strings.HasPrefix(name, "node-v"+ver) {
			targetDir = filepath.Join(nodeBase, name)
			break
		}
	}

	if targetDir == "" {
		return fmt.Errorf("versi Node.js %s belum terpasang di %s", ver, nodeBase)
	}

	bins := []string{"node", "npm", "npx", "corepack"}
	for _, b := range bins {
		src := filepath.Join(targetDir, "bin", b)
		dst := filepath.Join("/usr/local/bin", b)
		if _, err := os.Stat(src); err == nil {
			_ = os.Remove(dst)
			_ = os.Symlink(src, dst)
		}
	}

	return nil
}

// PHP
func (m *Manager) GetPHPInfo() RuntimeInfo {
	active := "None"
	if out, err := exec.Command("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;").Output(); err == nil {
		active = strings.TrimSpace(string(out))
	}

	var available []string
	if entries, err := os.ReadDir("/etc/php"); err == nil {
		for _, e := range entries {
			if e.IsDir() && (strings.HasPrefix(e.Name(), "8.") || strings.HasPrefix(e.Name(), "7.")) {
				available = append(available, e.Name())
			}
		}
	}
	if len(available) == 0 {
		available = []string{"8.1", "8.2", "8.3", "8.4"}
	}

	return RuntimeInfo{
		Name:              "PHP",
		ActiveVersion:     active,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchPHP(ver string) error {
	bin := fmt.Sprintf("/usr/bin/php%s", ver)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("biner php %s tidak ditemukan di sistem", bin)
	}

	// Native update-alternatives
	cmd := exec.Command("update-alternatives", "--set", "php", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("update-alternatives gagal: %s", string(out))
	}

	// Restart relevant PHP-FPM service
	fpmService := fmt.Sprintf("php%s-fpm", ver)
	_ = exec.Command("systemctl", "restart", fpmService).Run()

	return nil
}

// PostgreSQL
func (m *Manager) GetPGInfo() RuntimeInfo {
	active := "None"
	if out, err := exec.Command("psql", "--version").Output(); err == nil {
		parts := strings.Fields(string(out))
		if len(parts) >= 3 {
			active = parts[2]
		}
	}

	var available []string
	if entries, err := os.ReadDir("/etc/postgresql"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				available = append(available, e.Name())
			}
		}
	}
	if len(available) == 0 {
		available = []string{"16", "17", "18"}
	}

	return RuntimeInfo{
		Name:              "PostgreSQL",
		ActiveVersion:     active,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchPG(ver string) error {
	clusterConf := fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", ver)
	if _, err := os.Stat(clusterConf); os.IsNotExist(err) {
		return fmt.Errorf("cluster postgresql %s tidak ditemukan di %s", ver, clusterConf)
	}

	// Set active port to 5432
	content, err := os.ReadFile(clusterConf)
	if err == nil {
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "port =") {
				lines[i] = "port = 5432"
				break
			}
		}
		_ = os.WriteFile(clusterConf, []byte(strings.Join(lines, "\n")), 0644)
	}

	_ = exec.Command("systemctl", "restart", fmt.Sprintf("postgresql@%s-main", ver)).Run()
	_ = exec.Command("systemctl", "restart", "postgresql").Run()
	return nil
}

// Java
func (m *Manager) GetJavaInfo() RuntimeInfo {
	active := "None"
	if out, err := exec.Command("java", "-version").CombinedOutput(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 {
			active = lines[0]
		}
	}

	var available []string
	seen := make(map[string]bool)
	if entries, err := os.ReadDir("/usr/lib/jvm"); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			for _, part := range strings.Split(name, "-") {
				if _, err := strconv.Atoi(part); err == nil && len(part) <= 2 {
					if !seen[part] {
						seen[part] = true
						available = append(available, part)
					}
				}
			}
		}
	}
	if len(available) == 0 {
		available = []string{"17", "21"}
	}

	return RuntimeInfo{
		Name:              "Java OpenJDK",
		ActiveVersion:     active,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchJava(ver string) error {
	jvmDir := "/usr/lib/jvm"
	entries, err := os.ReadDir(jvmDir)
	if err != nil {
		return fmt.Errorf("direktori %s tidak ditemukan", jvmDir)
	}

	var targetBin string
	for _, e := range entries {
		if strings.Contains(e.Name(), ver) && !strings.HasPrefix(e.Name(), ".") {
			bin := filepath.Join(jvmDir, e.Name(), "bin", "java")
			if _, err := os.Stat(bin); err == nil {
				targetBin = bin
				break
			}
		}
	}

	if targetBin == "" {
		return fmt.Errorf("versi java %s tidak ditemukan di %s", ver, jvmDir)
	}

	if resolved, err := filepath.EvalSymlinks(targetBin); err == nil {
		targetBin = resolved
	}

	cmd := exec.Command("update-alternatives", "--set", "java", targetBin)
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "update-alternatives", "--set", "java", targetBin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("update-alternatives java gagal: %s", string(out))
	}

	return nil
}
