package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"wdev/internal/pdns"
)

const (
	actionOK    = "ok"
	actionWarn  = "warn"
	actionFail  = "fail"
	actionFixed = "fixed"
)

// DomainCheck adalah satu langkah hasil perbaikan/penghapusan domain mail.
type DomainCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// DomainActionReport adalah ringkasan aksi perbaikan atau penghapusan domain.
type DomainActionReport struct {
	Domain string        `json:"domain"`
	Zone   string        `json:"zone,omitempty"`
	OK     bool          `json:"ok"`
	Checks []DomainCheck `json:"checks"`
}

func (r *DomainActionReport) add(name, status, detail string) {
	r.Checks = append(r.Checks, DomainCheck{Name: name, Status: status, Detail: detail})
	if status == actionFail {
		r.OK = false
	}
}

func (r *DomainActionReport) hasFail() bool { return !r.OK }

// cleanMailDomain menormalkan nama domain (lowercase, tanpa trailing dot).
func cleanMailDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

// recordNameForZone mengubah FQDN menjadi nama record relatif terhadap zone.
func recordNameForZone(zone, fqdn string) string {
	zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	if fqdn == "" || fqdn == zone {
		return "@"
	}
	if strings.HasSuffix(fqdn, "."+zone) {
		return strings.TrimSuffix(fqdn, "."+zone)
	}
	return fqdn
}

func recordSetEqual(have []string, want string) bool {
	if len(have) == 0 {
		return false
	}
	want = strings.TrimSpace(strings.TrimSuffix(strings.Trim(want, `"`), "."))
	for _, h := range have {
		got := strings.TrimSpace(strings.TrimSuffix(strings.Trim(h, `"`), "."))
		if !strings.EqualFold(got, want) {
			return false
		}
	}
	return true
}

// removeAdminCred menghapus entri kredensial admin satu domain.
func (m *Manager) removeAdminCred(domain string) error {
	domain = cleanMailDomain(domain)
	data, err := readRootFile(m.adminCredPath())
	if err != nil {
		return nil
	}
	credMap := make(map[string]adminCredEntry)
	if err := json.Unmarshal(data, &credMap); err != nil {
		return err
	}
	if _, ok := credMap[domain]; !ok {
		return nil
	}
	delete(credMap, domain)
	out, err := json.MarshalIndent(credMap, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.adminCredPath(), out, 0600)
}

// removeDomainState menghapus catatan state setup satu domain.
func (m *Manager) removeDomainState(domain string) error {
	domain = cleanMailDomain(domain)
	all := m.loadDomainsState()
	if _, ok := all[domain]; !ok {
		return nil
	}
	delete(all, domain)
	data, err := json.MarshalIndent(all, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.domainsStatePath(), data, 0600)
}

func (m *Manager) adminCredPath() string {
	return filepath.Join(m.cfg.WdevEtc, "mail-admin.cred")
}

func (m *Manager) listAccountsForDomain(adminEmail, adminPass, domainID string) []string {
	out, err := m.runStalwartCli(adminEmail, adminPass, "query", "Account", "--where", "domainId="+domainID, "--fields", "id", "--json")
	if err != nil {
		return nil
	}
	var ids []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &item); err == nil && item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

