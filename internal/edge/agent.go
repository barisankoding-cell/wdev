package edge

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Lokasi konfigurasi & state yang dipakai agent di VPS.
const (
	ConfigDir       = "/etc/wdev-edge"
	ConfigFile      = ConfigDir + "/edge.conf"
	APIKeyFile      = ConfigDir + "/api_key.secret"
	NginxAvailable  = "/etc/nginx/sites-available"
	NginxEnabled    = "/etc/nginx/sites-enabled"
	NginxStreamsDir = "/etc/nginx/streams-enabled"
	NginxConf       = "/etc/nginx/nginx.conf"
	MailStreamConf  = NginxStreamsDir + "/wdev_mail.conf"
	MailTargetFile  = ConfigDir + "/mail_target.json"
	MailDomainsFile = ConfigDir + "/mail_domains.json"
	MailRelaysFile  = ConfigDir + "/mail_relays.json"

	DefaultAgentPort = "8082"
	DefaultPDNSPort  = "8081"
)

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
var tldRe = regexp.MustCompile(`^[a-z]{2,}$`)

// validDomain memvalidasi FQDN tanpa lookahead (didukung regexp Go).
func validDomain(d string) bool {
	d = strings.ToLower(strings.TrimSpace(d))
	if d == "" || len(d) > 253 {
		return false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return false
	}
	for i, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if i == len(labels)-1 {
			if !tldRe.MatchString(label) {
				return false
			}
			continue
		}
		if !labelRe.MatchString(label) {
			return false
		}
	}
	return true
}

