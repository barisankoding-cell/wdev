package monitor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// MountDriveInfo merepresentasikan partisi atau drive tambahan yang terpasang (seperti di /mnt atau media eksternal).
type MountDriveInfo struct {
	Name         string  `json:"name"`
	MountPoint   string  `json:"mount_point"`
	Device       string  `json:"device,omitempty"`
	FSType       string  `json:"fs_type,omitempty"`
	TotalGB      int64   `json:"total_gb"`
	UsedGB       int64   `json:"used_gb"`
	FreeGB       int64   `json:"free_gb"`
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	Percent      int     `json:"percent"`
	PercentExact float64 `json:"percent_exact,omitempty"`
}

var ignoredMountFSTypes = map[string]bool{
	"autofs":      true,
	"proc":        true,
	"sysfs":       true,
	"devtmpfs":    true,
	"devpts":      true,
	"cgroup":      true,
	"cgroup2":     true,
	"nsfs":        true,
	"mqueue":      true,
	"hugetlbfs":   true,
	"debugfs":     true,
	"tracefs":     true,
	"securityfs":  true,
	"pstore":      true,
	"efivarfs":    true,
	"configfs":    true,
	"fusectl":     true,
	"rpc_pipefs":  true,
	"binfmt_misc": true,
}

type statfsFunc func(path string, buf *syscall.Statfs_t) error

func readDiskUsage(path string, statfsFn statfsFunc) (totalGB, usedGB, freeGB int64, totalBytes, usedBytes uint64, percent int, ok bool) {
	if statfsFn == nil {
		statfsFn = syscall.Statfs
	}
	var stat syscall.Statfs_t
	if err := statfsFn(path, &stat); err != nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if stat.Bsize <= 0 || stat.Blocks == 0 {
		return 0, 0, 0, 0, 0, 0, false
	}

	bsize := uint64(stat.Bsize)
	totalBytes = stat.Blocks * bsize
	freeBytes := stat.Bfree * bsize
	if freeBytes > totalBytes {
		freeBytes = totalBytes
	}
	usedBytes = totalBytes - freeBytes

	const gib = 1024 * 1024 * 1024
	totalGB = int64(totalBytes / gib)
	usedGB = int64(usedBytes / gib)
	freeGB = totalGB - usedGB
	if freeGB < 0 {
		freeGB = 0
	}
	if totalBytes > 0 {
		percent = int((float64(usedBytes) / float64(totalBytes)) * 100)
	}
	return totalGB, usedGB, freeGB, totalBytes, usedBytes, percent, true
}

