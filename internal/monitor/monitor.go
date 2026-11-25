package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"wdev/internal/version"
)

type SystemStats struct {
	WdevVersion     string            `json:"wdev_version"`
	Hostname        string            `json:"hostname"`
	OSName          string            `json:"os_name"`
	Kernel          string            `json:"kernel"`
	KernelFull      string            `json:"kernel_full,omitempty"`
	BootMode        string            `json:"boot_mode,omitempty"`
	UptimeString    string            `json:"uptime_string"`
	UptimeSeconds   int64             `json:"uptime_seconds"`
	LoadAvg         string            `json:"load_avg"`
	CPUCores        int               `json:"cpu_cores"`
	CPUModel        string            `json:"cpu_model,omitempty"`
	CPUSockets      int               `json:"cpu_sockets,omitempty"`
	CPUUsagePercent  float64           `json:"cpu_usage_percent"`
	CPUCoresUsage    []CPUCoreUsage    `json:"cpu_cores_usage,omitempty"`
	LoadPercent      int               `json:"load_percent"`
	Load1m           float64           `json:"load_1m"`
	Load5m           float64           `json:"load_5m"`
	Load15m          float64           `json:"load_15m"`
	Load1mPct        int               `json:"load_1m_percent"`
	Load5mPct        int               `json:"load_5m_percent"`
	Load15mPct       int               `json:"load_15m_percent"`
	MemTotalMB       int64             `json:"mem_total_mb"`
	MemUsedMB        int64             `json:"mem_used_mb"`
	MemPercent       int               `json:"mem_percent"`
	MemTotalBytes    uint64            `json:"mem_total_bytes"`
	MemUsedBytes     uint64            `json:"mem_used_bytes"`
	MemPercentExact  float64           `json:"mem_percent_exact"`
	SwapTotalBytes   uint64            `json:"swap_total_bytes"`
	SwapUsedBytes    uint64            `json:"swap_used_bytes"`
	SwapPercent      float64           `json:"swap_percent"`
	DiskTotalGB      int64             `json:"disk_total_gb"`
	DiskUsedGB       int64             `json:"disk_used_gb"`
	DiskPercent      int               `json:"disk_percent"`
	DiskTotalBytes   uint64            `json:"disk_total_bytes"`
	DiskUsedBytes    uint64            `json:"disk_used_bytes"`
	DiskPercentExact float64           `json:"disk_percent_exact"`
	MountDrives      []MountDriveInfo  `json:"mount_drives,omitempty"`
	OtherMounts      []MountDriveInfo  `json:"other_mounts,omitempty"`
	LanIP           string            `json:"lan_ip"`
	NetbirdIP       string            `json:"netbird_ip"`
	PublicIP        string            `json:"public_ip,omitempty"`
	ServerMode      string            `json:"server_mode,omitempty"`
	Services        []ServiceStatus   `json:"services"`
	Runtimes        map[string]string `json:"runtimes"`
}

type CPUCoreUsage struct {
	Core    int `json:"core"`
	Percent int `json:"percent"`
}

