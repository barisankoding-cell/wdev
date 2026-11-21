package switcher

import (
	"testing"
	"wdev/internal/config"
	"wdev/internal/installer"
)

func TestNewWithInstallerAndGetPGInfo(t *testing.T) {
	inst := installer.New(&config.Config{}, nil)
	mgr := NewWithInstaller(inst)
	if mgr.getInstaller() != inst {
		t.Fatal("expected NewWithInstaller to attach provided installer")
	}

	info := mgr.GetPGInfo()
	if info.Name != "PostgreSQL" {
		t.Fatalf("Name = %q, want PostgreSQL", info.Name)
	}
	if len(info.AvailableVersions) < 3 {
		t.Fatalf("expected default available versions, got %v", info.AvailableVersions)
	}
	if err := mgr.SwitchPG("not-a-version"); err == nil {
		t.Fatal("expected invalid version error from SwitchPG")
	}
}
