package site

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

func TestValidateDomain(t *testing.T) {
	valid := []string{"app.test", "my-site.local", "sub.example.com", "a1.b2.io"}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) unexpected error: %v", d, err)
		}
	}

	invalid := []string{
		"",
		"../etc/nginx",
		"app.test'; system('id'); '",
		"app.test;\nlocation / {}",
		"-flag.test",
		"localhost",
		"a/b.test",
	}
	for _, d := range invalid {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) expected error, got nil", d)
		}
	}
}

func TestValidateRootPath(t *testing.T) {
	valid := []string{"/var/www/app.test", "/home/user/projects/app", "/srv/web"}
	for _, p := range valid {
		if _, err := ValidateRootPath(p); err != nil {
			t.Errorf("ValidateRootPath(%q) unexpected error: %v", p, err)
		}
	}

	invalid := []string{
		"",
		"relative/path",
		"/etc/nginx",
		"/var/www/../../etc/shadow",
		"/var/www/app; include /etc/passwd;",
		"/var/www/app\nmalicious",
	}
	for _, p := range invalid {
		if _, err := ValidateRootPath(p); err == nil {
			t.Errorf("ValidateRootPath(%q) expected error, got nil", p)
		}
	}
}

func newTestSiteManager(t *testing.T) (*Manager, *config.Config, *store.Store) {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hostsPath := filepath.Join(tmp, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		WdevEtc:          filepath.Join(tmp, "etc"),
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
		HostsFile:        hostsPath,
	}
	mgr := New(cfg, db)
	mgr.SetSkipNginxExec(true)
	return mgr, cfg, db
}

func TestEnsureDomainStaticVhostCreatesTemplateAndNginxConfig(t *testing.T) {
	mgr, cfg, db := newTestSiteManager(t)

	res, err := mgr.EnsureDomainStaticVhost("Example.COM.")
	if err != nil {
		t.Fatalf("EnsureDomainStaticVhost failed: %v", err)
	}
	if !res.Created || res.Status != "created" {
		t.Fatalf("expected created status, got %+v", res)
	}
	wantRoot := filepath.Join(cfg.WebRootBase, "example.com")
	if res.RootPath != wantRoot || res.URL != "http://example.com" {
		t.Fatalf("unexpected vhost result: %+v", res)
	}

	indexBytes, err := os.ReadFile(filepath.Join(wantRoot, "index.html"))
	if err != nil {
		t.Fatalf("expected index.html to be created: %v", err)
	}
	indexHTML := string(indexBytes)
	if !strings.Contains(indexHTML, "<!DOCTYPE html>") || !strings.Contains(indexHTML, "example.com") || !strings.Contains(indexHTML, wantRoot) {
		t.Fatalf("unexpected default index.html content: %s", indexHTML)
	}
	if _, err := os.Stat(filepath.Join(wantRoot, "index.php")); !os.IsNotExist(err) {
		t.Fatalf("did not expect index.php for static vhost")
	}

	confBytes, err := os.ReadFile(filepath.Join(cfg.NginxSitesAvail, "example.com"))
	if err != nil {
		t.Fatalf("expected nginx vhost file: %v", err)
	}
	confStr := string(confBytes)
	for _, want := range []string{
		"server_name example.com www.example.com;",
		"root " + wantRoot + ";",
		"index index.html index.htm;",
		"try_files $uri $uri/ =404;",
	} {
		if !strings.Contains(confStr, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, confStr)
		}
	}
	for _, unwanted := range []string{"fastcgi_pass", "snippets/fastcgi-php.conf", "return 301 https://"} {
		if strings.Contains(confStr, unwanted) {
			t.Fatalf("static nginx config should not contain %q:\n%s", unwanted, confStr)
		}
	}

	linkTarget, err := os.Readlink(filepath.Join(cfg.NginxSitesEnable, "example.com"))
	if err != nil || linkTarget != filepath.Join(cfg.NginxSitesAvail, "example.com") {
		t.Fatalf("unexpected symlink target: %q err=%v", linkTarget, err)
	}

	hostsBytes, err := os.ReadFile(cfg.HostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostsBytes), "127.0.0.1 example.com www.example.com # wdev-managed") {
		t.Fatalf("hosts file not updated: %s", string(hostsBytes))
	}

	sites, err := db.ListSites()
	if err != nil || len(sites) != 1 {
		t.Fatalf("expected 1 site in store, got %+v err=%v", sites, err)
	}
	if sites[0].Domain != "example.com" || sites[0].Framework != "static" || sites[0].SSLEnabled || sites[0].PHPVersion != "" {
		t.Fatalf("unexpected stored site record: %+v", sites[0])
	}

	// Panggilan kedua tidak boleh menimpa vhost yang sudah ada
	res2, err := mgr.EnsureDomainStaticVhost("example.com")
	if err != nil {
		t.Fatalf("second EnsureDomainStaticVhost failed: %v", err)
	}
	if res2.Created || res2.Status != "exists" {
		t.Fatalf("expected existing status on second call, got %+v", res2)
	}
}

