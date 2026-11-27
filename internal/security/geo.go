package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxGeoCacheEntries = 4096

var (
	geoCache    = make(map[string]string)
	geoInFlight = make(map[string]bool)
	geoCacheMu  sync.RWMutex
	httpClient  = &http.Client{
		Timeout: 1500 * time.Millisecond,
	}
	geoLookupURLPrefix = "https://ipwho.is/"
)

// IsPrivateOrLoopback mengecek apakah sebuah IP berada di jaringan privat, loopback, atau khusus.
func IsPrivateOrLoopback(ipStr string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ipStr))
	if parsed == nil {
		return false
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() {
		return true
	}
	// Cek CGNAT (100.64.0.0/10) yang digunakan NetBird/Tailscale
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	if cgnat != nil && cgnat.Contains(parsed) {
		return true
	}
	return false
}

// NormalizeIPOrCIDR memvalidasi dan menormalkan entri IP tunggal atau CIDR untuk whitelist.
func NormalizeIPOrCIDR(raw string) (string, error) {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return "", fmt.Errorf("entri IP/CIDR tidak boleh kosong")
	}
	if strings.Contains(entry, "/") {
		ip, network, err := net.ParseCIDR(entry)
		if err != nil || ip == nil || network == nil {
			return "", fmt.Errorf("format CIDR tidak valid: %q", entry)
		}
		return network.String(), nil
	}
	ip := net.ParseIP(entry)
	if ip == nil {
		return "", fmt.Errorf("format alamat IP tidak valid: %q", entry)
	}
	return ip.String(), nil
}

// MatchesIPList mengecek apakah ipStr cocok dengan salah satu IP atau CIDR di list.
func MatchesIPList(ipStr string, list []string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ipStr))
	if parsed == nil {
		return false
	}
	for _, item := range list {
		entry := strings.TrimSpace(item)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, cidr, err := net.ParseCIDR(entry); err == nil && cidr.Contains(parsed) {
				return true
			}
			continue
		}
		if p := net.ParseIP(entry); p != nil && p.Equal(parsed) {
			return true
		}
	}
	return false
}

// ResolveCountry mencari kode negara (2 huruf, contoh ID, US, SG) untuk IP publik.
// Menggunakan in-memory cache dan non-blocking fallback agar tidak menghambat pemrosesan request.
func ResolveCountry(ipStr string) string {
	ip := strings.TrimSpace(ipStr)
	if ip == "" {
		return ""
	}
	if IsPrivateOrLoopback(ip) {
		return "LAN"
	}

	geoCacheMu.RLock()
	if code, found := geoCache[ip]; found {
		geoCacheMu.RUnlock()
		return code
	}
	geoCacheMu.RUnlock()

	return "EXT"
}

// FetchCountryAsync mengambil kode negara secara asinkron dari API publik gratis (tanpa token)
// dan menyimpannya di cache in-memory serta memanggil onResolved ketika berhasil.
func FetchCountryAsync(ipStr string, onResolved func(country string)) {
	ip := strings.TrimSpace(ipStr)
	if ip == "" || IsPrivateOrLoopback(ip) {
		return
	}
	if net.ParseIP(ip) == nil {
		return
	}

	geoCacheMu.Lock()
	if code, found := geoCache[ip]; found {
		geoCacheMu.Unlock()
		if onResolved != nil && code != "" && code != "EXT" {
			onResolved(code)
		}
		return
	}
	if geoInFlight[ip] {
		geoCacheMu.Unlock()
		return
	}
	geoInFlight[ip] = true
	baseURL := geoLookupURLPrefix
	geoCacheMu.Unlock()

	go func() {
		defer func() {
			geoCacheMu.Lock()
			delete(geoInFlight, ip)
			geoCacheMu.Unlock()
		}()

		if baseURL == "" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+ip, nil)
		if err != nil {
			return
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return
		}

		var payload struct {
			Success     bool   `json:"success"`
			CountryCode string `json:"country_code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return
		}

		if payload.Success && payload.CountryCode != "" {
			code := strings.ToUpper(strings.TrimSpace(payload.CountryCode))
			if code == "" {
				return
			}
			geoCacheMu.Lock()
			if len(geoCache) >= maxGeoCacheEntries {
				geoCache = make(map[string]string)
			}
			geoCache[ip] = code
			geoCacheMu.Unlock()
			if onResolved != nil {
				onResolved(code)
			}
		}
	}()
}
