package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"wdev/internal/auth"
)

// isOriginAllowed memverifikasi apakah header Origin atau Referer berasal dari host panel
// atau dari host publik proxy/tunnel tepercaya (misalnya cloudflared / reverse proxy HTTPS).
func (s *Server) isOriginAllowed(r *http.Request, rawOrigin string) bool {
	u, err := url.Parse(strings.TrimSpace(rawOrigin))
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	reqScheme := "http"
	if r.TLS != nil || auth.IsHTTPS(r) {
		reqScheme = "https"
	}

	originNorm := normalizeAuthority(scheme, u.Host)
	if originNorm == "" {
		return false
	}

	// Cocok langsung dengan Host request (dengan normalisasi port default :80/:443).
	if r.Host != "" && (originNorm == normalizeAuthority(reqScheme, r.Host) || originNorm == normalizeAuthority(scheme, r.Host)) {
		return true
	}

	// Header proxy dan fallback tunnel hanya dipercaya bila koneksi berasal dari proxy tepercaya.
	if !auth.IsTrustedProxyRequest(r) {
		return false
	}

	// Cocok dengan domain panel yang dikonfigurasi admin.
	if s != nil && s.authMgr != nil {
		if panelDomain := strings.TrimSpace(s.authMgr.GetPanelDomain()); panelDomain != "" {
			if originNorm == normalizeAuthority(scheme, panelDomain) {
				return true
			}
		}
	}

	// Cocok dengan X-Forwarded-Host atau header Forwarded (RFC 7239) dari proxy tepercaya.
	forwardedHosts := extractForwardedHosts(r)
	for _, fwdHost := range forwardedHosts {
		if originNorm == normalizeAuthority(reqScheme, fwdHost) || originNorm == normalizeAuthority(scheme, fwdHost) {
			return true
		}
	}
	if len(forwardedHosts) > 0 {
		return false
	}

	// Fallback untuk cloudflared / proxy tepercaya yang menulis ulang header Host
	// tanpa mengirim X-Forwarded-Host: izinkan hanya bila browser menegaskan Sec-Fetch-Site: same-origin.
	fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	return fetchSite == "same-origin" && hasProxyForwardingHeaders(r)
}

func (s *Server) checkWSOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	return s.isOriginAllowed(r, origin)
}

func normalizeAuthority(scheme, hostport string) string {
	hp := strings.TrimSpace(hostport)
	if hp == "" {
		return ""
	}
	if host, port, err := net.SplitHostPort(hp); err == nil {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if host == "" {
			return ""
		}
		if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			return host
		}
		return net.JoinHostPort(host, port)
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(hp, "[]"), "."))
}

func extractForwardedHosts(r *http.Request) []string {
	var out []string
	seen := make(map[string]struct{})
	add := func(h string) {
		h = strings.Trim(strings.TrimSpace(h), `"`)
		if h == "" {
			return
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}

	if xfh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xfh != "" {
		first := strings.Split(xfh, ",")[0]
		add(first)
	}
	if fwd := strings.TrimSpace(r.Header.Get("Forwarded")); fwd != "" {
		firstElem := strings.Split(fwd, ",")[0]
		for _, param := range strings.Split(firstElem, ";") {
			kv := strings.SplitN(strings.TrimSpace(param), "=", 2)
			if len(kv) == 2 && strings.EqualFold(strings.TrimSpace(kv[0]), "host") {
				add(kv[1])
			}
		}
	}
	return out
}

func hasProxyForwardingHeaders(r *http.Request) bool {
	for _, key := range []string{
		"Cf-Ray",
		"Cf-Connecting-Ip",
		"Cf-Visitor",
		"Cdn-Loop",
		"X-Forwarded-For",
		"X-Forwarded-Proto",
		"Forwarded",
		"X-Real-Ip",
	} {
		if strings.TrimSpace(r.Header.Get(key)) != "" {
			return true
		}
	}
	return false
}
