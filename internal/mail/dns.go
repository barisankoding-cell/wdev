package mail

import (
	"net"
	"strings"

	"wdev/internal/pdns"
)

// normalizeTXTContent menggabungkan chunk TXT PowerDNS ("bagian1" "bagian2")
// menjadi satu string tanpa tanda kutip pembungkus agar DKIM/SPF > 255 byte dapat dibandingkan akurat.
func normalizeTXTContent(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, `"`) {
		return s
	}
	var out strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && (ch == ' ' || ch == '\t') {
			continue
		}
		out.WriteByte(ch)
	}
	return strings.TrimSpace(out.String())
}

// normalizeDNSRecordValue menormalkan nilai DNS berdasarkan tipenya untuk perbandingan idempoten.
func normalizeDNSRecordValue(rtype, raw string) string {
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	switch rtype {
	case "TXT":
		return normalizeTXTContent(raw)
	case "CNAME":
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	case "MX":
		fields := strings.Fields(strings.TrimSpace(strings.Trim(raw, `"`)))
		if len(fields) >= 2 {
			fields[len(fields)-1] = strings.ToLower(strings.TrimSuffix(fields[len(fields)-1], "."))
			return strings.Join(fields, " ")
		}
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	default:
		return strings.TrimSpace(strings.Trim(raw, `"`))
	}
}

func containsSPF(records []string) bool {
	for _, r := range records {
		v := strings.ToLower(normalizeTXTContent(r))
		if strings.HasPrefix(v, "v=spf1") {
			return true
		}
	}
	return false
}

func recordContentExists(records []string, want string) bool {
	return recordContentMatches("", records, want)
}

func recordContentMatches(rtype string, records []string, want string) bool {
	wantNorm := normalizeDNSRecordValue(rtype, want)
	if rtype == "" {
		wantNorm = normalizeTXTContent(want)
	}
	for _, r := range records {
		gotNorm := normalizeDNSRecordValue(rtype, r)
		if rtype == "" {
			gotNorm = normalizeTXTContent(r)
		}
		if strings.EqualFold(rtype, "MX") || strings.EqualFold(rtype, "CNAME") {
			if strings.EqualFold(gotNorm, wantNorm) {
				return true
			}
			continue
		}
		if gotNorm == wantNorm {
			return true
		}
	}
	return false
}

func hostnameMatch(names []string, host string) (string, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, n := range names {
		clean := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
		if clean == host {
			return clean, true
		}
	}
	if len(names) > 0 {
		return strings.TrimSuffix(names[0], "."), false
	}
	return "", false
}

// ptrMatches memeriksa PTR/rDNS IP publik VPS terhadap hostname mail.
func ptrMatches(m *pdns.Manager, ip, host string) (string, bool) {
	if ip == "" {
		return "", false
	}
	names, err := net.LookupAddr(ip)
	if err != nil || len(names) == 0 {
		return "", false
	}
	return hostnameMatch(names, host)
}

// relayDNSPlan mengembalikan checklist DNS yang harus dipasang manual di
// provider relay. wdev tidak bisa membuat record ini karena nilainya hanya
// diketahui setelah domain diverifikasi di dashboard provider.
func relayDNSPlan(provider, domain string) []string {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	switch provider {
	case "brevo":
		return []string{
			"Brevo: pasang CNAME brevo1._domainkey." + domain + " dan brevo2._domainkey." + domain + " dari dashboard Brevo.",
			"Brevo (shared IP): include SPF tidak wajib; DMARC lolos lewat DKIM. Jangan buat dua record SPF.",
			"Brevo: From: harus memakai domain yang diautentikasi (" + domain + ").",
		}
	case "resend":
		return []string{
			"Resend: daftarkan domain pengirim di dashboard, lalu pasang DKIM TXT resend._domainkey dan MX/SPF pada subdomain send sesuai dashboard.",
			"Resend: From: harus memakai domain/subdomain yang diautentikasi.",
			"Resend: jangan tambahkan include:_spf.resend.com ke apex; SPF dikelola di subdomain bounce.",
		}
	case "custom":
		return []string{
			"Custom relay: publish DKIM selector dari penyedia relay.",
			"Custom relay: jika relay mengirim dengan envelope-from domain ini, tambahkan include SPF penyedia ke satu record TXT SPF.",
			"Custom relay: pastikan relay mengizinkan pengiriman untuk " + domain + ".",
		}
	case "", "local", "direct", "mx":
		return []string{
			"SMTP Lokal (Direct MX): pengiriman keluar memakai mesin SMTP Stalwart bawaan (pastikan port 25 outbound terbuka & PTR/rDNS mengarah ke mail." + domain + ").",
			"SMTP Lokal (Direct MX): pastikan record TXT SPF mengizinkan MX/IP server (mis. v=spf1 mx ~all) dan publikasikan DKIM dari panel Stalwart bila diperlukan.",
		}
	default:
		return []string{"SMTP Lokal (Direct MX): pengiriman keluar memakai mesin SMTP Stalwart bawaan."}
	}
}
