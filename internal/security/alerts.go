package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidateWebhookURL memastikan URL webhook menggunakan skema http/https yang valid.
func ValidateWebhookURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("URL webhook tidak valid (harus diawali http:// atau https://)")
	}
	return u.String(), nil
}

func severityRank(sev string) int {
	switch strings.ToUpper(strings.TrimSpace(sev)) {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

func (m *Manager) maybeDispatchWebhook(ev Event) {
	policy := m.GetPolicy()
	if strings.TrimSpace(policy.AlertWebhookURL) == "" {
		return
	}
	minSev := policy.AlertMinSeverity
	if minSev == "" {
		minSev = SeverityCritical
	}
	if severityRank(ev.Severity) < severityRank(minSev) {
		return
	}

	// Rate-limit webhook per IP+AttackType agar serangan flood tidak membanjiri channel alert
	key := ev.SourceIP + "|" + ev.AttackType
	now := time.Now()
	m.mu.Lock()
	if last, ok := m.webhookCooldown[key]; ok && now.Sub(last) < 60*time.Second {
		m.mu.Unlock()
		return
	}
	if len(m.webhookCooldown) > 2048 {
		m.webhookCooldown = make(map[string]time.Time)
	}
	m.webhookCooldown[key] = now
	m.mu.Unlock()

	go func(targetURL string, event Event) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.sendWebhookPayload(ctx, targetURL, event)
	}(policy.AlertWebhookURL, ev)
}

// SendTestWebhook mengirim notifikasi uji coba ke webhook yang dikonfigurasi.
func (m *Manager) SendTestWebhook(ctx context.Context, overrideURL string) error {
	target := strings.TrimSpace(overrideURL)
	if target == "" {
		target = m.GetPolicy().AlertWebhookURL
	}
	cleanURL, err := ValidateWebhookURL(target)
	if err != nil {
		return err
	}
	if cleanURL == "" {
		return fmt.Errorf("URL webhook alert belum diisi")
	}

	testEv := Event{
		Timestamp:   time.Now(),
		SourceIP:    "203.0.113.99",
		CountryCode: "TEST",
		AttackType:  AttackHoneypot,
		Severity:    SeverityCritical,
		Target:      "/.env",
		ActionTaken: ActionLogged,
		Details:     "Uji coba notifikasi webhook dari wdev Security Monitor",
	}
	return m.sendWebhookPayload(ctx, cleanURL, testEv)
}

func (m *Manager) sendWebhookPayload(ctx context.Context, webhookURL string, ev Event) error {
	title := fmt.Sprintf("[wdev Security] %s (%s)", ev.AttackType, ev.Severity)
	summary := fmt.Sprintf("%s • IP: %s (%s) -> %s [%s] | %s",
		title, ev.SourceIP, ev.CountryCode, ev.Target, ev.ActionTaken, ev.Details)

	payload := map[string]any{
		"title":        title,
		"text":         summary, // Kompatibel dengan Slack / Mattermost / Telegram relay
		"content":      summary, // Kompatibel dengan Discord webhook
		"timestamp":    ev.Timestamp.Format(time.RFC3339),
		"source_ip":    ev.SourceIP,
		"country_code": ev.CountryCode,
		"attack_type":  ev.AttackType,
		"severity":     ev.Severity,
		"target":       ev.Target,
		"action_taken": ev.ActionTaken,
		"details":      ev.Details,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wdev-security-monitor")

	client := m.webhookClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook merespons HTTP %d", resp.StatusCode)
	}
	return nil
}

// syncCloudflareAccessRule menambahkan atau mencabut IP Access Rule di Cloudflare WAF Zone API.
func (m *Manager) syncCloudflareAccessRule(ctx context.Context, ip, reason string, block bool) {
	policy := m.GetPolicy()
	if !policy.CFBlockSyncEnabled || strings.TrimSpace(policy.CFZoneID) == "" || strings.TrimSpace(policy.CFAPIToken) == "" {
		return
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || m.IsWhitelisted(parsed.String()) {
		return
	}
	cleanIP := parsed.String()

	go func(zoneID, token, targetIP, note string, isBlock bool) {
		base := strings.TrimRight(strings.TrimSpace(m.cfAPIBaseURL), "/")
		if base == "" {
			base = "https://api.cloudflare.com/client/v4"
		}
		client := m.webhookClient
		if client == nil {
			client = &http.Client{Timeout: 8 * time.Second}
		}
		reqCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if isBlock {
			payload := map[string]any{
				"mode": "block",
				"configuration": map[string]string{
					"target": "ip",
					"value":  targetIP,
				},
				"notes": "wdev Security Monitor: " + note,
			}
			b, _ := json.Marshal(payload)
			endpoint := fmt.Sprintf("%s/zones/%s/firewall/access_rules/rules", base, zoneID)
			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(b))
			if err != nil {
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
			}
			return
		}

		// Unblock: cari rule ID lalu hapus
		listURL := fmt.Sprintf("%s/zones/%s/firewall/access_rules/rules?configuration.target=ip&configuration.value=%s", base, zoneID, url.QueryEscape(targetIP))
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, listURL, nil)
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		var listRes struct {
			Success bool `json:"success"`
			Result  []struct {
				ID string `json:"id"`
			} `json:"result"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&listRes)
		_ = resp.Body.Close()

		for _, r := range listRes.Result {
			if r.ID == "" {
				continue
			}
			delURL := fmt.Sprintf("%s/zones/%s/firewall/access_rules/rules/%s", base, zoneID, r.ID)
			delReq, dErr := http.NewRequestWithContext(reqCtx, http.MethodDelete, delURL, nil)
			if dErr != nil {
				continue
			}
			delReq.Header.Set("Authorization", "Bearer "+token)
			if dResp, dErr2 := client.Do(delReq); dErr2 == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(dResp.Body, 2048))
				_ = dResp.Body.Close()
			}
		}
	}(policy.CFZoneID, policy.CFAPIToken, cleanIP, reason, block)
}
