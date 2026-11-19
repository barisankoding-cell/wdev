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
	ActiveMajor       string   `json:"active_major"`
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
	activeMajor := ""
	if out, err := exec.Command("node", "-v").Output(); err == nil {
		active = strings.TrimSpace(string(out))
		clean := strings.TrimPrefix(active, "v")
		parts := strings.Split(clean, ".")
		if len(parts) > 0 {
			activeMajor = parts[0]
		}
	}

	available := []string{"20", "22", "24"}
	nodeBase := "/usr/local/lib/nodejs"
	if entries, err := os.ReadDir(nodeBase); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				name := e.Name()
				if strings.HasPrefix(name, "node-v") {
					parts := strings.Split(name, "-")
					if len(parts) >= 2 {
						vClean := strings.TrimPrefix(parts[1], "v")
						major := strings.Split(vClean, ".")[0]
						hasMajor := false
						for _, a := range available {
							if a == major {
								hasMajor = true
								break
							}
						}
						if !hasMajor && major != "" {
							available = append(available, major)
						}
					}
				}
			}
		}
	}

	return RuntimeInfo{
		Name:              "Node.js",
		ActiveVersion:     active,
		ActiveMajor:       activeMajor,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchNode(ver string) error {
	nodeBase := "/usr/local/lib/nodejs"
	entries, err := os.ReadDir(nodeBase)
	if err != nil {
		return fmt.Errorf("direktori %s tidak ditemukan: %w", nodeBase, err)
	}

	cleanVer := strings.TrimPrefix(strings.TrimSpace(ver), "v")
	var targetDir string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "node-v"+cleanVer) ||
			strings.Contains(name, "node-v"+cleanVer+".") ||
			strings.Contains(name, "node-v"+cleanVer+"-") {
			targetDir = filepath.Join(nodeBase, name)
			break
		}
	}

	if targetDir == "" {
		for _, e := range entries {
			if e.Name() == "v"+cleanVer || e.Name() == cleanVer {
				if resolved, err := filepath.EvalSymlinks(filepath.Join(nodeBase, e.Name())); err == nil {
					targetDir = resolved
					break
				}
			}
		}
	}

	if targetDir == "" {
		return fmt.Errorf("versi Node.js %s belum terpasang di %s", ver, nodeBase)
	}

	_ = os.Remove(filepath.Join(nodeBase, "current"))
	_ = os.Symlink(targetDir, filepath.Join(nodeBase, "current"))

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
	activeMajor := ""
	if out, err := exec.Command("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;").Output(); err == nil {
		active = strings.TrimSpace(string(out))
		activeMajor = active
	}

	available := []string{"8.1", "8.2", "8.3", "8.4"}
	if entries, err := os.ReadDir("/etc/php"); err == nil {
		for _, e := range entries {
			if e.IsDir() && (strings.HasPrefix(e.Name(), "8.") || strings.HasPrefix(e.Name(), "7.")) {
				found := false
				for _, a := range available {
					if a == e.Name() {
						found = true
						break
					}
				}
				if !found {
					available = append(available, e.Name())
				}
			}
		}
	}

	return RuntimeInfo{
		Name:              "PHP",
		ActiveVersion:     active,
		ActiveMajor:       activeMajor,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchPHP(ver string) error {
	cleanVer := strings.TrimSpace(ver)
	bin := fmt.Sprintf("/usr/bin/php%s", cleanVer)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("biner php %s tidak ditemukan di sistem", bin)
	}

	cmd := exec.Command("update-alternatives", "--set", "php", bin)
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "update-alternatives", "--set", "php", bin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("update-alternatives gagal: %s", string(out))
	}

	for _, tool := range []string{"phar", "phpize", "php-config"} {
		toolBin := fmt.Sprintf("/usr/bin/%s%s", tool, cleanVer)
		if _, err := os.Stat(toolBin); err == nil {
			tCmd := exec.Command("update-alternatives", "--set", tool, toolBin)
			if os.Geteuid() != 0 {
				tCmd = exec.Command("sudo", "update-alternatives", "--set", tool, toolBin)
			}
			_ = tCmd.Run()
		}
	}

	fpmService := fmt.Sprintf("php%s-fpm", cleanVer)
	if os.Geteuid() == 0 {
		_ = exec.Command("systemctl", "enable", "--now", fpmService).Run()
		_ = exec.Command("systemctl", "restart", fpmService).Run()
	} else {
		_ = exec.Command("sudo", "systemctl", "enable", "--now", fpmService).Run()
		_ = exec.Command("sudo", "systemctl", "restart", fpmService).Run()
	}

	return nil
}