type ServiceStatus struct {
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	Category         string `json:"category"`
	Active           bool   `json:"active"`
	Starting         bool   `json:"starting,omitempty"`
	StatusText       string `json:"status_text"`
	Version          string `json:"version,omitempty"`
	Port             string `json:"port,omitempty"`
	IsRemote         bool   `json:"is_remote,omitempty"`
	Installed        bool   `json:"installed"`
	LatestVersion    string `json:"latest_version,omitempty"`
	HasUpdate        bool   `json:"has_update"`
	InstallComponent string `json:"install_component,omitempty"`
	ConfigPreset     string `json:"config_preset,omitempty"`
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
	go startCPUSampler()
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
	kernelFull := getKernelFull(kernel)
	bootMode := getBootMode()
	uptimeStr, uptimeSec := getUptimeInfo()
	loadAvg, cpuCores, l1, l5, l15, p1, p5, p15 := getLoadAvgInfo()
	cpuModel, cpuSockets := getCPUHardwareInfo()
	memStats := getDetailedMemInfo()
	diskTotal, diskUsed, diskTotalBytes, diskUsedBytes, diskPct, diskPctExact := getDetailedDiskInfo("/")
	mountDrives := DetectMountDrives()
	otherMounts := detectOtherMounts("/proc/mounts", syscall.Statfs, mountDrives)
	lanIP, netbirdIP, publicIP := getNetworkIPs()
	services := getServiceStatuses()
	runtimes := getRuntimeVersions()
	cpuCoresUsage, cpuUsagePct := getCPUUsageMetrics()

	serverMode := ""
	if publicIP != "" && publicIP != "-" && publicIP != "127.0.0.1" {
		serverMode = "vps"
	}

	return &SystemStats{
		WdevVersion:      version.Version,
		Hostname:         hostname,
		OSName:           osName,
		Kernel:           kernel,
		KernelFull:       kernelFull,
		BootMode:         bootMode,
		UptimeString:     uptimeStr,
		UptimeSeconds:    uptimeSec,
		LoadAvg:          loadAvg,
		CPUCores:         cpuCores,
		CPUModel:         cpuModel,
		CPUSockets:       cpuSockets,
		CPUUsagePercent:  cpuUsagePct,
		CPUCoresUsage:    cpuCoresUsage,
		LoadPercent:      p1,
		Load1m:           l1,
		Load5m:           l5,
		Load15m:          l15,
		Load1mPct:        p1,
		Load5mPct:        p5,
		Load15mPct:       p15,
		MemTotalMB:       memStats.totalMB,
		MemUsedMB:        memStats.usedMB,
		MemPercent:       memStats.percentInt,
		MemTotalBytes:    memStats.totalBytes,
		MemUsedBytes:     memStats.usedBytes,
		MemPercentExact:  memStats.percentExact,
		SwapTotalBytes:   memStats.swapTotalBytes,
		SwapUsedBytes:    memStats.swapUsedBytes,
		SwapPercent:      memStats.swapPercent,
		DiskTotalGB:      diskTotal,
		DiskUsedGB:       diskUsed,
		DiskPercent:      diskPct,
		DiskTotalBytes:   diskTotalBytes,
		DiskUsedBytes:    diskUsedBytes,
		DiskPercentExact: diskPctExact,
		MountDrives:      mountDrives,
		OtherMounts:      otherMounts,
		LanIP:            lanIP,
		NetbirdIP:        netbirdIP,
		PublicIP:         publicIP,
		ServerMode:       serverMode,
		Services:         services,
		Runtimes:         runtimes,
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

func getKernelFull(fallback string) string {
	out, err := exec.Command("uname", "-s", "-r", "-v").Output()
	if err == nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return trimmed
		}
	}
	if fallback != "" {
		return "Linux " + fallback
	}
	return ""
}

func getBootMode() string {
	if dirExists("/sys/firmware/efi") {
		return "EFI"
	}
	if dirExists("/sys/firmware") {
		return "Legacy BIOS"
	}
	return "-"
}

func getUptimeInfo() (string, int64) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "N/A", 0
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return "N/A", 0
	}
	secondsFloat, _ := strconv.ParseFloat(parts[0], 64)
	totalSec := int64(secondsFloat)
	if totalSec < 0 {
		totalSec = 0
	}
	days := totalSec / 86400
	hours := (totalSec % 86400) / 3600
	minutes := (totalSec % 3600) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes), totalSec
	}
	return fmt.Sprintf("%dh %dm", hours, minutes), totalSec
}

func loadToPercent(load float64, cpuCores int) int {
	if cpuCores <= 0 {
		cpuCores = 1
	}
	if load <= 0 {
		return 0
	}
	return int(math.Round((load / float64(cpuCores)) * 100.0))
}

func parseLoadAvg(raw string, cpuCores int) (summary string, l1, l5, l15 float64, p1, p5, p15 int) {
	if cpuCores <= 0 {
		cpuCores = 1
	}
	parts := strings.Fields(raw)
	if len(parts) < 3 {
		return "N/A", 0, 0, 0, 0, 0, 0
	}
	summary = fmt.Sprintf("%s, %s, %s", parts[0], parts[1], parts[2])
	l1, _ = strconv.ParseFloat(parts[0], 64)
	l5, _ = strconv.ParseFloat(parts[1], 64)
	l15, _ = strconv.ParseFloat(parts[2], 64)
	p1 = loadToPercent(l1, cpuCores)
	p5 = loadToPercent(l5, cpuCores)
	p15 = loadToPercent(l15, cpuCores)
	return summary, l1, l5, l15, p1, p5, p15
}

func getLoadAvgInfo() (summary string, cpuCores int, l1, l5, l15 float64, p1, p5, p15 int) {
	cpuCores = runtime.NumCPU()
	if cpuCores <= 0 {
		cpuCores = 1
	}
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "N/A", cpuCores, 0, 0, 0, 0, 0, 0
	}
	summary, l1, l5, l15, p1, p5, p15 = parseLoadAvg(string(data), cpuCores)
	return summary, cpuCores, l1, l5, l15, p1, p5, p15
}

type coreTickSample struct {
	idle   uint64
	iowait uint64
	total  uint64
}

const aggregateCPUKey = -1