// deleteMailDNS menghapus record DNS khusus mail milik satu domain.
func (m *Manager) deleteMailDNS(zone, domain string, rep *DomainActionReport) {
	privateNames := []string{
		"mail." + domain,
		"mx." + domain,
		"webmail." + domain,
		"mailadmin." + domain,
	}
	removed := 0
	for _, fqdn := range privateNames {
		name := recordNameForZone(zone, fqdn)
		if err := m.pdnsMgr.DeleteRecord(zone, name, "A"); err != nil {
			rep.add("DNS A "+name, actionWarn, err.Error())
			continue
		}
		removed++
	}

	// MX hanya dihapus bila seluruh isinya memang menunjuk ke mail.<domain>.
	if mx, err := m.pdnsMgr.LookupRecords(zone, "@", "MX"); err == nil && len(mx) > 0 {
		allOurs := true
		for _, content := range mx {
			c := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(content), "."))
			if !strings.HasSuffix(c, "mail."+domain) {
				allOurs = false
				break
			}
		}
		if allOurs {
			if err := m.pdnsMgr.DeleteRecord(zone, "@", "MX"); err == nil {
				removed++
			}
		}
	}

	// DMARC hanya dihapus jika memang merujuk domain ini.
	if txt, err := m.pdnsMgr.LookupRecords(zone, "_dmarc", "TXT"); err == nil {
		for _, content := range txt {
			c := strings.ToLower(content)
			if strings.Contains(c, "dmarc@"+domain) || strings.Contains(c, "mailto:dmarc@"+domain) {
				if err := m.pdnsMgr.DeleteRecord(zone, "_dmarc", "TXT"); err == nil {
					removed++
				}
				break
			}
		}
	}

	// SPF hanya dihapus bila TXT apex tunggal dan persis kebijakan bawaan wdev
	// (v=spf1 -all atau v=spf1 mx ~all); TXT lain seperti verifikasi domain tidak boleh ikut hilang.
	if txt, err := m.pdnsMgr.LookupRecords(zone, "@", "TXT"); err == nil && len(txt) == 1 {
		val := strings.Trim(strings.TrimSpace(txt[0]), `"`)
		if strings.EqualFold(val, "v=spf1 -all") || strings.EqualFold(val, "v=spf1 mx ~all") || strings.EqualFold(val, "v=spf1 a mx ~all") {
			if err := m.pdnsMgr.DeleteRecord(zone, "@", "TXT"); err == nil {
				removed++
			}
		}
	}

	rep.add("DNS mail", actionOK, fmt.Sprintf("%d record mail dihapus dari zone %s (record SPF kustom lain dipertahankan)", removed, zone))
}

