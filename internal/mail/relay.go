package mail

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mode relay-only: pengiriman langsung via port 25 tidak dipakai. Semua email
// keluar harus lewat relay, dengan satu relay default sebagai fallback.
const (
	RelayScopeAll     = "global"
	DefaultRelayRoute = "relay"
)

const relayOnlyMode = "mode relay-only"

type relayCredential struct {
	Provider    string `json:"provider"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	ImplicitTLS bool   `json:"implicitTls"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Route       string `json:"route,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type relayBranch struct {
	If    string
	Then  string
	Extra map[string]any
}

type outboundRoute struct {
	Else     string
	Branches []relayBranch
	Raw      map[string]any
}

// OutboundRouteInfo adalah kondisi efektif routing outbound yang dibaca dari
// Stalwart, bukan dari file kredensial.
type OutboundRouteInfo struct {
	RelayOnly bool              `json:"relay_only"`
	Default   string            `json:"default"`
	Domains   map[string]string `json:"domains"`
}

func relayRouteName(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || domain == RelayScopeAll {
		return DefaultRelayRoute
	}
	return "relay_" + strings.ReplaceAll(domain, ".", "_")
}

func relayMatchCondition(domain string) string {
	return fmt.Sprintf("sender_domain == '%s'", strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), ".")))
}

func domainFromCondition(cond string) (string, bool) {
	const prefix = "sender_domain =="
	c := strings.TrimSpace(cond)
	if !strings.HasPrefix(c, prefix) {
		return "", false
	}
	domain := strings.Trim(strings.TrimSpace(strings.TrimPrefix(c, prefix)), `'"`)
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return "", false
	}
	return domain, true
}

func relayRouteExpr(route string) string {
	return fmt.Sprintf("'%s'", route)
}

func matchKeyLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	if aerr == nil {
		return true
	}
	if berr == nil {
		return false
	}
	return a < b
}

func parseOutboundRoute(strat map[string]any) (*outboundRoute, error) {
	routeRaw, _ := strat["route"].(map[string]any)
	if routeRaw == nil {
		routeRaw = make(map[string]any)
	}
	res := &outboundRoute{Raw: routeRaw}
	if elseStr, ok := routeRaw["else"].(string); ok {
		res.Else = strings.Trim(strings.TrimSpace(elseStr), `'"`)
	}
	match, ok := routeRaw["match"].(map[string]any)
	if !ok {
		return res, nil
	}
	keys := make([]string, 0, len(match))
	for k := range match {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return matchKeyLess(keys[i], keys[j]) })
	for _, k := range keys {
		vm, ok := match[k].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entri match %q pada MtaOutboundStrategy tidak dikenali", k)
		}
		ifStr, _ := vm["if"].(string)
		thenStr, _ := vm["then"].(string)
		extra := make(map[string]any, len(vm))
		for ek, ev := range vm {
			if ek != "if" && ek != "then" {
				extra[ek] = ev
			}
		}
		res.Branches = append(res.Branches, relayBranch{
			If:    strings.TrimSpace(ifStr),
			Then:  strings.Trim(strings.TrimSpace(thenStr), `'"`),
			Extra: extra,
		})
	}
	return res, nil
}

// marshal mempertahankan field lain pada route, menulis ulang match secara
// terurut 0..n agar hasil update deterministik.
func (r *outboundRoute) marshal() map[string]any {
	route := make(map[string]any, len(r.Raw)+2)
	for k, v := range r.Raw {
		route[k] = v
	}
	delete(route, "match")
	if len(r.Branches) > 0 {
		match := make(map[string]any, len(r.Branches))
		for i, b := range r.Branches {
			vm := make(map[string]any, len(b.Extra)+2)
			for k, v := range b.Extra {
				vm[k] = v
			}
			vm["if"] = b.If
			vm["then"] = relayRouteExpr(b.Then)
			match[strconv.Itoa(i)] = vm
		}
		route["match"] = match
	}
	if r.Else != "" {
		route["else"] = relayRouteExpr(r.Else)
	}
	return route
}

func (r *outboundRoute) upsertDomain(domain, routeName string) {
	cond := relayMatchCondition(domain)
	for i := range r.Branches {
		if strings.TrimSpace(r.Branches[i].If) == cond {
			r.Branches[i].Then = routeName
			return
		}
	}
	r.Branches = append(r.Branches, relayBranch{If: cond, Then: routeName})
}

