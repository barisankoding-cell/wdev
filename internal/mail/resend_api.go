package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultResendAPIBase = "https://api.resend.com"

type flexPriority int

func (f *flexPriority) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(strings.Trim(string(data), `"`))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	*f = flexPriority(v)
	return nil
}

// ResendDNSRecord mewakili satu record DNS domain dari REST API Resend.
type ResendDNSRecord struct {
	Record   string       `json:"record"`
	Name     string       `json:"name"`
	Type     string       `json:"type"`
	Value    string       `json:"value"`
	Priority flexPriority `json:"priority,omitempty"`
	TTL      string       `json:"ttl,omitempty"`
	Status   string       `json:"status,omitempty"`
}

// ResendDomainInfo merangkum domain & daftar DNS dari API Resend.
type ResendDomainInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Region  string            `json:"region,omitempty"`
	Records []ResendDNSRecord `json:"records,omitempty"`
}

// ResendSyncResult adalah hasil sinkronisasi DNS domain Resend ke PowerDNS.
type ResendSyncResult struct {
	Domain                string               `json:"domain"`
	DomainID              string               `json:"domain_id,omitempty"`
	DomainStatus          string               `json:"domain_status,omitempty"`
	VerificationTriggered bool                 `json:"verification_triggered,omitempty"`
	Records               []ManagedRelayRecord `json:"records"`
	Skipped               []string             `json:"skipped,omitempty"`
	Report                RelayDNSReport       `json:"report"`
}

func (m *Manager) resendAPIBase() string {
	if m != nil && strings.TrimSpace(m.resendAPIBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(m.resendAPIBaseURL), "/")
	}
	return defaultResendAPIBase
}

// SetResendAPIBase mengganti base URL Resend API untuk pengujian terisolasi.
func (m *Manager) SetResendAPIBase(baseURL string) {
	if m != nil {
		m.resendAPIBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
}

func (m *Manager) resendDo(ctx context.Context, apiKey, method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, m.resendAPIBase()+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gagal terhubung ke Resend API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBytes, resp.StatusCode, nil
	}

	var apiErr struct {
		Name       string `json:"name"`
		Message    string `json:"message"`
		StatusCode int    `json:"statusCode"`
	}
	_ = json.Unmarshal(respBytes, &apiErr)
	msg := strings.TrimSpace(apiErr.Message)
	if msg == "" {
		msg = strings.TrimSpace(string(respBytes))
	}
	low := strings.ToLower(apiErr.Name + " " + msg)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || strings.Contains(low, "restricted") {
		return respBytes, resp.StatusCode, fmt.Errorf("API Key Resend tidak diizinkan mengakses endpoint Domains (%s). Gunakan API Key Full Access atau tempel record DNS manual dari dashboard Resend", msg)
	}
	return respBytes, resp.StatusCode, fmt.Errorf("Resend API %s %s merespons %d: %s", method, path, resp.StatusCode, msg)
}

func resolveResendRecordFQDN(name, domain string) string {
	cleanDom := cleanMailDomain(domain)
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if n == "" || n == "@" || n == cleanDom {
		return cleanDom
	}
	if strings.HasSuffix(n, "."+cleanDom) {
		return n
	}
	return n + "." + cleanDom
}

func resendRecordsToManaged(domain string, recs []ResendDNSRecord) ([]ManagedRelayRecord, []string) {
	managed := make([]ManagedRelayRecord, 0, len(recs))
	var skipped []string
	seen := make(map[string]bool, len(recs))

	for _, r := range recs {
		rtype := strings.ToUpper(strings.TrimSpace(r.Type))
		val := strings.TrimSpace(r.Value)
		if rtype == "" || val == "" {
			continue
		}
		fqdn := resolveResendRecordFQDN(r.Name, domain)

		// Jangan menimpa MX inbound Stalwart di apex (@) oleh record Receiving opsional milik Resend.
		if strings.EqualFold(strings.TrimSpace(r.Record), "Receiving") && fqdn == cleanMailDomain(domain) && rtype == "MX" {
			skipped = append(skipped, fmt.Sprintf("MX %s (%s) dilewati agar MX inbound Stalwart tetap aman", fqdn, val))
			continue
		}

		content := val
		switch rtype {
		case "MX":
			prio := int(r.Priority)
			fields := strings.Fields(val)
			if len(fields) == 1 {
				if prio <= 0 {
					prio = 10
				}
				content = fmt.Sprintf("%d %s.", prio, strings.TrimSuffix(val, "."))
			} else {
				fields[len(fields)-1] = strings.TrimSuffix(fields[len(fields)-1], ".") + "."
				content = strings.Join(fields, " ")
			}
		case "CNAME":
			content = strings.TrimSuffix(val, ".") + "."
		}

		item := ManagedRelayRecord{
			Name:     fqdn,
			Type:     rtype,
			Content:  content,
			Provider: "resend",
		}
		k := relayRecordKey(item)
		if seen[k] {
			continue
		}
		seen[k] = true
		managed = append(managed, item)
	}
	return managed, skipped
}

