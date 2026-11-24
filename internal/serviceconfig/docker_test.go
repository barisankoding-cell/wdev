package serviceconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAndApplyDockerDaemonJSON(t *testing.T) {
	existing := `{
  "storage-driver": "overlay2",
  "data-root": "/mnt/data/docker",
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "50m",
    "max-file": "5"
  },
  "live-restore": true,
  "no-new-privileges": true,
  "dns": ["1.1.1.1", "8.8.8.8"]
}`
	cfg, err := ParseDockerDaemonJSON(existing)
	if err != nil {
		t.Fatalf("ParseDockerDaemonJSON failed: %v", err)
	}
	if cfg.DataRoot != "/mnt/data/docker" || cfg.LogMaxSize != "50m" || cfg.LogMaxFile != 5 || !cfg.LiveRestore || !cfg.NoNewPrivileges || len(cfg.DNS) != 2 {
		t.Fatalf("unexpected parsed DockerConfig: %+v", cfg)
	}

	// Modify log rotation and ensure unknown key "storage-driver" is preserved
	cfg.LogMaxSize = "10m"
	cfg.LogMaxFile = 3
	cfg.DefaultAddressPoolBase = "172.28.0.0/16"
	cfg.DefaultAddressPoolSize = 24
	if err := ValidateDockerConfig(cfg); err != nil {
		t.Fatalf("ValidateDockerConfig failed: %v", err)
	}

	out, err := ApplyDockerDaemonJSON(existing, cfg)
	if err != nil {
		t.Fatalf("ApplyDockerDaemonJSON failed: %v", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(out, &merged); err != nil {
		t.Fatal(err)
	}
	if merged["storage-driver"] != "overlay2" {
		t.Fatalf("expected existing unknown key storage-driver to be preserved, got %+v", merged)
	}
	if !strings.Contains(string(out), `"172.28.0.0/16"`) {
		t.Fatalf("expected default-address-pools in output JSON: %s", string(out))
	}
}

func TestValidateDockerConfigGuardsAndPresets(t *testing.T) {
	bad := DefaultDockerConfig()
	bad.DataRoot = "relative/path"
	if err := ValidateDockerConfig(bad); err == nil {
		t.Fatalf("expected relative data-root to fail")
	}

	badDNS := DefaultDockerConfig()
	badDNS.DNS = []string{"not-an-ip"}
	if err := ValidateDockerConfig(badDNS); err == nil {
		t.Fatalf("expected invalid DNS IP to fail")
	}

	std := DefaultDockerConfig()
	if got := DetectDockerPreset(std); got != "Dev Standar" {
		t.Fatalf("expected Dev Standar preset, got %q", got)
	}

	prod := DefaultDockerConfig()
	prod.LogMaxSize = "50m"
	prod.LogMaxFile = 5
	prod.NoNewPrivileges = true
	if got := DetectDockerPreset(prod); got != "Production Hardened" {
		t.Fatalf("expected Production Hardened preset, got %q", got)
	}
}