// DeleteDomain menghapus domain mail dari Stalwart, Edge Gateway, dan state lokal.
// Record DNS hanya dihapus bila deleteDNS=true.
func (m *Manager) DeleteDomain(domain string, deleteDNS bool) (DomainActionReport, error) {
	domain = cleanMailDomain(domain)
	rep := DomainActionReport{Domain: domain, OK: true}
	if domain == "" {
		return rep, fmt.Errorf("domain wajib diisi")
	}
	if !validMailDomain(domain) {
		return rep, fmt.Errorf("domain tidak valid: %s", domain)
	}

	adminEmail, adminPass, credErr := m.pickValidAdminCred(domain)
	if credErr == nil {
		// Relay dibersihkan lebih dulu: akun admin domain ini ikut terhapus
		// bersama domain Stalwart, sehingga kredensial tidak valid setelahnya.
		_, hasCred := m.loadDomainRelayCred(domain)
		hasBranch := false
		if route, err := m.loadOutboundRoute(adminEmail, adminPass); err == nil {
			hasBranch = route.hasDomain(domain)
		}
		if !hasCred && !hasBranch {
			rep.add("Relay outbound", actionOK, "tidak ada override relay domain")
		} else if err := m.removeDomainRelay(adminEmail, adminPass, domain); err != nil {
			rep.add("Relay outbound", actionWarn, err.Error())
		} else {
			rep.add("Relay outbound", actionOK, "override relay domain dihapus (fallback ke relay default)")
		}

		// Bila akun admin yang dipakai berasal dari domain ini dan masih ada
		// domain lain, siapkan admin pengganti agar akses Stalwart tidak hilang.
		if strings.HasSuffix(strings.ToLower(adminEmail), "@"+domain) {
			other := ""
			for _, d := range m.stalwartDomainNames() {
				if d = cleanMailDomain(d); d != "" && d != domain {
					other = d
					break
				}
			}
			if other != "" {
				if _, _, err := m.resetAdminInternal(other); err != nil {
					rep.add("Admin Stalwart", actionWarn, "admin pengganti untuk "+other+" gagal disiapkan: "+err.Error())
				} else {
					rep.add("Admin Stalwart", actionOK, "admin pengganti disiapkan di "+other)
				}
			}
		}

		domainID, _ := m.findDomainID(adminEmail, adminPass, domain)
		if domainID == "" {
			rep.add("Domain Stalwart", actionWarn, "domain tidak ditemukan di Stalwart")
		} else {
			accIDs := m.listAccountsForDomain(adminEmail, adminPass, domainID)
			deleted := 0
			for _, id := range accIDs {
				if out, err := m.runStalwartCli(adminEmail, adminPass, "delete", "--ids", id, "Account"); err != nil {
					rep.add("Akun Stalwart", actionWarn, fmt.Sprintf("gagal hapus akun %s: %v (%s)", id, err, strings.TrimSpace(string(out))))
					continue
				}
				deleted++
			}
			rep.add("Akun Stalwart", actionOK, fmt.Sprintf("%d akun dihapus", deleted))
			if out, err := m.runStalwartCli(adminEmail, adminPass, "delete", "--ids", domainID, "Domain"); err != nil {
				rep.add("Domain Stalwart", actionFail, fmt.Sprintf("%v (%s)", err, strings.TrimSpace(string(out))))
			} else {
				rep.add("Domain Stalwart", actionOK, "domain "+domain+" dihapus dari Stalwart")
			}
		}

	} else {
		rep.add("Kredensial Stalwart", actionWarn, "kredensial admin tidak valid; domain Stalwart tidak dapat dihapus: "+credErr.Error())
	}

	if err := m.removeDomainRelayCred(domain); err != nil {
		rep.add("Kredensial relay", actionWarn, err.Error())
	}
	if err := m.removeAdminCred(domain); err != nil {
		rep.add("Kredensial admin", actionWarn, err.Error())
	}
	if err := m.removeDomainState(domain); err != nil {
		rep.add("State domain", actionWarn, err.Error())
	}
	rep.add("State lokal", actionOK, "kredensial admin, relay, dan state domain dibersihkan")

	mode := m.pdnsMgr.GetMode()
	// Bersihkan artefak lokal (/etc/hosts, vhost lokal, cert SSL lokal) di semua mode agar
	// domain yang sebelumnya dibuat di mode lain tidak meninggalkan sisa entri /etc/hosts atau vhost.
	m.removeMailHostsEntries(domain)
	sslDir := m.mailSSLDir(domain)
	if err := os.RemoveAll(sslDir); err != nil {
		_ = runRoot("rm", "-rf", sslDir)
	}
	available, enabled := m.nginxMailPaths(domain)
	if err := os.Remove(enabled); err != nil && !os.IsNotExist(err) {
		_ = runRoot("rm", "-f", enabled)
	}
	if err := os.Remove(available); err != nil && !os.IsNotExist(err) {
		_ = runRoot("rm", "-f", available)
	}
	_ = reloadLocalNginx()

	if mode == "edge" {
		if _, err := m.pdnsMgr.AgentPost("/api/v1/mail/remove-domain", map[string]any{"domain": domain}); err != nil {
			rep.add("VPS Edge Gateway", actionWarn, "gagal membersihkan vhost/stream/proxy di VPS: "+err.Error())
		} else {
			rep.add("VPS Edge Gateway", actionOK, "vhost webmail/admin, postfix relay, dan stream mail dibersihkan di VPS")
		}
	} else {
		rep.add("Vhost Nginx Lokal", actionOK, "vhost webmail/admin lokal & entri akses dibersihkan (mode "+mode+")")
	}

	if deleteDNS {
		zone, err := m.pdnsMgr.FindMatchingZone(domain)
		if err != nil {
			if mode == "local" {
				rep.add("DNS", actionOK, "PowerDNS / zone tidak terkonfigurasi; dilewati")
			} else {
				rep.add("DNS", actionWarn, err.Error())
			}
		} else {
			rep.Zone = zone
			m.deleteMailDNS(zone, domain, &rep)
		}
	} else {
		rep.add("DNS", actionOK, "record DNS dipertahankan (hapus manual dari tab PowerDNS bila perlu)")
	}

	_ = restartStalwart()
	return rep, nil
}

