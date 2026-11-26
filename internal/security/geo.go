package security

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	geoCache   = make(map[string]string)
	geoCacheMu sync.RWMutex
	httpClient = &http.Client{
		Timeout: 1500 * time.Millisecond,
	}
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
// dan menyimpannya di cache in-memory untuk event berikutnya.
func FetchCountryAsync(ipStr string, onResolved func(country string)) {
	ip := strings.TrimSpace(ipStr)
	if ip == "" || IsPrivateOrLoopback(ip) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipwho.is/"+ip, nil)
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
			code := strings.ToUpper(payload.CountryCode)
			geoCacheMu.Lock()
			geoCache[ip] = code
			geoCacheMu.Unlock()
			if onResolved != nil {
				onResolved(code)
			}
		}
	}()
}
