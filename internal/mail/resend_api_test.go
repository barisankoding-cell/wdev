package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"wdev/internal/config"
	"wdev/internal/pdns"
)

type fakePDNSServer struct {
	mu    sync.Mutex
	zones map[string]*pdns.ZoneInfo
}

func newFakePDNSServer(t *testing.T, initialZones ...string) (*fakePDNSServer, *httptest.Server) {
	t.Helper()
	fp := &fakePDNSServer{zones: make(map[string]*pdns.ZoneInfo)}
	for _, z := range initialZones {
		zn := strings.TrimSuffix(z, ".") + "."
		fp.zones[zn] = &pdns.ZoneInfo{
			ID:   zn,
			Name: zn,
			Kind: "Native",
			RRsets: []pdns.RRSet{
				{Name: zn, Type: "SOA", TTL: 3600, Records: []pdns.Record{{Content: "ns1." + zn + " hostmaster." + zn + " 1795165945 10800 3600 604800 3600"}}},
				{Name: zn, Type: "NS", TTL: 3600, Records: []pdns.Record{{Content: "ns1." + zn}, {Content: "ns2." + zn}}},
			},
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp.mu.Lock()
		defer fp.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/api/v1/servers/localhost")
		switch {
		case p == "" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"localhost"}`))
		case p == "/zones" && r.Method == http.MethodGet:
			list := make([]pdns.ZoneInfo, 0, len(fp.zones))
			for _, zi := range fp.zones {
				list = append(list, *zi)
			}
			_ = json.NewEncoder(w).Encode(list)
		case p == "/zones" && r.Method == http.MethodPost:
			var req struct {
				Name        string   `json:"name"`
				Nameservers []string `json:"nameservers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			zn := strings.TrimSuffix(req.Name, ".") + "."
			if _, ok := fp.zones[zn]; ok {
				w.WriteHeader(http.StatusConflict)
				return
			}
			zi := &pdns.ZoneInfo{ID: zn, Name: zn, Kind: "Native"}
			if len(req.Nameservers) > 0 {
				var nsRecs []pdns.Record
				for _, ns := range req.Nameservers {
					nsRecs = append(nsRecs, pdns.Record{Content: strings.TrimSuffix(ns, ".") + "."})
				}
				zi.RRsets = append(zi.RRsets, pdns.RRSet{Name: zn, Type: "NS", TTL: 3600, Records: nsRecs})
			}
			fp.zones[zn] = zi
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(zi)
		case strings.HasPrefix(p, "/zones/") && strings.Contains(p, "/metadata/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(p, "/zones/") && r.Method == http.MethodGet:
			zn := strings.TrimPrefix(p, "/zones/")
			zi, ok := fp.zones[zn]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(zi)
		case strings.HasPrefix(p, "/zones/") && r.Method == http.MethodPatch:
			zn := strings.TrimPrefix(p, "/zones/")
			zi, ok := fp.zones[zn]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var patch struct {
				RRsets []struct {
					Name       string `json:"name"`
					Type       string `json:"type"`
					TTL        int    `json:"ttl"`
					Changetype string `json:"changetype"`
					Records    []struct {
						Content  string `json:"content"`
						Disabled bool   `json:"disabled"`
					} `json:"records"`
				} `json:"rrsets"`
			}
			_ = json.NewDecoder(r.Body).Decode(&patch)
			for _, rr := range patch.RRsets {
				if rr.Type == "TXT" {
					for _, rec := range rr.Records {
						// Simulasikan batas PowerDNS Authoritative 255 byte per quoted string chunk
						if strings.HasPrefix(rec.Content, `"`) && strings.HasSuffix(rec.Content, `"`) && !strings.Contains(rec.Content, `" "`) && len(rec.Content) > 257 {
							w.WriteHeader(http.StatusUnprocessableEntity)
							_, _ = w.Write([]byte(`TXT string chunk > 255 bytes`))
							return
						}
					}
				}
				filtered := make([]pdns.RRSet, 0, len(zi.RRsets))
				for _, ex := range zi.RRsets {
					if !(ex.Name == rr.Name && strings.EqualFold(ex.Type, rr.Type)) {
						filtered = append(filtered, ex)
					}
				}
				if strings.ToUpper(rr.Changetype) == "REPLACE" {
					recs := make([]pdns.Record, 0, len(rr.Records))
					for _, rec := range rr.Records {
						recs = append(recs, pdns.Record{Content: rec.Content, Disabled: rec.Disabled})
					}
					filtered = append(filtered, pdns.RRSet{Name: rr.Name, Type: rr.Type, TTL: rr.TTL, Records: recs})
				}
				zi.RRsets = filtered
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return fp, srv
}

func configureFakePDNS(t *testing.T, cfg *config.Config, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	content := "VPS_NETBIRD_IP=\"" + u.Hostname() + "\"\nVPS_PUBLIC_IP=\"103.171.85.72\"\nAPI_KEY=\"test-key\"\nPDNS_PORT=\"" + u.Port() + "\"\n"
	if err := os.WriteFile(filepath.Join(cfg.WdevEtc, "edge-dns.conf"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSyncResendDomainDNSBuildsPowerDNSAndVerifiesIdempotently(t *testing.T) {
	mgr, cfg := newTestMailManager(t, "edge")
	fp, pdnsSrv := newFakePDNSServer(t, "nikaget.my.id")
	configureFakePDNS(t, cfg, pdnsSrv)

	// Panjang > 260 byte untuk menguji pemecahan chunk TXT > 255 byte di PowerDNS
	longDKIM := "p=" + strings.Repeat("ABCDEF0123456789", 20)

	var createdDomain, verifyCalled, restrictedMode bool
	var mu sync.Mutex
	resendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if restrictedMode {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"statusCode":401,"name":"restricted_api_key","message":"This API key is restricted to only send emails"}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer re_live_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains":
			if createdDomain {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{{"id": "dom-123", "name": "nikaget.my.id", "status": "not_started"}},
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/domains":
			createdDomain = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "dom-123",
				"name":   "nikaget.my.id",
				"status": "not_started",
				"records": []map[string]any{
					{"record": "DKIM", "name": "resend._domainkey", "type": "TXT", "value": longDKIM, "ttl": "Auto"},
					{"record": "SPF", "name": "send", "type": "MX", "value": "feedback-smtp.us-east-1.amazonses.com", "priority": 10, "ttl": "Auto"},
					{"record": "SPF", "name": "send", "type": "TXT", "value": "v=spf1 include:amazonses.com ~all", "ttl": "Auto"},
					{"record": "Receiving", "name": "@", "type": "MX", "value": "inbound-smtp.us-east-1.amazonaws.com", "priority": 10, "ttl": "Auto"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/domains/dom-123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "dom-123",
				"name":   "nikaget.my.id",
				"status": "pending",
				"records": []map[string]any{
					{"record": "DKIM", "name": "resend._domainkey", "type": "TXT", "value": longDKIM, "ttl": "Auto"},
					{"record": "SPF", "name": "send", "type": "MX", "value": "feedback-smtp.us-east-1.amazonses.com", "priority": 10, "ttl": "Auto"},
					{"record": "SPF", "name": "send", "type": "TXT", "value": "v=spf1 include:amazonses.com ~all", "ttl": "Auto"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/domains/dom-123/verify":
			verifyCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "domain", "id": "dom-123"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer resendSrv.Close()
	mgr.SetResendAPIBase(resendSrv.URL)

	// Simpan kredensial domain relay Resend untuk menguji fallback API key tersimpan
	if err := mgr.saveDomainRelayCred("nikaget.my.id", relayCredential{
		Provider:    "resend",
		Host:        "smtp.resend.com",
		Port:        465,
		ImplicitTLS: true,
		Username:    "resend",
		Password:    "re_live_secret",
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Sinkronisasi awal memakai API key tersimpan (empty arg) -> mendaftarkan domain + pasang DNS + verify
	res, err := mgr.SyncResendDomainDNS("nikaget.my.id", "", true)
	if err != nil {
		t.Fatalf("SyncResendDomainDNS gagal: %v", err)
	}
	if !createdDomain || !verifyCalled {
		t.Fatalf("expected Resend POST /domains & /verify called, got created=%v verify=%v", createdDomain, verifyCalled)
	}
	if len(res.Report.Added) != 3 {
		t.Fatalf("expected 3 DNS records created, got added=%v", res.Report.Added)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "MX inbound Stalwart") {
		t.Fatalf("expected apex Receiving MX skipped to preserve Stalwart MX, got skipped=%v", res.Skipped)
	}

	// Pastikan record benar-benar tertulis di PowerDNS & mail-relay-dns.json
	dkimGot, err := mgr.pdnsMgr.LookupRecords("nikaget.my.id", "resend._domainkey", "TXT")
	if err != nil || len(dkimGot) != 1 || normalizeTXTContent(dkimGot[0]) != longDKIM {
		t.Fatalf("DKIM TXT di PowerDNS tidak cocok: %v (err=%v)", dkimGot, err)
	}
	sendMXGot, err := mgr.pdnsMgr.LookupRecords("nikaget.my.id", "send", "MX")
	if err != nil || len(sendMXGot) != 1 || sendMXGot[0] != "10 feedback-smtp.us-east-1.amazonses.com." {
		t.Fatalf("send MX di PowerDNS tidak cocok: %v (err=%v)", sendMXGot, err)
	}
	if tracked := mgr.ListRelayDNS("nikaget.my.id"); len(tracked) != 3 {
		t.Fatalf("expected 3 tracked records in mail-relay-dns.json, got %+v", tracked)
	}

	// 2. Jalankan ulang sinkronisasi -> harus idempoten (0 added, 3 kept meski TXT di-chunk >255 byte!)
	res2, err := mgr.SyncResendDomainDNS("nikaget.my.id", "", false)
	if err != nil {
		t.Fatalf("SyncResendDomainDNS ulang gagal: %v", err)
	}
	if len(res2.Report.Added) != 0 || len(res2.Report.Kept) != 3 {
		t.Fatalf("expected idempotent 0 added & 3 kept, got added=%v kept=%v", res2.Report.Added, res2.Report.Kept)
	}

	// 3. Simulasikan seluruh zone hilang / record Resend dihapus lalu panggil RepairDomain secara langsung
	fp.mu.Lock()
	delete(fp.zones, "nikaget.my.id.")
	fp.mu.Unlock()
	_ = os.Remove(mgr.relayDNSPath())
	mu.Lock()
	verifyCalled = false
	mu.Unlock()

	var logs []string
	// Setel installer no-op agar RepairDomain tidak memblokir sebelum menguji jalur DNS + Resend
	mgr.SetInstaller(func(_ context.Context, _ func(string), _ string) error {
		return nil
	})
	rep, _ := mgr.RepairDomain("nikaget.my.id", func(s string) {
		logs = append(logs, s)
	})
	if rep.Zone == "" {
		t.Fatalf("expected RepairDomain to auto-create zone nikaget.my.id, got report=%+v logs=%v", rep, logs)
	}
	if mxAfter, _ := mgr.pdnsMgr.LookupRecords("nikaget.my.id", "@", "MX"); len(mxAfter) != 1 || mxAfter[0] != "10 mail.nikaget.my.id." {
		t.Fatalf("expected MX inbound Stalwart dibuat oleh RepairDomain, got %v (logs=%v)", mxAfter, logs)
	}
	if spfAfter, _ := mgr.pdnsMgr.LookupRecords("nikaget.my.id", "@", "TXT"); len(spfAfter) != 1 || normalizeTXTContent(spfAfter[0]) != "v=spf1 -all" {
		t.Fatalf("expected apex SPF -all saat pakai relay eksternal, got %v", spfAfter)
	}
	if dkimAfter, _ := mgr.pdnsMgr.LookupRecords("nikaget.my.id", "resend._domainkey", "TXT"); len(dkimAfter) != 1 || normalizeTXTContent(dkimAfter[0]) != longDKIM {
		t.Fatalf("expected DKIM TXT Resend dipulihkan oleh RepairDomain, got %v (logs=%v)", dkimAfter, logs)
	}
	if sendSPF, _ := mgr.pdnsMgr.LookupRecords("nikaget.my.id", "send", "TXT"); len(sendSPF) != 1 {
		t.Fatalf("expected send TXT SPF Resend dipulihkan oleh RepairDomain, got %v", sendSPF)
	}
	mu.Lock()
	if !verifyCalled {
		mu.Unlock()
		t.Fatalf("expected RepairDomain to trigger Resend verify")
	}
	mu.Unlock()

	// 4. Restricted / Sending-Only Resend API key menghasilkan pesan error yang jelas
	mu.Lock()
	restrictedMode = true
	mu.Unlock()
	if _, err := mgr.SyncResendDomainDNS("nikaget.my.id", "re_send_only", false); err == nil || !strings.Contains(err.Error(), "Full Access") {
		t.Fatalf("expected clear Full Access guidance error for restricted key, got %v", err)
	}
}