// RepairDomain memperbaiki NS/SOA zone dan memastikan record MX, A, SPF, DMARC,
// edge gateway, serta routing relay untuk satu domain mail sudah tepat.
func (m *Manager) RepairDomain(domain string, cb func(string)) (DomainActionReport, error) {
	if cb == nil {
		cb = func(string) {}
	}
	domain = cleanMailDomain(domain)
	rep := DomainActionReport{Domain: domain, OK: true}
	if domain == "" {
		return rep, fmt.Errorf("domain wajib diisi")
	}
	if !validMailDomain(domain) {
		return rep, fmt.Errorf("domain tidak valid: %s", domain)
	}

	mode := m.pdnsMgr.GetMode()
	zone, err := m.pdnsMgr.FindMatchingZone(domain)
	if err != nil && strings.Contains(err.Error(), "tidak ditemukan") {
		pdnsCfg := m.pdnsMgr.LoadConfig()
		if pdnsCfg.Configured && pdnsCfg.HasAPIKey {
			if addErr := m.pdnsMgr.AddZone(domain); addErr == nil {
				cb(fmt.Sprintf("✓ Zone DNS otomatis dibuat di PowerDNS: %s", domain))
				rep.add("Zone DNS", actionFixed, "zone "+domain+" otomatis dibuat di PowerDNS")
				zone, err = m.pdnsMgr.FindMatchingZone(domain)
			} else {
				cb(fmt.Sprintf("! Gagal membuat zone DNS otomatis (%s): %v", domain, addErr))
			}
		}
	}
	if err != nil {
		if mode != "local" {
			rep.add("Zone DNS", actionFail, err.Error())
			return rep, err
		}
		rep.add("Zone DNS", actionWarn, "zone DNS belum tersedia di PowerDNS; perbaikan lokal dilanjutkan via /etc/hosts")
		zone = ""
	} else {
		rep.Zone = zone
		cb(fmt.Sprintf("Zone DNS: %s", zone))

		// 1. Perbaiki NS/SOA/glue di level zone (idempoten).
		zoneReport, zoneErr := m.pdnsMgr.RepairZone(zone)
		if zoneErr != nil {
			rep.add("NS & SOA zone", actionFail, zoneErr.Error())
		} else {
			for _, c := range zoneReport.Checks {
				if strings.Contains(strings.ToLower(c.Name), "ns") || strings.Contains(strings.ToLower(c.Name), "soa") {
					status := actionOK
					if c.Status != "ok" {
						status = actionWarn
					}
					rep.add("NS & SOA zone · "+c.Name, status, c.Detail)
				}
			}
			rep.add("NS & SOA zone", actionOK, "NS, SOA primary/serial, dan glue A diperiksa ulang")
		}
	}

	// 2. Pastikan Mail Stack (Stalwart + SnappyMail) terpasang, service aktif, dan bootstrap siap.
	stackReady := true
	if stackErr := m.ensureMailStackInstalled(context.Background(), cb, domain); stackErr != nil {
		rep.add("Mail Stack", actionFail, stackErr.Error())
		stackReady = false
	} else if localErr := m.ensureLocalMailStack(); localErr != nil {
		rep.add("Service Stalwart", actionFail, localErr.Error())
		stackReady = false
	} else if bsErr := m.ensureStalwartBootstrap(domain); bsErr != nil {
		rep.add("Bootstrap Stalwart", actionFail, bsErr.Error())
		stackReady = false
	} else {
		rep.add("Service Stalwart", actionOK, "engine Stalwart & SnappyMail siap")
	}

	// 3. Pastikan domain & admin Stalwart siap.
	var adminUser, adminPass string
	if stackReady {
		var admErr error
		adminUser, adminPass, admErr = m.ensureStalwartMailDomain(domain)
		if admErr != nil {
			rep.add("Domain Stalwart", actionFail, admErr.Error())
		} else {
			rep.add("Domain Stalwart", actionOK, "domain & akun admin siap ("+adminUser+")")
		}
	}
	_ = m.writeSnappyMailDomainConfig(domain)

	// 4. Sinkronkan record DNS inbound/outbound bila zone tersedia.
	if zone != "" {
		m.syncMailDNS(zone, domain, &rep, cb)
	}

	// 4. Segarkan gateway / vhost sesuai mode server.
	switch mode {
	case "edge":
		m.removeMailHostsEntries(domain)
		if localNB := m.pdnsMgr.GetLocalNetbirdIP(); localNB == "" {
			rep.add("Edge Gateway", actionWarn, "IP NetBird lokal (wt0) tidak terdeteksi; vhost VPS tidak bisa disegarkan")
		} else {
			ssl := true
			if st, ok := m.loadDomainsState()[domain]; ok && !st.SSL {
				ssl = false
			}
			payload := map[string]any{"domain": domain, "target_ip": localNB, "ssl": ssl}
			resp, agentErr := m.pdnsMgr.AgentPost("/api/v1/mail/setup-domain", payload)
			if agentErr != nil {
				rep.add("Edge Gateway", actionWarn, "gagal menyegarkan vhost/proxy VPS: "+agentErr.Error())
			} else {
				var respMap map[string]any
				_ = json.Unmarshal(resp, &respMap)
				adminURL, _ := respMap["admin_url"].(string)
				webmailURL, _ := respMap["webmail_url"].(string)
				m.recordDomainState(domain, ssl, adminURL, webmailURL)
				rep.add("Edge Gateway", actionOK, "vhost webmail/admin, postfix relay, dan stream mail disegarkan")
			}
		}
	default: // "vps" atau "local"
		wantSSL := mode == "vps"
		if st, ok := m.loadDomainsState()[domain]; ok {
			wantSSL = st.SSL
		}
		_ = m.ensureLocalMailFirewall()
		_ = m.ensureLocalMailVhost(domain)
		_ = m.ensureWebmailFallbackVhost()
		_ = reloadLocalNginx()
		sslActive := false
		if wantSSL {
			if mode == "local" {
				if ok, sslErr := m.issueLocalMailSSLVhost(domain); ok {
					sslActive = true
				} else if sslErr != nil {
					rep.add("SSL Lokal", actionWarn, sslErr.Error())
				}
			} else {
				sslActive = m.issueVPSLetsEncrypt(domain, cb)
			}
		}
		if mode == "local" {
			m.ensureMailHostsEntries(domain, "127.0.0.1")
		} else {
			m.removeMailHostsEntries(domain)
		}
		scheme := "http"
		if sslActive {
			scheme = "https"
		}
		adminURL := fmt.Sprintf("%s://mailadmin.%s", scheme, domain)
		webmailURL := fmt.Sprintf("%s://webmail.%s", scheme, domain)
		m.recordDomainState(domain, sslActive, adminURL, webmailURL)
		rep.add("Vhost Nginx Lokal", actionOK, "vhost webmail/admin lokal & entri akses disegarkan (mode "+mode+")")
	}

	// 5. Pastikan routing relay outbound konsisten.
	m.repairRelayRouting(domain, adminUser, adminPass, &rep)

	// 6. Sinkronkan & pasang ulang record DNS relay aktif (termasuk DKIM/SPF/MX dari Resend API).
	if cred, ok := m.effectiveRelayCred(domain); ok {
		prov := strings.ToLower(strings.TrimSpace(cred.Provider))
		if prov == "resend" && strings.TrimSpace(cred.Password) != "" {
			syncRes, syncErr := m.SyncResendDomainDNS(domain, cred.Password, true)
			for _, rem := range syncRes.Report.Removed {
				rep.add("DNS Resend", actionFixed, "hapus record lama: "+rem)
				cb("✓ Record DNS Resend lama dibersihkan: " + rem)
			}
			for _, a := range syncRes.Report.Added {
				rep.add("DNS Resend", actionFixed, a+" (dibuat)")
				cb("✓ Record DNS Resend dibuat: " + a)
			}
			if syncRes.VerificationTriggered {
				rep.add("Verifikasi Resend", actionOK, "permintaan verifikasi DNS dikirim ke Resend")
				cb("✓ Permintaan verifikasi domain Resend dikirim")
			}
			if syncErr != nil {
				rep.add("DNS Resend", actionWarn, syncErr.Error())
				cb("! Sinkronisasi Resend API: " + syncErr.Error())
				// Fallback pasang ulang record terkelola lokal bila sudah pernah tersimpan.
				dnsRep := m.EnsureManagedRelayDNS(domain, prov)
				for _, a := range dnsRep.Added {
					rep.add("DNS relay", actionFixed, a)
					cb("✓ Record DNS relay dibuat ulang: " + a)
				}
			} else if len(syncRes.Report.Added) == 0 && len(syncRes.Report.Removed) == 0 {
				if len(syncRes.Report.Kept) > 0 {
					rep.add("DNS Resend", actionOK, fmt.Sprintf("%d record Resend sudah lengkap di PowerDNS", len(syncRes.Report.Kept)))
				}
			}
			for _, warn := range syncRes.Report.Manual {
				rep.add("DNS Resend", actionWarn, warn)
			}
		} else {
			dnsRep := m.EnsureManagedRelayDNS(domain, prov)
			for _, a := range dnsRep.Added {
				rep.add("DNS relay", actionFixed, a)
				cb("✓ Record DNS relay dibuat ulang: " + a)
			}
			for _, warn := range dnsRep.Manual {
				rep.add("DNS relay", actionWarn, warn)
			}
			if len(dnsRep.Added) == 0 && len(dnsRep.Manual) == 0 {
				rep.add("DNS relay", actionOK, "record relay terkelola sudah sesuai")
			}
		}
	}

	if rep.hasFail() {
		cb("Perbaikan selesai dengan beberapa kegagalan; lihat detail.")
	} else {
		cb("Perbaikan domain selesai.")
	}
	return rep, nil
}

