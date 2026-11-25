package siteapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
	"wdev/internal/store"
)

const serviceNamePrefix = "wdev-app-"

// ServiceNameForDomain menghasilkan nama unit systemd yang deterministik dan aman
// (hanya huruf kecil, angka, serta tanda hubung, tanpa ".service").
func ServiceNameForDomain(domain string) string {
	clean := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	var b strings.Builder
	prevDash := false
	for i := 0; i < len(clean); i++ {
		c := clean[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "site"
	}
	full := serviceNamePrefix + slug
	if len(full) <= 48 {
		return full
	}
	sum := sha256.Sum256([]byte(clean))
	hashSuffix := hex.EncodeToString(sum[:])[:6]
	maxSlugLen := 48 - len(serviceNamePrefix) - 1 - len(hashSuffix)
	if maxSlugLen < 6 {
		maxSlugLen = 6
	}
	trimmedSlug := strings.TrimRight(slug[:maxSlugLen], "-")
	return serviceNamePrefix + trimmedSlug + "-" + hashSuffix
}

// UnitFilenameForDomain mengembalikan nama file unit lengkap berakhiran .service.
func UnitFilenameForDomain(domain string) string {
	return ServiceNameForDomain(domain) + ".service"
}

func escapeSystemdValue(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func buildToolchainPATH(runUser string) (string, string) {
	homeDir := "/home/" + runUser
	if runUser == "root" {
		homeDir = "/root"
	}
	if u, err := user.Lookup(runUser); err == nil && strings.TrimSpace(u.HomeDir) != "" {
		homeDir = strings.TrimSpace(u.HomeDir)
	}

	dirs := []string{
		"/usr/local/bin",
		"/usr/local/go/bin",
		"/usr/local/cargo/bin",
		"/usr/local/lib/nodejs/current/bin",
		filepath.Join(homeDir, ".local/bin"),
		filepath.Join(homeDir, ".cargo/bin"),
		filepath.Join(homeDir, ".bun/bin"),
		"/usr/local/sbin",
		"/usr/sbin",
		"/usr/bin",
		"/sbin",
		"/bin",
	}
	return strings.Join(dirs, ":"), homeDir
}

// RenderSystemdUnit menghasilkan isi unit systemd untuk Managed App VHost.
func RenderSystemdUnit(rec store.SiteAppRecord) string {
	serviceName := strings.TrimSuffix(strings.TrimSpace(rec.ServiceName), ".service")
	if serviceName == "" {
		serviceName = ServiceNameForDomain(rec.Domain)
	}
	pathVal, homeDir := buildToolchainPATH(rec.RunUser)
	execCmd := "/bin/sh -lc " + shellSingleQuote(rec.StartCommand)

	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\n")
	fmt.Fprintf(&b, "Description=wdev Managed App (%s)\n", escapeSystemdValue(rec.Domain))
	fmt.Fprintf(&b, "After=network-online.target\n")
	fmt.Fprintf(&b, "Wants=network-online.target\n\n")

	fmt.Fprintf(&b, "[Service]\n")
	fmt.Fprintf(&b, "Type=simple\n")
	fmt.Fprintf(&b, "User=%s\n", escapeSystemdValue(rec.RunUser))
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", escapeSystemdValue(rec.WorkingDir))
	fmt.Fprintf(&b, "Environment=PORT=%d\n", rec.Port)
	if strings.TrimSpace(rec.EnvFile) != "" {
		fmt.Fprintf(&b, "EnvironmentFile=-%s\n", escapeSystemdValue(rec.EnvFile))
	}
	fmt.Fprintf(&b, "Environment=HOME=%s\n", escapeSystemdValue(homeDir))
	fmt.Fprintf(&b, "Environment=PATH=%s\n", escapeSystemdValue(pathVal))
	fmt.Fprintf(&b, "Environment=CARGO_HOME=/usr/local/cargo\n")
	fmt.Fprintf(&b, "Environment=RUSTUP_HOME=/usr/local/rustup\n")
	fmt.Fprintf(&b, "ExecStart=%s\n", escapeSystemdValue(execCmd))
	fmt.Fprintf(&b, "MemoryAccounting=yes\n")
	fmt.Fprintf(&b, "Restart=on-failure\n")
	fmt.Fprintf(&b, "RestartSec=3\n")
	fmt.Fprintf(&b, "StandardOutput=journal\n")
	fmt.Fprintf(&b, "StandardError=journal\n")
	fmt.Fprintf(&b, "SyslogIdentifier=%s\n\n", escapeSystemdValue(serviceName))

	fmt.Fprintf(&b, "[Install]\n")
	fmt.Fprintf(&b, "WantedBy=multi-user.target\n")
	return b.String()
}
