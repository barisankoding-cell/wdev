package mail

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ManagedRelayRecord adalah record DNS relay yang dibuat lewat wdev. Hanya
// record yang tercatat di sini yang boleh dihapus otomatis, sehingga record
// buatan manual (atau milik layanan lain) tidak pernah tersentuh.
type ManagedRelayRecord struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	Provider  string `json:"provider"`
	CreatedAt string `json:"created_at,omitempty"`
}

// RelayDNSReport merangkum rekonsiliasi DNS saat relay berubah.
type RelayDNSReport struct {
	Domain                string   `json:"domain"`
	PreviousProvider      string   `json:"previous_provider,omitempty"`
	Provider              string   `json:"provider,omitempty"`
	Removed               []string `json:"removed"`
	Added                 []string `json:"added"`
	Kept                  []string `json:"kept"`
	Manual                []string `json:"manual"`
	VerificationTriggered bool     `json:"verification_triggered,omitempty"`
}

func (r *RelayDNSReport) summary() string {
	parts := []string{}
	if len(r.Removed) > 0 {
		parts = append(parts, fmt.Sprintf("%d record relay lama dihapus", len(r.Removed)))
	}
	if len(r.Added) > 0 {
		parts = append(parts, fmt.Sprintf("%d record relay baru dibuat", len(r.Added)))
	}
	if len(r.Kept) > 0 {
		parts = append(parts, fmt.Sprintf("%d record dipertahankan", len(r.Kept)))
	}
	if len(r.Manual) > 0 {
		parts = append(parts, fmt.Sprintf("%d record perlu dipasang manual", len(r.Manual)))
	}
	if len(parts) == 0 {
		return "tidak ada record DNS relay yang dikelola"
	}
	return strings.Join(parts, "; ")
}

func (m *Manager) relayDNSPath() string {
	return filepath.Join(m.cfg.WdevEtc, "mail-relay-dns.json")
}

// loadRelayDNS memuat seluruh record relay yang dikelola wdev.
func (m *Manager) loadRelayDNS() map[string][]ManagedRelayRecord {
	res := make(map[string][]ManagedRelayRecord)
	data, err := readRootFile(m.relayDNSPath())
	if err != nil {
		return res
	}
	_ = json.Unmarshal(data, &res)
	return res
}

func (m *Manager) saveRelayDNS(all map[string][]ManagedRelayRecord) error {
	data, err := json.MarshalIndent(all, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.relayDNSPath(), data, 0600)
}

// ListRelayDNS mengembalikan record relay terkelola untuk satu domain.
func (m *Manager) ListRelayDNS(domain string) []ManagedRelayRecord {
	all := m.loadRelayDNS()
	list := all[cleanMailDomain(domain)]
	if list == nil {
		return []ManagedRelayRecord{}
	}
	return list
}

func relayRecordKey(r ManagedRelayRecord) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.Name), ".")) + "|" +
		strings.ToUpper(strings.TrimSpace(r.Type)) + "|" +
		strings.TrimSpace(r.Content)
}