var (
	cpuMu             sync.RWMutex
	prevCPUTicks      map[int]coreTickSample
	lastCPUTickTime   time.Time
	cachedCoresUsage  []CPUCoreUsage
	cachedAggCPUUsage float64
)

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func clampPercent2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	if v >= 100 {
		return 100
	}
	return round2(v)
}

func parseProcStat(content string) map[int]coreTickSample {
	res := make(map[int]coreTickSample)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		name := fields[0]
		coreID := aggregateCPUKey
		if name != "cpu" {
			coreIDStr := strings.TrimPrefix(name, "cpu")
			parsedID, err := strconv.Atoi(coreIDStr)
			if err != nil {
				continue
			}
			coreID = parsedID
		}

		var values []uint64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			values = append(values, v)
		}
		if len(values) < 4 {
			continue
		}
		user := values[0]
		nice := values[1]
		sys := values[2]
		idle := values[3]
		var iowait, irq, softirq, steal uint64
		if len(values) > 4 {
			iowait = values[4]
		}
		if len(values) > 5 {
			irq = values[5]
		}
		if len(values) > 6 {
			softirq = values[6]
		}
		if len(values) > 7 {
			steal = values[7]
		}
		idleTotal := idle + iowait
		nonIdle := user + nice + sys + irq + softirq + steal
		total := idleTotal + nonIdle
		res[coreID] = coreTickSample{
			idle:   idleTotal,
			iowait: iowait,
			total:  total,
		}
	}
	return res
}

func calculateCoresUsage(prev, curr map[int]coreTickSample) []CPUCoreUsage {
	var list []CPUCoreUsage
	for coreID, curSamp := range curr {
		if coreID == aggregateCPUKey {
			continue
		}
		prevSamp, ok := prev[coreID]
		if !ok {
			continue
		}
		totalDelta := int64(curSamp.total) - int64(prevSamp.total)
		idleDelta := int64(curSamp.idle) - int64(prevSamp.idle)
		pct := 0
		if totalDelta > 0 && totalDelta >= idleDelta {
			usedDelta := totalDelta - idleDelta
			pct = int(math.Round(float64(usedDelta) / float64(totalDelta) * 100.0))
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
		}
		list = append(list, CPUCoreUsage{
			Core:    coreID,
			Percent: pct,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Core < list[j].Core
	})
	return list
}

func calculateAggregateCPUUsage(prev, curr map[int]coreTickSample) float64 {
	prevAgg, okPrev := prev[aggregateCPUKey]
	currAgg, okCurr := curr[aggregateCPUKey]
	if !okPrev || !okCurr {
		return 0
	}
	totalDelta := int64(currAgg.total) - int64(prevAgg.total)
	if totalDelta <= 0 {
		return 0
	}
	idleDelta := int64(currAgg.idle) - int64(prevAgg.idle)
	if idleDelta < 0 {
		idleDelta = 0
	}
	if totalDelta >= idleDelta {
		usedDelta := totalDelta - idleDelta
		return clampPercent2(float64(usedDelta) / float64(totalDelta) * 100.0)
	}
	return 0
}

func startCPUSampler() {
	raw, err := os.ReadFile("/proc/stat")
	if err == nil {
		cpuMu.Lock()
		prevCPUTicks = parseProcStat(string(raw))
		lastCPUTickTime = time.Now()
		cpuMu.Unlock()
	}

	ticker := time.NewTicker(2 * time.Second)
	for range ticker.C {
		raw, err := os.ReadFile("/proc/stat")
		if err != nil {
			continue
		}
		curr := parseProcStat(string(raw))
		cpuMu.Lock()
		if len(prevCPUTicks) > 0 {
			cachedCoresUsage = calculateCoresUsage(prevCPUTicks, curr)
			cachedAggCPUUsage = calculateAggregateCPUUsage(prevCPUTicks, curr)
		}
		prevCPUTicks = curr
		lastCPUTickTime = time.Now()
		cpuMu.Unlock()
	}
}

func getCPUUsageMetrics() ([]CPUCoreUsage, float64) {
	cpuMu.RLock()
	cached := cachedCoresUsage
	aggCached := cachedAggCPUUsage
	lastTime := lastCPUTickTime
	hasPrev := len(prevCPUTicks) > 0
	cpuMu.RUnlock()

	if hasPrev && time.Since(lastTime) < 5*time.Second && (len(cached) > 0 || aggCached > 0) {
		res := make([]CPUCoreUsage, len(cached))
		copy(res, cached)
		return res, aggCached
	}

	raw1, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, 0
	}
	sample1 := parseProcStat(string(raw1))
	if len(sample1) == 0 {
		return nil, 0
	}

	cpuMu.Lock()
	if hasPrev && time.Since(lastTime) < 10*time.Second {
		usage := calculateCoresUsage(prevCPUTicks, sample1)
		aggPct := calculateAggregateCPUUsage(prevCPUTicks, sample1)
		prevCPUTicks = sample1
		lastCPUTickTime = time.Now()
		cachedCoresUsage = usage
		cachedAggCPUUsage = aggPct
		cpuMu.Unlock()
		return usage, aggPct
	}
	cpuMu.Unlock()

	time.Sleep(150 * time.Millisecond)
	raw2, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, 0
	}
	sample2 := parseProcStat(string(raw2))
	usage := calculateCoresUsage(sample1, sample2)
	aggPct := calculateAggregateCPUUsage(sample1, sample2)

	cpuMu.Lock()
	prevCPUTicks = sample2
	lastCPUTickTime = time.Now()
	cachedCoresUsage = usage
	cachedAggCPUUsage = aggPct
	cpuMu.Unlock()

	return usage, aggPct
}

