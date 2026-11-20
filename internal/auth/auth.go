package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

const (
	SettingAuthEnabled      = "panel_auth_enabled"
	SettingSecurityEntrance = "panel_security_entrance"
	SettingUsername         = "panel_username"
	SettingPasswordHash     = "panel_password_hash"
	SettingInitialPassword  = "panel_initial_password"
	SettingEntranceSecret   = "panel_entrance_secret"

	DefaultIterations = 25000
	SaltLength        = 16
	KeyLength         = 32

	CookieSessionName  = "wdev_session"
	CookieEntranceName = "wdev_entrance"

	SessionDuration  = 7 * 24 * time.Hour
	EntranceDuration = 4 * time.Hour

	MaxFailedAttempts = 5
	LockoutDuration   = 15 * time.Minute
	AttemptWindow     = 5 * time.Minute
)

// attemptInfo mencatat percobaan login per IP untuk anti-brute force.
type attemptInfo struct {
	count       int
	firstFailed time.Time
	lockedUntil time.Time
}

type Manager struct {
	cfg   *config.Config
	store *store.Store

	limiterMu sync.Mutex
	attempts  map[string]*attemptInfo
}

type PanelAccessInfo struct {
	PublicURL        string `json:"public_url"`
	NetbirdURL       string `json:"netbird_url"`
	LocalURL         string `json:"local_url"`
	PublicIP         string `json:"public_ip"`
	NetbirdIP        string `json:"netbird_ip"`
	Port             string `json:"port"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	PasswordIsCustom bool   `json:"password_is_custom"`
	SecurityEntrance string `json:"security_entrance"`
	AuthEnabled      bool   `json:"auth_enabled"`
}

func New(cfg *config.Config, s *store.Store) *Manager {
	m := &Manager{
		cfg:      cfg,
		store:    s,
		attempts: make(map[string]*attemptInfo),
	}
	_ = m.EnsureInitialized()
	return m
}

// randomBytes menghasilkan byte acak aman.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// randomHex menghasilkan string heksadesimal acak sepanjang 2*n karakter.
func randomHex(n int) string {
	b, err := randomBytes(n)
	if err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// randomReadableString menghasilkan string alphanumeric acak mudah dibaca.
func randomReadablePassword(length int) string {
	const charset = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789!#$%"
	b, err := randomBytes(length)
	if err != nil {
		return randomHex(length / 2)
	}
	res := make([]byte, length)
	for i := 0; i < length; i++ {
		res[i] = charset[int(b[i])%len(charset)]
	}
	return string(res)
}

// pbkdf2SHA256 mengimplementasikan RFC 2898 PBKDF2 menggunakan HMAC-SHA256.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var dk []byte
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		var blockBytes [4]byte
		binary.BigEndian.PutUint32(blockBytes[:], uint32(block))
		prf.Write(blockBytes[:])
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for j := 0; j < len(t); j++ {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

// HashPassword membuat hash password PBKDF2-SHA256 dengan salt acak.
func HashPassword(password string) (string, error) {
	salt, err := randomBytes(SaltLength)
	if err != nil {
		return "", err
	}
	derived := pbkdf2SHA256([]byte(password), salt, DefaultIterations, KeyLength)
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s",
		DefaultIterations,
		hex.EncodeToString(salt),
		hex.EncodeToString(derived),
	), nil
}

// VerifyPassword memeriksa password teks polos dengan hash PBKDF2-SHA256.
func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[2])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	expectedHash, err := hex.DecodeString(parts[4])
	if err != nil {
		return false
	}

	actualHash := pbkdf2SHA256([]byte(password), salt, iter, len(expectedHash))
	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1
}

// EnsureInitialized memastikan akun admin dan security entrance sudah disiapkan.
func (m *Manager) EnsureInitialized() error {
	user, _ := m.store.GetSetting(SettingUsername)
	if user == "" {
		initialUser := "admin"
		initialPass := "wdev-" + randomReadablePassword(10)
		hash, err := HashPassword(initialPass)
		if err != nil {
			return err
		}
		entrance := randomHex(4) // 8-char hex e.g. "a4f89d12"
		secret := randomHex(16)

		_ = m.store.SetSetting(SettingUsername, initialUser)
		_ = m.store.SetSetting(SettingPasswordHash, hash)
		_ = m.store.SetSetting(SettingInitialPassword, initialPass)
		_ = m.store.SetSetting(SettingSecurityEntrance, entrance)
		_ = m.store.SetSetting(SettingEntranceSecret, secret)
		_ = m.store.SetSetting(SettingAuthEnabled, "1")
	}

	// Pastikan entrance secret selalu tersedia
	sec, _ := m.store.GetSetting(SettingEntranceSecret)
	if sec == "" {
		_ = m.store.SetSetting(SettingEntranceSecret, randomHex(16))
	}
	return nil
}

// IsAuthEnabled mengecek apakah proteksi login aktif.
func (m *Manager) IsAuthEnabled() bool {
	val, err := m.store.GetSetting(SettingAuthEnabled)
	if err != nil || val == "" {
		return true
	}
	return val == "1" || strings.ToLower(val) == "true"
}

// SetAuthEnabled mengaktifkan atau menonaktifkan proteksi login.
func (m *Manager) SetAuthEnabled(enabled bool) error {
	val := "0"
	if enabled {
		val = "1"
	}
	return m.store.SetSetting(SettingAuthEnabled, val)
}

// GetSecurityEntrance mengambil path URL security entrance (contoh: "a4f89d12").
func (m *Manager) GetSecurityEntrance() string {
	val, _ := m.store.GetSetting(SettingSecurityEntrance)
	val = strings.TrimSpace(strings.Trim(val, "/"))
	if val == "off" || val == "disabled" || val == "none" {
		return ""
	}
	return val
}

// SetSecurityEntrance mengubah path security entrance. Jika path == "" atau "off", dinonaktifkan.
func (m *Manager) SetSecurityEntrance(path string) error {
	clean := strings.TrimSpace(strings.Trim(path, "/"))
	if clean == "off" || clean == "none" || clean == "disable" || clean == "disabled" {
		clean = ""
	}
	return m.store.SetSetting(SettingSecurityEntrance, clean)
}

// GetUsername mengambil username panel.
func (m *Manager) GetUsername() string {
	user, _ := m.store.GetSetting(SettingUsername)
	if user == "" {
		return "admin"
	}
	return user
}

// SetUsername mengubah username panel.
func (m *Manager) SetUsername(user string) error {
	user = strings.TrimSpace(user)
	if user == "" {
		return errors.New("username tidak boleh kosong")
	}
	return m.store.SetSetting(SettingUsername, user)
}

// SetPassword memperbarui kata sandi panel.
func (m *Manager) SetPassword(newPass string) error {
	newPass = strings.TrimSpace(newPass)
	if len(newPass) < 6 {
		return errors.New("kata sandi minimal 6 karakter")
	}
	hash, err := HashPassword(newPass)
	if err != nil {
		return err
	}
	if err := m.store.SetSetting(SettingPasswordHash, hash); err != nil {
		return err
	}
	// Hapus initial password karena sudah diganti manual oleh admin
	_ = m.store.SetSetting(SettingInitialPassword, "")
	return nil
}

// ValidateCredentials memvalidasi login username & password.
func (m *Manager) ValidateCredentials(user, pass string) bool {
	if !m.IsAuthEnabled() {
		return true
	}
	currentUsername := m.GetUsername()
	if user != currentUsername {
		return false
	}
	hash, err := m.store.GetSetting(SettingPasswordHash)
	if err != nil || hash == "" {
		return false
	}
	return VerifyPassword(pass, hash)
}

// CheckRateLimit memeriksa apakah IP terkunci akibat kegagalan login beruntun.
func (m *Manager) CheckRateLimit(ip string) (bool, time.Duration) {
	m.limiterMu.Lock()
	defer m.limiterMu.Unlock()

	now := time.Now()
	info, exists := m.attempts[ip]
	if !exists {
		return true, 0
	}

	if now.Before(info.lockedUntil) {
		return false, time.Until(info.lockedUntil)
	}

	// Reset jika jendela percobaan sudah lewat
	if now.Sub(info.firstFailed) > AttemptWindow && info.lockedUntil.IsZero() {
		delete(m.attempts, ip)
	}
	return true, 0
}

// RecordLoginAttempt mencatat percobaan login.
func (m *Manager) RecordLoginAttempt(ip string, success bool) {
	m.limiterMu.Lock()
	defer m.limiterMu.Unlock()

	now := time.Now()
	if success {
		delete(m.attempts, ip)
		return
	}

	info, exists := m.attempts[ip]
	if !exists {
		m.attempts[ip] = &attemptInfo{
			count:       1,
			firstFailed: now,
		}
		return
	}

	if now.Sub(info.firstFailed) > AttemptWindow {
		info.count = 1
		info.firstFailed = now
		info.lockedUntil = time.Time{}
		return
	}

	info.count++
	if info.count >= MaxFailedAttempts {
		info.lockedUntil = now.Add(LockoutDuration)
	}
}

// CreateSession membuat token sesi baru dan menyimpannya di database.
func (m *Manager) CreateSession(username string) (string, error) {
	token := randomHex(32)
	expiresAt := time.Now().Add(SessionDuration)
	if err := m.store.SaveSession(token, username, expiresAt); err != nil {
		return "", err
	}
	_ = m.store.DeleteExpiredSessions()
	return token, nil
}

// ValidateSession memeriksa keabsahan token sesi.
func (m *Manager) ValidateSession(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	username, exp, err := m.store.GetSession(token)
	if err != nil || username == "" {
		return "", false
	}
	if time.Now().After(exp) {
		_ = m.store.DeleteSession(token)
		return "", false
	}
	return username, true
}

// DeleteSession menghapus sesi pengguna saat logout.
func (m *Manager) DeleteSession(token string) error {
	return m.store.DeleteSession(token)
}

// SignEntranceCookie membuat cookie penanda bahwa pengunjung telah melewati security entrance.
func (m *Manager) GenerateEntranceCookie() (*http.Cookie, error) {
	secret, _ := m.store.GetSetting(SettingEntranceSecret)
	if secret == "" {
		secret = "wdev-fallback-entrance-secret"
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	sig := hex.EncodeToString(mac.Sum(nil))
	val := fmt.Sprintf("%s.%s", ts, sig)

	return &http.Cookie{
		Name:     CookieEntranceName,
		Value:    val,
		Path:     "/",
		Expires:  time.Now().Add(EntranceDuration),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// ValidateEntranceCookie memverifikasi keabsahan cookie security entrance.
func (m *Manager) ValidateEntranceCookie(val string) bool {
	secret, _ := m.store.GetSetting(SettingEntranceSecret)
	if secret == "" {
		return false
	}
	parts := strings.Split(val, ".")
	if len(parts) != 2 {
		return false
	}
	tsStr, sigHex := parts[0], parts[1]
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false
	}

	// Kadaluwarsa jika lebih dari EntranceDuration
	if time.Since(time.Unix(ts, 0)) > EntranceDuration {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(tsStr))
	expected := mac.Sum(nil)
	actual, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// ResetCredentials mengembalikan kredensial ke nilai acak baru.
func (m *Manager) ResetCredentials() (PanelAccessInfo, error) {
	newPass := "wdev-" + randomReadablePassword(10)
	hash, err := HashPassword(newPass)
	if err != nil {
		return PanelAccessInfo{}, err
	}
	entrance := randomHex(4)
	_ = m.store.SetSetting(SettingUsername, "admin")
	_ = m.store.SetSetting(SettingPasswordHash, hash)
	_ = m.store.SetSetting(SettingInitialPassword, newPass)
	_ = m.store.SetSetting(SettingSecurityEntrance, entrance)
	_ = m.store.SetSetting(SettingAuthEnabled, "1")

	return m.GetPanelAccessInfo("", ""), nil
}

// GetPanelAccessInfo mengumpulkan informasi akses panel untuk dicetak di terminal.
func (m *Manager) GetPanelAccessInfo(publicIP, netbirdIP string) PanelAccessInfo {
	user := m.GetUsername()
	initPass, _ := m.store.GetSetting(SettingInitialPassword)
	entrance := m.GetSecurityEntrance()
	authEnabled := m.IsAuthEnabled()
	port := m.cfg.HTTPPort
	if port == "" {
		port = "3200"
	}

	isCustom := initPass == ""
	dispPass := initPass
	if isCustom {
		dispPass = "(Password kustom telah dikonfigurasi)"
	}

	entrancePath := ""
	if entrance != "" {
		entrancePath = "/" + entrance
	}

	localURL := fmt.Sprintf("http://127.0.0.1:%s%s", port, entrancePath)
	pubURL := ""
	if publicIP != "" {
		pubURL = fmt.Sprintf("http://%s:%s%s", publicIP, port, entrancePath)
	}

	netbirdURL := ""
	if netbirdIP != "" && netbirdIP != "-" {
		netbirdURL = fmt.Sprintf("http://%s:%s%s", netbirdIP, port, entrancePath)
	}

	return PanelAccessInfo{
		PublicURL:        pubURL,
		NetbirdURL:       netbirdURL,
		LocalURL:         localURL,
		PublicIP:         publicIP,
		NetbirdIP:        netbirdIP,
		Port:             port,
		Username:         user,
		Password:         dispPass,
		PasswordIsCustom: isCustom,
		SecurityEntrance: entrancePath,
		AuthEnabled:      authEnabled,
	}
}

// ClientIP mengekstrak IP klien dengan memperhatikan header proxy.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		ip := strings.TrimSpace(xrip)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
