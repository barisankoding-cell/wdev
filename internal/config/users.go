package config

import (
	"bufio"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// SystemUser mendeskripsikan akun Linux yang tersedia di sistem.
type SystemUser struct {
	Username string `json:"username"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	HomeDir  string `json:"home_dir"`
	Shell    string `json:"shell"`
	IsRoot   bool   `json:"is_root"`
}

// PasswdEntry merepresentasikan satu baris terurai dari /etc/passwd.
type PasswdEntry struct {
	Username string
	UID      uint32
	GID      uint32
	HomeDir  string
	Shell    string
}

// ReadPasswdEntries membaca dan mengurai entri akun dari file passwd (default /etc/passwd).
func ReadPasswdEntries(passwdPath string) []PasswdEntry {
	if strings.TrimSpace(passwdPath) == "" {
		passwdPath = "/etc/passwd"
	}
	f, err := os.Open(passwdPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []PasswdEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		uid64, err1 := strconv.ParseUint(fields[2], 10, 32)
		gid64, err2 := strconv.ParseUint(fields[3], 10, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		entries = append(entries, PasswdEntry{
			Username: fields[0],
			UID:      uint32(uid64),
			GID:      uint32(gid64),
			HomeDir:  fields[5],
			Shell:    fields[6],
		})
	}
	return entries
}

// IsUsableShell memeriksa apakah path shell merupakan binary executable interaktif yang sah.
func IsUsableShell(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	base := strings.ToLower(filepath.Base(p))
	if base == "nologin" || base == "false" || base == "true" || base == "halt" || base == "sync" || base == "shutdown" {
		return false
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0111 != 0
}

func isExistingDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// ListSystemUsers mengembalikan daftar akun non-root interaktif di sistem (UID 1000..60000),
// dengan prioritas SUDO_USER lalu UID terkecil. Bila includeRoot true, root diletakkan di akhir.
func ListSystemUsers(includeRoot bool) []SystemUser {
	return ListSystemUsersFrom("/etc/passwd", strings.TrimSpace(os.Getenv("SUDO_USER")), isExistingDir, IsUsableShell, includeRoot)
}

// ListSystemUsersFrom mengembalikan daftar SystemUser dari berkas passwd dan validator yang dapat disuntikkan untuk pengujian.
func ListSystemUsersFrom(
	passwdPath string,
	preferredUser string,
	dirExistsFn func(string) bool,
	shellUsableFn func(string) bool,
	includeRoot bool,
) []SystemUser {
	if dirExistsFn == nil {
		dirExistsFn = isExistingDir
	}
	if shellUsableFn == nil {
		shellUsableFn = IsUsableShell
	}

	var nonRoot []SystemUser
	var rootEntry *SystemUser

	for _, pe := range ReadPasswdEntries(passwdPath) {
		if pe.UID == 0 {
			if includeRoot && rootEntry == nil {
				h := pe.HomeDir
				if h == "" {
					h = "/root"
				}
				rootEntry = &SystemUser{
					Username: pe.Username,
					UID:      pe.UID,
					GID:      pe.GID,
					HomeDir:  h,
					Shell:    pe.Shell,
					IsRoot:   true,
				}
			}
			continue
		}
		// Abaikan akun sistem/nobody; ambil akun manusia interaktif (UID 1000..60000).
		if pe.UID < 1000 || pe.UID > 60000 || pe.Username == "nobody" || pe.Username == "nfsnobody" {
			continue
		}
		if !shellUsableFn(pe.Shell) {
			continue
		}
		home := pe.HomeDir
		if home == "" {
			home = "/"
		}
		if !dirExistsFn(home) {
			continue
		}
		nonRoot = append(nonRoot, SystemUser{
			Username: pe.Username,
			UID:      pe.UID,
			GID:      pe.GID,
			HomeDir:  home,
			Shell:    pe.Shell,
			IsRoot:   false,
		})
	}

	sort.SliceStable(nonRoot, func(i, j int) bool {
		if preferredUser != "" {
			if nonRoot[i].Username == preferredUser && nonRoot[j].Username != preferredUser {
				return true
			}
			if nonRoot[j].Username == preferredUser && nonRoot[i].Username != preferredUser {
				return false
			}
		}
		if nonRoot[i].UID != nonRoot[j].UID {
			return nonRoot[i].UID < nonRoot[j].UID
		}
		return nonRoot[i].Username < nonRoot[j].Username
	})

	if !includeRoot {
		return nonRoot
	}

	out := make([]SystemUser, 0, len(nonRoot)+1)
	out = append(out, nonRoot...)
	if rootEntry != nil {
		out = append(out, *rootEntry)
	}
	return out
}

// DetectDirOwnerUser mengembalikan username non-root pemilik direktori (UID 1000..60000), atau "" bila root/tidak dikenal.
func DetectDirOwnerUser(dirPath string) string {
	clean := strings.TrimSpace(dirPath)
	if clean == "" {
		return ""
	}
	fi, err := os.Stat(clean)
	if err != nil {
		return ""
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid < 1000 || st.Uid > 60000 {
		return ""
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(u.Username)
	if name == "" || name == "root" || name == "nobody" || name == "nfsnobody" {
		return ""
	}
	return name
}
