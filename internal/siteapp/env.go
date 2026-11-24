package siteapp

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnvEntry merepresentasikan satu pasang kunci-nilai pada file .env aplikasi.
type EnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ParseEnvContent mengurai teks .env menjadi daftar EnvEntry tervalidasi.
// Baris kosong dan komentar (#) diabaikan; spasi serta tanda kutip dibersihkan.
func ParseEnvContent(raw string) ([]EnvEntry, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var entries []EnvEntry
	seenIdx := make(map[string]int)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("format .env tidak valid pada baris %d", i+1)
		}
		key := strings.TrimSpace(trimmed[:eq])
		if !envKeyRe.MatchString(key) {
			return nil, fmt.Errorf("nama variabel environment tidak valid: %q", key)
		}
		val := strings.TrimSpace(trimmed[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				quote := val[0]
				inner := val[1 : len(val)-1]
				if quote == '"' {
					inner = strings.ReplaceAll(inner, `\"`, `"`)
					inner = strings.ReplaceAll(inner, `\\`, `\`)
				}
				val = inner
			}
		}
		if strings.ContainsRune(val, '\x00') {
			return nil, fmt.Errorf("nilai variabel %s mengandung null byte", key)
		}

		if idx, exists := seenIdx[key]; exists {
			entries[idx].Value = val
		} else {
			seenIdx[key] = len(entries)
			entries = append(entries, EnvEntry{Key: key, Value: val})
		}
	}
	return entries, nil
}

// SerializeEnvEntries menyusun daftar EnvEntry ke format file EnvironmentFile systemd yang aman.
func SerializeEnvEntries(entries []EnvEntry) (string, error) {
	var b strings.Builder
	seen := make(map[string]bool)
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		if !envKeyRe.MatchString(key) {
			return "", fmt.Errorf("nama variabel environment tidak valid: %q", key)
		}
		if strings.ContainsAny(entry.Value, "\r\n\x00") {
			return "", fmt.Errorf("nilai variabel %s tidak boleh mengandung baris baru atau null byte", key)
		}
		if seen[key] {
			return "", fmt.Errorf("variabel environment duplikat: %s", key)
		}
		seen[key] = true

		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatEnvValue(entry.Value))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func formatEnvValue(val string) string {
	if val == "" {
		return ""
	}
	needsQuote := strings.ContainsAny(val, " \t#\"'\\$`")
	if !needsQuote {
		return val
	}
	escaped := strings.ReplaceAll(val, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// ReadEnvFile membaca dan mengurai file .env di disk.
func ReadEnvFile(path string) ([]EnvEntry, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	raw := string(data)
	entries, parseErr := ParseEnvContent(raw)
	if parseErr != nil {
		return nil, raw, parseErr
	}
	return entries, raw, nil
}

// UpsertPortInEnvEntries memastikan variabel PORT di dalam daftar .env sesuai port VHost.
func UpsertPortInEnvEntries(entries []EnvEntry, port int) []EnvEntry {
	if port <= 0 {
		return entries
	}
	portStr := fmt.Sprintf("%d", port)
	out := make([]EnvEntry, 0, len(entries)+1)
	replaced := false
	for _, e := range entries {
		if strings.TrimSpace(e.Key) == "PORT" {
			out = append(out, EnvEntry{Key: "PORT", Value: portStr})
			replaced = true
			continue
		}
		out = append(out, e)
	}
	if !replaced {
		out = append([]EnvEntry{{Key: "PORT", Value: portStr}}, out...)
	}
	return out
}

// UpsertPortInRawEnv memperbarui nilai PORT di dalam isi teks .env yang sudah ada
// sambil mempertahankan komentar, baris kosong, dan urutan baris lainnya.
// Bila PORT belum ada di dalam raw atau sudah sama, (raw, false) dikembalikan apa adanya.
func UpsertPortInRawEnv(raw string, port int) (string, bool) {
	if port <= 0 {
		return raw, false
	}
	portStr := fmt.Sprintf("%d", port)
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	changed := false
	found := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		prefix := ""
		rest := trimmed
		if strings.HasPrefix(rest, "export ") {
			prefix = "export "
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "export "))
		}
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(rest[:eq])
		if key != "PORT" {
			continue
		}
		found = true
		existingVal := strings.Trim(strings.TrimSpace(rest[eq+1:]), `"'`)
		if existingVal != portStr {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + prefix + "PORT=" + portStr
			changed = true
		}
	}
	if !found || !changed {
		return raw, false
	}
	return strings.Join(lines, "\n"), true
}

// EnsurePortInRawEnv memastikan isi teks .env mengandung PORT=<port>, baik dengan memperbarui
// baris PORT yang ada maupun menambahkannya bila belum ada, tanpa menghapus komentar.
func EnsurePortInRawEnv(raw string, port int) string {
	if port <= 0 {
		return raw
	}
	if updated, changed := UpsertPortInRawEnv(raw, port); changed {
		return updated
	}
	entries, err := ParseEnvContent(raw)
	if err == nil {
		for _, e := range entries {
			if e.Key == "PORT" {
				return raw
			}
		}
	}
	portLine := fmt.Sprintf("PORT=%d\n", port)
	if strings.TrimSpace(raw) == "" {
		return portLine
	}
	if !strings.HasSuffix(raw, "\n") {
		return raw + "\n" + portLine
	}
	return raw + portLine
}

// FindEnvPort mengambil nilai integer PORT dari daftar EnvEntry jika valid.
func FindEnvPort(entries []EnvEntry) int {
	for _, e := range entries {
		if strings.TrimSpace(e.Key) == "PORT" {
			val := strings.TrimSpace(e.Value)
			if p, err := strconv.Atoi(val); err == nil && p > 0 && p <= 65535 {
				return p
			}
		}
	}
	return 0
}