func LoadAPIKey() string {
	data, err := os.ReadFile(APIKeyFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runStdin(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func serviceActive(name string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", name).Run() == nil
}

func nginxTest() error {
	out, err := run("nginx", "-t")
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func nginxReload() error {
	_, err := run("systemctl", "reload", "nginx")
	return err
}

func hasIPv6() bool {
	if _, err := os.Stat("/proc/net/if_inet6"); err != nil {
		return false
	}
	out, _ := run("sysctl", "-n", "net.ipv6.conf.all.disable_ipv6")
	return strings.TrimSpace(out) != "1"
}

func ipv6Listen() string {
	if hasIPv6() {
		return "\tlisten [::]:80;\n"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Proxy publik (HTTP reverse proxy)
// ---------------------------------------------------------------------------

func proxyBlock(domain string, targetIP string, targetPort int, ws bool) string {
	var b strings.Builder
	b.WriteString("# Managed by wdev-edge (native Go)\n")
	b.WriteString("server {\n")
	b.WriteString("\tlisten 80;\n")
	b.WriteString(ipv6Listen())
	fmt.Fprintf(&b, "\tserver_name %s;\n\n", domain)
	b.WriteString("\tlocation / {\n")
	fmt.Fprintf(&b, "\t\tproxy_pass http://%s:%d;\n", targetIP, targetPort)
	b.WriteString("\t\tproxy_set_header Host $host;\n")
	b.WriteString("\t\tproxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString("\t\tproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString("\t\tproxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString("\t\tproxy_connect_timeout 10s;\n")
	b.WriteString("\t\tproxy_read_timeout 300s;\n")
	b.WriteString("\t\tproxy_send_timeout 300s;\n")
	b.WriteString("\t\tproxy_max_temp_file_size 0;\n")
	b.WriteString("\t\tproxy_request_buffering off;\n")
	if ws {
		b.WriteString("\t\tproxy_http_version 1.1;\n")
		b.WriteString("\t\tproxy_set_header Upgrade $http_upgrade;\n")
		b.WriteString("\t\tproxy_set_header Connection \"upgrade\";\n")
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func enableSite(name string) error {
	src := filepath.Join(NginxAvailable, name)
	dst := filepath.Join(NginxEnabled, name)
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	return os.Symlink(src, dst)
}

// writeProxy menulis config Nginx + SSL opsional, dengan rollback bila test gagal.
func writeProxy(domain, targetIP string, targetPort int, useSSL, ws bool) (string, error) {
	name := "wdev_" + domain + ".conf"
	confPath := filepath.Join(NginxAvailable, name)
	linkPath := filepath.Join(NginxEnabled, name)

	if err := os.MkdirAll(NginxAvailable, 0755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(NginxEnabled, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(confPath, []byte(proxyBlock(domain, targetIP, targetPort, ws)), 0644); err != nil {
		return "", err
	}
	if err := enableSite(name); err != nil {
		return "", err
	}
	if err := nginxTest(); err != nil {
		_ = os.Remove(linkPath)
		_ = os.Remove(confPath)
		return "", fmt.Errorf("nginx config test gagal: %w", err)
	}
	_ = nginxReload()

	sslStatus := "none"
	if useSSL {
		out, err := run("certbot", "--nginx", "-d", domain, "--non-interactive", "--agree-tos",
			"--register-unsafely-without-email", "--redirect", "--deploy-hook", "systemctl reload nginx")
		if err == nil {
			sslStatus = "active"
		} else {
			sslStatus = "failed: " + truncate(out, 200)
		}
	}
	return sslStatus, nil
}

func removeProxy(domain string) bool {
	name := "wdev_" + domain + ".conf"
	removed := false
	for _, p := range []string{filepath.Join(NginxEnabled, name), filepath.Join(NginxAvailable, name)} {
		if err := os.Remove(p); err == nil {
			removed = true
		}
	}
	if removed {
		_ = nginxTest()
		_ = nginxReload()
	}
	return removed
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---------------------------------------------------------------------------
// Mail gateway (Postfix + TCP stream proxy ke Stalwart lokal)
// ---------------------------------------------------------------------------

func mailPorts() []int { return []int{25, 465, 587, 993, 143, 995, 110} }

func isListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func mailListeners() map[string]bool {
	res := make(map[string]bool, len(mailPorts()))
	for _, p := range mailPorts() {
		res[fmt.Sprintf("%d", p)] = isListening(p)
	}
	return res
}

func firewallStatus() string {
	parts := []string{}
	if _, err := exec.LookPath("ufw"); err == nil {
		out, _ := run("ufw", "status")
		if strings.Contains(out, "Status: active") {
			parts = append(parts, "ufw=active")
		} else {
			parts = append(parts, "ufw=inactive")
		}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		out, _ := run("firewall-cmd", "--state")
		if out == "" {
			out = "unknown"
		}
		parts = append(parts, "firewalld="+out)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

func ensureMailFirewall() string {
	results := []string{}
	if _, err := exec.LookPath("ufw"); err == nil {
		out, _ := run("ufw", "status")
		if strings.Contains(out, "Status: active") {
			for _, spec := range []string{"22/tcp", "80/tcp", "443/tcp", "53/tcp", "53/udp"} {
				_, _ = run("ufw", "allow", spec)
			}
			for _, p := range mailPorts() {
				_, _ = run("ufw", "allow", fmt.Sprintf("%d/tcp", p))
			}
			_, _ = run("ufw", "reload")
			results = append(results, "ufw aktif: port web, DNS, SSH, mail diizinkan")
		} else {
			results = append(results, "ufw terpasang tetapi tidak aktif")
		}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		state, _ := run("firewall-cmd", "--state")
		if state == "running" {
			for _, spec := range []string{"22/tcp", "80/tcp", "443/tcp", "53/tcp", "53/udp"} {
				_, _ = run("firewall-cmd", "--permanent", "--add-port", spec)
			}
			for _, p := range mailPorts() {
				_, _ = run("firewall-cmd", "--permanent", "--add-port", fmt.Sprintf("%d/tcp", p))
			}
			_, _ = run("firewall-cmd", "--reload")
			results = append(results, "firewalld aktif: port web, DNS, SSH, mail diizinkan")
		} else {
			results = append(results, "firewalld tidak berjalan")
		}
	}
	if len(results) == 0 {
		return "tidak ada firewall host yang terdeteksi"
	}
	return strings.Join(results, "; ")
}

func ensureNginxStreamSupport() bool {
	_ = os.MkdirAll(NginxStreamsDir, 0755)
	if data, err := os.ReadFile(NginxConf); err == nil {
		if !strings.Contains(string(data), "streams-enabled") {
			f, err := os.OpenFile(NginxConf, os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				_, _ = f.WriteString("\n# Managed by wdev-edge\ninclude " + NginxStreamsDir + "/*.conf;\n")
				_ = f.Close()
			}
		}
	}
	out, _ := run("nginx", "-V")
	if strings.Contains(out, "with-stream") {
		return true
	}
	if entries, err := os.ReadDir("/etc/nginx/modules-enabled"); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), "stream") {
				return true
			}
		}
	}
	if _, err := exec.LookPath("apt-get"); err == nil {
		_, _ = run("apt-get", "install", "-y", "libnginx-mod-stream")
		return true
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		_, _ = run("dnf", "install", "-y", "nginx-mod-stream")
		return true
	}
	return false
}

func findLetsEncryptCert(hostname string) (string, string) {
	base := "/etc/letsencrypt/live"
	entries, err := os.ReadDir(base)
	if err != nil || hostname == "" {
		return "", ""
	}
	target := strings.ToLower(hostname)
	for _, e := range entries {
		cert := filepath.Join(base, e.Name(), "fullchain.pem")
		key := filepath.Join(base, e.Name(), "privkey.pem")
		if _, err := os.Stat(cert); err != nil {
			continue
		}
		if _, err := os.Stat(key); err != nil {
			continue
		}
		out, err := run("openssl", "x509", "-in", cert, "-noout", "-text")
		if err != nil {
			continue
		}
		for _, m := range regexp.MustCompile(`(?i)DNS:([^,\s]+)`).FindAllStringSubmatch(out, -1) {
			name := strings.ToLower(m[1])
			if name == target {
				return cert, key
			}
			if strings.HasPrefix(name, "*.") && strings.HasSuffix(target, name[1:]) {
				return cert, key
			}
		}
	}
	return "", ""
}

type mailCert struct {
	Cert string
	Key  string
}

// mailStreamBlock membuat proxy semua port mail. Port implicit TLS (465/993/995)
// diterminasi di VPS memakai semua sertifikat yang ada (SNI), lalu diteruskan ke
// Stalwart lokal. Port STARTTLS/plain (587/143/110) diteruskan apa adanya.
func mailStreamBlock(targetIP string, certs []mailCert) string {
	var b strings.Builder
	mode := "passthrough"
	if len(certs) > 0 {
		mode = "tls"
	}
	fmt.Fprintf(&b, "# Managed by wdev-edge: %s mail ports -> Stalwart lokal (%s)\n", mode, targetIP)
	b.WriteString("stream {\n")
	tlsServer := func(port int, timeout string) {
		fmt.Fprintf(&b, "\tserver {\n\t\tlisten %d ssl;\n", port)
		for _, c := range certs {
			fmt.Fprintf(&b, "\t\tssl_certificate %s;\n\t\tssl_certificate_key %s;\n", c.Cert, c.Key)
		}
		b.WriteString("\t\tssl_protocols TLSv1.2 TLSv1.3;\n")
		fmt.Fprintf(&b, "\t\tproxy_pass %s:%d;\n", targetIP, port)
		b.WriteString("\t\tproxy_ssl on;\n\t\tproxy_ssl_verify off;\n")
		b.WriteString("\t\tproxy_connect_timeout 10s;\n")
		fmt.Fprintf(&b, "\t\tproxy_timeout %s;\n\t}\n", timeout)
	}
	plainServer := func(port int, timeout string) {
		fmt.Fprintf(&b, "\tserver {\n\t\tlisten %d;\n\t\tproxy_pass %s:%d;\n", port, targetIP, port)
		b.WriteString("\t\tproxy_connect_timeout 10s;\n")
		fmt.Fprintf(&b, "\t\tproxy_timeout %s;\n\t}\n", timeout)
	}
	for _, p := range []struct {
		port    int
		timeout string
	}{{465, "5m"}, {993, "24h"}, {995, "24h"}} {
		if len(certs) > 0 {
			tlsServer(p.port, p.timeout)
		} else {
			plainServer(p.port, p.timeout)
		}
	}
	for _, p := range []struct {
		port    int
		timeout string
	}{{587, "5m"}, {143, "24h"}, {110, "24h"}} {
		plainServer(p.port, p.timeout)
	}
	b.WriteString("}\n")
	return b.String()
}

func collectMailCerts(domains []string) []mailCert {
	seen := make(map[string]bool)
	var certs []mailCert
	for _, d := range domains {
		cert, key := findLetsEncryptCert("mail." + d)
		if cert == "" || key == "" || seen[cert] {
			continue
		}
		seen[cert] = true
		certs = append(certs, mailCert{Cert: cert, Key: key})
	}
	return certs
}

func applyMailStream(conf string, moduleInstalled bool) error {
	if err := os.MkdirAll(NginxStreamsDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(MailStreamConf, []byte(conf), 0644); err != nil {
		return err
	}
	if err := nginxTest(); err != nil {
		_ = os.Remove(MailStreamConf)
		return fmt.Errorf("nginx -t gagal: %w", err)
	}
	action := "reload"
	if moduleInstalled {
		action = "restart"
	}
	if _, err := run("systemctl", action, "nginx"); err != nil {
		_ = os.Remove(MailStreamConf)
		return fmt.Errorf("nginx %s gagal", action)
	}
	return nil
}

// removeMailTransport menghapus entri relay_domains & transport Postfix untuk
// satu domain agar VPS tidak lagi meneruskan email ke mesin lokal.
func removeMailTransport(domain string) {
	removeLineWithPrefix("/etc/postfix/relay_domains", domain+" ")
	removeLineWithPrefix("/etc/postfix/transport", domain+" ")
	_, _ = run("postmap", "/etc/postfix/relay_domains")
	_, _ = run("postmap", "/etc/postfix/transport")
	_, _ = run("systemctl", "reload", "postfix")
}

func removeLineWithPrefix(path, prefix string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := []string{}
	for _, l := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, prefix) {
			continue
		}
		lines = append(lines, t)
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// removeMailDomainMembership menghapus domain dari daftar mail_domains.json
// sehingga sertifikat & stream mail tidak lagi memuatnya.
func removeMailDomainMembership(domain string) bool {
	domains := loadMailDomains()
	kept := make([]string, 0, len(domains))
	removed := false
	for _, d := range domains {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			removed = true
			continue
		}
		kept = append(kept, d)
	}
	if removed {
		saveMailDomains(kept)
	}
	return removed
}

// rebuildMailStream menulis ulang stream proxy memakai daftar domain tersisa.
func rebuildMailStream() string {
	target := ""
	if v := loadJSON(MailTargetFile); v != nil {
		target, _ = v["target_ip"].(string)
	}
	if target == "" {
		return "skipped"
	}
	domains := loadMailDomains()
	certs := collectMailCerts(domains)
	moduleInstalled := ensureNginxStreamSupport()
	if len(domains) == 0 {
		_ = os.Remove(MailStreamConf)
		action := "reload"
		if moduleInstalled {
			action = "restart"
		}
		_, _ = run("systemctl", action, "nginx")
		return "empty"
	}
	if err := applyMailStream(mailStreamBlock(target, certs), moduleInstalled); err != nil {
		return "error: " + truncate(err.Error(), 160)
	}
	return "updated"
}

// cleanupMailCertificate menerbitkan ulang lineage sertifikat mail tanpa domain
// yang sudah dihapus, atau menghapus lineage bila tidak ada domain tersisa.
func cleanupMailCertificate() string {
	if _, err := os.Stat("/etc/letsencrypt/live/wdev-mail/fullchain.pem"); err != nil {
		return "skipped: lineage wdev-mail belum ada"
	}
	domains := loadMailDomains()
	if len(domains) == 0 {
		if out, err := run("certbot", "delete", "--cert-name", "wdev-mail", "--non-interactive"); err != nil {
			return "skipped: " + truncate(out, 120)
		}
		return "deleted"
	}
	args := []string{"--nginx", "--cert-name", "wdev-mail", "--expand", "--non-interactive", "--agree-tos", "--register-unsafely-without-email", "--deploy-hook", "systemctl reload nginx"}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if !validDomain(d) {
			continue
		}
		args = append(args, "-d", "mailadmin."+d, "-d", "webmail."+d, "-d", "mail."+d)
	}
	if out, err := run("certbot", args...); err != nil {
		return "partial: " + truncate(out, 160)
	}
	return "reissued"
}

// removeMailDomain membersihkan seluruh artefak domain mail di VPS.
func removeMailDomain(domain string) (map[string]any, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !validDomain(domain) {
		return nil, fmt.Errorf("domain tidak valid: %s", domain)
	}

	removedFiles := 0
	adminSub := "mailadmin." + domain
	webmailSub := "webmail." + domain
	mailHost := "mail." + domain
	for _, name := range []string{"wdev_" + adminSub + ".conf", "wdev_" + webmailSub + ".conf", "wdev_" + mailHost + ".conf"} {
		removed := false
		for _, p := range []string{filepath.Join(NginxEnabled, name), filepath.Join(NginxAvailable, name)} {
			if err := os.Remove(p); err == nil {
				removed = true
			}
		}
		if removed {
			removedFiles++
		}
	}
	_ = nginxTest()
	_ = nginxReload()

	removeMailTransport(domain)
	membershipRemoved := removeMailDomainMembership(domain)
	streamStatus := rebuildMailStream()
	certStatus := cleanupMailCertificate()

	relayRemoved := false
	if relays := loadMailRelays(); relays != nil {
		if _, ok := relays[domain]; ok {
			delete(relays, domain)
			saveJSON(MailRelaysFile, relays)
			relayRemoved = true
		}
	}

	return map[string]any{
		"success": true, "domain": domain, "vhosts_removed": removedFiles, "membership_removed": membershipRemoved, "stream": streamStatus, "certificate": certStatus, "relay_removed": relayRemoved}, nil
}

func writeMailStream(targetIP, mailHost string) string {
	if targetIP == "" {
		return "skipped"
	}
	moduleInstalled := ensureNginxStreamSupport()
	_ = os.MkdirAll(NginxStreamsDir, 0755)

	domains := loadMailDomains()
	added := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(mailHost)), "mail.")
	if added != "" && added != strings.ToLower(strings.TrimSpace(mailHost)) {
		found := false
		for _, existing := range domains {
			if strings.EqualFold(existing, added) {
				found = true
				break
			}
		}
		if !found {
			domains = append(domains, added)
		}
	}
	certs := collectMailCerts(domains)
	mode := "passthrough"
	if len(certs) > 0 {
		mode = "tls"
	}
	if err := applyMailStream(mailStreamBlock(targetIP, certs), moduleInstalled); err != nil {
		return "failed: " + err.Error()
	}
	saveJSON(MailTargetFile, map[string]any{"target_ip": targetIP, "mail_host": mailHost, "mode": mode, "domains": domains, "certs": len(certs)})
	if mode == "tls" {
		return "active-tls"
	}
	return "active-passthrough"
}

func restoreMailStream() {
	data := loadJSON(MailTargetFile)
	if data == nil {
		return
	}
	target, _ := data["target_ip"].(string)
	host, _ := data["mail_host"].(string)
	if target != "" {
		writeMailStream(target, host)
	}
}

func saveJSON(path string, v any) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

func loadJSON(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	return v
}

func loadMailDomains() []string {
	data, err := os.ReadFile(MailDomainsFile)
	if err != nil {
		return nil
	}
	var domains []string
	_ = json.Unmarshal(data, &domains)
	return domains
}

func saveMailDomains(domains []string) { saveJSON(MailDomainsFile, domains) }

func loadMailRelays() map[string]any {
	if v := loadJSON(MailRelaysFile); v != nil {
		return v
	}
	return map[string]any{}
}

// mergeMynetworks menyusun mynetworks tanpa membuka seluruh subnet NetBird.
func mergeMynetworks(current, targetIP string) string {
	seen := map[string]bool{"127.0.0.0/8": true, "[::1]/128": true}
	res := []string{"127.0.0.0/8", "[::1]/128"}
	for _, f := range strings.Fields(current) {
		if f == "127.0.0.1/32" || f == "::1/128" || f == "100.64.0.0/10" || seen[f] {
			continue
		}
		seen[f] = true
		res = append(res, f)
	}
	if targetIP != "" && !seen[targetIP] {
		res = append(res, targetIP)
	}
	return strings.Join(res, " ")
}

func ensurePostfixInstalled(mailHost, targetIP string) {
	if _, err := os.Stat("/usr/sbin/postfix"); err != nil {
		_, _ = runStdin("postfix postfix/mailname string mail.wdev.local\n"+
			"postfix postfix/main_mailer_type string \"Internet Site\"\n", "debconf-set-selections")
		cmd := exec.Command("apt-get", "install", "-y", "--no-install-recommends", "postfix")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		_ = cmd.Run()
	}
	_ = os.MkdirAll("/etc/postfix", 0755)
	for _, f := range []string{"/etc/postfix/relay_domains", "/etc/postfix/transport"} {
		if _, err := os.Stat(f); err != nil {
			_ = os.WriteFile(f, nil, 0644)
		}
		_, _ = run("postmap", f)
	}

	currentNetworks, _ := run("postconf", "-h", "mynetworks")
	base := strings.TrimPrefix(mailHost, "mail.")
	myhost := "mail.wdev.local"
	if base != "" && base != mailHost {
		myhost = "mx." + base
	}
	settings := [][2]string{
		{"mydestination", "localhost.localdomain, localhost"},
		{"relay_domains", "hash:/etc/postfix/relay_domains"},
		{"transport_maps", "hash:/etc/postfix/transport"},
		{"smtp_sasl_auth_enable", "no"},
		{"smtp_tls_security_level", "may"},
		{"smtp_tls_verify_cert", "no"},
		{"mynetworks", mergeMynetworks(currentNetworks, targetIP)},
		{"inet_interfaces", "all"},
		{"maximal_queue_lifetime", "5d"},
		{"bounce_queue_lifetime", "5d"},
	}
	for _, s := range settings {
		_, _ = run("postconf", "-e", s[0]+"="+s[1])
	}
	current, _ := run("postconf", "-h", "myhostname")
	current = strings.TrimSpace(current)
	if current == "" || current == "mail.wdev.local" || !strings.HasPrefix(current, "mx.") {
		_, _ = run("postconf", "-e", "myhostname="+myhost)
		_, _ = run("postconf", "-e", "smtp_helo_name="+myhost)
	}
	_, _ = run("systemctl", "enable", "postfix")
	_, _ = run("systemctl", "restart", "postfix")
}

func configurePostfixTLS(mailHost string) bool {
	cert, key := findLetsEncryptCert(mailHost)
	if cert == "" {
		return false
	}
	for _, s := range [][2]string{
		{"smtpd_tls_cert_file", cert},
		{"smtpd_tls_key_file", key},
		{"smtpd_tls_security_level", "may"},
		{"smtp_tls_security_level", "may"},
	} {
		_, _ = run("postconf", "-e", s[0]+"="+s[1])
	}
	_, _ = run("systemctl", "reload", "postfix")
	return true
}

func postfixStartTLS() bool {
	level, _ := run("postconf", "-h", "smtpd_tls_security_level")
	cert, _ := run("postconf", "-h", "smtpd_tls_cert_file")
	if level != "may" && level != "encrypt" {
		return false
	}
	if cert == "" {
		return false
	}
	_, err := os.Stat(cert)
	return err == nil
}

func queueInfo() (bool, int, string) {
	out, _ := run("mailq")
	empty := strings.Contains(out, "Mail queue is empty")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	count := 0
	if !empty {
		for _, l := range lines {
			if l != "" && !strings.HasPrefix(l, "-") && !strings.HasPrefix(l, " ") {
				count++
			}
		}
	}
	summary := "Empty"
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		summary = strings.TrimSpace(lines[len(lines)-1])
	}
	return empty, count, summary
}

func setupMailDomain(domain, targetIP string, useSSL bool, _ map[string]any) (map[string]any, error) {
	if !validDomain(domain) {
		return nil, fmt.Errorf("domain tidak valid: %s", domain)
	}
	if targetIP == "" || targetIP == "127.0.0.1" {
		return nil, fmt.Errorf("target_ip mesin lokal (NetBird) wajib diisi; VPS hanya gateway")
	}
	fw := ensureMailFirewall()

	adminSub := "mailadmin." + domain
	webmailSub := "webmail." + domain
	mailHost := "mail." + domain

	ensurePostfixInstalled(mailHost, targetIP)
	appendLineOnce("/etc/postfix/relay_domains", domain+" OK")
	replaceTransport(domain, fmt.Sprintf("%s smtp:[%s]:25", domain, targetIP))
	_, _ = run("postmap", "/etc/postfix/relay_domains")
	_, _ = run("postmap", "/etc/postfix/transport")
	_, _ = run("systemctl", "reload", "postfix")

	proxyTo := func(sub, target string, maxBody bool) error {
		name := "wdev_" + sub + ".conf"
		var b strings.Builder
		b.WriteString("# Managed by wdev-edge (native Go)\nserver {\n\tlisten 80;\n")
		b.WriteString(ipv6Listen())
		fmt.Fprintf(&b, "\tserver_name %s;\n", sub)
		if maxBody {
			b.WriteString("\tclient_max_body_size 50M;\n")
		}
		b.WriteString("\tlocation / {\n")
		fmt.Fprintf(&b, "\t\tproxy_pass %s;\n", target)
		b.WriteString("\t\tproxy_set_header Host $host;\n")
		b.WriteString("\t\tproxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("\t\tproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		b.WriteString("\t\tproxy_set_header X-Forwarded-Proto $scheme;\n")
		b.WriteString("\t\tproxy_http_version 1.1;\n")
		b.WriteString("\t\tproxy_set_header Upgrade $http_upgrade;\n")
		b.WriteString("\t\tproxy_set_header Connection \"upgrade\";\n")
		b.WriteString("\t}\n}\n")
		if err := os.WriteFile(filepath.Join(NginxAvailable, name), []byte(b.String()), 0644); err != nil {
			return err
		}
		return enableSite(name)
	}

	if err := proxyTo(adminSub, fmt.Sprintf("http://%s:80", targetIP), false); err != nil {
		return nil, err
	}
	if err := proxyTo(webmailSub, fmt.Sprintf("http://%s:80", targetIP), true); err != nil {
		return nil, err
	}
	// Vhost verifikasi mail.<domain>
	mailConf := filepath.Join(NginxAvailable, "wdev_"+mailHost+".conf")
	mailBlock := fmt.Sprintf("# Managed by wdev-edge (mail host)\nserver {\n\tlisten 80;\n%sserver_name %s;\n\tlocation / { return 200 \"mail server host ok\"; }\n}\n", ipv6Listen(), mailHost)
	if err := os.WriteFile(mailConf, []byte(mailBlock), 0644); err != nil {
		return nil, err
	}
	_ = enableSite("wdev_" + mailHost + ".conf")
	_ = nginxTest()
	_ = nginxReload()

	domains := loadMailDomains()
	found := false
	for _, d := range domains {
		if strings.EqualFold(d, domain) {
			found = true
			break
		}
	}
	if !found {
		domains = append(domains, domain)
		saveMailDomains(domains)
	}

	sslStatus := "none"
	if useSSL {
		// Satu lineage sertifikat (SAN) untuk semua domain mail agar Postfix,
		// nginx stream, dan web memakai cert yang sama.
		args := []string{"--nginx", "--cert-name", "wdev-mail", "--expand",
			"--non-interactive", "--agree-tos", "--register-unsafely-without-email",
			"--redirect", "--deploy-hook", "systemctl reload nginx"}
		for _, d := range domains {
			d = strings.ToLower(strings.TrimSpace(d))
			if !validDomain(d) {
				continue
			}
			args = append(args, "-d", "mailadmin."+d, "-d", "webmail."+d, "-d", "mail."+d)
		}
		if out, err := run("certbot", args...); err == nil {
			sslStatus = "active"
		} else {
			sslStatus = "partial: " + truncate(out, 160)
		}
	}

	smtpStartTLS := configurePostfixTLS(mailHost)
	streamStatus := writeMailStream(targetIP, mailHost)

	listeners := mailListeners()
	allMail := listeners["993"] && listeners["465"] && listeners["587"]
	scheme := "http"
	if sslStatus == "active" {
		scheme = "https"
	}
	return map[string]any{
		"success":              true,
		"domain":               domain,
		"admin_url":            scheme + "://" + adminSub + "/admin/",
		"webmail_url":          scheme + "://" + webmailSub,
		"mail_host":            mailHost,
		"mail_ports_proxy":     streamStatus,
		"mail_ports_listening": allMail,
		"smtp_starttls":        smtpStartTLS,
		"firewall":             fw,
		"listeners":            listeners,
		"ssl":                  sslStatus,
	}, nil
}

func appendLineOnce(path, line string) {
	data, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		_, _ = f.WriteString("\n")
	}
	_, _ = f.WriteString(line + "\n")
}

func replaceTransport(domain, entry string) {
	path := "/etc/postfix/transport"
	data, _ := os.ReadFile(path)
	lines := []string{}
	for _, l := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, domain+" ") {
			continue
		}
		lines = append(lines, t)
	}
	lines = append(lines, entry)
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// ---------------------------------------------------------------------------
// HTTP server
// ---------------------------------------------------------------------------

func audit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := LoadAPIKey()
		if key == "" || r.Header.Get("X-API-Key") != key {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized: Invalid or missing X-API-Key"})
			return
		}
		next(w, r)
	}
}

func readBody(r *http.Request) (map[string]any, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("Invalid JSON body")
	}
	return data, nil
}

// NewAgentHandler membuat handler HTTP agent edge (dipasang di VPS).
func NewAgentHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/ping", audit(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":  "healthy",
			"service": "wdev-edge-agent",
			"nginx":   activeText("nginx"),
			"pdns":    activeText("pdns"),
		})
	}))

	mux.HandleFunc("/api/v1/proxies", audit(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			proxies := []map[string]any{}
			entries, _ := os.ReadDir(NginxAvailable)
			for _, e := range entries {
				name := e.Name()
				if !strings.HasPrefix(name, "wdev_") || !strings.HasSuffix(name, ".conf") {
					continue
				}
				domain := strings.TrimSuffix(strings.TrimPrefix(name, "wdev_"), ".conf")
				_, err := os.Lstat(filepath.Join(NginxEnabled, name))
				proxies = append(proxies, map[string]any{
					"domain":      domain,
					"enabled":     err == nil,
					"config_file": name,
				})
			}
			writeJSON(w, 200, map[string]any{"proxies": proxies})
		case http.MethodPost:
			data, err := readBody(r)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			domain, _ := data["domain"].(string)
			domain = strings.ToLower(strings.TrimSpace(domain))
			targetIP, _ := data["target_ip"].(string)
			useSSL, _ := data["ssl"].(bool)
			ws := true
			if v, ok := data["websocket"].(bool); ok {
				ws = v
			}
			port := 80
			if v, ok := data["target_port"].(float64); ok && v > 0 {
				port = int(v)
			}
			if !validDomain(domain) {
				writeJSON(w, 400, map[string]string{"error": "Invalid domain: " + domain})
				return
			}
			if strings.TrimSpace(targetIP) == "" {
				writeJSON(w, 400, map[string]string{"error": "target_ip is required"})
				return
			}
			sslStatus, err := writeProxy(domain, targetIP, port, useSSL, ws)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "Nginx config test failed", "details": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{
				"success": true,
				"domain":  domain,
				"target":  fmt.Sprintf("http://%s:%d", targetIP, port),
				"ssl":     sslStatus,
			})
		default:
			writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		}
	}))

	mux.HandleFunc("/api/v1/proxies/", audit(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
			return
		}
		domain := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/api/v1/proxies/"))
		if !validDomain(domain) {
			writeJSON(w, 404, map[string]string{"error": "Invalid DELETE path"})
			return
		}
		if !removeProxy(domain) {
			writeJSON(w, 404, map[string]string{"error": "Proxy for " + domain + " not found."})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "message": "Proxy for " + domain + " removed."})
	}))

	mux.HandleFunc("/api/v1/mail/status", audit(func(w http.ResponseWriter, r *http.Request) {
		empty, _, summary := queueInfo()
		mailTLS := false
		if data, err := os.ReadFile(MailStreamConf); err == nil {
			mailTLS = strings.Contains(string(data), "ssl_certificate")
		}
		listeners := mailListeners()
		writeJSON(w, 200, map[string]any{
			"postfix_active":       serviceActive("postfix"),
			"stalwart_active":      serviceActive("stalwart"),
			"mail_ports_proxy":     fileExists(MailStreamConf),
			"mail_ports_tls":       mailTLS,
			"mail_ports_listening": listeners["993"] && listeners["465"] && listeners["995"],
			"smtp_starttls":        postfixStartTLS(),
			"firewall":             firewallStatus(),
			"listeners":            listeners,
			"queue_empty":          empty,
			"queue_summary":        summary,
			"domains":              loadMailDomains(),
			"relays":               loadMailRelays(),
		})
	}))

	mux.HandleFunc("/api/v1/mail/queue", audit(func(w http.ResponseWriter, r *http.Request) {
		empty, count, summary := queueInfo()
		out, _ := run("mailq")
		_ = empty
		_ = summary
		writeJSON(w, 200, map[string]any{"queue_count": count, "output": out})
	}))

	mux.HandleFunc("/api/v1/mail/setup-domain", audit(func(w http.ResponseWriter, r *http.Request) {
		data, err := readBody(r)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		domain := strings.ToLower(strings.TrimSpace(stringOr(data, "domain")))
		targetIP := strings.TrimSpace(stringOr(data, "target_ip"))
		useSSL := true
		if v, ok := data["ssl"].(bool); ok {
			useSSL = v
		}
		var relay map[string]any
		if v, ok := data["relay"].(map[string]any); ok {
			relay = v
		}
		res, err := setupMailDomain(domain, targetIP, useSSL, relay)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	}))

	mux.HandleFunc("/api/v1/mail/remove-domain", audit(func(w http.ResponseWriter, r *http.Request) {
		data, err := readBody(r)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		domain := strings.ToLower(strings.TrimSpace(stringOr(data, "domain")))
		res, err := removeMailDomain(domain)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	}))

	mux.HandleFunc("/api/v1/mail/open-ports", audit(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"success":   true,
			"firewall":  ensureMailFirewall(),
			"listeners": mailListeners(),
		})
	}))

	mux.HandleFunc("/api/v1/mail/set-relay", audit(func(w http.ResponseWriter, r *http.Request) {
		data, err := readBody(r)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		host := strings.TrimSpace(stringOr(data, "host"))
		if host == "" {
			writeJSON(w, 400, map[string]string{"error": "Relay host is required"})
			return
		}
		scope := strings.ToLower(strings.TrimSpace(stringOr(data, "domain")))
		if scope == "" {
			scope = "global"
		}
		port := 587
		if v, ok := data["port"].(float64); ok && v > 0 {
			port = int(v)
		}
		relays := loadMailRelays()
		relays[scope] = map[string]any{
			"host": host,
			"port": port,
			"user": stringOr(data, "user"),
			"pass": stringOr(data, "pass"),
		}
		saveJSON(MailRelaysFile, relays)
		writeJSON(w, 200, map[string]any{
			"success": true,
			"message": fmt.Sprintf("SMTP Relay configured for '%s' (%s:%d)", scope, host, port),
		})
	}))

	mux.HandleFunc("/api/v1/mail/account", audit(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 501, map[string]string{
			"error": "Akun email dikelola di Stalwart pada mesin lokal. Gunakan: wdev mail add-account <email> <password>",
		})
	}))

	mux.HandleFunc("/api/v1/mail/flush-queue", audit(func(w http.ResponseWriter, r *http.Request) {
		_, _ = run("postfix", "flush")
		writeJSON(w, 200, map[string]any{"success": true, "message": "Mail queue flush triggered successfully."})
	}))

	return mux
}

func stringOr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func activeText(unit string) string {
	if serviceActive(unit) {
		return "active"
	}
	return "inactive"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// RunAgent menjalankan HTTP server edge-agent di VPS.
func RunAgent(addr string, cb func(string)) error {
	if cb == nil {
		cb = func(string) {}
	}
	if _, err := os.Stat(ConfigDir); err != nil {
		return fmt.Errorf("direktori %s belum ada; jalankan: wdev edge-agent provision", ConfigDir)
	}
	restoreMailStream()
	if addr == "" {
		addr = "0.0.0.0:" + DefaultAgentPort
	}
	cb("wdev-edge-agent (Go) listening on " + addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           NewAgentHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}