func parseCPUInfo(content string) (model string, sockets int) {
	physicalIDs := make(map[string]struct{})
	var fallbackModel string

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if val == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "model name":
			if model == "" {
				model = val
			}
		case "model", "hardware", "cpu model", "processor":
			if fallbackModel == "" && !isDigits(val) {
				fallbackModel = val
			}
		case "physical id":
			physicalIDs[val] = struct{}{}
		}
	}

	if model == "" {
		model = fallbackModel
	}
	sockets = len(physicalIDs)
	if sockets <= 0 {
		sockets = 1
	}
	return model, sockets
}

func getCPUHardwareInfo() (string, int) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", 1
	}
	return parseCPUInfo(string(data))
}

type detailedMemStats struct {
	totalMB        int64
	usedMB         int64
	percentInt     int
	totalBytes     uint64
	usedBytes      uint64
	percentExact   float64
	swapTotalBytes uint64
	swapUsedBytes  uint64
	swapPercent    float64
}

func parseDetailedMemInfo(content string) detailedMemStats {
	var totalKB, availKB, freeKB, buffersKB, cachedKB int64
	var swapTotalKB, swapFreeKB int64
	hasAvail := false

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			totalKB = val
		case "MemAvailable:":
			availKB = val
			hasAvail = true
		case "MemFree:":
			freeKB = val
		case "Buffers:":
			buffersKB = val
		case "Cached:":
			cachedKB = val
		case "SwapTotal:":
			swapTotalKB = val
		case "SwapFree:":
			swapFreeKB = val
		}
	}

	if !hasAvail {
		availKB = freeKB + buffersKB + cachedKB
	}
	if availKB > totalKB {
		availKB = totalKB
	}

	var res detailedMemStats
	if totalKB > 0 {
		usedKB := totalKB - availKB
		if usedKB < 0 {
			usedKB = 0
		}
		res.totalMB = totalKB / 1024
		res.usedMB = usedKB / 1024
		res.totalBytes = uint64(totalKB) * 1024
		res.usedBytes = uint64(usedKB) * 1024
		ratio := float64(usedKB) / float64(totalKB) * 100.0
		res.percentInt = int(ratio)
		res.percentExact = clampPercent2(ratio)
	}

	if swapTotalKB > 0 {
		swapUsedKB := swapTotalKB - swapFreeKB
		if swapUsedKB < 0 {
			swapUsedKB = 0
		}
		res.swapTotalBytes = uint64(swapTotalKB) * 1024
		res.swapUsedBytes = uint64(swapUsedKB) * 1024
		res.swapPercent = clampPercent2(float64(swapUsedKB) / float64(swapTotalKB) * 100.0)
	}

	return res
}

func getDetailedMemInfo() detailedMemStats {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return detailedMemStats{}
	}
	return parseDetailedMemInfo(string(data))
}

func getDetailedDiskInfo(path string) (totalGB, usedGB int64, totalBytes, usedBytes uint64, percent int, percentExact float64) {
	totalGB, usedGB, _, totalBytes, usedBytes, percent, _ = readDiskUsage(path, syscall.Statfs)
	if totalBytes > 0 {
		percentExact = clampPercent2(float64(usedBytes) / float64(totalBytes) * 100.0)
	}
	return
}

func isPublicIPv4(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127 {
		return false
	}
	return true
}

func IsPublicIPv4(ip net.IP) bool {
	return isPublicIPv4(ip)
}

func loadEdgeDNSPublicIP() string {
	paths := []string{
		"/etc/wdev/edge-dns.conf",
		"/etc/wdev-edge/edge.conf",
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "wdev", "edge-dns.conf"))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			if (k == "VPS_PUBLIC_IP" || k == "PUBLIC_IP") && v != "" && v != "127.0.0.1" && v != "localhost" {
				parsed := net.ParseIP(v)
				if parsed != nil && isPublicIPv4(parsed.To4()) {
					return v
				}
			}
		}
	}
	return ""
}