func unescapeProcMountField(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+3 < len(raw) {
			switch raw[i+1 : i+4] {
			case "040":
				b.WriteByte(' ')
				i += 3
				continue
			case "011":
				b.WriteByte('\t')
				i += 3
				continue
			case "012":
				b.WriteByte('\n')
				i += 3
				continue
			case "134":
				b.WriteByte('\\')
				i += 3
				continue
			}
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

func formatMountDriveName(baseDir, mountPoint string) string {
	cleanBase := filepath.Clean(baseDir)
	cleanMP := filepath.Clean(mountPoint)
	if cleanMP == cleanBase {
		return fmt.Sprintf("Drive (%s)", cleanMP)
	}
	return fmt.Sprintf("Drive %s (%s)", filepath.Base(cleanMP), cleanMP)
}

// DetectMountDrives mendeteksi seluruh drive atau mountpoint aktif pada /mnt.
func DetectMountDrives() []MountDriveInfo {
	return detectMountDrives("/mnt", "/proc/mounts", syscall.Statfs)
}

func detectMountDrives(baseDir, mountsPath string, statfsFn statfsFunc) []MountDriveInfo {
	cleanBase := filepath.Clean(baseDir)
	mntInfo, err := os.Stat(cleanBase)
	if err != nil || !mntInfo.IsDir() {
		return nil
	}

	type rawMount struct {
		mountPoint string
		device     string
		fsType     string
	}

	byPath := make(map[string]rawMount)

	if f, openErr := os.Open(mountsPath); openErr == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 3 {
				continue
			}
			dev := unescapeProcMountField(fields[0])
			mp := filepath.Clean(unescapeProcMountField(fields[1]))
			fsType := strings.ToLower(strings.TrimSpace(fields[2]))
			if mp != cleanBase && !strings.HasPrefix(mp, cleanBase+string(filepath.Separator)) {
				continue
			}
			if ignoredMountFSTypes[fsType] {
				delete(byPath, mp)
				continue
			}
			st, statErr := os.Stat(mp)
			if statErr != nil || !st.IsDir() {
				continue
			}
			byPath[mp] = rawMount{
				mountPoint: mp,
				device:     dev,
				fsType:     fsType,
			}
		}
	}

	// Fallback deteksi lewat perbedaan device ID (st_dev) bila /proc/mounts tidak tersedia atau belum mencantumkan mount.
	if mntSys, ok := mntInfo.Sys().(*syscall.Stat_t); ok {
		parentDir := filepath.Dir(cleanBase)
		if parentInfo, pErr := os.Stat(parentDir); pErr == nil {
			if parentSys, ok2 := parentInfo.Sys().(*syscall.Stat_t); ok2 && mntSys.Dev != parentSys.Dev {
				if _, exists := byPath[cleanBase]; !exists {
					byPath[cleanBase] = rawMount{mountPoint: cleanBase}
				}
			}
		}

		if entries, rErr := os.ReadDir(cleanBase); rErr == nil {
			for _, ent := range entries {
				if !ent.IsDir() {
					continue
				}
				childPath := filepath.Join(cleanBase, ent.Name())
				childInfo, cErr := os.Stat(childPath)
				if cErr != nil || !childInfo.IsDir() {
					continue
				}
				if childSys, ok2 := childInfo.Sys().(*syscall.Stat_t); ok2 && childSys.Dev != mntSys.Dev {
					if _, exists := byPath[childPath]; !exists {
						byPath[childPath] = rawMount{mountPoint: childPath}
					}
				}
			}
		}
	}

	if len(byPath) == 0 {
		return nil
	}

	var drives []MountDriveInfo
	for _, rm := range byPath {
		totalGB, usedGB, freeGB, totalBytes, usedBytes, pct, ok := readDiskUsage(rm.mountPoint, statfsFn)
		if !ok {
			continue
		}
		var pctExact float64
		if totalBytes > 0 {
			pctExact = clampPercent2(float64(usedBytes) / float64(totalBytes) * 100.0)
		}
		drives = append(drives, MountDriveInfo{
			Name:         formatMountDriveName(cleanBase, rm.mountPoint),
			MountPoint:   rm.mountPoint,
			Device:       rm.device,
			FSType:       rm.fsType,
			TotalGB:      totalGB,
			UsedGB:       usedGB,
			FreeGB:       freeGB,
			TotalBytes:   totalBytes,
			UsedBytes:    usedBytes,
			Percent:      pct,
			PercentExact: pctExact,
		})
	}

	sort.Slice(drives, func(i, j int) bool {
		return drives[i].MountPoint < drives[j].MountPoint
	})

	return drives
}

var ignoredOtherMountFSTypes = map[string]bool{
	"tmpfs":                    true,
	"ramfs":                    true,
	"devfs":                    true,
	"overlay":                  true,
	"squashfs":                 true,
	"bpf":                      true,
	"fuse.portal":              true,
	"fuse.gvfsd-fuse":          true,
	"fuse.lxcfs":               true,
	"fuse.xdg-document-portal": true,
}

func isIgnoredOtherMountFS(fsType string) bool {
	return ignoredMountFSTypes[fsType] || ignoredOtherMountFSTypes[fsType]
}

func isSystemOrVirtualMountPoint(mp string) bool {
	clean := filepath.Clean(mp)
	if clean == "/" || clean == "/boot" || strings.HasPrefix(clean, "/boot/") ||
		clean == "/proc" || strings.HasPrefix(clean, "/proc/") ||
		clean == "/sys" || strings.HasPrefix(clean, "/sys/") ||
		clean == "/dev" || strings.HasPrefix(clean, "/dev/") ||
		clean == "/tmp" ||
		clean == "/snap" || strings.HasPrefix(clean, "/snap/") ||
		strings.HasPrefix(clean, "/var/lib/docker/") ||
		strings.HasPrefix(clean, "/var/lib/containers/") ||
		strings.HasPrefix(clean, "/var/lib/kubelet/") ||
		strings.HasPrefix(clean, "/var/lib/lxcfs") {
		return true
	}
	if clean == "/run" || strings.HasPrefix(clean, "/run/") {
		// Izinkan /run/media/* karena udisks2/systemd me-mount USB & drive eksternal di sana.
		if clean == "/run/media" || strings.HasPrefix(clean, "/run/media/") {
			return false
		}
		return true
	}
	return false
}

