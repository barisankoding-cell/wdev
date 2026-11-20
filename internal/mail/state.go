package mail

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// domainState menyimpan hasil setup per domain agar URL admin/webmail yang
// ditampilkan sesuai skema SSL yang benar-benar aktif (bukan selalu https).
type domainState struct {
	SSL        bool   `json:"ssl"`
	AdminURL   string `json:"admin_url,omitempty"`
	WebmailURL string `json:"webmail_url,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

func (m *Manager) domainsStatePath() string {
	return filepath.Join(m.cfg.WdevEtc, "mail-domains.json")
}

func (m *Manager) loadDomainsState() map[string]domainState {
	res := make(map[string]domainState)
	data, err := readRootFile(m.domainsStatePath())
	if err != nil {
		return res
	}
	_ = json.Unmarshal(data, &res)
	return res
}

func (m *Manager) saveDomainState(domain string, st domainState) error {
	all := m.loadDomainsState()
	all[strings.ToLower(domain)] = st
	data, err := json.MarshalIndent(all, "", " ")
	if err != nil {
		return err
	}
	return writeRootFile(m.domainsStatePath(), data, 0600)
}

// adminURL/webmailURL memakai skema dari state setup; fallback https karena
// edge gateway memasang Let's Encrypt secara default.
func (m *Manager) adminURL(domain string) string {
	st, ok := m.loadDomainsState()[strings.ToLower(domain)]
	if ok && st.AdminURL != "" {
		return st.AdminURL
	}
	scheme := "https"
	if ok && !st.SSL {
		scheme = "http"
	}
	return fmt.Sprintf("%s://mailadmin.%s/admin/", scheme, domain)
}

func (m *Manager) webmailURL(domain string) string {
	st, ok := m.loadDomainsState()[strings.ToLower(domain)]
	if ok && st.WebmailURL != "" {
		return st.WebmailURL
	}
	scheme := "https"
	if ok && !st.SSL {
		scheme = "http"
	}
	return fmt.Sprintf("%s://webmail.%s", scheme, domain)
}

// recordDomainState mencatat skema efektif setelah edge agent melapor.
func (m *Manager) recordDomainState(domain string, ssl bool, adminURL, webmailURL string) {
	if adminURL == "" {
		scheme := "https"
		if !ssl {
			scheme = "http"
		}
		adminURL = fmt.Sprintf("%s://mailadmin.%s/admin/", scheme, domain)
		webmailURL = fmt.Sprintf("%s://webmail.%s", scheme, domain)
	}
	_ = m.saveDomainState(domain, domainState{SSL: ssl, AdminURL: adminURL, WebmailURL: webmailURL})
}