var (
	cachedPublicIP     string
	cachedPublicIPTime time.Time
	publicIPMu         sync.RWMutex
)

func getCachedExternalPublicIP() string {
	publicIPMu.RLock()
	if cachedPublicIP != "" && time.Since(cachedPublicIPTime) < 5*time.Minute {
		ip := cachedPublicIP
		publicIPMu.RUnlock()
		return ip
	}
	publicIPMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	transport := &http.Transport{
		DialContext: func(c context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext(c, "tcp4", addr)
		},
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport}
	for _, u := range []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://ifconfig.me/ip",
	} {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		parsed := net.ParseIP(ip)
		if parsed != nil && isPublicIPv4(parsed.To4()) {
			publicIPMu.Lock()
			cachedPublicIP = ip
			cachedPublicIPTime = time.Now()
			publicIPMu.Unlock()
			return ip
		}
	}

	publicIPMu.RLock()
	defer publicIPMu.RUnlock()
	return cachedPublicIP
}

func getNetworkIPs() (lanIP string, netbirdIP string, publicIP string) {
	lanIP = "127.0.0.1"
	netbirdIP = "-"
	publicIP = ""

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			name := strings.ToLower(iface.Name)
			if name == "lo" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") ||
				strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "virbr") || strings.HasPrefix(name, "cni") {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ipnet, ok := a.(*net.IPNet)
				if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
					continue
				}
				ip4 := ipnet.IP.To4()
				ipStr := ip4.String()
				if name == "wt0" {
					netbirdIP = ipStr
				} else if isPublicIPv4(ip4) {
					if publicIP == "" {
						publicIP = ipStr
					}
				} else if lanIP == "127.0.0.1" {
					lanIP = ipStr
				}
			}
		}
	}
	if publicIP == "" {
		publicIP = loadEdgeDNSPublicIP()
	}
	if publicIP == "" {
		publicIP = getCachedExternalPublicIP()
	}
	return
}

// parseSystemdIsActiveOutput memetakan output `systemctl is-active` menjadi status aktif atau sedang memulai.
func parseSystemdIsActiveOutput(out string, runErr error) (active bool, starting bool) {
	fields := strings.Fields(out)
	state := ""
	if len(fields) > 0 {
		state = strings.ToLower(fields[0])
	}
	switch state {
	case "active":
		return true, false
	case "activating", "reloading":
		return false, true
	case "":
		return runErr == nil, false
	default:
		return false, false
	}
}

func systemdServiceState(serviceName string) (active bool, starting bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", serviceName).Output()
	return parseSystemdIsActiveOutput(string(out), err)
}

func anySystemdServiceState(units ...string) (active bool, starting bool) {
	return pickAnySystemdServiceState(systemdServiceState, units...)
}

func pickAnySystemdServiceState(probe func(string) (bool, bool), units ...string) (active bool, starting bool) {
	anyStarting := false
	for _, u := range units {
		a, s := probe(u)
		if a {
			return true, false
		}
		if s {
			anyStarting = true
		}
	}
	return false, anyStarting
}