// syncMailDNS menegakkan record A, MX, SPF, dan DMARC yang benar untuk domain.
func (m *Manager) syncMailDNS(zone, domain string, rep *DomainActionReport, cb func(string)) {
	conf := m.pdnsMgr.LoadConfig()
	publicIP := strings.TrimSpace(conf.VPSPublicIP)
	if conf.Mode == "vps" && !pdns.IsPublicIPv4String(publicIP) {
		if pub := pdns.DetectPublicIP(); pdns.IsPublicIPv4String(pub) {
			publicIP = pub
		}
	}
	if publicIP == "" && conf.Mode == "local" {
		if lan := pdns.DetectLocalLANIP(); lan != "" {
			publicIP = lan
		} else {
			publicIP = "127.0.0.1"
		}
	}
	if publicIP == "" {
		rep.add("Record A", actionFail, "IP server belum tercatat; atur VPS_PUBLIC_IP atau jalankan 'wdev pdns connect'")
		return
	}

	hosts := []string{"mail", "mx", "webmail", "mailadmin"}
	for _, host := range hosts {
		fqdn := host + "." + domain
		name := recordNameForZone(zone, fqdn)
		existing, err := m.pdnsMgr.LookupRecords(zone, name, "A")
		if err != nil {
			rep.add("A "+fqdn, actionFail, err.Error())
			continue
		}
		if len(existing) == 1 && strings.TrimSpace(existing[0]) == publicIP {
			rep.add("A "+fqdn, actionOK, publicIP)
			continue
		}
		if err := m.pdnsMgr.AddRecord(zone, name, "A", publicIP, 300); err != nil {
			rep.add("A "+fqdn, actionFail, err.Error())
			continue
		}
		if len(existing) == 0 {
			rep.add("A "+fqdn, actionFixed, publicIP+" (dibuat)")
			cb(fmt.Sprintf("✓ Record A dibuat: %s -> %s", fqdn, publicIP))
		} else {
			rep.add("A "+fqdn, actionFixed, publicIP+" (mengganti "+strings.Join(existing, ", ")+")")
			cb(fmt.Sprintf("Perbaiki A %s: %s -> %s", fqdn, strings.Join(existing, ", "), publicIP))
		}
	}

	// MX harus menunjuk mail.<domain>.
	wantMX := "10 mail." + domain + "."
	mx, err := m.pdnsMgr.LookupRecords(zone, "@", "MX")
	if err != nil {
		rep.add("MX", actionFail, err.Error())
	} else if recordSetEqual(mx, wantMX) {
		rep.add("MX "+domain, actionOK, wantMX)
	} else {
		if err := m.pdnsMgr.AddRecord(zone, "@", "MX", wantMX, 300); err != nil {
			rep.add("MX "+domain, actionFail, err.Error())
		} else if len(mx) == 0 {
			rep.add("MX "+domain, actionFixed, wantMX+" (dibuat)")
			cb(fmt.Sprintf("✓ Record MX dibuat: %s -> %s", domain, wantMX))
		} else {
			rep.add("MX "+domain, actionFixed, wantMX+" (mengganti "+strings.Join(mx, ", ")+")")
			cb(fmt.Sprintf("Perbaiki MX %s: %s -> %s", domain, strings.Join(mx, ", "), wantMX))
		}
	}

	// SPF: tambah hanya jika belum ada agar record custom tidak tertimpa.
	spf, err := m.pdnsMgr.LookupRecords(zone, "@", "TXT")
	if err != nil {
		rep.add("SPF", actionWarn, err.Error())
	} else if containsSPF(spf) {
		rep.add("SPF", actionOK, "record SPF sudah ada; nilai custom dipertahankan")
	} else {
		prov := m.ActiveRelayProvider(domain)
		wantSPF := `"v=spf1 mx ~all"`
		if prov != "" && prov != "local" && prov != "direct" && prov != "mx" {
			wantSPF = `"v=spf1 -all"`
		}
		if err := m.pdnsMgr.AddRecordKeep(zone, "@", "TXT", wantSPF, 300); err != nil {
			rep.add("SPF", actionFail, err.Error())
		} else {
			rep.add("SPF", actionFixed, wantSPF+" (dibuat)")
			cb(fmt.Sprintf("✓ Record TXT SPF dibuat: %s -> %s", domain, wantSPF))
		}
	}

	// DMARC: tambah hanya jika belum ada.
	dmarc, err := m.pdnsMgr.LookupRecords(zone, "_dmarc", "TXT")
	if err != nil {
		rep.add("DMARC", actionWarn, err.Error())
	} else if len(dmarc) > 0 {
		rep.add("DMARC", actionOK, "record _dmarc sudah ada")
	} else {
		value := fmt.Sprintf(`"v=DMARC1; p=none; sp=none; rua=mailto:dmarc@%s"`, domain)
		if err := m.pdnsMgr.AddRecordKeep(zone, "_dmarc", "TXT", value, 300); err != nil {
			rep.add("DMARC", actionFail, err.Error())
		} else {
			rep.add("DMARC", actionFixed, value+" (dibuat)")
			cb(fmt.Sprintf("✓ Record TXT DMARC dibuat: _dmarc.%s", domain))
		}
	}
}

