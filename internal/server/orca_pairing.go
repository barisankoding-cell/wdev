package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/i18n"
	"wdev/internal/installer"
	"wdev/internal/monitor"
	"wdev/internal/pdns"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	settingOrcaPairingAddressMode   = "orca_pairing_address_mode"
	settingOrcaPairingCustomAddress = "orca_pairing_custom_address"

	orcaPairingModeAuto   = "auto"
	orcaPairingModeLocal  = "local"
	orcaPairingModeVPN    = "vpn"
	orcaPairingModeCustom = "custom"
)

var (
	detectOrcaLANIPFn     = pdns.DetectLocalLANIP
	detectOrcaVPNIPFn     = detectLocalCGNATFallback
	isOrcaActiveForUserFn = isOrcaActiveForUser
)

// OrcaPairingState merangkum konfigurasi alamat pairing Orca beserta hasil link terkininya.
type OrcaPairingState struct {
	Status               string `json:"status,omitempty"`
	Active               bool   `json:"active"`
	Mode                 string `json:"mode"`
	CustomAddress        string `json:"custom_address"`
	TargetAddress        string `json:"target_address,omitempty"`
	LocalIP              string `json:"local_ip,omitempty"`
	VPNIP                string `json:"vpn_ip,omitempty"`
	PairingURL           string `json:"pairing_url,omitempty"`
	WebClientURL         string `json:"web_client_url,omitempty"`
	Endpoint             string `json:"endpoint,omitempty"`
	Error                string `json:"error,omitempty"`
	MobilePairingURL     string `json:"mobile_pairing_url,omitempty"`
	MobileEndpoint       string `json:"mobile_endpoint,omitempty"`
	MobileConnectionMode string `json:"mobile_connection_mode,omitempty"`
	MobileError          string `json:"mobile_error,omitempty"`
	Message              string `json:"message,omitempty"`
}

func (s *Server) detectHostNetworkIPs() (mode string, pdnsCfg pdns.EdgeConfig, lanIP string, vpnIP string) {
	mode = "local"
	if s.pdnsMgr != nil {
		mode = s.pdnsMgr.GetMode()
		pdnsCfg = s.pdnsMgr.LoadConfig()
		vpnIP = strings.TrimSpace(pdnsCfg.LocalNetbirdIP)
		if vpnIP == "" {
			vpnIP = strings.TrimSpace(s.pdnsMgr.GetLocalNetbirdIP())
		}
	}
	if vpnIP == "" {
		vpnIP = strings.TrimSpace(detectOrcaVPNIPFn())
	}
	lanIP = strings.TrimSpace(detectOrcaLANIPFn())
	return mode, pdnsCfg, lanIP, vpnIP
}

func normalizeOrcaPairingMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", orcaPairingModeAuto:
		return orcaPairingModeAuto, true
	case orcaPairingModeLocal:
		return orcaPairingModeLocal, true
	case orcaPairingModeVPN:
		return orcaPairingModeVPN, true
	case orcaPairingModeCustom:
		return orcaPairingModeCustom, true
	default:
		return "", false
	}
}

func isWildcardOrcaHost(host string) bool {
	h := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	return h == "" || h == "*" || h == "0.0.0.0" || h == "::" || h == "::ffff:0:0"
}