func isSystemdServiceActive(serviceName string) bool {
	active, _ := systemdServiceState(serviceName)
	return active
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
	active, starting := systemdServiceState(serviceName)
	return ServiceStatus{
		Name:       serviceName,
		Active:     active,
		Starting:   starting,
		StatusText: serviceStatusText(active, starting),
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
		val = strings.TrimSpace(val)
		if val == "" || val == "-" {
			return
		}
		mu.Lock()
		cur := latest[key]
		if cur == "" || cur == "-" || hasNewerVersion(cur, val) {
			latest[key] = val
		}
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

	// 3. NetBird (rilis resmi pkgs.netbird.io)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://pkgs.netbird.io/releases/latest", nil)
		if err == nil {
			req.Header.Set("User-Agent", "wdev-control-plane")
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var rel struct {
					TagName string `json:"tag_name"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil && rel.TagName != "" {
					setVal("netbird", strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v"))
				}
			}
		}
	}()

	// 4. Docker Compose v5 (GitHub docker/compose release)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/docker/compose/releases/latest", nil)
		if err == nil {
			req.Header.Set("User-Agent", "wdev-control-plane")
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var rel struct {
					TagName string `json:"tag_name"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil && rel.TagName != "" {
					setVal("docker", strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v"))
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
	nginxActive, nginxStarting := systemdServiceState("nginx")
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
		Starting:         nginxStarting,
		StatusText:       serviceStatusText(nginxActive, nginxStarting),
		Version:          nginxVer,
		Port:             "80, 443",
		Installed:        nginxInstalled,
		LatestVersion:    nginxLatest,
		HasUpdate:        nginxInstalled && hasNewerVersion(nginxVer, nginxLatest),
		InstallComponent: "nginx",
	})

	// 2. PHP-FPM (check multiple versions + Fedora/RHEL php-fpm.service)
	phpActive := false
	phpStarting := false
	phpVerActive := ""
	for _, v := range []string{"8.4", "8.3", "8.2", "8.1"} {
		a, s := systemdServiceState("php" + v + "-fpm")
		if a {
			phpActive = true
			phpStarting = false
			phpVerActive = v
			break
		}
		if s {
			phpStarting = true
			if phpVerActive == "" {
				phpVerActive = v
			}
		}
	}
	if !phpActive {
		a, s := systemdServiceState("php-fpm")
		if a {
			phpActive = true
			phpStarting = false
			if out := runCmdStdout("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;"); out != "" {
				phpVerActive = parsePHPMajorMinor(out)
			}
		} else if s {
			phpStarting = true
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
		Starting:         phpStarting,
		StatusText:       serviceStatusText(phpActive, phpStarting),
		Version:          phpVer,
		Port:             "Unix Socket",
		Installed:        phpInstalled,
		LatestVersion:    phpLatest,
		HasUpdate:        phpInstalled && hasNewerVersion(phpVer, phpLatest),
		InstallComponent: "php",
	})

	// 3. PostgreSQL
	pgUnits := make([]string, 0, 1+len(supportedPostgresMajors))
	pgUnits = append(pgUnits, "postgresql")
	for _, v := range supportedPostgresMajors {
		pgUnits = append(pgUnits, "postgresql-"+v)
	}
	pgActive, pgStarting := anySystemdServiceState(pgUnits...)
	pgInstalled := fileExists("/usr/bin/psql") || fileExists("/usr/bin/postgres") || dirExists("/usr/lib/postgresql")
	for _, v := range supportedPostgresMajors {
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
		Starting:         pgStarting,
		StatusText:       serviceStatusText(pgActive, pgStarting),
		Version:          pgVer,
		Port:             "5432",
		Installed:        pgInstalled,
		LatestVersion:    pgLatest,
		HasUpdate:        pgInstalled && hasNewerVersion(pgVer, pgLatest),
		InstallComponent: "postgres",
	})

	// 4. Redis / Valkey
	redisActive, redisStarting := anySystemdServiceState("redis-server", "valkey-server", "valkey", "redis")
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
		Starting:         redisStarting,
		StatusText:       serviceStatusText(redisActive, redisStarting),
		Version:          redisVer,
		Port:             "6379",
		Installed:        redisInstalled,
		LatestVersion:    redisLatest,
		HasUpdate:        redisInstalled && hasNewerVersion(redisVer, redisLatest),
		InstallComponent: "redis",
	})

	// 5. Cloudflare Tunnel
	cfActive, cfStarting := systemdServiceState("cloudflared")
	if !cfActive && exec.Command("pgrep", "cloudflared").Run() == nil {
		cfActive = true
		cfStarting = false
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
		Starting:         cfStarting,
		StatusText:       serviceStatusText(cfActive, cfStarting),
		Version:          cfVer,
		Installed:        cfInstalled,
		LatestVersion:    cfLatest,
		HasUpdate:        cfInstalled && hasNewerVersion(cfVer, cfLatest),
		InstallComponent: "cloudflare",
	})

	// 6. Netbird
	nbActive, nbStarting := systemdServiceState("netbird")
	nbVer := getNetbirdVersion()
	nbInstalled := nbVer != "" || fileExists("/usr/bin/netbird") || fileExists("/usr/local/bin/netbird")
	nbStatus := buildNetbirdServiceStatus(nbActive, nbVer, nbInstalled, cachedLatest["netbird"])
	if !nbActive && nbStarting {
		nbStatus.Starting = true
		nbStatus.StatusText = serviceStatusText(false, true)
	}
	list = append(list, nbStatus)

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
	stalwartActive, stalwartStarting := systemdServiceState("stalwart")
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
		Starting:         stalwartStarting,
		StatusText:       serviceStatusText(stalwartActive, stalwartStarting),
		Version:          stalwartVer,
		Port:             "25, 465, 993, 8080",
		Installed:        stalwartInstalled,
		LatestVersion:    stalwartLatest,
		HasUpdate:        false,
		InstallComponent: "mail",
	})

	// 9. Docker Engine & Compose v5
	dockerActive, dockerStarting := systemdServiceState("docker")
	dockerVer := getDockerComposeOrEngineVersion()
	dockerInstalled := dockerVer != "" || fileExists("/usr/bin/docker") || fileExists("/usr/local/bin/docker")
	dockerLatest := cachedLatest["docker"]
	if dockerLatest == "" && dockerVer != "" {
		dockerLatest = cleanVersion(strings.TrimPrefix(dockerVer, "Compose "))
	}
	dockerVerClean := cleanVersion(strings.TrimPrefix(dockerVer, "Compose "))
	list = append(list, ServiceStatus{
		Name:             "docker",
		DisplayName:      "Docker & Compose",
		Category:         "Container / Runtime",
		Active:           dockerActive,
		Starting:         dockerStarting,
		StatusText:       serviceStatusText(dockerActive, dockerStarting),
		Version:          dockerVer,
		Port:             "docker.sock",
		Installed:        dockerInstalled,
		LatestVersion:    dockerLatest,
		HasUpdate:        dockerInstalled && dockerVerClean != "" && hasNewerVersion(dockerVerClean, dockerLatest),
		InstallComponent: "docker",
	})

	// 10. wdev Control Plane Service
	wdevActive, wdevStarting := systemdServiceState("wdev")
	if !wdevActive && exec.Command("pgrep", "-f", "wdev serve").Run() == nil {
		wdevActive = true
		wdevStarting = false
	}
	list = append(list, ServiceStatus{
		Name:             "wdev",
		DisplayName:      "wdev Control Plane",
		Category:         "Core Service",
		Active:           wdevActive,
		Starting:         wdevStarting,
		StatusText:       serviceStatusText(wdevActive, wdevStarting),
		Version:          version.Version,
		Port:             "3200",
		Installed:        true,
		LatestVersion:    version.Version,
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
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func runCmdStdout(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func getDockerComposeOrEngineVersion() string {
	if out := runCmdVersion("docker", "compose", "version", "--short"); out != "" {
		v := strings.TrimSpace(out)
		if v != "" {
			if !strings.HasPrefix(v, "v") {
				v = "v" + v
			}
			return "Compose " + v
		}
	}
	if out := runCmdVersion("docker", "--version"); out != "" {
		fields := strings.Fields(out)
		if len(fields) >= 3 {
			v := strings.TrimSuffix(strings.TrimPrefix(fields[2], "v"), ",")
			if v != "" {
				return "v" + v
			}
		}
	}
	return ""
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
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[2] == "5432" && fields[3] == "online" {
				return fields[0]
			}
		}
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[2] == "5432" {
				return fields[0]
			}
		}
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 1 && fields[0] != "" {
				return fields[0]
			}
		}
	}

	return resolveActivePostgresMajorFromProbes(
		isSystemdServiceActive,
		isSystemdServiceEnabled,
		readPGActiveStateFile,
		fileExists,
		runCmdVersion,
	)
}

