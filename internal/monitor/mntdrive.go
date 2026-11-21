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

// MountDriveInfo merepresentasikan partisi atau drive tambahan yang terpasang (seperti di /mnt).
type MountDriveInfo struct {
	Name       string `json:"name"`
	MountPoint string `json:"mount_point"`
	Device     string `json:"device,omitempty"`
	FSType     string `json:"fs_type,omitempty"`
	TotalGB    int64  `json:"total_gb"`
	UsedGB     int64  `json:"used_gb"`
	FreeGB     int64  `json:"free_gb"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	Percent    int    `json:"percent"`
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
		drives = append(drives, MountDriveInfo{
			Name:       formatMountDriveName(cleanBase, rm.mountPoint),
			MountPoint: rm.mountPoint,
			Device:     rm.device,
			FSType:     rm.fsType,
			TotalGB:    totalGB,
			UsedGB:     usedGB,
			FreeGB:     freeGB,
			TotalBytes: totalBytes,
			UsedBytes:  usedBytes,
			Percent:    pct,
		})
	}

	sort.Slice(drives, func(i, j int) bool {
		return drives[i].MountPoint < drives[j].MountPoint
	})

	return drives
}
