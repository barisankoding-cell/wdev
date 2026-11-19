package cloudflare

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"wdev/internal/config"
	"wdev/internal/store"
	"gopkg.in/yaml.v3"
)

type Manager struct {
	cfg   *config.Config
	store *store.Store
}

type TunnelStatus struct {
	Installed bool       `json:"installed"`
	Binary    string     `json:"binary"`
	Active    bool       `json:"active"`
	Profiles  []string   `json:"profiles"`
	Exposures []Exposure `json:"exposures"`
}

type Exposure struct {
	LocalDomain string `json:"local_domain"`
	PublicHost  string `json:"public_host"`
	Profile     string `json:"profile"`
}

type CloudflaredConfig struct {
	TunnelID      string        `yaml:"tunnel,omitempty"`
	Credentials   string        `yaml:"credentials-file,omitempty"`
	Ingress       []IngressRule `yaml:"ingress"`
}

type IngressRule struct {
	Hostname string `yaml:"hostname,omitempty"`
	Service  string `yaml:"service"`
}

func New(cfg *config.Config, s *store.Store) *Manager {
	return &Manager{cfg: cfg, store: s}
}

func (m *Manager) GetStatus() TunnelStatus {
	bin, err := exec.LookPath("cloudflared")
	installed := (err == nil)

	active := false
	if installed {
		active = (exec.Command("systemctl", "is-active", "--quiet", "cloudflared").Run() == nil)
		if !active {
			active = (exec.Command("pgrep", "cloudflared").Run() == nil)
		}
	}

	var profiles []string
	accountsDir := filepath.Join(m.cfg.WdevEtc, "cloudflare", "accounts")
	if entries, err := os.ReadDir(accountsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".cert") {
				name := strings.TrimSuffix(e.Name(), ".json")
				name = strings.TrimSuffix(name, ".cert")
				found := false
				for _, p := range profiles {
					if p == name {
						found = true
						break
					}
				}
				if !found && name != "" {
					profiles = append(profiles, name)
				}
			}
		}
	}

	var exposures []Exposure
	exposuresDir := filepath.Join(m.cfg.WdevEtc, "exposures")
	if entries, err := os.ReadDir(exposuresDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			localDomain := e.Name()
			content, err := os.ReadFile(filepath.Join(exposuresDir, localDomain))
			if err == nil {
				publicHost := strings.TrimSpace(string(content))
				exposures = append(exposures, Exposure{
					LocalDomain: localDomain,
					PublicHost:  publicHost,
					Profile:     "default",
				})
			}
		}
	}

	return TunnelStatus{
		Installed: installed,
		Binary:    bin,
		Active:    active,
		Profiles:  profiles,
		Exposures: exposures,
	}
}

// Expose adds an ingress rule natively into Cloudflare configuration
func (m *Manager) Expose(localDomain, publicHost, profile string) error {
	localDomain = strings.ToLower(strings.TrimSpace(localDomain))
	publicHost = strings.ToLower(strings.TrimSpace(publicHost))
	if profile == "" {
		profile = "default"
	}

	// 1. Write exposure file in /etc/wdev/exposures
	exposuresDir := filepath.Join(m.cfg.WdevEtc, "exposures")
	_ = os.MkdirAll(exposuresDir, 0755)
	if err := os.WriteFile(filepath.Join(exposuresDir, localDomain), []byte(publicHost+"\n"), 0644); err != nil {
		return err
	}

	// 2. Update config.yml for cloudflared
	configFile := "/etc/cloudflared/config.yml"
	routesDir := filepath.Join(m.cfg.WdevEtc, "cloudflare", "routes")
	_ = os.MkdirAll(routesDir, 0755)
	profileConfigFile := filepath.Join(routesDir, profile+".yml")

	targetConfig := configFile
	if _, err := os.Stat(profileConfigFile); err == nil {
		targetConfig = profileConfigFile
	}

	var cfg CloudflaredConfig
	if data, err := os.ReadFile(targetConfig); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}

	// Add or update rule
	found := false
	serviceTarget := fmt.Sprintf("http://127.0.0.1:80")
	// If local ssl cert exists, route to https
	if _, err := os.Stat(fmt.Sprintf("/etc/ssl/local-certs/%s.pem", localDomain)); err == nil {
		serviceTarget = fmt.Sprintf("https://127.0.0.1:443")
	}

	var newRules []IngressRule
	for _, rule := range cfg.Ingress {
		if rule.Hostname == publicHost {
			newRules = append(newRules, IngressRule{Hostname: publicHost, Service: serviceTarget})
			found = true
		} else if rule.Service != "http_status:404" {
			newRules = append(newRules, rule)
		}
	}
	if !found {
		newRules = append(newRules, IngressRule{Hostname: publicHost, Service: serviceTarget})
	}
	// Fallback catch-all must be last
	newRules = append(newRules, IngressRule{Service: "http_status:404"})
	cfg.Ingress = newRules

	if outData, err := yaml.Marshal(&cfg); err == nil {
		_ = os.MkdirAll(filepath.Dir(targetConfig), 0755)
		_ = os.WriteFile(targetConfig, outData, 0644)
	}

	// 3. Register DNS route if cloudflared is installed
	if bin, err := exec.LookPath("cloudflared"); err == nil {
		_ = exec.Command(bin, "tunnel", "route", "dns", profile, publicHost).Run()
	}

	// 4. Reload cloudflared service
	_ = exec.Command("systemctl", "reload", "cloudflared").Run()
	_ = exec.Command("systemctl", "reload", fmt.Sprintf("cloudflared@%s", profile)).Run()

	return nil
}

// Unexpose removes an ingress rule natively
func (m *Manager) Unexpose(localDomain string) error {
	localDomain = strings.ToLower(strings.TrimSpace(localDomain))
	exposuresDir := filepath.Join(m.cfg.WdevEtc, "exposures")
	expFile := filepath.Join(exposuresDir, localDomain)

	publicHost := ""
	if data, err := os.ReadFile(expFile); err == nil {
		publicHost = strings.TrimSpace(string(data))
	}
	_ = os.Remove(expFile)

	// Remove from YAML config
	for _, configFile := range []string{"/etc/cloudflared/config.yml"} {
		if data, err := os.ReadFile(configFile); err == nil {
			var cfg CloudflaredConfig
			if err := yaml.Unmarshal(data, &cfg); err == nil {
				var newRules []IngressRule
				for _, r := range cfg.Ingress {
					if r.Hostname != publicHost {
						newRules = append(newRules, r)
					}
				}
				cfg.Ingress = newRules
				if outData, err := yaml.Marshal(&cfg); err == nil {
					_ = os.WriteFile(configFile, outData, 0644)
				}
			}
		}
	}

	_ = exec.Command("systemctl", "reload", "cloudflared").Run()
	return nil
}