func isSystemdServiceEnabled(name string) bool {
	return exec.Command("systemctl", "is-enabled", "--quiet", name).Run() == nil
}

func readPGActiveStateFile() string {
	data, err := os.ReadFile("/etc/wdev/pg-active-major")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func resolveActivePostgresMajorFromProbes(
	isActive func(string) bool,
	isEnabled func(string) bool,
	readState func() string,
	exists func(string) bool,
	cmdVersion func(string, ...string) string,
) string {
	// 2. Fedora/RHEL PGDG systemd units aktif (postgresql-19 .. postgresql-14)
	for _, v := range supportedPostgresMajors {
		if isActive("postgresql-" + v) {
			return v
		}
	}

	// 3. Fedora/RHEL distro unit aktif (postgresql.service)
	if isActive("postgresql") {
		if out := cmdVersion("/usr/bin/postgres", "--version"); out != "" {
			if m := parsePostgresMajorFromVersion(out); m != "" {
				return m
			}
		}
	}

	// 4. Unit PGDG yang enabled saat service sedang berhenti (agar tidak kembali ke versi lebih tinggi saat stop/downgrade)
	for _, v := range supportedPostgresMajors {
		if isEnabled("postgresql-" + v) {
			return v
		}
	}
	if isEnabled("postgresql") {
		if out := cmdVersion("/usr/bin/postgres", "--version"); out != "" {
			if m := parsePostgresMajorFromVersion(out); m != "" {
				return m
			}
		}
	}

	// 5. State terakhir yang dicatat wdev saat switch/install
	if state := readState(); state != "" {
		if exists(fmt.Sprintf("/usr/pgsql-%s/bin/postgres", state)) ||
			exists(fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", state)) {
			return state
		}
	}

	// 6. Biner PGDG terpasang di /usr/pgsql-<major>
	for _, v := range supportedPostgresMajors {
		if exists(fmt.Sprintf("/usr/pgsql-%s/bin/postgres", v)) {
			return v
		}
	}

	// 7. Fallback dari postgres / psql di PATH
	if out := cmdVersion("/usr/bin/postgres", "--version"); out != "" {
		if m := parsePostgresMajorFromVersion(out); m != "" {
			return m
		}
	}
	if out := cmdVersion("psql", "--version"); out != "" {
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

func buildNetbirdServiceStatus(active bool, ver string, installed bool, latest string) ServiceStatus {
	if latest == "" && ver != "" {
		latest = cleanVersion(ver)
	}
	return ServiceStatus{
		Name:             "netbird",
		DisplayName:      "NetBird Mesh VPN",
		Category:         "Network / Ingress",
		Active:           active,
		StatusText:       statusText(active),
		Version:          ver,
		Port:             "wt0",
		Installed:        installed,
		LatestVersion:    latest,
		HasUpdate:        installed && hasNewerVersion(ver, latest),
		InstallComponent: "netbird",
	}
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

	// 2. Check configured PowerDNS API (loopback local atau Edge VPS via NetBird)
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
					if verStr == "" && isLoopbackHost(vpsIP) {
						verStr = getLocalPDNSVersion()
					}
					return buildPDNSStatusFromEdgeConf(vpsIP, port, verStr, true)
				}
			}
		}
		ver := ""
		if isLoopbackHost(vpsIP) {
			ver = getLocalPDNSVersion()
		}
		return buildPDNSStatusFromEdgeConf(vpsIP, port, ver, false)
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

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func buildPDNSStatusFromEdgeConf(vpsIP, port, version string, reachable bool) ServiceStatus {
	if port == "" {
		port = "8081"
	}
	if isLoopbackHost(vpsIP) {
		stText := "Stopped (Local)"
		if reachable {
			stText = "Running (Local API)"
		}
		return ServiceStatus{
			Name:        "pdns",
			DisplayName: "PowerDNS Authoritative",
			Category:    "DNS & Mail",
			Active:      reachable,
			StatusText:  stText,
			Version:     version,
			Port:        "53, " + port,
			IsRemote:    false,
		}
	}
	if reachable {
		return ServiceStatus{
			Name:        "pdns",
			DisplayName: "PowerDNS Authoritative (VPS Edge)",
			Category:    "DNS & Mail",
			Active:      true,
			StatusText:  "Online",
			Version:     version,
			Port:        fmt.Sprintf("NetBird %s:%s", vpsIP, port),
			IsRemote:    true,
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

func serviceStatusText(active, starting bool) string {
	if active {
		return "Running"
	}
	if starting {
		return "Starting"
	}
	return "Stopped"
}

func statusText(active bool) string {
	return serviceStatusText(active, false)
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
	var pgPsqlOut string
	var pgPsqlErr error = fmt.Errorf("not found")
	for _, candidate := range []string{
		fmt.Sprintf("/usr/pgsql-%s/bin/psql", pgMajor),
		fmt.Sprintf("/usr/lib/postgresql/%s/bin/psql", pgMajor),
		"psql",
	} {
		if out, err := runToolVersion(candidate, "--version"); err == nil && out != "" {
			pgPsqlOut, pgPsqlErr = out, nil
			break
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

// isAllowedManagedService membatasi kontrol systemctl hanya ke unit layanan yang dikelola wdev.
func isAllowedManagedService(name string) bool {
	switch name {
	case "nginx", "php-fpm", "postgresql", "redis", "redis-server", "valkey", "valkey-server",
		"cloudflared", "netbird", "pdns", "stalwart", "postfix", "docker", "docker.service", "wdev", "orca":
		return true
	}
	for _, v := range []string{"8.5", "8.4", "8.3", "8.2", "8.1", "8.0", "7.4"} {
		if name == "php"+v+"-fpm" {
			return true
		}
	}
	for _, v := range supportedPostgresMajors {
		if name == "postgresql-"+v || name == "postgresql@"+v+"-main" {
			return true
		}
	}
	return false
}

// ControlService executes systemctl start/stop/restart
func ControlService(serviceName, action string) error {
	switch action {
	case "start", "stop", "restart", "reload":
		// Allowed actions
	default:
		return fmt.Errorf("aksi tidak valid: %s", action)
	}

	serviceName = strings.TrimSpace(serviceName)
	if !isAllowedManagedService(serviceName) {
		return fmt.Errorf("layanan tidak diizinkan untuk dikontrol melalui panel: %s", serviceName)
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
		if vpsIP != "" && !isLoopbackHost(vpsIP) {
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