func TestEnsureDomainStaticVhostPreservesExistingIndexAndVhost(t *testing.T) {
	mgr, cfg, _ := newTestSiteManager(t)

	// 1. Pre-existing index.html in docroot without vhost config must not be overwritten
	docRoot := filepath.Join(cfg.WebRootBase, "custom.test")
	if err := os.MkdirAll(docRoot, 0755); err != nil {
		t.Fatal(err)
	}
	customHTML := "<h1>My Custom App</h1>"
	if err := os.WriteFile(filepath.Join(docRoot, "index.html"), []byte(customHTML), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := mgr.EnsureDomainStaticVhost("custom.test")
	if err != nil || !res.Created {
		t.Fatalf("EnsureDomainStaticVhost failed: res=%+v err=%v", res, err)
	}
	gotHTML, _ := os.ReadFile(filepath.Join(docRoot, "index.html"))
	if string(gotHTML) != customHTML {
		t.Fatalf("pre-existing index.html was overwritten: got %q", string(gotHTML))
	}

	// 2. DeleteSite cleans up nginx & hosts & store without deleting web files
	if err := mgr.DeleteSite("custom.test"); err != nil {
		t.Fatalf("DeleteSite failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.NginxSitesAvail, "custom.test")); !os.IsNotExist(err) {
		t.Fatalf("expected sites-available file to be deleted")
	}
	if _, err := os.Stat(filepath.Join(docRoot, "index.html")); err != nil {
		t.Fatalf("expected user web files to be preserved after DeleteSite: %v", err)
	}
	hostsBytes, _ := os.ReadFile(cfg.HostsFile)
	if strings.Contains(string(hostsBytes), "custom.test") {
		t.Fatalf("expected custom.test removed from hosts, got: %s", string(hostsBytes))
	}
}

func TestEnableLetsEncryptValidationAndFallbackRetry(t *testing.T) {
	mgr, cfg, db := newTestSiteManager(t)

	// 1. Tolak domain lokal .test / .local
	if err := mgr.EnableLetsEncrypt(context.Background(), nil, "myapp.test", ""); err == nil {
		t.Fatal("expected EnableLetsEncrypt to reject .test domain")
	}

	// 2. Tolak domain yang belum memiliki vhost
	if err := mgr.EnableLetsEncrypt(context.Background(), nil, "missing.my.id", ""); err == nil {
		t.Fatal("expected EnableLetsEncrypt to fail when vhost missing")
	}

	// 3. Buat static vhost (punya server_name nikaget.my.id www.nikaget.my.id)
	if _, err := mgr.EnsureDomainStaticVhost("nikaget.my.id"); err != nil {
		t.Fatalf("EnsureDomainStaticVhost failed: %v", err)
	}

	var calls [][]string
	mgr.SetCertbotExecFn(func(ctx context.Context, cb func(string), bin string, args []string) error {
		calls = append(calls, append([]string{}, args...))
		// Simulasikan panggilan pertama (dengan -d nikaget.my.id -d www.nikaget.my.id) gagal karena www belum diarahkan,
		// lalu retry khusus nikaget.my.id sukses dan menulis ssl_certificate Let's Encrypt ke vhost.
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "www.nikaget.my.id") {
			return fmt.Errorf("DNS problem for www.nikaget.my.id")
		}
		confPath := filepath.Join(cfg.NginxSitesAvail, "nikaget.my.id")
		data, _ := os.ReadFile(confPath)
		updated := string(data) + "\n# managed by Certbot\nssl_certificate /etc/letsencrypt/live/nikaget.my.id/fullchain.pem;\n"
		return os.WriteFile(confPath, []byte(updated), 0644)
	})

	if err := mgr.EnableLetsEncrypt(context.Background(), nil, "nikaget.my.id", "admin@nikaget.my.id"); err != nil {
		t.Fatalf("EnableLetsEncrypt failed: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 certbot invocations (with alias then fallback), got %d: %+v", len(calls), calls)
	}
	firstArgs := strings.Join(calls[0], " ")
	if !strings.Contains(firstArgs, "-d nikaget.my.id -d www.nikaget.my.id") || !strings.Contains(firstArgs, "--email admin@nikaget.my.id") {
		t.Fatalf("unexpected first certbot args: %s", firstArgs)
	}
	secondArgs := strings.Join(calls[1], " ")
	if !strings.Contains(secondArgs, "-d nikaget.my.id") || strings.Contains(secondArgs, "www.nikaget.my.id") {
		t.Fatalf("unexpected fallback certbot args: %s", secondArgs)
	}

	sites, err := db.ListSites()
	if err != nil || len(sites) != 1 {
		t.Fatalf("expected 1 site in store, got %+v err=%v", sites, err)
	}
	if !sites[0].SSLEnabled || sites[0].SSLProvider != "letsencrypt" {
		t.Fatalf("expected DB SSLProvider=letsencrypt, got %+v", sites[0])
	}

	listed, err := mgr.ListSites()
	if err != nil || len(listed) != 1 || !listed[0].SSLEnabled || listed[0].SSLProvider != "letsencrypt" {
		t.Fatalf("unexpected mgr.ListSites result: %+v err=%v", listed, err)
	}
}

func writeTestCertPEM(t *testing.T, path, cn string, notAfter time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestListSitesParsesSSLExpirationFromCertificate(t *testing.T) {
	mgr, cfg, db := newTestSiteManager(t)

	// 1. Sertifikat Let's Encrypt aktif (berlaku ~89 hari)
	validCertPath := filepath.Join(t.TempDir(), "etc", "letsencrypt", "live", "aktif.my.id", "fullchain.pem")
	wantExp := time.Now().Add(89*24*time.Hour + 6*time.Hour).UTC().Truncate(time.Second)
	writeTestCertPEM(t, validCertPath, "aktif.my.id", wantExp)

	if err := os.MkdirAll(cfg.NginxSitesAvail, 0755); err != nil {
		t.Fatal(err)
	}
	vhostValid := fmt.Sprintf("server {\n  listen 443 ssl;\n  server_name aktif.my.id;\n  ssl_certificate %s;\n}\n", validCertPath)
	if err := os.WriteFile(filepath.Join(cfg.NginxSitesAvail, "aktif.my.id"), []byte(vhostValid), 0644); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSite(store.SiteRecord{Domain: "aktif.my.id", Framework: "static", RootPath: "/var/www/aktif.my.id"}); err != nil {
		t.Fatal(err)
	}

	// 2. Sertifikat kedaluwarsa (-48 jam)
	expCertPath := filepath.Join(t.TempDir(), "etc", "letsencrypt", "live", "expired.my.id", "fullchain.pem")
	pastExp := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	writeTestCertPEM(t, expCertPath, "expired.my.id", pastExp)

	vhostExpired := fmt.Sprintf("server {\n  listen 443 ssl;\n  server_name expired.my.id;\n  ssl_certificate %s;\n}\n", expCertPath)
	if err := os.WriteFile(filepath.Join(cfg.NginxSitesAvail, "expired.my.id"), []byte(vhostExpired), 0644); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSite(store.SiteRecord{Domain: "expired.my.id", Framework: "static", RootPath: "/var/www/expired.my.id"}); err != nil {
		t.Fatal(err)
	}

	sites, err := mgr.ListSites()
	if err != nil {
		t.Fatalf("ListSites failed: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(sites))
	}

	if sites[0].Domain != "aktif.my.id" || !sites[0].SSLEnabled || sites[0].SSLProvider != "letsencrypt" {
		t.Fatalf("unexpected aktif site metadata: %+v", sites[0])
	}
	if sites[0].SSLExpiresAt == nil || !sites[0].SSLExpiresAt.Equal(wantExp) {
		t.Fatalf("expected SSLExpiresAt=%v, got %v", wantExp, sites[0].SSLExpiresAt)
	}
	if sites[0].SSLDaysLeft == nil || *sites[0].SSLDaysLeft != 89 {
		t.Fatalf("expected SSLDaysLeft=89, got %v", sites[0].SSLDaysLeft)
	}

	if sites[1].Domain != "expired.my.id" || sites[1].SSLDaysLeft == nil || *sites[1].SSLDaysLeft >= 0 {
		t.Fatalf("expected negative SSLDaysLeft for expired cert, got %+v", sites[1])
	}
}

func TestCreateSiteSkipHostsPreservesHostsFile(t *testing.T) {
	mgr, cfg, _ := newTestSiteManager(t)

	if err := mgr.CreateSite(CreateOptions{
		Domain:    "app.nikaget.my.id",
		Framework: "static",
		SkipHosts: true,
	}); err != nil {
		t.Fatalf("CreateSite with SkipHosts failed: %v", err)
	}
	hostsBytes, err := os.ReadFile(cfg.HostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hostsBytes), "app.nikaget.my.id") {
		t.Fatalf("expected hosts file untouched when SkipHosts=true, got: %s", string(hostsBytes))
	}

	if err := mgr.CreateSite(CreateOptions{
		Domain:    "lokal.test",
		Framework: "static",
		SkipHosts: false,
	}); err != nil {
		t.Fatalf("CreateSite local failed: %v", err)
	}
	hostsBytes, _ = os.ReadFile(cfg.HostsFile)
	if !strings.Contains(string(hostsBytes), "127.0.0.1 lokal.test") {
		t.Fatalf("expected lokal.test in hosts file when SkipHosts=false, got: %s", string(hostsBytes))
	}
}

func TestDeleteSiteRemovesConfVariantsExposuresAndDoesNotResurrectOnSyncFromDisk(t *testing.T) {
	mgr, cfg, db := newTestSiteManager(t)

	docRoot := filepath.Join(cfg.WebRootBase, "hapus.my.id")
	if err := os.MkdirAll(filepath.Join(docRoot, "storage"), 0755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(docRoot, "storage", "penting.txt")
	if err := os.WriteFile(userFile, []byte("jangan-hapus"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.CreateSite(CreateOptions{
		Domain:    "hapus.my.id",
		Framework: "static",
		RootPath:  docRoot,
	}); err != nil {
		t.Fatalf("CreateSite failed: %v", err)
	}

	// Simulasikan berkas legacy .conf, -le-ssl.conf, dan penanda exposures di disk.
	confVariant := filepath.Join(cfg.NginxSitesAvail, "hapus.my.id.conf")
	leVariant := filepath.Join(cfg.NginxSitesAvail, "hapus.my.id-le-ssl.conf")
	leEnabled := filepath.Join(cfg.NginxSitesEnable, "hapus.my.id-le-ssl.conf")
	if err := os.WriteFile(confVariant, []byte("server { server_name hapus.my.id; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leVariant, []byte("server { listen 443 ssl; server_name hapus.my.id; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(leVariant, leEnabled); err != nil {
		t.Fatal(err)
	}
	expDir := filepath.Join(cfg.WdevEtc, "exposures")
	if err := os.MkdirAll(expDir, 0755); err != nil {
		t.Fatal(err)
	}
	expFile := filepath.Join(expDir, "hapus.my.id.conf")
	if err := os.WriteFile(expFile, []byte("hapus.my.id\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.DeleteSite("hapus.my.id"); err != nil {
		t.Fatalf("DeleteSite failed: %v", err)
	}

	for _, deletedPath := range []string{
		filepath.Join(cfg.NginxSitesAvail, "hapus.my.id"),
		filepath.Join(cfg.NginxSitesEnable, "hapus.my.id"),
		confVariant,
		leVariant,
		leEnabled,
		expFile,
	} {
		if _, err := os.Lstat(deletedPath); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be deleted, stat err=%v", deletedPath, err)
		}
	}

	// Folder proyek vhost wajib tetap ada beserta isinya.
	gotUserData, err := os.ReadFile(userFile)
	if err != nil || string(gotUserData) != "jangan-hapus" {
		t.Fatalf("expected vhost folder & user files preserved, got err=%v data=%q", err, string(gotUserData))
	}

	// Simulasikan restart / install ulang wdev memanggil SyncFromDisk: vhost yang dihapus tidak boleh muncul lagi.
	db.SyncFromDisk(cfg.NginxSitesAvail, expDir)
	sites, err := db.ListSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Fatalf("expected 0 sites after SyncFromDisk following DeleteSite, got: %+v", sites)
	}
}

func TestPHPVHostTemplatePortableWithoutDebianSnippets(t *testing.T) {
	mgr, cfg, _ := newTestSiteManager(t)

	if err := mgr.CreateSite(CreateOptions{
		Domain:     "appsone.test",
		Framework:  "laravel",
		PHPVersion: "8.4",
	}); err != nil {
		t.Fatalf("CreateSite laravel failed: %v", err)
	}

	confBytes, err := os.ReadFile(filepath.Join(cfg.NginxSitesAvail, "appsone.test"))
	if err != nil {
		t.Fatalf("expected vhost file: %v", err)
	}
	confStr := string(confBytes)
	if strings.Contains(confStr, "snippets/fastcgi-php.conf") {
		t.Fatalf("PHP vhost must not reference Debian-only snippets/fastcgi-php.conf:\n%s", confStr)
	}
	for _, want := range []string{
		"try_files $uri $uri/ /index.php?$query_string;",
		"fastcgi_split_path_info ^(.+?\\.php)(/.*)$;",
		"set $path_info $fastcgi_path_info;",
		"try_files $fastcgi_script_name =404;",
		"include fastcgi_params;",
		"fastcgi_param PATH_INFO $path_info;",
		"fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;",
		"fastcgi_pass ",
	} {
		if !strings.Contains(confStr, want) {
			t.Fatalf("PHP vhost missing portable directive %q:\n%s", want, confStr)
		}
	}

	// Render template directly with TCP fastcgi_pass to verify non-unix target formatting.
	tpl, err := template.New("nginx").Parse(nginxVHostTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var tcpBuf strings.Builder
	if err := tpl.Execute(&tcpBuf, TemplateData{
		Domain:         "tcp.test",
		ServerNames:    "tcp.test",
		EffectiveRoot:  "/var/www/tcp.test/public",
		Framework:      "laravel",
		PHPFastCGIPass: "127.0.0.1:9000",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tcpBuf.String(), "fastcgi_pass 127.0.0.1:9000;") {
		t.Fatalf("expected TCP fastcgi_pass, got:\n%s", tcpBuf.String())
	}
}

func TestResolvePHPFastCGIPassCrossDistro(t *testing.T) {
	noStat := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	noGlob := func(string) ([]string, error) { return nil, nil }
	noRead := func(string) ([]byte, error) { return nil, os.ErrNotExist }

	statSet := func(paths ...string) func(string) (os.FileInfo, error) {
		m := make(map[string]bool, len(paths))
		for _, p := range paths {
			m[p] = true
		}
		return func(p string) (os.FileInfo, error) {
			if m[p] {
				return nil, nil
			}
			return nil, os.ErrNotExist
		}
	}

	// 1. Debian/Ubuntu multi-PHP layout
	pass, sock := resolvePHPFastCGIPassWithFS("8.3", fsProbe{
		statFn: statSet("/etc/php", "/run/php/php8.3-fpm.sock"),
		globFn: noGlob,
		readFn: noRead,
	})
	if pass != "unix:/run/php/php8.3-fpm.sock" || sock != "/run/php/php8.3-fpm.sock" {
		t.Fatalf("debian 8.3 got (%q, %q)", pass, sock)
	}

	// 2. Fedora/RHEL layout with /etc/php-fpm.d/www.conf -> /run/php-fpm/www.sock
	pass, sock = resolvePHPFastCGIPassWithFS("8.4", fsProbe{
		statFn: statSet("/etc/php-fpm.d/www.conf", "/run/php-fpm/www.sock"),
		globFn: noGlob,
		readFn: func(p string) ([]byte, error) {
			if p == "/etc/php-fpm.d/www.conf" {
				return []byte("[www]\nuser = apache\n;listen = 127.0.0.1:9000\nlisten = /run/php-fpm/www.sock\n"), nil
			}
			return nil, os.ErrNotExist
		},
	})
	if pass != "unix:/run/php-fpm/www.sock" || sock != "/run/php-fpm/www.sock" {
		t.Fatalf("fedora www.sock got (%q, %q)", pass, sock)
	}

	// 3. Remi SCL multi-PHP on Fedora/RHEL
	pass, sock = resolvePHPFastCGIPassWithFS("8.2", fsProbe{
		statFn: statSet("/var/opt/remi/php82/run/php-fpm/www.sock"),
		globFn: noGlob,
		readFn: noRead,
	})
	if pass != "unix:/var/opt/remi/php82/run/php-fpm/www.sock" || sock != "/var/opt/remi/php82/run/php-fpm/www.sock" {
		t.Fatalf("remi scl got (%q, %q)", pass, sock)
	}

	// 4. Fedora/RHEL configured to listen via TCP
	pass, sock = resolvePHPFastCGIPassWithFS("8.4", fsProbe{
		statFn: statSet("/etc/php-fpm.d/www.conf"),
		globFn: noGlob,
		readFn: func(p string) ([]byte, error) {
			if p == "/etc/php-fpm.d/www.conf" {
				return []byte("[www]\nlisten = 127.0.0.1:9000\n"), nil
			}
			return nil, os.ErrNotExist
		},
	})
	if pass != "127.0.0.1:9000" || sock != "" {
		t.Fatalf("tcp listen got (%q, %q)", pass, sock)
	}

	// 5. Fallback when nothing exists on disk
	pass, sock = resolvePHPFastCGIPassWithFS("8.4", fsProbe{
		statFn: noStat,
		globFn: noGlob,
		readFn: noRead,
	})
	if pass != "unix:/run/php/php8.4-fpm.sock" || sock != "/run/php/php8.4-fpm.sock" {
		t.Fatalf("fallback got (%q, %q)", pass, sock)
	}
}

func TestPHPVHostPropagatesProxyHTTPSAndHealsLegacyConfigs(t *testing.T) {
	mgr, cfg, _ := newTestSiteManager(t)

	// 1. Vhost baru Laravel harus langsung menyertakan parameter FastCGI proxy HTTPS
	if err := mgr.CreateSite(CreateOptions{
		Domain:     "lara.example.my.id",
		Framework:  "laravel",
		PHPVersion: "8.4",
		SSLEnabled: false,
		SkipHosts:  true,
	}); err != nil {
		t.Fatalf("CreateSite failed: %v", err)
	}
	newConf, err := os.ReadFile(filepath.Join(cfg.NginxSitesAvail, "lara.example.my.id"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`if ($http_x_forwarded_proto ~* "^https(,|$)")`,
		"fastcgi_param HTTPS $fe_https if_not_empty;",
		"fastcgi_param REQUEST_SCHEME $fe_scheme;",
		"fastcgi_param SERVER_PORT $fe_port;",
	} {
		if !strings.Contains(string(newConf), want) {
			t.Fatalf("new vhost missing %q:\n%s", want, string(newConf))
		}
	}

	// 2. Vhost PHP lama tanpa fastcgi_param HTTPS harus diperbaiki oleh EnsureProxyHTTPSForPHPVhosts (idempoten)
	legacyVhost := `server {
    listen 80;
    server_name legacy.example.my.id;
    root /var/www/legacy/public;
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php8.4-fpm.sock;
    }
}
`
	legacyPath := filepath.Join(cfg.NginxSitesAvail, "legacy.example.my.id")
	if err := os.WriteFile(legacyPath, []byte(legacyVhost), 0644); err != nil {
		t.Fatal(err)
	}
	// Static/custom file tidak boleh disentuh
	customPath := filepath.Join(cfg.NginxSitesAvail, "legacy.example.my.id.custom.conf")
	customBody := "# custom include\n"
	if err := os.WriteFile(customPath, []byte(customBody), 0644); err != nil {
		t.Fatal(err)
	}

	count, err := mgr.EnsureProxyHTTPSForPHPVhosts()
	if err != nil {
		t.Fatalf("EnsureProxyHTTPSForPHPVhosts failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 legacy vhost patched, got %d", count)
	}
	patched, _ := os.ReadFile(legacyPath)
	if !strings.Contains(string(patched), "fastcgi_param HTTPS $fe_https if_not_empty;") ||
		!strings.Contains(string(patched), "fastcgi_param REQUEST_SCHEME $fe_scheme;") ||
		!strings.Contains(string(patched), "fastcgi_param SERVER_PORT $fe_port;") {
		t.Fatalf("legacy vhost not patched properly:\n%s", string(patched))
	}
	gotCustom, _ := os.ReadFile(customPath)
	if string(gotCustom) != customBody {
		t.Fatalf("custom conf modified unexpectedly: %q", string(gotCustom))
	}

	// Panggilan kedua tidak boleh mengubah apapun lagi
	count2, err := mgr.EnsureProxyHTTPSForPHPVhosts()
	if err != nil || count2 != 0 {
		t.Fatalf("expected idempotent 0 updates on second call, got count=%d err=%v", count2, err)
	}
}