func (r *outboundRoute) removeDomain(domain string) bool {
	cond := relayMatchCondition(domain)
	kept := make([]relayBranch, 0, len(r.Branches))
	for _, b := range r.Branches {
		if strings.TrimSpace(b.If) != cond {
			kept = append(kept, b)
		}
	}
	removed := len(kept) != len(r.Branches)
	r.Branches = kept
	return removed
}

func (r *outboundRoute) hasDomain(domain string) bool {
	cond := relayMatchCondition(domain)
	for _, b := range r.Branches {
		if strings.TrimSpace(b.If) == cond {
			return true
		}
	}
	return false
}

// normalizeExpr membuat perbandingan tahan terhadap perbedaan kutip/spasi
// yang mungkin dinormalisasi oleh Stalwart saat menyimpan strategy.
func normalizeExpr(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, `"`, `'`)
	return strings.Join(strings.Fields(v), " ")
}

func routeBranchMap(branches []relayBranch) map[string]string {
	res := make(map[string]string, len(branches))
	for _, b := range branches {
		res[normalizeExpr(b.If)] = strings.Trim(strings.TrimSpace(b.Then), `'"`)
	}
	return res
}

func (r *outboundRoute) verify(got *outboundRoute) error {
	if strings.Trim(r.Else, `'"`) != strings.Trim(got.Else, `'"`) {
		return fmt.Errorf("verifikasi relay gagal: default diharapkan %q, terbaca %q", r.Else, got.Else)
	}
	want := routeBranchMap(r.Branches)
	have := routeBranchMap(got.Branches)
	if len(want) != len(have) {
		return fmt.Errorf("verifikasi relay gagal: jumlah route domain diharapkan %d, terbaca %d", len(want), len(have))
	}
	for cond, then := range want {
		if have[cond] != then {
			return fmt.Errorf("verifikasi relay gagal: %s diharapkan %q, terbaca %q", cond, then, have[cond])
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// MtaOutboundStrategy
// -----------------------------------------------------------------------------

func (m *Manager) loadOutboundRoute(adminEmail, adminPass string) (*outboundRoute, error) {
	out, err := m.runStalwartCli(adminEmail, adminPass, "get", "MtaOutboundStrategy", "--json")
	if err != nil {
		return nil, fmt.Errorf("gagal membaca MtaOutboundStrategy: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var strat map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out), &strat); err != nil {
		return nil, fmt.Errorf("format MtaOutboundStrategy tidak valid: %w", err)
	}
	return parseOutboundRoute(strat)
}

func (m *Manager) saveOutboundRoute(adminEmail, adminPass string, route *outboundRoute) error {
	payload, err := json.Marshal(map[string]any{"route": route.marshal()})
	if err != nil {
		return err
	}
	out, runErr := m.runStalwartCli(adminEmail, adminPass, "update", "MtaOutboundStrategy", "--json", string(payload))
	if runErr != nil {
		return fmt.Errorf("update MtaOutboundStrategy gagal: %w (%s)", runErr, strings.TrimSpace(string(out)))
	}
	got, err := m.loadOutboundRoute(adminEmail, adminPass)
	if err != nil {
		return err
	}
	return route.verify(got)
}

func (m *Manager) findMtaRoute(adminEmail, adminPass, name string) (map[string]any, error) {
	out, err := m.runStalwartCli(adminEmail, adminPass, "query", "MtaRoute", "--json")
	if err != nil {
		return nil, fmt.Errorf("gagal query MtaRoute: %w: %s", err, strings.TrimSpace(string(out)))
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if n, _ := item["name"].(string); n == name {
			return item, nil
		}
	}
	return nil, nil
}

func (m *Manager) upsertRelayRoute(adminEmail, adminPass, routeName string, cred relayCredential) error {
	payload := map[string]any{
		"name":        routeName,
		"host":        cred.Host,
		"port":        cred.Port,
		"protocol":    "smtp",
		"implicitTls": cred.ImplicitTLS,
	}
	if cred.Username != "" {
		payload["authUsername"] = cred.Username
		payload["authSecret"] = map[string]any{"@type": "Value", "secret": cred.Password}
	}
	pBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tmpFile := fmt.Sprintf("/tmp/wdev_route_%d.json", time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, pBytes, 0600); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	existing, err := m.findMtaRoute(adminEmail, adminPass, routeName)
	if err != nil {
		return err
	}
	if existing != nil {
		id, _ := existing["id"].(string)
		if id == "" {
			return fmt.Errorf("MtaRoute %s tidak memiliki id", routeName)
		}
		if out, err := m.runStalwartCli(adminEmail, adminPass, "update", "MtaRoute", id, "--file", tmpFile); err != nil {
			return fmt.Errorf("update MtaRoute %s gagal: %w (%s)", routeName, err, strings.TrimSpace(string(out)))
		}
	} else {
		if out, err := m.runStalwartCli(adminEmail, adminPass, "create", "MtaRoute/Relay", "--file", tmpFile); err != nil {
			return fmt.Errorf("create MtaRoute %s gagal: %w (%s)", routeName, err, strings.TrimSpace(string(out)))
		}
	}

	verify, err := m.findMtaRoute(adminEmail, adminPass, routeName)
	if err != nil {
		return err
	}
	if verify == nil {
		return fmt.Errorf("MtaRoute %s tidak ditemukan setelah disimpan", routeName)
	}
	return nil
}

func (m *Manager) deleteRelayRoute(adminEmail, adminPass, routeName string) error {
	existing, err := m.findMtaRoute(adminEmail, adminPass, routeName)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	id, _ := existing["id"].(string)
	if id == "" {
		return fmt.Errorf("MtaRoute %s tidak memiliki id", routeName)
	}
	if out, err := m.runStalwartCli(adminEmail, adminPass, "delete", "--ids", id, "MtaRoute"); err != nil {
		return fmt.Errorf("hapus MtaRoute %s gagal: %w (%s)", routeName, err, strings.TrimSpace(string(out)))
	}
	verify, err := m.findMtaRoute(adminEmail, adminPass, routeName)
	if err != nil {
		return err
	}
	if verify != nil {
		return fmt.Errorf("MtaRoute %s masih ada setelah dihapus", routeName)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Relay credentials
// -----------------------------------------------------------------------------

func (m *Manager) globalRelayCredPath() string {
	return filepath.Join(m.cfg.WdevEtc, "mail-relay.cred")
}

func (m *Manager) loadGlobalRelayCred() (relayCredential, bool) {
	data, err := readRootFile(m.globalRelayCredPath())
	if err != nil {
		return relayCredential{}, false
	}
	var cred relayCredential
	if err := json.Unmarshal(data, &cred); err != nil || cred.Host == "" {
		return relayCredential{}, false
	}
	return cred, true
}

func (m *Manager) saveGlobalRelayCred(cred relayCredential) error {
	cred.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(cred, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.globalRelayCredPath(), data, 0600)
}

func (m *Manager) domainRelayCredPath() string {
	return filepath.Join(m.cfg.WdevEtc, "mail-relay-domains.cred")
}

func (m *Manager) saveDomainRelayCred(domain string, cred relayCredential) error {
	credMap := make(map[string]relayCredential)
	if data, err := readRootFile(m.domainRelayCredPath()); err == nil {
		_ = json.Unmarshal(data, &credMap)
	}
	cred.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	credMap[domain] = cred
	data, err := json.MarshalIndent(credMap, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.domainRelayCredPath(), data, 0600)
}

func (m *Manager) loadDomainRelayCred(domain string) (relayCredential, bool) {
	data, err := readRootFile(m.domainRelayCredPath())
	if err != nil {
		return relayCredential{}, false
	}
	var credMap map[string]relayCredential
	if err := json.Unmarshal(data, &credMap); err != nil {
		return relayCredential{}, false
	}
	cred, ok := credMap[domain]
	return cred, ok && cred.Host != ""
}

func (m *Manager) removeDomainRelayCred(domain string) error {
	data, err := readRootFile(m.domainRelayCredPath())
	if err != nil {
		return nil
	}
	credMap := make(map[string]relayCredential)
	if err := json.Unmarshal(data, &credMap); err != nil {
		return err
	}
	if _, ok := credMap[domain]; !ok {
		return nil
	}
	delete(credMap, domain)
	data, err = json.MarshalIndent(credMap, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.domainRelayCredPath(), data, 0600)
}

// ensureDefaultRelay memastikan relay default ada dan dipakai sebagai fallback.
func (m *Manager) ensureDefaultRelay(adminEmail, adminPass string) (string, error) {
	cred, ok := m.loadGlobalRelayCred()
	if !ok {
		return "", fmt.Errorf("%s: relay default belum dikonfigurasi", relayOnlyMode)
	}
	if err := m.upsertRelayRoute(adminEmail, adminPass, DefaultRelayRoute, cred); err != nil {
		return "", err
	}
	route, err := m.loadOutboundRoute(adminEmail, adminPass)
	if err != nil {
		return "", err
	}
	if route.Else != DefaultRelayRoute {
		route.Else = DefaultRelayRoute
		if err := m.saveOutboundRoute(adminEmail, adminPass, route); err != nil {
			return "", err
		}
	}
	return DefaultRelayRoute, nil
}

// ensureRelayReady memastikan relay default tersedia. Untuk migrasi dari
// konfigurasi lama yang hanya punya relay per domain, relay domain tersebut
// dipromosikan menjadi default agar mode relay-only tetap berlaku.
func (m *Manager) ensureRelayReady(domain, adminEmail, adminPass string) error {
	if _, ok := m.loadGlobalRelayCred(); ok {
		return nil
	}
	cred, ok := m.loadDomainRelayCred(strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), ".")))
	if !ok {
		return fmt.Errorf("%s: relay outbound wajib dikonfigurasi (resend/brevo/custom) sebelum setup domain", relayOnlyMode)
	}
	cred.Route = DefaultRelayRoute
	if err := m.saveGlobalRelayCred(cred); err != nil {
		return err
	}
	if adminEmail == "" || adminPass == "" {
		return fmt.Errorf("%s: kredensial admin Stalwart belum tersedia untuk mengaktifkan relay default", relayOnlyMode)
	}
	_, err := m.ensureDefaultRelay(adminEmail, adminPass)
	return err
}

// ActiveRelayProvider mengembalikan provider relay efektif untuk satu domain
// (override domain lebih diprioritaskan daripada relay default).
func (m *Manager) ActiveRelayProvider(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain != "" && domain != RelayScopeAll {
		if cred, ok := m.loadDomainRelayCred(domain); ok {
			return strings.ToLower(cred.Provider)
		}
	}
	if cred, ok := m.loadGlobalRelayCred(); ok {
		return strings.ToLower(cred.Provider)
	}
	return ""
}

// GetOutboundRoutes mengembalikan kondisi routing efektif dari Stalwart.
func (m *Manager) GetOutboundRoutes() (OutboundRouteInfo, error) {
	info := OutboundRouteInfo{Domains: make(map[string]string)}
	adminEmail, adminPass, err := m.pickValidAdminCred("")
	if err != nil {
		return info, fmt.Errorf("kredensial admin Stalwart belum valid: %w", err)
	}
	route, err := m.loadOutboundRoute(adminEmail, adminPass)
	if err != nil {
		return info, err
	}
	info.Default = route.Else
	info.RelayOnly = route.Else != "" && route.Else != "mx"
	for _, b := range route.Branches {
		if domain, ok := domainFromCondition(b.If); ok {
			info.Domains[domain] = b.Then
		}
	}
	return info, nil
}

// -----------------------------------------------------------------------------
// Relay operations
// -----------------------------------------------------------------------------

func (m *Manager) applyRelay(adminEmail, adminPass, domain, provider, host string, port int, implicitTLS bool, user, pass string) error {
	isGlobal := domain == "" || domain == RelayScopeAll
	routeName := relayRouteName(domain)
	cred := relayCredential{
		Provider:    provider,
		Host:        host,
		Port:        port,
		ImplicitTLS: implicitTLS,
		Username:    user,
		Password:    pass,
		Route:       routeName,
	}

	if err := m.upsertRelayRoute(adminEmail, adminPass, routeName, cred); err != nil {
		return err
	}

	route, err := m.loadOutboundRoute(adminEmail, adminPass)
	if err != nil {
		return err
	}

	if isGlobal {
		route.Else = DefaultRelayRoute
		if err := m.saveGlobalRelayCred(cred); err != nil {
			return err
		}
	} else {
		route.upsertDomain(domain, routeName)
		// Fallback tidak boleh kosong/direct: jadikan relay ini default pertama.
		if route.Else == "" || route.Else == "mx" {
			defaultCred := cred
			defaultCred.Route = DefaultRelayRoute
			if err := m.upsertRelayRoute(adminEmail, adminPass, DefaultRelayRoute, defaultCred); err != nil {
				return err
			}
			route.Else = DefaultRelayRoute
			if err := m.saveGlobalRelayCred(defaultCred); err != nil {
				return err
			}
		}
	}

	if err := m.saveOutboundRoute(adminEmail, adminPass, route); err != nil {
		return err
	}
	if !isGlobal {
		if err := m.saveDomainRelayCred(domain, cred); err != nil {
			return err
		}
	}
	_ = restartStalwart()
	return nil
}

func (m *Manager) removeDomainRelay(adminEmail, adminPass, domain string) error {
	if domain == "" || domain == RelayScopeAll {
		return fmt.Errorf("%s: relay default tidak bisa dihapus; setel provider relay lain", relayOnlyMode)
	}
	routeName := relayRouteName(domain)
	route, err := m.loadOutboundRoute(adminEmail, adminPass)
	if err != nil {
		return err
	}
	removed := route.removeDomain(domain)
	if route.Else == "" || route.Else == "mx" {
		if cred, ok := m.loadGlobalRelayCred(); ok {
			if err := m.upsertRelayRoute(adminEmail, adminPass, DefaultRelayRoute, cred); err != nil {
				return err
			}
			route.Else = DefaultRelayRoute
		} else if len(route.Branches) > 0 {
			route.Else = route.Branches[0].Then
		} else {
			return fmt.Errorf("%s: tidak ada relay default untuk fallback", relayOnlyMode)
		}
	}
	if removed || route.Else != "" {
		if err := m.saveOutboundRoute(adminEmail, adminPass, route); err != nil {
			return err
		}
	}
	if err := m.deleteRelayRoute(adminEmail, adminPass, routeName); err != nil {
		return err
	}
	if err := m.removeDomainRelayCred(domain); err != nil {
		return err
	}
	_ = restartStalwart()
	return nil
}

// RemoveRelay menghapus override relay satu domain (fallback ke relay default).
func (m *Manager) RemoveRelay(domain string) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || domain == RelayScopeAll {
		return fmt.Errorf("%s: relay default tidak bisa dihapus; setel provider relay lain", relayOnlyMode)
	}
	adminEmail, adminPass, err := m.pickValidAdminCred(domain)
	if err != nil || adminEmail == "" {
		return fmt.Errorf("kredensial admin Stalwart belum ada atau tidak valid")
	}
	previous := ""
	if prev, ok := m.loadDomainRelayCred(domain); ok {
		previous = strings.ToLower(prev.Provider)
	}
	if err := m.removeDomainRelay(adminEmail, adminPass, domain); err != nil {
		return err
	}
	effective := ""
	if cred, ok := m.loadGlobalRelayCred(); ok {
		effective = strings.ToLower(cred.Provider)
	}
	m.setLastRelayDNS(m.reconcileRelayDNS(domain, previous, effective))
	return nil
}

// EnsureManagedRelayDNS memasang ulang record relay yang tercatat milik provider
// aktif tetapi sudah hilang dari PowerDNS.
func (m *Manager) EnsureManagedRelayDNS(domain, provider string) RelayDNSReport {
	domain = cleanMailDomain(domain)
	provider = strings.ToLower(strings.TrimSpace(provider))
	rep := RelayDNSReport{Domain: domain, Provider: provider, Removed: []string{}, Added: []string{}, Kept: []string{}, Manual: []string{}}
	if provider == "" {
		return rep
	}
	zone, err := m.pdnsMgr.FindMatchingZone(domain)
	if err != nil {
		rep.Manual = append(rep.Manual, err.Error())
		return rep
	}
	all := m.loadRelayDNS()
	list := all[domain]
	for _, r := range list {
		if !strings.EqualFold(r.Provider, provider) {
			continue
		}
		name := recordNameForZone(zone, r.Name)
		existing, err := m.pdnsMgr.LookupRecords(zone, name, r.Type)
		if err != nil {
			rep.Manual = append(rep.Manual, fmt.Sprintf("gagal membaca %s %s: %v", r.Type, r.Name, err))
			continue
		}
		if recordContentExists(existing, r.Content) {
			rep.Kept = append(rep.Kept, fmt.Sprintf("%s %s", r.Type, r.Name))
			continue
		}
		if err := m.pdnsMgr.AddRecordKeep(zone, name, r.Type, r.Content, 300); err != nil {
			rep.Manual = append(rep.Manual, fmt.Sprintf("gagal memasang %s %s: %v", r.Type, r.Name, err))
			continue
		}
		rep.Added = append(rep.Added, fmt.Sprintf("%s %s %s", r.Type, r.Name, r.Content))
	}
	return rep
}