// repairRelayRouting memastikan MtaRoute + cabang outbound untuk domain konsisten
// dengan kredensial relay yang tersimpan.
func (m *Manager) repairRelayRouting(domain, adminEmail, adminPass string, rep *DomainActionReport) {
	if adminEmail == "" || adminPass == "" {
		var err error
		adminEmail, adminPass, err = m.pickValidAdminCred(domain)
		if err != nil {
			rep.add("Relay outbound", actionWarn, "kredensial admin tidak valid; routing relay tidak diperiksa")
			return
		}
	}

	route, err := m.loadOutboundRoute(adminEmail, adminPass)
	if err != nil {
		rep.add("Relay outbound", actionWarn, err.Error())
		return
	}

	hasBranch := route.hasDomain(domain)
	cred, hasCred := m.loadDomainRelayCred(domain)
	if hasCred {
		if !hasBranch || routeBranchTarget(route, domain) != cred.Route {
			cred.Route = relayRouteName(domain)
			if err := m.applyRelay(adminEmail, adminPass, domain, cred.Provider, cred.Host, cred.Port, cred.ImplicitTLS, cred.Username, cred.Password); err != nil {
				rep.add("Relay outbound", actionFail, "gagal menerapkan ulang relay domain: "+err.Error())
				return
			}
			rep.add("Relay outbound", actionFixed, "routing relay domain dipasang ulang ("+cred.Provider+" "+cred.Host+")")
		} else {
			rep.add("Relay outbound", actionOK, "routing relay domain sudah tepat ("+cred.Provider+" "+cred.Host+")")
		}
		return
	}

	if hasBranch {
		target := routeBranchTarget(route, domain)
		if target == LocalSMTPRoute {
			rep.add("Relay outbound", actionOK, "domain memakai SMTP lokal Stalwart (direct MX)")
			return
		}
		if target != "" {
			if existing, _ := m.findMtaRoute(adminEmail, adminPass, target); existing != nil {
				rep.add("Relay outbound", actionOK, "domain memakai route "+target+" yang valid")
				return
			}
		}
		route.removeDomain(domain)
		if route.Else == "" || route.Else == LocalSMTPRoute {
			if _, err := m.ensureDefaultRelay(adminEmail, adminPass); err != nil {
				route.Else = LocalSMTPRoute
			}
		}
		if err := m.saveOutboundRoute(adminEmail, adminPass, route); err != nil {
			rep.add("Relay outbound", actionFail, err.Error())
			return
		}
		rep.add("Relay outbound", actionFixed, "cabang relay domain yang menunjuk route tidak valid dibersihkan")
		return
	}

	if _, ok := m.loadGlobalRelayCred(); ok {
		if _, err := m.ensureDefaultRelay(adminEmail, adminPass); err != nil {
			rep.add("Relay outbound", actionWarn, err.Error())
			return
		}
		rep.add("Relay outbound", actionOK, "domain memakai relay default; routing default valid")
		return
	}
	if route.Else != LocalSMTPRoute {
		route.Else = LocalSMTPRoute
		if err := m.saveOutboundRoute(adminEmail, adminPass, route); err != nil {
			rep.add("Relay outbound", actionWarn, err.Error())
			return
		}
		rep.add("Relay outbound", actionFixed, "routing outbound default diarahkan ke SMTP lokal Stalwart (direct MX)")
		return
	}
	rep.add("Relay outbound", actionOK, "domain memakai SMTP lokal Stalwart (direct MX)")
}

func routeBranchTarget(route *outboundRoute, domain string) string {
	want := normalizeExpr(relayMatchCondition(domain))
	for _, b := range route.Branches {
		if normalizeExpr(b.If) == want {
			return strings.Trim(strings.TrimSpace(b.Then), `'"`)
		}
	}
	return ""
}
