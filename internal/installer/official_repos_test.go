package installer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOSReleaseHelpers(t *testing.T) {
	tests := []struct {
		name     string
		info     osReleaseInfo
		ubuntu   bool
		codename string
		rhelMaj  string
	}{
		{
			name:     "Debian 12",
			info:     osReleaseInfo{ID: "debian", VersionID: "12", VersionCodename: "bookworm"},
			ubuntu:   false,
			codename: "bookworm",
		},
		{
			name:     "Ubuntu 24.04",
			info:     osReleaseInfo{ID: "ubuntu", IDLike: "debian", VersionID: "24.04", VersionCodename: "noble"},
			ubuntu:   true,
			codename: "noble",
		},
		{
			name:     "Linux Mint",
			info:     osReleaseInfo{ID: "linuxmint", IDLike: "ubuntu debian", VersionID: "22", VersionCodename: "wilma", UbuntuCodename: "noble"},
			ubuntu:   true,
			codename: "noble",
		},
		{
			name:    "Fedora 41",
			info:    osReleaseInfo{ID: "fedora", VersionID: "41"},
			ubuntu:  false,
			rhelMaj: "41",
		},
		{
			name:    "Rocky 9.4",
			info:    osReleaseInfo{ID: "rocky", IDLike: "rhel centos fedora", VersionID: "9.4"},
			ubuntu:  false,
			rhelMaj: "9",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.isUbuntuBased(); got != tc.ubuntu {
				t.Fatalf("isUbuntuBased() = %v, want %v", got, tc.ubuntu)
			}
			if tc.codename != "" {
				if got := tc.info.debianCodename(); got != tc.codename {
					t.Fatalf("debianCodename() = %q, want %q", got, tc.codename)
				}
			}
			if tc.rhelMaj != "" {
				if got := tc.info.rhelMajor(); got != tc.rhelMaj {
					t.Fatalf("rhelMajor() = %q, want %q", got, tc.rhelMaj)
				}
			}
		})
	}
}

func TestNeedsSudoExtensions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needsSudo selalu false saat berjalan sebagai root")
	}
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"/usr/sbin/alternatives", []string{"--set", "java", "/usr/lib/jvm/java-21-temurin/bin/java"}, true},
		{"/usr/pgsql-16/bin/postgresql-16-setup", []string{"initdb"}, true},
		{"mkdir", []string{"-p", "/usr/lib/jvm"}, true},
		{"cp", []string{"-f", "/tmp/a", "/etc/apt/sources.list.d/pgdg.list"}, true},
		{"mv", []string{"/tmp/jdk", "/usr/lib/jvm/java-21-temurin"}, true},
		{"tar", []string{"-xzf", "/tmp/a.tar.gz", "-C", "/tmp/out"}, false},
	}

	for _, tc := range cases {
		if got := needsSudo(tc.name, tc.args...); got != tc.want {
			t.Errorf("needsSudo(%q, %v) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestNginxCompatibilityPatching(t *testing.T) {
	orig := `user  nginx;
http {
    include       /etc/nginx/mime.types;
    include /etc/nginx/conf.d/*.conf;
}
`
	patched, changed := patchNginxMainConfig(orig, true)
	if !changed {
		t.Fatal("expected patchNginxMainConfig to modify nginx.org default config")
	}
	if !strings.Contains(patched, "user  www-data;") || !strings.Contains(patched, "include /etc/nginx/sites-enabled/*;") {
		t.Fatalf("unexpected patched nginx.conf:\n%s", patched)
	}
	if _, changedAgain := patchNginxMainConfig(patched, true); changedAgain {
		t.Fatal("expected patchNginxMainConfig to be idempotent")
	}

	tmp := t.TempDir()
	confPath := filepath.Join(tmp, "nginx.conf")
	passwdPath := filepath.Join(tmp, "passwd")
	if err := os.WriteFile(confPath, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwdPath, []byte("www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin\n"), 0644); err != nil {
		t.Fatal(err)
	}
	inst := &Installer{isDebian: true}
	inst.ensureNginxCompatibilityAt(t.Context(), func(string) {}, confPath, passwdPath)
	diskBytes, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diskBytes), "user  www-data;") || !strings.Contains(string(diskBytes), "include /etc/nginx/sites-enabled/*;") {
		t.Fatalf("ensureNginxCompatibilityAt failed:\n%s", string(diskBytes))
	}
}

func TestAptSafeConffileArgs(t *testing.T) {
	gotInstall := aptSafeConffileArgs("install", "nginx")
	wantInstall := []string{
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"install", "-y", "nginx",
	}
	if !reflect.DeepEqual(gotInstall, wantInstall) {
		t.Fatalf("aptSafeConffileArgs(install) = %v, want %v", gotInstall, wantInstall)
	}

	gotUpgrade := aptSafeConffileArgs("upgrade")
	wantUpgrade := []string{
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"upgrade", "-y",
	}
	if !reflect.DeepEqual(gotUpgrade, wantUpgrade) {
		t.Fatalf("aptSafeConffileArgs(upgrade) = %v, want %v", gotUpgrade, wantUpgrade)
	}
}

func TestResolveTargetUserAndHome(t *testing.T) {
	u, home := ResolveTargetUserAndHome()
	if u == "" || home == "" {
		t.Fatalf("ResolveTargetUserAndHome() returned empty: user=%q home=%q", u, home)
	}
}

func TestHasNewerVersionOrca(t *testing.T) {
	if !HasNewerVersion("1.4.261", "1.4.264") {
		t.Fatal("expected 1.4.264 to be newer than 1.4.261")
	}
	if HasNewerVersion("1.4.264", "1.4.264") {
		t.Fatal("expected same version not to report update")
	}
	if HasNewerVersion("1.4.265", "1.4.264") {
		t.Fatal("expected older release not to report update")
	}
}

func TestNetbirdRepoHelpers(t *testing.T) {
	debLine := netbirdDebianRepoLine("/etc/apt/keyrings/netbird-archive-keyring.gpg")
	wantDeb := "deb [signed-by=/etc/apt/keyrings/netbird-archive-keyring.gpg] https://pkgs.netbird.io/debian stable main\n"
	if debLine != wantDeb {
		t.Fatalf("netbirdDebianRepoLine() = %q, want %q", debLine, wantDeb)
	}
	for _, needle := range []string{"[NetBird]", "baseurl=https://pkgs.netbird.io/yum/", "gpgcheck=1", "repo_gpgcheck=1"} {
		if !strings.Contains(netbirdRPMRepoContent, needle) {
			t.Fatalf("netbirdRPMRepoContent missing %q", needle)
		}
	}
	if !HasNewerVersion("0.83.1", "0.83.2") {
		t.Fatal("expected 0.83.2 to be newer than 0.83.1")
	}
	if HasNewerVersion("0.83.2", "0.83.2") {
		t.Fatal("expected same netbird version not to report update")
	}
}
