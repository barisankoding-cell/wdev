package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	SettingTOTPEnabled          = "panel_totp_enabled"
	SettingTOTPSecret           = "panel_totp_secret"
	SettingTOTPPendingSecret    = "panel_totp_pending_secret"
	SettingTOTPRecoveryHashes   = "panel_totp_recovery_hashes"
	SettingTOTPTerminalRequired = "panel_totp_terminal_required"

	totpPeriodSeconds    = 30
	totpDigits           = 6
	totpRecoveryCount    = 8
	totpRecoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// TOTPStatus merangkum status aktivasi Two-Factor Authentication (2FA) panel.
type TOTPStatus struct {
	Enabled           bool   `json:"enabled"`
	Pending           bool   `json:"pending"`
	TerminalRequired  bool   `json:"terminal_required"`
	RecoveryRemaining int    `json:"recovery_remaining"`
	Issuer            string `json:"issuer"`
	Account           string `json:"account"`
}

// TOTPEnrollment menyimpan rahasia sementara dan URI otpauth:// selama pendaftaran 2FA.
type TOTPEnrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
	Issuer     string `json:"issuer"`
	Account    string `json:"account"`
}

// GenerateTOTPSecret menghasilkan secret Base32 acak 160-bit (RFC 6238).
func GenerateTOTPSecret() (string, error) {
	b, err := randomBytes(20)
	if err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func decodeBase32Secret(secret string) ([]byte, error) {
	clean := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""), "-", ""))
	if clean == "" {
		return nil, errors.New("secret TOTP kosong")
	}
	if rem := len(clean) % 8; rem != 0 {
		clean += strings.Repeat("=", 8-rem)
	}
	return base32.StdEncoding.DecodeString(clean)
}

// ComputeTOTPCode menghitung kode TOTP 6-digit standar RFC 6238 (HMAC-SHA1, 30 detik).
func ComputeTOTPCode(secret string, at time.Time) (string, error) {
	key, err := decodeBase32Secret(secret)
	if err != nil {
		return "", fmt.Errorf("secret TOTP tidak valid: %w", err)
	}
	counter := uint64(at.Unix() / totpPeriodSeconds)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	binCode := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	otp := binCode % 1000000
	return fmt.Sprintf("%06d", otp), nil
}

// VerifyTOTPCode memverifikasi kode 6-digit terhadap secret dengan toleransi clock skew ±1 langkah (90 detik).
func VerifyTOTPCode(secret, code string, now time.Time) (int64, bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(clean) != totpDigits {
		return 0, false
	}
	for i := 0; i < len(clean); i++ {
		if clean[i] < '0' || clean[i] > '9' {
			return 0, false
		}
	}

	baseStep := now.Unix() / totpPeriodSeconds
	matched := false
	var matchedStep int64
	for _, offset := range []int64{-1, 0, 1} {
		stepTime := time.Unix((baseStep+offset)*totpPeriodSeconds, 0)
		expected, err := ComputeTOTPCode(secret, stepTime)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(clean), []byte(expected)) == 1 {
			matched = true
			matchedStep = baseStep + offset
		}
	}
	return matchedStep, matched
}

// BuildOTPAuthURL membentuk URI otpauth://totp/... untuk QR code aplikasi Authenticator.
func BuildOTPAuthURL(issuer, account, secret string) string {
	cleanIssuer := strings.TrimSpace(issuer)
	if cleanIssuer == "" {
		cleanIssuer = "wdev"
	}
	cleanAccount := strings.TrimSpace(account)
	if cleanAccount == "" {
		cleanAccount = "admin"
	}
	label := url.PathEscape(cleanIssuer + ":" + cleanAccount)
	q := url.Values{}
	q.Set("secret", strings.ToUpper(strings.TrimSpace(secret)))
	q.Set("issuer", cleanIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode())
}

func normalizeRecoveryCode(code string) string {
	replacer := strings.NewReplacer("-", "", " ", "", "\t", "")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(code)))
}

func hashRecoveryCode(code string) string {
	norm := normalizeRecoveryCode(code)
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// GenerateRecoveryCodes membuat daftar kode pemulihan sekali pakai beserta hash SHA-256-nya.
func GenerateRecoveryCodes(count int) ([]string, []string, error) {
	if count <= 0 {
		count = totpRecoveryCount
	}
	codes := make([]string, 0, count)
	hashes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		raw, err := randomBytes(8)
		if err != nil {
			return nil, nil, err
		}
		var chars [8]byte
		for j := 0; j < 8; j++ {
			chars[j] = totpRecoveryAlphabet[int(raw[j])%len(totpRecoveryAlphabet)]
		}
		formatted := string(chars[:4]) + "-" + string(chars[4:])
		codes = append(codes, formatted)
		hashes = append(hashes, hashRecoveryCode(formatted))
	}
	return codes, hashes, nil
}

