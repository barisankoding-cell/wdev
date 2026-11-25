package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CloudflareZone merepresentasikan zona domain yang diambil dari Cloudflare REST API (/zones).
type CloudflareZone struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	AccountID   string `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	Plan        string `json:"plan,omitempty"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfAPIResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

type cfAPIEnvelope struct {
	Success    bool             `json:"success"`
	Errors     []cfAPIError     `json:"errors"`
	Messages   []cfAPIError     `json:"messages"`
	Result     json.RawMessage  `json:"result"`
	ResultInfo *cfAPIResultInfo `json:"result_info,omitempty"`
}

type cfZoneRaw struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Account struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
	Plan struct {
		Name string `json:"name"`
	} `json:"plan"`
}

type cfDNSRecordRaw struct {
	ID      string `json:"id"`
	ZoneID  string `json:"zone_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

type cfRemoteConfigEnvelope struct {
	Config map[string]any `json:"config"`
}

func (m *Manager) apiTokenFilePath(profile string) string {
	return filepath.Join(m.tokensDir(), profile+".apitoken")
}

// SetAPIBaseURL menyetel base URL Cloudflare REST API (berguna untuk pengujian/mock server).
func (m *Manager) SetAPIBaseURL(baseURL string) {
	if clean := strings.TrimRight(strings.TrimSpace(baseURL), "/"); clean != "" {
		m.apiBaseURL = clean
	}
}

func normalizeAPIToken(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	t = strings.Trim(t, "\"'`")
	if len(t) >= 7 && strings.EqualFold(t[:7], "Bearer ") {
		t = strings.TrimSpace(t[7:])
	}
	if t == "" {
		return "", nil
	}
	if strings.ContainsAny(t, " \t\r\n") {
		return "", errors.New("format Cloudflare API Token tidak valid (tidak boleh mengandung spasi atau baris baru)")
	}
	return t, nil
}

func (m *Manager) readProfileAPIToken(profile string) string {
	data, err := os.ReadFile(m.apiTokenFilePath(profile))
	if err != nil {
		return ""
	}
	tok, err := normalizeAPIToken(string(data))
	if err != nil {
		return ""
	}
	return tok
}

func (m *Manager) saveProfileAPIToken(profile, rawToken string) error {
	tok, err := normalizeAPIToken(rawToken)
	if err != nil {
		return err
	}
	path := m.apiTokenFilePath(profile)
	if tok == "" {
		if _, statErr := os.Stat(path); statErr == nil {
			if rmErr := os.Remove(path); rmErr != nil {
				_, _ = m.runPrivileged("rm", "-f", path)
			}
		}
		return nil
	}
	if err := m.ensureDirs(); err != nil {
		return err
	}
	return m.writeConfigFile(path, []byte(tok+"\n"), 0600)
}

// SetProfileAPIToken menyimpan atau menghapus Cloudflare API Token opsional untuk sebuah profil.
func (m *Manager) SetProfileAPIToken(profile, apiToken string) (*ProfileInfo, error) {
	name := normalizeCFProfile(profile)
	if err := validateCFProfile(name); err != nil {
		return nil, err
	}
	profiles := m.collectProfileMap()
	if _, ok := profiles[name]; !ok {
		return nil, fmt.Errorf("profil Cloudflare %q tidak ditemukan", name)
	}
	if err := m.saveProfileAPIToken(name, apiToken); err != nil {
		return nil, fmt.Errorf("gagal menyimpan Cloudflare API Token: %w", err)
	}
	st := m.GetStatus()
	for _, p := range st.ProfileDetails {
		if p.Name == name {
			cp := p
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("profil Cloudflare %q tidak ditemukan setelah pembaruan", name)
}

type cfHTTPStatusError struct {
	StatusCode int
	Message    string
}

func (e *cfHTTPStatusError) Error() string {
	return e.Message
}

func (m *Manager) doCFAPI(ctx context.Context, apiToken, method, endpoint string, reqBody any, outResult any, outInfo **cfAPIResultInfo) error {
	base := strings.TrimRight(strings.TrimSpace(m.apiBaseURL), "/")
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("gagal menyusun payload Cloudflare API: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+endpoint, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := m.httpClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal menghubungi Cloudflare API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("gagal membaca respons Cloudflare API: %w", err)
	}

	var env cfAPIEnvelope
	if len(respBytes) > 0 {
		if jErr := json.Unmarshal(respBytes, &env); jErr != nil && resp.StatusCode >= 400 {
			return &cfHTTPStatusError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("Cloudflare API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes))),
			}
		}
	}

	if resp.StatusCode >= 400 || !env.Success {
		var msgs []string
		for _, e := range env.Errors {
			if e.Code != 0 {
				msgs = append(msgs, fmt.Sprintf("[%d] %s", e.Code, e.Message))
			} else if e.Message != "" {
				msgs = append(msgs, e.Message)
			}
		}
		msg := strings.Join(msgs, "; ")
		if msg == "" {
			msg = fmt.Sprintf("Cloudflare API HTTP %d", resp.StatusCode)
		}
		return &cfHTTPStatusError{
			StatusCode: resp.StatusCode,
			Message:    msg,
		}
	}

	if outInfo != nil {
		*outInfo = env.ResultInfo
	}
	if outResult != nil && len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, outResult); err != nil {
			return fmt.Errorf("gagal mem-parsing hasil Cloudflare API: %w", err)
		}
	}
	return nil
}