// DetectOtherMounts mendeteksi seluruh disk eksternal dan mount tambahan aktif selain root & partisi sistem.
func DetectOtherMounts() []MountDriveInfo {
	return detectOtherMounts("/proc/mounts", syscall.Statfs, DetectMountDrives())
}

func detectOtherMounts(mountsPath string, statfsFn statfsFunc, mntFallback []MountDriveInfo) []MountDriveInfo {
	type rawMount struct {
		mountPoint string
		device     string
		fsType     string
	}

	var entries []rawMount
	var rootDevice, rootFSType string

	if f, err := os.Open(mountsPath); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 3 {
				continue
			}
			dev := unescapeProcMountField(fields[0])
			mp := filepath.Clean(unescapeProcMountField(fields[1]))
			fsType := strings.ToLower(strings.TrimSpace(fields[2]))

			if mp == "/" {
				rootDevice = dev
				rootFSType = fsType
				continue
			}
			entries = append(entries, rawMount{
				mountPoint: mp,
				device:     dev,
				fsType:     fsType,
			})
		}
	}

	byMount := make(map[string]rawMount)
	for _, rm := range entries {
		if isIgnoredOtherMountFS(rm.fsType) {
			delete(byMount, rm.mountPoint)
			continue
		}
		if isSystemOrVirtualMountPoint(rm.mountPoint) {
			continue
		}
		if rm.device == "none" || rm.device == "udev" || rm.device == "portal" {
			continue
		}
		if rootDevice != "" && rm.device == rootDevice && rm.fsType == rootFSType {
			continue
		}
		st, statErr := os.Stat(rm.mountPoint)
		if statErr != nil || !st.IsDir() {
			continue
		}
		byMount[rm.mountPoint] = rm
	}

	var drives []MountDriveInfo
	seenDevSize := make(map[string]int) // dedup bind-mount perangkat + ukuran identik

	// Urutkan agar mount path yang lebih pendek dipilih lebih dulu saat deduplikasi bind-mount.
	paths := make([]string, 0, len(byMount))
	for mp := range byMount {
		paths = append(paths, mp)
	}
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})

	for _, mp := range paths {
		rm := byMount[mp]
		totalGB, usedGB, freeGB, totalBytes, usedBytes, pct, ok := readDiskUsage(rm.mountPoint, statfsFn)
		if !ok || totalBytes == 0 {
			continue
		}
		if rm.device != "" {
			key := fmt.Sprintf("%s|%s|%d", rm.device, rm.fsType, totalBytes)
			if _, exists := seenDevSize[key]; exists {
				continue
			}
			seenDevSize[key] = len(drives)
		}
		pctExact := clampPercent2(float64(usedBytes) / float64(totalBytes) * 100.0)
		label := filepath.Base(rm.mountPoint)
		if label == "" || label == "." || label == "/" {
			label = rm.mountPoint
		}
		drives = append(drives, MountDriveInfo{
			Name:         fmt.Sprintf("%s (%s)", label, rm.mountPoint),
			MountPoint:   rm.mountPoint,
			Device:       rm.device,
			FSType:       rm.fsType,
			TotalGB:      totalGB,
			UsedGB:       usedGB,
			FreeGB:       freeGB,
			TotalBytes:   totalBytes,
			UsedBytes:    usedBytes,
			Percent:      pct,
			PercentExact: pctExact,
		})
	}

	// Sertakan fallback /mnt jika belum ada di hasil /proc/mounts.
	present := make(map[string]bool, len(drives))
	for _, d := range drives {
		present[d.MountPoint] = true
	}
	for _, extra := range mntFallback {
		if !present[extra.MountPoint] && extra.TotalBytes > 0 {
			if extra.PercentExact == 0 {
				extra.PercentExact = clampPercent2(float64(extra.UsedBytes) / float64(extra.TotalBytes) * 100.0)
			}
			drives = append(drives, extra)
		}
	}

	sort.Slice(drives, func(i, j int) bool {
		return drives[i].MountPoint < drives[j].MountPoint
	})

	return drives
}