func (m *Manager) ensureResendDomain(ctx context.Context, domain, apiKey string) (ResendDomainInfo, error) {
	var out ResendDomainInfo
	listBytes, _, err := m.resendDo(ctx, apiKey, http.MethodGet, "/domains?limit=100", nil)
	if err != nil {
		return out, err
	}
	var listResp struct {
		Data []ResendDomainInfo `json:"data"`
	}
	if err := json.Unmarshal(listBytes, &listResp); err != nil {
		return out, fmt.Errorf("gagal mengurai daftar domain Resend: %w", err)
	}

	cleanDom := cleanMailDomain(domain)
	var existingID string
	for _, d := range listResp.Data {
		if strings.EqualFold(cleanMailDomain(d.Name), cleanDom) {
			existingID = d.ID
			break
		}
	}

	if existingID != "" {
		raw, _, err := m.resendDo(ctx, apiKey, http.MethodGet, "/domains/"+existingID, nil)
		if err != nil {
			return out, err
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return out, fmt.Errorf("gagal mengurai detail domain Resend: %w", err)
		}
		return out, nil
	}

	// Domain belum terdaftar di akun Resend -> daftarkan otomatis agar langsung mendapat DKIM/SPF/MX.
	raw, _, err := m.resendDo(ctx, apiKey, http.MethodPost, "/domains", map[string]any{"name": cleanDom})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("gagal mengurai domain Resend baru: %w", err)
	}
	if len(out.Records) == 0 && out.ID != "" {
		detail, _, derr := m.resendDo(ctx, apiKey, http.MethodGet, "/domains/"+out.ID, nil)
		if derr == nil {
			_ = json.Unmarshal(detail, &out)
		}
	}
	return out, nil
}

// SyncResendDomainDNS mengambil (atau mendaftarkan) domain di Resend memakai API Key,
// memasang seluruh record DNS (DKIM TXT, bounce send MX/SPF, Tracking) ke PowerDNS,
// menyimpan jejak di mail-relay-dns.json, dan opsional memicu verifikasi di Resend.
func (m *Manager) SyncResendDomainDNS(domain, apiKey string, triggerVerify bool) (ResendSyncResult, error) {
	cleanDom := cleanMailDomain(domain)
	res := ResendSyncResult{
		Domain:  cleanDom,
		Records: []ManagedRelayRecord{},
		Report: RelayDNSReport{
			Domain:   cleanDom,
			Provider: "resend",
			Removed:  []string{},
			Added:    []string{},
			Kept:     []string{},
			Manual:   []string{},
		},
	}
	if cleanDom == "" || !validMailDomain(cleanDom) {
		return res, fmt.Errorf("domain tidak valid: %s", domain)
	}

	key := strings.TrimSpace(apiKey)
	if key == "" {
		if cred, ok := m.effectiveRelayCred(cleanDom); ok && strings.EqualFold(cred.Provider, "resend") {
			key = strings.TrimSpace(cred.Password)
		}
	}
	if key == "" {
		return res, fmt.Errorf("API Key Resend untuk %s belum tersedia", cleanDom)
	}

	domInfo, err := m.ensureResendDomain(context.Background(), cleanDom, key)
	if err != nil {
		res.Report.Manual = append(res.Report.Manual, err.Error())
		return res, err
	}
	res.DomainID = domInfo.ID
	res.DomainStatus = domInfo.Status

	managed, skipped := resendRecordsToManaged(cleanDom, domInfo.Records)
	res.Records = managed
	res.Skipped = skipped
	if len(managed) == 0 {
		res.Report.Manual = append(res.Report.Manual, "Resend API tidak mengembalikan record DNS untuk domain "+cleanDom)
		return res, nil
	}

	dnsRep, syncErr := m.ReconcileProviderRelayDNS(cleanDom, "resend", managed)
	for _, s := range skipped {
		dnsRep.Manual = append(dnsRep.Manual, s)
	}
	res.Report = dnsRep
	if syncErr != nil {
		dnsRep.Manual = append(dnsRep.Manual, syncErr.Error())
		res.Report = dnsRep
		return res, syncErr
	}

	if triggerVerify && domInfo.ID != "" && !strings.EqualFold(domInfo.Status, "verified") {
		if _, _, verr := m.resendDo(context.Background(), key, http.MethodPost, "/domains/"+domInfo.ID+"/verify", nil); verr == nil {
			res.VerificationTriggered = true
			dnsRep.VerificationTriggered = true
			res.Report = dnsRep
		} else {
			dnsRep.Manual = append(dnsRep.Manual, "Verifikasi otomatis Resend menunggu propagasi DNS: "+verr.Error())
			res.Report = dnsRep
		}
	}
	return res, nil
}
