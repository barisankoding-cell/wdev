package mail

import (
	"net"
	"strings"

	"wdev/internal/pdns"
)

func containsSPF(records []string) bool {
	for _, r := range records {
		v := strings.ToLower(strings.Trim(r, `"`))
		if strings.HasPrefix(v, "v=spf1") {
			return true
		}
	}
	return false
}

func recordContentExists(records []string, want string) bool {
	want = strings.TrimSpace(strings.Trim(want, `"`))
	for _, r := range records {
		if strings.TrimSpace(strings.Trim(r, `"`)) == want {
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
	default:
		return []string{"Relay belum dikonfigurasi; email keluar tidak akan terkirim."}
	}
}