// PostgreSQL
func (m *Manager) GetPGInfo() RuntimeInfo {
	active := "None"
	activeMajor := ""
	if out, err := exec.Command("pg_lsclusters", "-h").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[2] == "5432" && fields[3] == "online" {
				active = fmt.Sprintf("PG %s (Port 5432)", fields[0])
				activeMajor = fields[0]
				break
			}
		}
	}
	if active == "None" {
		if out, err := exec.Command("psql", "--version").Output(); err == nil {
			parts := strings.Fields(string(out))
			if len(parts) >= 3 {
				active = parts[2]
				activeMajor = strings.Split(parts[2], ".")[0]
			}
		}
	}

	available := []string{"16", "17", "18"}
	if entries, err := os.ReadDir("/etc/postgresql"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				found := false
				for _, a := range available {
					if a == e.Name() {
						found = true
						break
					}
				}
				if !found {
					available = append(available, e.Name())
				}
			}
		}
	}

	return RuntimeInfo{
		Name:              "PostgreSQL",
		ActiveVersion:     active,
		ActiveMajor:       activeMajor,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchPG(ver string) error {
	cleanVer := strings.TrimSpace(ver)
	clusterConf := fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", cleanVer)
	if _, err := os.Stat(clusterConf); os.IsNotExist(err) {
		return fmt.Errorf("cluster postgresql %s belum dibuat di %s", cleanVer, clusterConf)
	}

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

	if os.Geteuid() == 0 {
		_ = exec.Command("systemctl", "restart", fmt.Sprintf("postgresql@%s-main", cleanVer)).Run()
		_ = exec.Command("systemctl", "restart", "postgresql").Run()
	} else {
		_ = exec.Command("sudo", "systemctl", "restart", fmt.Sprintf("postgresql@%s-main", cleanVer)).Run()
		_ = exec.Command("sudo", "systemctl", "restart", "postgresql").Run()
	}
	return nil
}

// Java
func (m *Manager) GetJavaInfo() RuntimeInfo {
	active := "None"
	activeMajor := ""
	if out, err := exec.Command("java", "-version").CombinedOutput(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 {
			// Extract version between quotes, e.g. openjdk version "21.0.13.1" 2026-11-17 -> 21.0.13.1
			line := lines[0]
			if start := strings.Index(line, "\""); start != -1 {
				rest := line[start+1:]
				if end := strings.Index(rest, "\""); end != -1 {
					fullVer := rest[:end]
					parts := strings.Split(fullVer, ".")
					if len(parts) > 0 {
						activeMajor = parts[0]
					}
					active = fullVer
				}
			}
			if active == "None" {
				active = line
			}
		}
	}

	available := []string{"17", "21"}
	if entries, err := os.ReadDir("/usr/lib/jvm"); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			for _, part := range strings.Split(name, "-") {
				if _, err := strconv.Atoi(part); err == nil && len(part) <= 2 {
					found := false
					for _, a := range available {
						if a == part {
							found = true
							break
						}
					}
					if !found {
						available = append(available, part)
					}
				}
			}
		}
	}

	return RuntimeInfo{
		Name:              "Java OpenJDK",
		ActiveVersion:     active,
		ActiveMajor:       activeMajor,
		AvailableVersions: available,
	}
}

func (m *Manager) SwitchJava(ver string) error {
	cleanVer := strings.TrimSpace(ver)
	jvmDir := "/usr/lib/jvm"
	entries, err := os.ReadDir(jvmDir)
	if err != nil {
		return fmt.Errorf("direktori %s tidak ditemukan", jvmDir)
	}

	var targetBin string
	for _, e := range entries {
		if strings.Contains(e.Name(), cleanVer) && !strings.HasPrefix(e.Name(), ".") {
			bin := filepath.Join(jvmDir, e.Name(), "bin", "java")
			if _, err := os.Stat(bin); err == nil {
				targetBin = bin
				break
			}
		}
	}

	if targetBin == "" {
		return fmt.Errorf("versi java %s tidak ditemukan di %s", cleanVer, jvmDir)
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

	javacBin := filepath.Join(filepath.Dir(targetBin), "javac")
	if _, err := os.Stat(javacBin); err == nil {
		jcmd := exec.Command("update-alternatives", "--set", "javac", javacBin)
		if os.Geteuid() != 0 {
			jcmd = exec.Command("sudo", "update-alternatives", "--set", "javac", javacBin)
		}
		_ = jcmd.Run()
	}

	return nil
}