// AddRelayDNS mencatat sekaligus memasang record DNS relay untuk satu domain.
// Record yang sudah ada dengan isi sama tidak diduplikasi.
func (m *Manager) AddRelayDNS(domain, provider string, records []ManagedRelayRecord) (RelayDNSReport, error) {
	domain = cleanMailDomain(domain)
	provider = strings.ToLower(strings.TrimSpace(provider))
	rep := RelayDNSReport{Domain: domain, Removed: []string{}, Added: []string{}, Kept: []string{}, Manual: []string{}}
	if domain == "" {
		return rep, fmt.Errorf("domain wajib diisi")
	}
	if !validMailDomain(domain) {
		return rep, fmt.Errorf("domain tidak valid: %s", domain)
	}
	zone, err := m.pdnsMgr.FindMatchingZone(domain)
	if err != nil {
		return rep, err
	}
	rep.Provider = provider

	all := m.loadRelayDNS()
	existing := all[domain]
	index := make(map[string]bool, len(existing))
	for _, r := range existing {
		index[relayRecordKey(r)] = true
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, r := range records {
		r.Name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.Name), "."))
		r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
		r.Content = strings.TrimSpace(r.Content)
		if r.Name == "" || r.Type == "" || r.Content == "" {
			return rep, fmt.Errorf("record relay tidak lengkap (name/type/content wajib)")
		}
		name := recordNameForZone(zone, r.Name)
		liveExisting, lErr := m.pdnsMgr.LookupRecords(zone, name, r.Type)
		alreadyTracked := index[relayRecordKey(r)]
		if lErr == nil && recordContentMatches(r.Type, liveExisting, r.Content) {
			if !alreadyTracked {
				r.Provider = provider
				r.CreatedAt = now
				existing = append(existing, r)
				index[relayRecordKey(r)] = true
			}
			rep.Kept = append(rep.Kept, fmt.Sprintf("%s %s %s", r.Type, r.Name, r.Content))
			continue
		}
		if err := m.pdnsMgr.AddRecordKeep(zone, name, r.Type, r.Content, 300); err != nil {
			return rep, fmt.Errorf("gagal menulis record %s %s: %w", r.Type, r.Name, err)
		}
		if !alreadyTracked {
			r.Provider = provider
			r.CreatedAt = now
			existing = append(existing, r)
			index[relayRecordKey(r)] = true
		}
		rep.Added = append(rep.Added, fmt.Sprintf("%s %s %s", r.Type, r.Name, r.Content))
	}
	all[domain] = existing
	if err := m.saveRelayDNS(all); err != nil {
		return rep, err
	}
	return rep, nil
}

// ReconcileProviderRelayDNS menyelaraskan record DNS terkelola milik provider aktif:
// menghapus record lama milik provider yang tidak lagi ada di desired, lalu
// memastikan seluruh desired terpasang di PowerDNS dan tercatat di mail-relay-dns.json.
func (m *Manager) ReconcileProviderRelayDNS(domain, provider string, desired []ManagedRelayRecord) (RelayDNSReport, error) {
	domain = cleanMailDomain(domain)
	provider = strings.ToLower(strings.TrimSpace(provider))
	rep := RelayDNSReport{Domain: domain, Provider: provider, Removed: []string{}, Added: []string{}, Kept: []string{}, Manual: []string{}}
	if domain == "" || !validMailDomain(domain) {
		return rep, fmt.Errorf("domain tidak valid: %s", domain)
	}
	zone, err := m.pdnsMgr.FindMatchingZone(domain)
	if err != nil {
		return rep, err
	}

	wantSet := make(map[string]bool, len(desired))
	for _, r := range desired {
		r.Name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.Name), "."))
		r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
		r.Content = strings.TrimSpace(r.Content)
		wantSet[relayRecordKey(r)] = true
	}

	all := m.loadRelayDNS()
	current := all[domain]
	kept := make([]ManagedRelayRecord, 0, len(current))
	staleRemoved := false
	for _, r := range current {
		if !strings.EqualFold(r.Provider, provider) || wantSet[relayRecordKey(r)] {
			kept = append(kept, r)
			continue
		}
		name := recordNameForZone(zone, r.Name)
		if delErr := m.deleteRelayRecordValue(zone, name, r.Type, r.Content); delErr == nil {
			rep.Removed = append(rep.Removed, fmt.Sprintf("%s %s %s", r.Type, r.Name, r.Content))
			staleRemoved = true
		} else {
			kept = append(kept, r)
		}
	}
	if staleRemoved {
		if len(kept) == 0 {
			delete(all, domain)
		} else {
			all[domain] = kept
		}
		if err := m.saveRelayDNS(all); err != nil {
			return rep, err
		}
	}

	addRep, addErr := m.AddRelayDNS(domain, provider, desired)
	rep.Added = append(rep.Added, addRep.Added...)
	rep.Kept = append(rep.Kept, addRep.Kept...)
	rep.Manual = append(rep.Manual, addRep.Manual...)
	return rep, addErr
}

