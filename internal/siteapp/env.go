package siteapp

import (
	"fmt"
	"os"
	"regexp"
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
