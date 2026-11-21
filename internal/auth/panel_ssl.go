package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	SettingPanelSSL = "panel_ssl_enabled"
)

// IsPanelSSLEnabled mengembalikan true bila Direct HTTPS (Panel SSL) aktif di konfigurasi CLI atau DB.
func (m *Manager) IsPanelSSLEnabled() bool {
	if m == nil {
		return false
	}
	if m.cfg != nil && strings.TrimSpace(m.cfg.TLSCert) != "" && strings.TrimSpace(m.cfg.TLSKey) != "" {
		return true
	}
	if m.store == nil {
		return false
	}
	val, _ := m.store.GetSetting(SettingPanelSSL)
	return val == "1" || strings.EqualFold(val, "true")
}

// SetPanelSSLEnabled mengaktifkan atau menonaktifkan Direct HTTPS (Panel SSL) pada port panel.
func (m *Manager) SetPanelSSLEnabled(enabled bool) error {
	if m == nil || m.store == nil {
		return fmt.Errorf("penyimpanan tidak tersedia")
	}
	val := "0"
	if enabled {
		val = "1"
	}
	return m.store.SetSetting(SettingPanelSSL, val)
}

// PanelSSLPaths mengembalikan path berkas sertifikat & private key HTTPS milik panel.
func (m *Manager) PanelSSLPaths() (certPath, keyPath string) {
	if m != nil && m.cfg != nil {
		if strings.TrimSpace(m.cfg.TLSCert) != "" && strings.TrimSpace(m.cfg.TLSKey) != "" {
			return strings.TrimSpace(m.cfg.TLSCert), strings.TrimSpace(m.cfg.TLSKey)
		}
		if strings.TrimSpace(m.cfg.WdevEtc) != "" {
			base := strings.TrimSpace(m.cfg.WdevEtc)
			return filepath.Join(base, "panel-ssl.crt"), filepath.Join(base, "panel-ssl.key")
		}
	}
	return "/etc/wdev/panel-ssl.crt", "/etc/wdev/panel-ssl.key"
}

// EnsurePanelSelfSignedCert memastikan sertifikat self-signed ECDSA + SAN IP/DNS tersedia dan valid.
func (m *Manager) EnsurePanelSelfSignedCert(extraHosts ...string) (certPath, keyPath string, err error) {
	certPath, keyPath = m.PanelSSLPaths()
	if certPath == "" || keyPath == "" {
		return "", "", fmt.Errorf("path sertifikat panel tidak valid")
	}

	// Jika memakai sertifikat kustom (--tls-cert/--tls-key), cukup verifikasi dapat dimuat.
	if m != nil && m.cfg != nil && m.cfg.TLSCert != "" && m.cfg.TLSKey != "" {
		if _, loadErr := tls.LoadX509KeyPair(certPath, keyPath); loadErr != nil {
			return "", "", fmt.Errorf("gagal memuat sertifikat TLS kustom: %w", loadErr)
		}
		return certPath, keyPath, nil
	}

	// Kumpulkan seluruh SAN IP & DNS agar sertifikat cocok untuk localhost, IP LAN, NetBird, dan IP Publik.
	ipSet := map[string]net.IP{
		"127.0.0.1": net.ParseIP("127.0.0.1"),
		"::1":       net.ParseIP("::1"),
	}
	dnsSet := map[string]bool{
		"localhost": true,
	}
	if hn, herr := os.Hostname(); herr == nil && strings.TrimSpace(hn) != "" {
		dnsSet[strings.TrimSpace(hn)] = true
	}
	if addrs, aerr := net.InterfaceAddrs(); aerr == nil {
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLinkLocalUnicast() && !ip.IsMulticast() && !ip.IsUnspecified() {
				ipSet[ip.String()] = ip
			}
		}
	}
	for _, h := range extraHosts {
		clean := strings.TrimSpace(strings.TrimSuffix(h, "."))
		if clean == "" || clean == "-" {
			continue
		}
		if hostOnly, _, splitErr := net.SplitHostPort(clean); splitErr == nil {
			clean = hostOnly
		}
		if ip := net.ParseIP(clean); ip != nil {
			ipSet[ip.String()] = ip
		} else {
			dnsSet[clean] = true
		}
	}

	// Pakai ulang sertifikat yang sudah ada jika masih valid >30 hari dan sudah memuat host tambahan.
	if existingCert, loadErr := tls.LoadX509KeyPair(certPath, keyPath); loadErr == nil && len(existingCert.Certificate) > 0 {
		if leaf, parseErr := x509.ParseCertificate(existingCert.Certificate[0]); parseErr == nil {
			if time.Until(leaf.NotAfter) > 30*24*time.Hour && certCoversHosts(leaf, ipSet, dnsSet) {
				return certPath, keyPath, nil
			}
		}
	}

	if mkErr := os.MkdirAll(filepath.Dir(certPath), 0755); mkErr != nil {
		return "", "", fmt.Errorf("gagal menyiapkan direktori sertifikat %s: %w", filepath.Dir(certPath), mkErr)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("gagal membuat kunci ECDSA: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", fmt.Errorf("gagal membuat nomor seri sertifikat: %w", err)
	}

	var ips []net.IP
	for _, ip := range ipSet {
		ips = append(ips, ip)
	}
	var dnsNames []string
	for d := range dnsSet {
		dnsNames = append(dnsNames, d)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "wdev-control-plane",
			Organization: []string{"wdev Control Plane"},
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           ips,
		DNSNames:              dnsNames,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("gagal membuat sertifikat X.509: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("gagal menyandikan private key ECDSA: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return "", "", fmt.Errorf("gagal menulis private key %s: %w", keyPath, err)
	}
	_ = os.Chmod(keyPath, 0600)

	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return "", "", fmt.Errorf("gagal menulis sertifikat %s: %w", certPath, err)
	}

	return certPath, keyPath, nil
}

func certCoversHosts(leaf *x509.Certificate, wantIPs map[string]net.IP, wantDNS map[string]bool) bool {
	haveIPs := make(map[string]bool, len(leaf.IPAddresses))
	for _, ip := range leaf.IPAddresses {
		haveIPs[ip.String()] = true
	}
	for k := range wantIPs {
		if !haveIPs[k] {
			return false
		}
	}
	haveDNS := make(map[string]bool, len(leaf.DNSNames))
	for _, d := range leaf.DNSNames {
		haveDNS[d] = true
	}
	for d := range wantDNS {
		if !haveDNS[d] {
			return false
		}
	}
	return true
}

// GetPanelCertExpiry membaca waktu kedaluwarsa sertifikat panel jika file sertifikat tersedia.
func (m *Manager) GetPanelCertExpiry() (*time.Time, *int) {
	certPath, _ := m.PanelSSLPaths()
	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil
	}
	for len(data) > 0 {
		var blk *pem.Block
		blk, data = pem.Decode(data)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		cert, parseErr := x509.ParseCertificate(blk.Bytes)
		if parseErr != nil {
			continue
		}
		exp := cert.NotAfter.UTC()
		days := int(time.Until(exp).Hours() / 24)
		return &exp, &days
	}
	return nil, nil
}