func (m *Manager) listZonesWithToken(ctx context.Context, apiToken string) ([]CloudflareZone, error) {
	seen := make(map[string]bool)
	var zones []CloudflareZone

	for page := 1; page <= 10; page++ {
		var rawZones []cfZoneRaw
		var info *cfAPIResultInfo
		endpoint := fmt.Sprintf("/zones?per_page=50&page=%d", page)
		if err := m.doCFAPI(ctx, apiToken, http.MethodGet, endpoint, nil, &rawZones, &info); err != nil {
			return nil, err
		}
		for _, z := range rawZones {
			if z.ID == "" || seen[z.ID] {
				continue
			}
			seen[z.ID] = true
			zones = append(zones, CloudflareZone{
				ID:          z.ID,
				Name:        strings.ToLower(strings.TrimSpace(z.Name)),
				Status:      z.Status,
				AccountID:   z.Account.ID,
				AccountName: z.Account.Name,
				Plan:        z.Plan.Name,
			})
		}
		if info == nil || page >= info.TotalPages || len(rawZones) == 0 {
			break
		}
	}
	return zones, nil
}

// ListProfileZones mengambil daftar zona domain dari Cloudflare API menggunakan API Token milik profil.
func (m *Manager) ListProfileZones(profile string) ([]CloudflareZone, error) {
	name := normalizeCFProfile(profile)
	if err := validateCFProfile(name); err != nil {
		return nil, err
	}
	profiles := m.collectProfileMap()
	profInfo, ok := profiles[name]
	if !ok {
		return nil, fmt.Errorf("profil Cloudflare %q tidak ditemukan", name)
	}
	apiToken := m.readProfileAPIToken(name)
	if apiToken == "" {
		return nil, fmt.Errorf("profil %q belum memiliki Cloudflare API Token; atur API Token terlebih dahulu (izin Zone:Read)", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	zones, err := m.listZonesWithToken(ctx, apiToken)
	if err != nil {
		return nil, err
	}

	sort.Slice(zones, func(i, j int) bool {
		if profInfo.AccountTag != "" {
			matchI := zones[i].AccountID == profInfo.AccountTag
			matchJ := zones[j].AccountID == profInfo.AccountTag
			if matchI != matchJ {
				return matchI
			}
		}
		actI := zones[i].Status == "active"
		actJ := zones[j].Status == "active"
		if actI != actJ {
			return actI
		}
		return zones[i].Name < zones[j].Name
	})
	if zones == nil {
		zones = []CloudflareZone{}
	}
	return zones, nil
}

func (m *Manager) findZoneForHost(ctx context.Context, apiToken, hostname string) (*CloudflareZone, error) {
	zones, err := m.listZonesWithToken(ctx, apiToken)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(strings.TrimSpace(hostname))
	var best *CloudflareZone
	for i := range zones {
		z := &zones[i]
		if host == z.Name || strings.HasSuffix(host, "."+z.Name) {
			if best == nil || len(z.Name) > len(best.Name) {
				best = z
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("zona Cloudflare untuk domain %q tidak ditemukan pada API Token ini", hostname)
	}
	return best, nil
}

func (m *Manager) upsertCFDNSRecord(ctx context.Context, apiToken, publicHost, targetCNAME string) error {
	zone, err := m.findZoneForHost(ctx, apiToken, publicHost)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("/zones/%s/dns_records?name=%s", url.PathEscape(zone.ID), url.QueryEscape(publicHost))
	var existing []cfDNSRecordRaw
	if err := m.doCFAPI(ctx, apiToken, http.MethodGet, endpoint, nil, &existing, nil); err != nil {
		return err
	}

	var cnameMatch *cfDNSRecordRaw
	var conflictIDs []string
	for i := range existing {
		rec := &existing[i]
		if !strings.EqualFold(rec.Name, publicHost) {
			continue
		}
		switch strings.ToUpper(rec.Type) {
		case "CNAME":
			if cnameMatch == nil {
				cnameMatch = rec
			}
		case "A", "AAAA":
			conflictIDs = append(conflictIDs, rec.ID)
		}
	}

	payload := map[string]any{
		"type":    "CNAME",
		"name":    publicHost,
		"content": targetCNAME,
		"ttl":     1,
		"proxied": true,
	}

	if cnameMatch != nil {
		if strings.EqualFold(cnameMatch.Content, targetCNAME) && cnameMatch.Proxied {
			return nil
		}
		putEndpoint := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zone.ID), url.PathEscape(cnameMatch.ID))
		return m.doCFAPI(ctx, apiToken, http.MethodPut, putEndpoint, payload, nil, nil)
	}

	for _, id := range conflictIDs {
		delEndpoint := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zone.ID), url.PathEscape(id))
		if err := m.doCFAPI(ctx, apiToken, http.MethodDelete, delEndpoint, nil, nil, nil); err != nil {
			return err
		}
	}

	postEndpoint := fmt.Sprintf("/zones/%s/dns_records", url.PathEscape(zone.ID))
	return m.doCFAPI(ctx, apiToken, http.MethodPost, postEndpoint, payload, nil, nil)
}

