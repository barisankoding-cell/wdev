package monitor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type SystemStats struct {
	Hostname       string             `json:"hostname"`
	OSName         string             `json:"os_name"`
	Kernel         string             `json:"kernel"`
	UptimeString   string             `json:"uptime_string"`
	LoadAvg        string             `json:"load_avg"`
	MemTotalMB     int64              `json:"mem_total_mb"`
	MemUsedMB      int64              `json:"mem_used_mb"`
	MemPercent     int                `json:"mem_percent"`
	DiskTotalGB    int64              `json:"disk_total_gb"`
	DiskUsedGB     int64              `json:"disk_used_gb"`
	DiskPercent    int                `json:"disk_percent"`
	LanIP          string             `json:"lan_ip"`
	NetbirdIP      string             `json:"netbird_ip"`
	Services       []ServiceStatus    `json:"services"`
	Runtimes       map[string]string  `json:"runtimes"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Active      bool   `json:"active"`
	StatusText  string `json:"status_text"`
	Port        string `json:"port,omitempty"`
	IsRemote    bool   `json:"is_remote,omitempty"`
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

func getServiceStatuses() []ServiceStatus {
	var list []ServiceStatus

	// 1. Nginx
	nginxActive := isSystemdServiceActive("nginx")
	list = append(list, ServiceStatus{
		Name:        "nginx",
		DisplayName: "Nginx Web Server",
		Category:    "Web Stack",
		Active:      nginxActive,
		StatusText:  statusText(nginxActive),
		Port:        "80, 443",
	})

	// 2. PHP-FPM (check multiple versions)
	phpActive := false
	phpVerActive := ""
	for _, v := range []string{"8.4", "8.3", "8.2", "8.1"} {
		if isSystemdServiceActive("php" + v + "-fpm") {
			phpActive = true
			phpVerActive = v
			break
		}
	}
	phpDisplayName := "PHP-FPM"
	if phpVerActive != "" {
		phpDisplayName = fmt.Sprintf("PHP-FPM (%s)", phpVerActive)
	}
	list = append(list, ServiceStatus{
		Name:        "php-fpm",
		DisplayName: phpDisplayName,
		Category:    "Web Stack",
		Active:      phpActive,
		StatusText:  statusText(phpActive),
		Port:        "Unix Socket",
	})

	// 3. PostgreSQL
	pgActive := isSystemdServiceActive("postgresql")
	list = append(list, ServiceStatus{
		Name:        "postgresql",
		DisplayName: "PostgreSQL Database",
		Category:    "Database",
		Active:      pgActive,
		StatusText:  statusText(pgActive),
		Port:        "5432",
	})

	// 4. Redis / Valkey
	redisActive := isSystemdServiceActive("redis-server") || isSystemdServiceActive("valkey-server") || isSystemdServiceActive("redis")
	list = append(list, ServiceStatus{
		Name:        "redis",
		DisplayName: "Redis / Valkey Cache",
		Category:    "Database",
		Active:      redisActive,
		StatusText:  statusText(redisActive),
		Port:        "6379",
	})

	// 5. Cloudflare Tunnel
	cfActive := isSystemdServiceActive("cloudflared")
	if !cfActive {
		// check if running as process
		_ = exec.Command("pgrep", "cloudflared").Run()
		cfActive = (exec.Command("pgrep", "cloudflared").Run() == nil)
	}
	list = append(list, ServiceStatus{
		Name:        "cloudflared",
		DisplayName: "Cloudflare Tunnel",
		Category:    "Network / Ingress",
		Active:      cfActive,
		StatusText:  statusText(cfActive),
	})

	// 6. Netbird
	nbActive := isSystemdServiceActive("netbird")
	list = append(list, ServiceStatus{
		Name:        "netbird",
		DisplayName: "NetBird Mesh VPN",
		Category:    "Network / Ingress",
		Active:      nbActive,
		StatusText:  statusText(nbActive),
		Port:        "wt0",
	})

	// 7. PowerDNS (Local systemd or VPS Edge Gateway)
	list = append(list, getPDNSServiceStatus())

	return list
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
					verStr := serverInfo.Version
					if verStr == "" {
						verStr = "Online"
					} else {
						verStr = "v" + verStr
					}
					return ServiceStatus{
						Name:        "pdns",
						DisplayName: "PowerDNS Authoritative (VPS Edge)",
						Category:    "DNS & Mail",
						Active:      true,
						StatusText:  fmt.Sprintf("Online (%s)", verStr),
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
	if out, err := exec.Command("node", "-v").Output(); err == nil {
		res["node"] = strings.TrimSpace(string(out))
	} else {
		res["node"] = "Not Installed"
	}

	// PHP
	if out, err := exec.Command("php", "-r", "echo PHP_VERSION;").Output(); err == nil {
		res["php"] = "v" + strings.TrimSpace(string(out))
	} else {
		res["php"] = "Not Installed"
	}

	// Go
	if out, err := exec.Command("go", "version").Output(); err == nil {
		parts := strings.Fields(string(out))
		if len(parts) >= 3 {
			res["go"] = parts[2]
		}
	} else {
		res["go"] = "Not Installed"
	}

	// Rust
	if out, err := exec.Command("rustc", "--version").Output(); err == nil {
		parts := strings.Fields(string(out))
		if len(parts) >= 2 {
			res["rust"] = parts[1]
		}
	} else {
		res["rust"] = "Not Installed"
	}

	// Composer
	if out, err := exec.Command("composer", "--version").Output(); err == nil {
		parts := strings.Fields(string(out))
		if len(parts) >= 3 {
			res["composer"] = parts[2]
		}
	} else {
		res["composer"] = "Not Installed"
	}

	// PostgreSQL
	if out, err := exec.Command("psql", "--version").Output(); err == nil {
		parts := strings.Fields(string(out))
		if len(parts) >= 3 {
			res["postgresql"] = parts[2]
		}
	} else {
		res["postgresql"] = "Not Installed"
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
		// Find active or latest php
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
	case "redis":
		if _, err := os.Stat("/lib/systemd/system/valkey-server.service"); err == nil {
			actualService = "valkey-server"
		} else {
			actualService = "redis-server"
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