func hasControlOrSpace(s string) bool {
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validateOrcaCustomAddress(raw, lang string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", errors.New(i18n.Pick(lang, "Custom pairing address is required", "Alamat pairing kustom wajib diisi"))
	}
	if len(clean) > 1024 || hasControlOrSpace(clean) {
		return "", errors.New(i18n.Pick(lang, "Custom pairing address contains invalid characters or is too long", "Alamat pairing kustom mengandung karakter tidak valid atau terlalu panjang"))
	}

	if strings.Contains(clean, "://") {
		u, err := url.Parse(clean)
		if err != nil {
			return "", errors.New(i18n.Pick(lang, "Invalid custom pairing URL format", "Format URL pairing kustom tidak valid"))
		}
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "ws", "wss", "http", "https":
		default:
			return "", errors.New(i18n.Pick(lang, "URL scheme must be ws://, wss://, http://, or https://", "Skema URL harus ws://, wss://, http://, atau https://"))
		}
		if u.User != nil || u.Fragment != "" || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
			return "", errors.New(i18n.Pick(lang, "Custom pairing URL must be an origin without path, query, fragment, or credentials", "URL pairing kustom harus berupa origin tanpa path, query, fragment, atau kredensial"))
		}
		host := u.Hostname()
		if isWildcardOrcaHost(host) {
			return "", errors.New(i18n.Pick(lang, "Custom pairing URL must specify a reachable host", "URL pairing kustom harus menyertakan host yang dapat dijangkau"))
		}
		if portStr := u.Port(); portStr != "" {
			p, pErr := strconv.Atoi(portStr)
			if pErr != nil || p < 1 || p > 65535 {
				return "", errors.New(i18n.Pick(lang, "Port in custom pairing URL must be between 1 and 65535", "Port pada URL pairing kustom harus antara 1 dan 65535"))
			}
		}
		u.Scheme = scheme
		if u.Path == "/" {
			u.Path = ""
		}
		return strings.TrimRight(u.String(), "/"), nil
	}

	if strings.ContainsAny(clean, "/?#@") {
		return "", errors.New(i18n.Pick(lang, "Custom host must not contain path, query, fragment, or user info", "Host kustom tidak boleh mengandung path, query, fragment, atau info pengguna"))
	}

	host := clean
	if strings.HasPrefix(clean, "[") {
		if strings.HasSuffix(clean, "]") {
			inner := strings.TrimSuffix(strings.TrimPrefix(clean, "["), "]")
			if ip := net.ParseIP(inner); ip == nil || ip.To4() != nil {
				return "", errors.New(i18n.Pick(lang, "Invalid IPv6 host in custom pairing address", "Host IPv6 pada alamat pairing kustom tidak valid"))
			}
			host = inner
		} else {
			h, portStr, err := net.SplitHostPort(clean)
			if err != nil {
				return "", errors.New(i18n.Pick(lang, "Invalid host:port in custom pairing address", "Format host:port pada alamat pairing kustom tidak valid"))
			}
			if ip := net.ParseIP(h); ip == nil || ip.To4() != nil {
				return "", errors.New(i18n.Pick(lang, "Invalid IPv6 host in custom pairing address", "Host IPv6 pada alamat pairing kustom tidak valid"))
			}
			p, pErr := strconv.Atoi(portStr)
			if pErr != nil || p < 1 || p > 65535 {
				return "", errors.New(i18n.Pick(lang, "Port in custom pairing address must be between 1 and 65535", "Port pada alamat pairing kustom harus antara 1 dan 65535"))
			}
			host = h
		}
	} else if ip := net.ParseIP(clean); ip != nil {
		host = ip.String()
	} else if strings.Contains(clean, ":") {
		h, portStr, err := net.SplitHostPort(clean)
		if err != nil {
			return "", errors.New(i18n.Pick(lang, "Invalid host:port in custom pairing address", "Format host:port pada alamat pairing kustom tidak valid"))
		}
		p, pErr := strconv.Atoi(portStr)
		if pErr != nil || p < 1 || p > 65535 {
			return "", errors.New(i18n.Pick(lang, "Port in custom pairing address must be between 1 and 65535", "Port pada alamat pairing kustom harus antara 1 dan 65535"))
		}
		host = h
	}

	if isWildcardOrcaHost(host) {
		return "", errors.New(i18n.Pick(lang, "Custom pairing address must specify a reachable host or IP", "Alamat pairing kustom harus menyertakan host atau IP yang dapat dijangkau"))
	}
	return clean, nil
}

func (s *Server) loadOrcaPairingPrefs() (mode, customAddr string) {
	mode = orcaPairingModeAuto
	if s.store == nil {
		return mode, ""
	}
	if rawMode, err := s.store.GetSetting(settingOrcaPairingAddressMode); err == nil {
		if norm, ok := normalizeOrcaPairingMode(rawMode); ok {
			mode = norm
		}
	}
	if rawCustom, err := s.store.GetSetting(settingOrcaPairingCustomAddress); err == nil {
		customAddr = strings.TrimSpace(rawCustom)
	}
	return mode, customAddr
}

func (s *Server) resolveOrcaPairingTarget(mode, customAddr, lanIP, vpnIP, lang string) (targetAddr string, err error) {
	switch mode {
	case "", orcaPairingModeAuto:
		return "", nil
	case orcaPairingModeLocal:
		if lanIP != "" {
			return lanIP, nil
		}
		return "127.0.0.1", nil
	case orcaPairingModeVPN:
		if vpnIP == "" {
			return "", errors.New(i18n.Pick(lang, "VPN IP (NetBird / Tailscale) is not detected on this server", "IP VPN (NetBird / Tailscale) tidak terdeteksi di server ini"))
		}
		return vpnIP, nil
	case orcaPairingModeCustom:
		return validateOrcaCustomAddress(customAddr, lang)
	default:
		return "", fmt.Errorf(i18n.Pick(lang, "Invalid pairing address mode: %s", "Mode alamat pairing tidak valid: %s"), mode)
	}
}

func isOrcaActiveForUser(ctx context.Context, targetUser, userHome string) bool {
	return installer.IsOrcaServiceActive(ctx, targetUser, userHome) || monitor.IsSystemdServiceActive("orca")
}