// IsTOTPEnabled melaporkan apakah 2FA TOTP aktif di panel.
func (m *Manager) IsTOTPEnabled() bool {
	if m == nil || m.store == nil {
		return false
	}
	enabled, _ := m.store.GetSetting(SettingTOTPEnabled)
	if enabled != "1" && !strings.EqualFold(enabled, "true") {
		return false
	}
	sec, _ := m.store.GetSetting(SettingTOTPSecret)
	return strings.TrimSpace(sec) != ""
}

// IsTOTPTerminalRequired melaporkan apakah verifikasi TOTP wajib saat membuka terminal root.
func (m *Manager) IsTOTPTerminalRequired() bool {
	if !m.IsTOTPEnabled() {
		return false
	}
	val, _ := m.store.GetSetting(SettingTOTPTerminalRequired)
	return val == "1" || strings.EqualFold(val, "true")
}

// SetTOTPTerminalRequired mengubah kebijakan apakah buka kunci terminal root mewajibkan 2FA.
func (m *Manager) SetTOTPTerminalRequired(req bool) error {
	if m == nil || m.store == nil {
		return errors.New("penyimpanan tidak tersedia")
	}
	val := "0"
	if req {
		val = "1"
	}
	return m.store.SetSetting(SettingTOTPTerminalRequired, val)
}

func (m *Manager) loadRecoveryHashes() []string {
	if m == nil || m.store == nil {
		return nil
	}
	raw, err := m.store.GetSetting(SettingTOTPRecoveryHashes)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var hashes []string
	if err := json.Unmarshal([]byte(raw), &hashes); err != nil {
		return nil
	}
	return hashes
}

func (m *Manager) saveRecoveryHashes(hashes []string) error {
	if m == nil || m.store == nil {
		return errors.New("penyimpanan tidak tersedia")
	}
	raw, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	return m.store.SetSetting(SettingTOTPRecoveryHashes, string(raw))
}

func (m *Manager) totpIssuerAndAccount(issuerHost string) (string, string) {
	issuer := "wdev"
	if host := extractTOTPHost(issuerHost); host != "" {
		issuer = "wdev (" + host + ")"
	} else if dom := m.GetPanelDomain(); dom != "" {
		issuer = "wdev (" + dom + ")"
	}
	account := m.GetUsername()
	return issuer, account
}

func extractTOTPHost(rawHost string) string {
	h := strings.TrimSpace(rawHost)
	if h == "" {
		return ""
	}
	if idx := strings.IndexByte(h, ':'); idx > 0 && !strings.HasPrefix(h, "[") {
		h = h[:idx]
	}
	return strings.TrimSpace(h)
}

// GetTOTPStatus mengembalikan ringkasan status 2FA panel saat ini.
func (m *Manager) GetTOTPStatus() TOTPStatus {
	issuer, account := m.totpIssuerAndAccount("")
	pending := ""
	if m != nil && m.store != nil {
		pending, _ = m.store.GetSetting(SettingTOTPPendingSecret)
	}
	return TOTPStatus{
		Enabled:           m.IsTOTPEnabled(),
		Pending:           strings.TrimSpace(pending) != "",
		TerminalRequired:  m.IsTOTPTerminalRequired(),
		RecoveryRemaining: len(m.loadRecoveryHashes()),
		Issuer:            issuer,
		Account:           account,
	}
}

// BeginTOTPEnrollment membuat secret TOTP sementara sebelum diverifikasi pengguna.
func (m *Manager) BeginTOTPEnrollment(issuerHost string) (TOTPEnrollment, error) {
	if m == nil || m.store == nil {
		return TOTPEnrollment{}, errors.New("penyimpanan tidak tersedia")
	}
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return TOTPEnrollment{}, err
	}
	if err := m.store.SetSetting(SettingTOTPPendingSecret, secret); err != nil {
		return TOTPEnrollment{}, err
	}
	issuer, account := m.totpIssuerAndAccount(issuerHost)
	return TOTPEnrollment{
		Secret:     secret,
		OTPAuthURL: BuildOTPAuthURL(issuer, account, secret),
		Issuer:     issuer,
		Account:    account,
	}, nil
}