func (m *Manager) deleteCFDNSRecordByHost(ctx context.Context, apiToken, publicHost, targetCNAME string) (bool, error) {
	zone, err := m.findZoneForHost(ctx, apiToken, publicHost)
	if err != nil {
		return false, err
	}
	endpoint := fmt.Sprintf("/zones/%s/dns_records?type=CNAME&name=%s", url.PathEscape(zone.ID), url.QueryEscape(publicHost))
	var existing []cfDNSRecordRaw
	if err := m.doCFAPI(ctx, apiToken, http.MethodGet, endpoint, nil, &existing, nil); err != nil {
		return false, err
	}
	deleted := false
	for _, rec := range existing {
		if !strings.EqualFold(rec.Name, publicHost) || !strings.EqualFold(rec.Type, "CNAME") {
			continue
		}
		if targetCNAME != "" && !strings.EqualFold(rec.Content, targetCNAME) {
			continue
		}
		delEndpoint := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zone.ID), url.PathEscape(rec.ID))
		if err := m.doCFAPI(ctx, apiToken, http.MethodDelete, delEndpoint, nil, nil, nil); err != nil {
			return deleted, err
		}
		deleted = true
	}
	return deleted, nil
}

func extractRemoteIngressSlice(cfgMap map[string]any) []map[string]any {
	raw, ok := cfgMap["ingress"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func remoteRuleHostHeader(rule map[string]any) string {
	orig, ok := rule["originRequest"].(map[string]any)
	if !ok {
		return ""
	}
	h, _ := orig["httpHostHeader"].(string)
	return h
}

func isCatchAllRemoteRule(rule map[string]any) bool {
	host, _ := rule["hostname"].(string)
	path, _ := rule["path"].(string)
	return strings.TrimSpace(host) == "" && strings.TrimSpace(path) == ""
}

func (m *Manager) upsertRemoteTunnelIngress(
	ctx context.Context,
	apiToken, accountID, tunnelID, publicHost, serviceURL, httpHostHeader string,
	noTLSVerify bool,
) error {
	endpoint := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", url.PathEscape(accountID), url.PathEscape(tunnelID))
	var current cfRemoteConfigEnvelope
	if err := m.doCFAPI(ctx, apiToken, http.MethodGet, endpoint, nil, &current, nil); err != nil {
		return err
	}
	cfgMap := current.Config
	if cfgMap == nil {
		cfgMap = make(map[string]any)
	}

	originReq := map[string]any{
		"httpHostHeader": httpHostHeader,
	}
	if noTLSVerify {
		originReq["noTLSVerify"] = true
	}
	newRule := map[string]any{
		"hostname":      publicHost,
		"service":       serviceURL,
		"originRequest": originReq,
	}

	existing := extractRemoteIngressSlice(cfgMap)
	var newRules []map[string]any
	var catchAll map[string]any
	replaced := false
	for _, rule := range existing {
		if isCatchAllRemoteRule(rule) {
			if svc, _ := rule["service"].(string); svc != "" && svc != "http_status:404" {
				catchAll = rule
			}
			continue
		}
		h, _ := rule["hostname"].(string)
		p, _ := rule["path"].(string)
		if strings.EqualFold(h, publicHost) && strings.TrimSpace(p) == "" {
			newRules = append(newRules, newRule)
			replaced = true
		} else {
			newRules = append(newRules, rule)
		}
	}
	if !replaced {
		newRules = append(newRules, newRule)
	}
	if catchAll == nil {
		catchAll = map[string]any{"service": "http_status:404"}
	}
	newRules = append(newRules, catchAll)
	cfgMap["ingress"] = newRules

	return m.doCFAPI(ctx, apiToken, http.MethodPut, endpoint, map[string]any{"config": cfgMap}, nil, nil)
}

func (m *Manager) removeRemoteTunnelIngress(ctx context.Context, apiToken, accountID, tunnelID, publicHost, localDomain string) error {
	endpoint := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", url.PathEscape(accountID), url.PathEscape(tunnelID))
	var current cfRemoteConfigEnvelope
	if err := m.doCFAPI(ctx, apiToken, http.MethodGet, endpoint, nil, &current, nil); err != nil {
		return err
	}
	cfgMap := current.Config
	if cfgMap == nil {
		return nil
	}
	existing := extractRemoteIngressSlice(cfgMap)
	if len(existing) == 0 {
		return nil
	}

	var newRules []map[string]any
	var catchAll map[string]any
	removed := false
	for _, rule := range existing {
		if isCatchAllRemoteRule(rule) {
			catchAll = rule
			continue
		}
		h, _ := rule["hostname"].(string)
		matchHost := publicHost != "" && strings.EqualFold(h, publicHost)
		matchHeader := localDomain != "" && strings.EqualFold(remoteRuleHostHeader(rule), localDomain)
		if matchHost || matchHeader {
			removed = true
			continue
		}
		newRules = append(newRules, rule)
	}
	if !removed {
		return nil
	}
	if catchAll == nil {
		catchAll = map[string]any{"service": "http_status:404"}
	}
	newRules = append(newRules, catchAll)
	cfgMap["ingress"] = newRules

	return m.doCFAPI(ctx, apiToken, http.MethodPut, endpoint, map[string]any{"config": cfgMap}, nil, nil)
}

func (m *Manager) deleteRemoteTunnelViaAPI(ctx context.Context, apiToken, accountID, tunnelID string) error {
	connEndpoint := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/connections", url.PathEscape(accountID), url.PathEscape(tunnelID))
	_ = m.doCFAPI(ctx, apiToken, http.MethodDelete, connEndpoint, nil, nil, nil)

	delEndpoint := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", url.PathEscape(accountID), url.PathEscape(tunnelID))
	err := m.doCFAPI(ctx, apiToken, http.MethodDelete, delEndpoint, nil, nil, nil)
	if err != nil {
		var statusErr *cfHTTPStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
			return nil
		}
	}
	return err
}