func normalizeOrcaRotateScope(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all":
		return "all", true
	case "mobile":
		return "mobile", true
	case "runtime", "desktop", "web":
		return "runtime", true
	default:
		return "", false
	}
}

func renderTerminalQRCode(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	q, err := qrcode.New(value, qrcode.Medium)
	if err != nil {
		return nil
	}
	raw := strings.ReplaceAll(q.ToSmallString(true), "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimRight(line, "\r")
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func extractOrcaPairError(res *installer.OrcaPairResult, pairErr error, fallback string) (msg string, rejected bool) {
	if res != nil && !res.Available {
		if g := strings.TrimSpace(res.Guidance); g != "" {
			return g, true
		}
		if r := strings.TrimSpace(res.Reason); r != "" {
			return r, true
		}
	}
	if pairErr != nil {
		return pairErr.Error(), false
	}
	return fallback, true
}

type orcaPairingFetchOutcome struct {
	state           OrcaPairingState
	runtimeExecErr  error
	runtimeRejected bool
	mobileExecErr   error
	mobileRejected  bool
}

func (s *Server) fetchOrcaPairingInternal(ctx context.Context, userHome, mode, customAddr string, rotate bool, rotateScope string, lang string) orcaPairingFetchOutcome {
	_, _, lanIP, vpnIP := s.detectHostNetworkIPs()
	if lanIP == "" {
		lanIP = "127.0.0.1"
	}

	out := orcaPairingFetchOutcome{
		state: OrcaPairingState{
			Mode:          mode,
			CustomAddress: customAddr,
			LocalIP:       lanIP,
			VPNIP:         vpnIP,
		},
	}

	targetAddr, err := s.resolveOrcaPairingTarget(mode, customAddr, lanIP, vpnIP, lang)
	if err != nil {
		out.state.Error = err.Error()
		return out
	}
	out.state.TargetAddress = targetAddr

	normScope, ok := normalizeOrcaRotateScope(rotateScope)
	if !ok {
		normScope = "all"
	}
	rotateRuntime := rotate && (normScope == "all" || normScope == "runtime")
	rotateMobile := rotate && (normScope == "all" || normScope == "mobile")

	var (
		wg            sync.WaitGroup
		rtRes, mobRes *installer.OrcaPairResult
		rtErr, mobErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		rtRes, rtErr = installer.OrcaPairInfoWithOptions(ctx, userHome, installer.OrcaPairOptions{
			Address: targetAddr,
			Rotate:  rotateRuntime,
			Mobile:  false,
		})
	}()
	go func() {
		defer wg.Done()
		mobRes, mobErr = installer.OrcaPairInfoWithOptions(ctx, userHome, installer.OrcaPairOptions{
			Address: targetAddr,
			Rotate:  rotateMobile,
			Mobile:  true,
		})
	}()
	wg.Wait()

	fallback := i18n.Pick(lang, "Pairing is currently unavailable", "Pairing saat ini belum tersedia")

	if rtErr == nil && rtRes != nil && rtRes.Available {
		out.state.PairingURL = rtRes.PairingURL
		out.state.WebClientURL = rtRes.WebClientURL
		out.state.Endpoint = rtRes.Endpoint
	} else {
		msg, rejected := extractOrcaPairError(rtRes, rtErr, fallback)
		out.state.Error = msg
		out.runtimeExecErr = rtErr
		out.runtimeRejected = rejected
	}

	if mobErr == nil && mobRes != nil && mobRes.Available {
		out.state.MobilePairingURL = mobRes.PairingURL
		out.state.MobileEndpoint = mobRes.Endpoint
		out.state.MobileConnectionMode = mobRes.ConnectionMode
		if out.state.Endpoint == "" {
			out.state.Endpoint = mobRes.Endpoint
		}
	} else {
		msg, rejected := extractOrcaPairError(mobRes, mobErr, fallback)
		out.state.MobileError = msg
		out.mobileExecErr = mobErr
		out.mobileRejected = rejected
	}

	return out
}

func (s *Server) fetchOrcaPairingWithConfig(ctx context.Context, userHome, mode, customAddr string, rotate bool, rotateScope string, lang string) OrcaPairingState {
	return s.fetchOrcaPairingInternal(ctx, userHome, mode, customAddr, rotate, rotateScope, lang).state
}

func (s *Server) handleWorkspaceOrcaPairing(w http.ResponseWriter, r *http.Request) {
	lang := i18n.FromRequest(r)
	targetUser, userHome := installer.ResolveOrcaUser()

	switch r.Method {
	case http.MethodGet:
		mode, customAddr := s.loadOrcaPairingPrefs()
		active := isOrcaActiveForUserFn(r.Context(), targetUser, userHome)
		_, _, lanIP, vpnIP := s.detectHostNetworkIPs()
		if lanIP == "" {
			lanIP = "127.0.0.1"
		}
		if !active {
			writeJSON(w, http.StatusOK, OrcaPairingState{
				Status:        "ok",
				Active:        false,
				Mode:          mode,
				CustomAddress: customAddr,
				LocalIP:       lanIP,
				VPNIP:         vpnIP,
			})
			return
		}
		pairCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		state := s.fetchOrcaPairingWithConfig(pairCtx, userHome, mode, customAddr, false, "", lang)
		state.Status = "ok"
		state.Active = true
		writeJSON(w, http.StatusOK, state)

	case http.MethodPost:
		var payload struct {
			Mode          string `json:"mode"`
			CustomAddress string `json:"custom_address"`
			Rotate        bool   `json:"rotate"`
			RotateScope   string `json:"rotate_scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, i18n.Pick(lang, "Invalid pairing address payload", "Payload alamat pairing tidak valid"))
			return
		}

		mode, ok := normalizeOrcaPairingMode(payload.Mode)
		if !ok {
			writeError(w, http.StatusBadRequest, i18n.Pick(lang, "Invalid pairing address mode (use auto, local, vpn, or custom)", "Mode alamat pairing tidak valid (gunakan auto, local, vpn, atau custom)"))
			return
		}

		rotateScope, ok := normalizeOrcaRotateScope(payload.RotateScope)
		if !ok {
			writeError(w, http.StatusBadRequest, i18n.Pick(lang, "Invalid rotate scope (use all, mobile, or runtime)", "Cakupan rotasi tidak valid (gunakan all, mobile, atau runtime)"))
			return
		}

		_, savedCustom := s.loadOrcaPairingPrefs()
		customInput := strings.TrimSpace(payload.CustomAddress)
		if customInput == "" && mode != orcaPairingModeCustom {
			customInput = savedCustom
		}

		_, _, lanIP, vpnIP := s.detectHostNetworkIPs()
		if lanIP == "" {
			lanIP = "127.0.0.1"
		}

		targetAddr, err := s.resolveOrcaPairingTarget(mode, customInput, lanIP, vpnIP, lang)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if mode == orcaPairingModeCustom {
			customInput = targetAddr
		} else if customInput != "" {
			if validated, vErr := validateOrcaCustomAddress(customInput, lang); vErr == nil {
				customInput = validated
			}
		}

		active := isOrcaActiveForUserFn(r.Context(), targetUser, userHome)
		var state OrcaPairingState
		if active {
			pairCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			outcome := s.fetchOrcaPairingInternal(pairCtx, userHome, mode, customInput, payload.Rotate, rotateScope, lang)
			state = outcome.state

			if outcome.state.PairingURL == "" && outcome.state.MobilePairingURL == "" {
				if outcome.runtimeRejected || outcome.mobileRejected {
					msg := outcome.state.Error
					if msg == "" {
						msg = i18n.Pick(lang, "Orca rejected the pairing address", "Orca menolak alamat pairing tersebut")
					}
					writeError(w, http.StatusBadRequest, msg)
					return
				}
				errText := outcome.state.Error
				if errText == "" {
					errText = outcome.state.MobileError
				}
				writeError(w, http.StatusBadGateway, fmt.Sprintf(i18n.Pick(lang, "Failed to generate Orca pairing link: %s", "Gagal membuat link pairing Orca: %s"), errText))
				return
			}
			if payload.Rotate && rotateScope == "mobile" && outcome.state.MobilePairingURL == "" {
				status := http.StatusBadGateway
				if outcome.mobileRejected {
					status = http.StatusBadRequest
				}
				writeError(w, status, outcome.state.MobileError)
				return
			}
			if outcome.runtimeRejected && mode == orcaPairingModeCustom {
				writeError(w, http.StatusBadRequest, outcome.state.Error)
				return
			}
		} else {
			state = OrcaPairingState{
				Mode:          mode,
				CustomAddress: customInput,
				TargetAddress: targetAddr,
				LocalIP:       lanIP,
				VPNIP:         vpnIP,
			}
		}

		if s.store != nil {
			if err := s.store.SetSetting(settingOrcaPairingAddressMode, mode); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if customInput != "" || mode == orcaPairingModeCustom {
				if err := s.store.SetSetting(settingOrcaPairingCustomAddress, customInput); err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
		}

		state.Status = "saved"
		state.Active = active
		state.Mode = mode
		state.CustomAddress = customInput
		state.TargetAddress = targetAddr
		state.LocalIP = lanIP
		state.VPNIP = vpnIP
		state.Message = i18n.Pick(lang, "Orca pairing address updated.", "Alamat pairing Orca berhasil diperbarui.")
		writeJSON(w, http.StatusOK, state)

	default:
		writeMethodNotAllowed(w, r)
	}
}