// DeleteRelayDNS menghapus record DNS relay terkelola (opsional hanya milik
// provider tertentu) beserta catatannya.
func (m *Manager) DeleteRelayDNS(domain, provider string) (RelayDNSReport, error) {
	domain = cleanMailDomain(domain)
	provider = strings.ToLower(strings.TrimSpace(provider))
	rep := RelayDNSReport{Domain: domain, Provider: provider, Removed: []string{}, Added: []string{}, Kept: []string{}, Manual: []string{}}
	all := m.loadRelayDNS()
	existing, ok := all[domain]
	if !ok || len(existing) == 0 {
		return rep, nil
	}
	zone, zoneErr := m.pdnsMgr.FindMatchingZone(domain)

	kept := make([]ManagedRelayRecord, 0, len(existing))
	for _, r := range existing {
		if provider != "" && !strings.EqualFold(r.Provider, provider) {
			kept = append(kept, r)
			rep.Kept = append(rep.Kept, fmt.Sprintf("%s %s %s (%s)", r.Type, r.Name, r.Content, r.Provider))
			continue
		}
		if zoneErr != nil {
			return rep, zoneErr
		}
		name := recordNameForZone(zone, r.Name)
		if err := m.deleteRelayRecordValue(zone, name, r.Type, r.Content); err != nil {
			return rep, fmt.Errorf("gagal menghapus record %s %s: %w", r.Type, r.Name, err)
		}
		rep.Removed = append(rep.Removed, fmt.Sprintf("%s %s %s", r.Type, r.Name, r.Content))
	}
	if len(kept) == 0 {
		delete(all, domain)
	} else {
		all[domain] = kept
	}
	if err := m.saveRelayDNS(all); err != nil {
		return rep, err
	}
	return rep, nil
}

// deleteRelayRecordValue menghapus satu nilai dari RRset. Bila RRset punya nilai
// lain, hanya nilai tersebut yang dibuang agar record lain tidak ikut hilang.
func (m *Manager) deleteRelayRecordValue(zone, name, rtype, content string) error {
	current, err := m.pdnsMgr.LookupRecords(zone, name, rtype)
	if err != nil {
		return err
	}
	wantNorm := normalizeDNSRecordValue(rtype, content)
	rest := make([]string, 0, len(current))
	found := false
	for _, c := range current {
		if strings.EqualFold(normalizeDNSRecordValue(rtype, c), wantNorm) {
			found = true
			continue
		}
		rest = append(rest, c)
	}
	if !found {
		return nil
	}
	if len(rest) == 0 {
		return m.pdnsMgr.DeleteRecord(zone, name, rtype)
	}
	return m.pdnsMgr.ReplaceRecords(zone, name, rtype, rest, 300)
}

// reconcileRelayDNS dijalankan setiap relay domain diganti: record relay milik
// provider lama dihapus, record provider baru dipertahankan, lalu checklist
// manual dilaporkan.
func (m *Manager) reconcileRelayDNS(domain, previousProvider, newProvider string) RelayDNSReport {
	domain = cleanMailDomain(domain)
	previousProvider = strings.ToLower(strings.TrimSpace(previousProvider))
	newProvider = strings.ToLower(strings.TrimSpace(newProvider))
	rep := RelayDNSReport{
		Domain:           domain,
		PreviousProvider: previousProvider,
		Provider:         newProvider,
		Removed:          []string{},
		Added:            []string{},
		Kept:             []string{},
		Manual:           []string{},
	}

	existing := m.ListRelayDNS(domain)
	for _, r := range existing {
		if newProvider == "" || !strings.EqualFold(r.Provider, newProvider) {
			rep.Kept = append(rep.Kept, fmt.Sprintf("%s %s (%s)", r.Type, r.Name, r.Provider))
		} else {
			rep.Kept = append(rep.Kept, fmt.Sprintf("%s %s %s", r.Type, r.Name, r.Content))
		}
	}

	if previousProvider == "" || previousProvider == newProvider {
		rep.Manual = relayDNSPlan(newProvider, domain)
		return rep
	}

	deleted, err := m.DeleteRelayDNS(domain, previousProvider)
	if err != nil {
		rep.Manual = append(rep.Manual, "gagal menghapus record relay "+previousProvider+": "+err.Error())
	} else {
		rep.Removed = append(rep.Removed, deleted.Removed...)
		// Sisa record (provider lain) tetap dipertahankan.
		keptOthers := make([]string, 0, len(deleted.Kept))
		for _, k := range deleted.Kept {
			keptOthers = append(keptOthers, k)
		}
		rep.Kept = keptOthers
	}
	rep.Manual = relayDNSPlan(newProvider, domain)
	return rep
}
