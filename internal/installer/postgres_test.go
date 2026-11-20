package installer

import (
	"os"
	"reflect"
	"testing"
)

func TestIsPGDGVendor(t *testing.T) {
	cases := []struct {
		vendor string
		want   bool
	}{
		{"PostgreSQL Global Development Group", true},
		{"PGDG", true},
		{"Fedora Project", false},
		{"Red Hat, Inc.", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isPGDGVendor(c.vendor); got != c.want {
			t.Errorf("isPGDGVendor(%q) = %v, want %v", c.vendor, got, c.want)
		}
	}
}

func TestRPMInstallUpgradePlan(t *testing.T) {
	installed := map[string]bool{
		"postgresql16-server": true,
		"postgresql16":        true,
		"postgresql16-libs":   false,
	}
	toInstall, toUpgrade := rpmInstallUpgradePlan(
		[]string{"postgresql16-server", "postgresql16", "postgresql16-libs", "postgresql16"},
		installed,
	)

	wantInstall := []string{"postgresql16-libs"}
	wantUpgrade := []string{"postgresql16-server", "postgresql16"}
	if !reflect.DeepEqual(toInstall, wantInstall) {
		t.Fatalf("toInstall = %v, want %v", toInstall, wantInstall)
	}
	if !reflect.DeepEqual(toUpgrade, wantUpgrade) {
		t.Fatalf("toUpgrade = %v, want %v", toUpgrade, wantUpgrade)
	}
}

func TestShouldMigrateLegacyFedoraPGData(t *testing.T) {
	cases := []struct {
		name              string
		legacyVer         string
		targetMajor       string
		targetInitialized bool
		want              bool
	}{
		{"same major uninitialized target", "16\n", "16", false, true},
		{"target already initialized", "16", "16", true, false},
		{"different major version", "15", "16", false, false},
		{"missing legacy version", "", "16", false, false},
		{"empty target major", "16", "", false, false},
	}
	for _, c := range cases {
		if got := shouldMigrateLegacyFedoraPGData(c.legacyVer, c.targetMajor, c.targetInitialized); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParsePGMajorFromOutput(t *testing.T) {
	cases := []struct {
		out  string
		want string
	}{
		{"postgres (PostgreSQL) 16.14", "16"},
		{"psql (PostgreSQL) 16.16", "16"},
		{"postgres (PostgreSQL) 18.3\n", "18"},
		{"", ""},
		{"invalid output", ""},
	}
	for _, c := range cases {
		if got := parsePGMajorFromOutput(c.out); got != c.want {
			t.Errorf("parsePGMajorFromOutput(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestRepoSectionEnabled(t *testing.T) {
	sample := `
[pgdg16]
name=PostgreSQL 16 for Fedora 45 - x86_64
enabled=1

[pgdg16-debuginfo]
name=PostgreSQL 16 Debuginfo
enabled=0

[pgdg17]
name=PostgreSQL 17 for Fedora 45 - x86_64
`
	if !repoSectionEnabled(sample, "pgdg16") {
		t.Fatal("pgdg16 seharusnya enabled")
	}
	if repoSectionEnabled(sample, "pgdg16-debuginfo") {
		t.Fatal("pgdg16-debuginfo seharusnya disabled")
	}
	if !repoSectionEnabled(sample, "pgdg17") {
		t.Fatal("pgdg17 tanpa baris enabled harus dianggap enabled (default DNF)")
	}
	if repoSectionEnabled(sample, "pgdg99") {
		t.Fatal("pgdg99 tidak ada di repo")
	}
}

func TestNeedsSudoPostgresHelpers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needsSudo selalu false saat berjalan sebagai root")
	}
	if !needsSudo("restorecon", "-RF", "/var/lib/pgsql/16") {
		t.Error("restorecon harus memerlukan sudo")
	}
	if !needsSudo("systemd-tmpfiles", "--create") {
		t.Error("systemd-tmpfiles harus memerlukan sudo")
	}
	if !needsSudo("ldconfig") {
		t.Error("ldconfig harus memerlukan sudo")
	}
	if !needsSudo("rmdir", "/var/lib/pgsql/16/data") {
		t.Error("rmdir di /var/lib/pgsql harus memerlukan sudo")
	}
	if needsSudo("rmdir", "/tmp/wdev-test-dir") {
		t.Error("rmdir di /tmp tidak boleh memerlukan sudo")
	}
}

func TestPGDGLdConfShadowsSystemLibpq(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"pgdg active entry", "/usr/pgsql-16/lib/\n", true},
		{"pgdg 18 entry", "  /usr/pgsql-18/lib\n", true},
		{"wdev comment only", wdevLdConfHeader, false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := pgdgLdConfShadowsSystemLibpq(c.content); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