// ConfirmTOTPEnrollment memverifikasi kode TOTP pertama, mengaktifkan 2FA, dan membuat kode pemulihan.
func (m *Manager) ConfirmTOTPEnrollment(code string) ([]string, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("penyimpanan tidak tersedia")
	}
	pending, err := m.store.GetSetting(SettingTOTPPendingSecret)
	if err != nil || strings.TrimSpace(pending) == "" {
		return nil, errors.New("tidak ada sesi pendaftaran 2FA yang tertunda")
	}
	step, ok := VerifyTOTPCode(pending, code, time.Now())
	if !ok {
		return nil, errors.New("kode verifikasi TOTP tidak valid")
	}

	codes, hashes, err := GenerateRecoveryCodes(totpRecoveryCount)
	if err != nil {
		return nil, err
	}

	if err := m.store.SetSetting(SettingTOTPSecret, pending); err != nil {
		return nil, err
	}
	if err := m.store.SetSetting(SettingTOTPEnabled, "1"); err != nil {
		return nil, err
	}
	if err := m.saveRecoveryHashes(hashes); err != nil {
		return nil, err
	}
	_ = m.store.SetSetting(SettingTOTPPendingSecret, "")

	m.totpMu.Lock()
	m.lastTOTPSecret = pending
	m.lastTOTPStep = step
	m.totpMu.Unlock()

	return codes, nil
}

// CancelTOTPEnrollment membatalkan secret sementara yang belum dikonfirmasi.
func (m *Manager) CancelTOTPEnrollment() error {
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.SetSetting(SettingTOTPPendingSecret, "")
}

// RegenerateRecoveryCodes membuat set baru kode pemulihan sekali pakai (membuat kode lama hangus).
func (m *Manager) RegenerateRecoveryCodes() ([]string, error) {
	if !m.IsTOTPEnabled() {
		return nil, errors.New("2FA belum aktif")
	}
	codes, hashes, err := GenerateRecoveryCodes(totpRecoveryCount)
	if err != nil {
		return nil, err
	}
	if err := m.saveRecoveryHashes(hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP menonaktifkan 2FA panel dan menghapus secret serta recovery code terkait.
func (m *Manager) DisableTOTP() error {
	if m == nil || m.store == nil {
		return nil
	}
	_ = m.store.SetSetting(SettingTOTPEnabled, "0")
	_ = m.store.SetSetting(SettingTOTPSecret, "")
	_ = m.store.SetSetting(SettingTOTPPendingSecret, "")
	_ = m.store.SetSetting(SettingTOTPRecoveryHashes, "[]")
	_ = m.store.SetSetting(SettingTOTPTerminalRequired, "0")
	m.totpMu.Lock()
	m.lastTOTPSecret = ""
	m.lastTOTPStep = -1
	m.totpMu.Unlock()
	return nil
}

// VerifyTOTP memvalidasi kode 6-digit TOTP (anti-replay) atau salah satu recovery code sekali pakai.
func (m *Manager) VerifyTOTP(codeOrRecovery string) (usedRecovery bool, remainingRecovery int, ok bool) {
	if !m.IsTOTPEnabled() {
		return false, 0, true
	}
	raw := strings.TrimSpace(codeOrRecovery)
	if raw == "" {
		return false, len(m.loadRecoveryHashes()), false
	}

	secret, _ := m.store.GetSetting(SettingTOTPSecret)

	m.totpMu.Lock()
	defer m.totpMu.Unlock()

	if step, matched := VerifyTOTPCode(secret, raw, time.Now()); matched {
		if m.lastTOTPSecret == secret && step <= m.lastTOTPStep {
			return false, len(m.loadRecoveryHashes()), false
		}
		m.lastTOTPSecret = secret
		m.lastTOTPStep = step
		return false, len(m.loadRecoveryHashes()), true
	}

	// Cek recovery codes (single-use)
	hashes := m.loadRecoveryHashes()
	if len(hashes) == 0 {
		return false, 0, false
	}
	candidateHash := hashRecoveryCode(raw)
	matchIdx := -1
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(candidateHash), []byte(h)) == 1 {
			matchIdx = i
		}
	}
	if matchIdx < 0 {
		return false, len(hashes), false
	}

	remaining := make([]string, 0, len(hashes)-1)
	for i, h := range hashes {
		if i != matchIdx {
			remaining = append(remaining, h)
		}
	}
	if err := m.saveRecoveryHashes(remaining); err != nil {
		return false, len(hashes), false
	}
	return true, len(remaining), true
}
