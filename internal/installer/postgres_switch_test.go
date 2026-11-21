package installer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsValidPGMajor(t *testing.T) {
	valid := []string{"14", "15", "16", "17", "18", "19"}
	for _, v := range valid {
		if !IsValidPGMajor(v) {
			t.Errorf("IsValidPGMajor(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "9", "100", "16.1", "16;rm -rf /", "../16", "abc"}
	for _, v := range invalid {
		if IsValidPGMajor(v) {
			t.Errorf("IsValidPGMajor(%q) = true, want false", v)
		}
	}
}

func TestPsqlSupportsRestrict(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"psql (PostgreSQL) 18.0", true},
		{"psql (PostgreSQL) 18.3", true},
		{"psql (PostgreSQL) 17.6", true},
		{"psql (PostgreSQL) 17.5", false},
		{"psql (PostgreSQL) 16.16", true},
		{"psql (PostgreSQL) 16.10 (Ubuntu 16.10-0ubuntu0.24.04.1)", true},
		{"psql (PostgreSQL) 16.9", false},
		{"psql (PostgreSQL) 15.14", true},
		{"psql (PostgreSQL) 15.13", false},
		{"psql (PostgreSQL) 14.19", true},
		{"psql (PostgreSQL) 14.18", false},
		{"psql (PostgreSQL) 13.20", false},
	}
	for _, tc := range cases {
		if got := psqlSupportsRestrict(tc.out); got != tc.want {
			t.Errorf("psqlSupportsRestrict(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}

func TestParsePGCreateRoleStmt(t *testing.T) {
	cases := []struct {
		in        string
		wantIdent string
		wantName  string
		wantOK    bool
	}{
		{"CREATE ROLE postgres;", "postgres", "postgres", true},
		{"CREATE ROLE MyApp_User;", "MyApp_User", "myapp_user", true},
		{`CREATE ROLE "My-App""User";`, `"My-App""User"`, `My-App"User`, true},
		{`CREATE ROLE postgres WITH SUPERUSER;`, "", "", false},
		{`ALTER ROLE postgres WITH SUPERUSER;`, "", "", false},
	}
	for _, tc := range cases {
		ident, name, ok := parsePGCreateRoleStmt(tc.in)
		if ok != tc.wantOK || ident != tc.wantIdent || name != tc.wantName {
			t.Errorf("parsePGCreateRoleStmt(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, ident, name, ok, tc.wantIdent, tc.wantName, tc.wantOK)
		}
	}
}

func TestTransformGlobalsSQLDowngradeAndIdempotency(t *testing.T) {
	raw := strings.Join([]string{
		"-- PostgreSQL database cluster dump",
		`\restrict key123`,
		"SET default_transaction_read_only = off;",
		"SET transaction_timeout = 0;",
		"CREATE ROLE postgres;",
		"ALTER ROLE postgres WITH SUPERUSER PASSWORD 'SCRAM-SHA-256$abc';",
		`CREATE ROLE "app-user";`,
		`ALTER ROLE "app-user" WITH NOSUPERUSER LOGIN PASSWORD 'SCRAM-SHA-256$xyz';`,
		"GRANT pg_read_all_data TO \"app-user\" WITH INHERIT TRUE, ADMIN FALSE, SET TRUE GRANTED BY postgres;",
		"GRANT pg_maintain TO \"app-user\" WITH INHERIT TRUE, ADMIN FALSE, SET TRUE GRANTED BY postgres;",
		`\unrestrict key123`,
		"",
	}, "\n")

	opts := pgSQLTransformOpts{
		TargetMajor: 15,
		SupportedGUCs: map[string]bool{
			"default_transaction_read_only": true,
		},
		TargetRoles: map[string]bool{
			"postgres":         true,
			"pg_read_all_data": true,
		},
		StripRestrict: true,
	}

	var out bytes.Buffer
	if err := transformGlobalsSQL(strings.NewReader(raw), &out, opts); err != nil {
		t.Fatalf("transformGlobalsSQL failed: %v", err)
	}
	got := out.String()

	if strings.Contains(got, `\restrict`) || strings.Contains(got, `\unrestrict`) {
		t.Fatalf("expected \\restrict/\\unrestrict stripped:\n%s", got)
	}
	if strings.Contains(got, "\nSET transaction_timeout = 0;\n") {
		t.Fatalf("expected unsupported transaction_timeout commented out:\n%s", got)
	}
	if !strings.Contains(got, "SET default_transaction_read_only = off;") {
		t.Fatalf("expected supported GUC preserved:\n%s", got)
	}
	if !strings.Contains(got, "IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'postgres') THEN CREATE ROLE postgres;") {
		t.Fatalf("expected idempotent postgres role block:\n%s", got)
	}
	if !strings.Contains(got, `IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app-user') THEN CREATE ROLE "app-user";`) {
		t.Fatalf("expected idempotent quoted role block:\n%s", got)
	}
	if !strings.Contains(got, "ALTER ROLE postgres WITH SUPERUSER PASSWORD 'SCRAM-SHA-256$abc';") {
		t.Fatalf("expected ALTER ROLE password preserved:\n%s", got)
	}
	if !strings.Contains(got, `GRANT pg_read_all_data TO "app-user" GRANTED BY postgres;`) {
		t.Fatalf("expected pre-16 normalized GRANT:\n%s", got)
	}
	if strings.Contains(got, "\nGRANT pg_maintain ") {
		t.Fatalf("expected unknown pg_maintain skipped on PG 15:\n%s", got)
	}
}

func TestTransformDatabaseSQLDowngradePreservesCopyAndDollarQuotes(t *testing.T) {
	bigPayload := strings.Repeat("x", 128*1024) // > 64KB bufio.Scanner limit
	raw := strings.Join([]string{
		"-- Dumped from database version 18.1",
		`\restrict tok999`,
		"SET statement_timeout = 0;",
		"SET transaction_timeout = 0;",
		"SET client_encoding = 'UTF8';",
		"SET myapp.custom_guc = 'keep_me';",
		"CREATE FUNCTION public.demo() RETURNS text LANGUAGE sql AS $$",
		"  SELECT 'SET transaction_timeout = 0;'::text;",
		"$$;",
		"CREATE TABLE public.logs (id integer, body text);",
		"COPY public.logs (id, body) FROM stdin;",
		"1\tSET transaction_timeout = 0;",
		`2	\restrict injected`,
		"3\t" + bigPayload,
		`\.`,
		`\unrestrict tok999`,
		"",
	}, "\n")

	opts := pgSQLTransformOpts{
		TargetMajor: 16,
		SupportedGUCs: map[string]bool{
			"statement_timeout": true,
			"client_encoding":   true,
		},
		StripRestrict: false, // PG 16.16 supports \restrict
	}

	var out bytes.Buffer
	if err := transformDatabaseSQL(strings.NewReader(raw), &out, opts); err != nil {
		t.Fatalf("transformDatabaseSQL failed: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, `\restrict tok999`) || !strings.Contains(got, `\unrestrict tok999`) {
		t.Fatalf("expected \\restrict preserved when StripRestrict=false:\n%s", got)
	}
	if !strings.Contains(got, "-- [wdev downgrade skipped unsupported GUC] SET transaction_timeout = 0;") {
		t.Fatalf("expected top-level transaction_timeout skipped:\n%s", got)
	}
	if !strings.Contains(got, "SET statement_timeout = 0;") || !strings.Contains(got, "SET myapp.custom_guc = 'keep_me';") {
		t.Fatalf("expected supported and extension GUCs preserved:\n%s", got)
	}
	if !strings.Contains(got, "  SELECT 'SET transaction_timeout = 0;'::text;") {
		t.Fatalf("dollar-quoted function body must remain untouched")
	}
	if !strings.Contains(got, "1\tSET transaction_timeout = 0;") || !strings.Contains(got, `2	\restrict injected`) {
		t.Fatalf("COPY data rows must remain completely untouched")
	}
	if !strings.Contains(got, "3\t"+bigPayload) {
		t.Fatalf("128KB+ line in COPY was truncated or altered")
	}
}

func TestPickSourcePGClusterPriority(t *testing.T) {
	clusters := []pgCluster{
		{Major: "18", Unit: "postgresql-18", Port: 5432, Initialized: true, Active: false},
		{Major: "17", Unit: "postgresql-17", Port: 5432, Initialized: true, Active: false},
		{Major: "16", Unit: "postgresql-16", Port: 5432, Initialized: true, Active: true},
	}

	// Active 16 wins even when switching to 18 (upgrade)
	src := pickSourcePGCluster(clusters, "18", "17", func(u string) bool { return u == "postgresql-17" })
	if src == nil || src.Major != "16" {
		t.Fatalf("expected active 16 to be chosen, got %+v", src)
	}

	// When all stopped, stateActiveMajor wins
	clusters[2].Active = false
	src = pickSourcePGCluster(clusters, "16", "18", func(u string) bool { return u == "postgresql-17" })
	if src == nil || src.Major != "18" {
		t.Fatalf("expected state file 18 to win when stopped, got %+v", src)
	}

	// When no state file, enabled unit wins
	src = pickSourcePGCluster(clusters, "16", "", func(u string) bool { return u == "postgresql-17" })
	if src == nil || src.Major != "17" {
		t.Fatalf("expected enabled unit 17 to win, got %+v", src)
	}

	// Switching to same version when no other cluster exists returns nil
	single := []pgCluster{{Major: "16", Unit: "postgresql-16", Port: 5432, Initialized: true, Active: true}}
	if src = pickSourcePGCluster(single, "16", "16", nil); src != nil {
		t.Fatalf("expected nil when only target cluster exists, got %+v", src)
	}
}

func TestSetPGConfPortContentAndOverlay(t *testing.T) {
	conf := "# PostgreSQL conf\n#port = 5432\nport = 5433\nlisten_addresses = 'localhost'\n"
	updated := setPGConfPortContent(conf, 5432)
	if !strings.Contains(updated, "\nport = 5432\n") || strings.Contains(updated, "5433") {
		t.Fatalf("unexpected port update:\n%s", updated)
	}

	commentedOnly := "# PostgreSQL conf\n#port = 5432\t\t# comment\nmax_connections = 100\n"
	updated2 := setPGConfPortContent(commentedOnly, 5416)
	if !strings.Contains(updated2, "#port = 5432\t\t# comment\nport = 5416\n") {
		t.Fatalf("expected port inserted below commented template:\n%s", updated2)
	}

	srcConf := "port = 5432\nlisten_addresses = '*'\ntimezone = 'Asia/Jakarta'\nmax_connections = 250\ndata_directory = '/var/lib/postgresql/16/main'\n"
	srcHba := "local all postgres peer\n# BEGIN WDEV REMOTE ACCESS\nhost all all 100.64.0.0/10 scram-sha-256\n# END WDEV REMOTE ACCESS\n"
	dstConf := "port = 5432\nlisten_addresses = 'localhost'\ntimezone = 'UTC'\ndata_directory = '/var/lib/postgresql/18/main'\n"
	dstHba := "local all postgres peer\n"

	newConf, newHba := applyWdevPGConfigOverlay(srcConf, srcHba, dstConf, dstHba)
	if !strings.Contains(newConf, "listen_addresses = '*'") ||
		!strings.Contains(newConf, "timezone = 'Asia/Jakarta'") ||
		!strings.Contains(newConf, "max_connections = 250") ||
		!strings.Contains(newConf, "data_directory = '/var/lib/postgresql/18/main'") {
		t.Fatalf("overlay corrupted or missed target config:\n%s", newConf)
	}
	if strings.Contains(newConf, "/var/lib/postgresql/16/main") {
		t.Fatalf("overlay must never copy source data_directory:\n%s", newConf)
	}
	if !strings.Contains(newHba, "100.64.0.0/10 scram-sha-256") {
		t.Fatalf("overlay missed wdev pg_hba block:\n%s", newHba)
	}
}

func TestNeedsSudoRunuserAndSu(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needsSudo selalu false saat berjalan sebagai root")
	}
	if !needsSudo("runuser", "-u", "postgres", "--", "psql") {
		t.Error("runuser harus memerlukan sudo")
	}
	if !needsSudo("su", "-", "postgres", "-c", "psql") {
		t.Error("su harus memerlukan sudo")
	}
}

// TestPostgresDumpTransformRestoreUserspaceIntegration menguji siklus penuh
// initdb -> isi role & beberapa database -> pg_dumpall -g + pg_dump ->
// simulasi downgrade header (SET transaction_timeout) -> transform -> restore ke cluster baru.
func TestPostgresDumpTransformRestoreUserspaceIntegration(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skip userspace initdb integration test when running as root")
	}
	binDir := ""
	for _, cand := range []string{"/usr/pgsql-18/bin", "/usr/pgsql-17/bin", "/usr/pgsql-16/bin", "/usr/lib/postgresql/16/bin", "/usr/bin"} {
		if _, err := os.Stat(filepath.Join(cand, "initdb")); err == nil {
			if _, err2 := os.Stat(filepath.Join(cand, "pg_ctl")); err2 == nil {
				binDir = cand
				break
			}
		}
	}
	if binDir == "" {
		t.Skip("initdb/pg_ctl tidak ditemukan di sistem; melewati uji integrasi userspace")
	}

	tmp := t.TempDir()
	srcData := filepath.Join(tmp, "src-data")
	srcSock := filepath.Join(tmp, "src-sock")
	dstData := filepath.Join(tmp, "dst-data")
	dstSock := filepath.Join(tmp, "dst-sock")
	for _, d := range []string{srcSock, dstSock} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}

	initAndStart := func(dataDir, sockDir string) func() {
		cmd := exec.Command(filepath.Join(binDir, "initdb"), "-D", dataDir, "-U", "postgres", "--auth=trust", "--encoding=UTF8")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("initdb %s failed: %v\n%s", dataDir, err, string(out))
		}
		pgCtl := filepath.Join(binDir, "pg_ctl")
		opts := fmt.Sprintf("-k %s -h ''", sockDir)
		startCmd := exec.Command(pgCtl, "-D", dataDir, "-l", filepath.Join(dataDir, "pg.log"), "-o", opts, "-w", "start")
		if out, err := startCmd.CombinedOutput(); err != nil {
			t.Fatalf("pg_ctl start %s failed: %v\n%s", dataDir, err, string(out))
		}
		return func() {
			_ = exec.Command(pgCtl, "-D", dataDir, "-m", "immediate", "-w", "stop").Run()
		}
	}

	stopSrc := initAndStart(srcData, srcSock)
	defer stopSrc()

	runPsql := func(sockDir, db, sql string) string {
		c := exec.Command(filepath.Join(binDir, "psql"), "-h", sockDir, "-U", "postgres", "-d", db, "-At", "-v", "ON_ERROR_STOP=1", "-c", sql)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("psql (%s) failed: %v\n%s", sql, err, string(out))
		}
		return strings.TrimSpace(string(out))
	}

	// Buat role, database custom, objek di postgres DB, dan data dengan string khusus
	runPsql(srcSock, "postgres", "CREATE ROLE app_owner WITH LOGIN PASSWORD 'secret_pass';")
	runPsql(srcSock, "postgres", "CREATE TABLE public.root_notes (id int primary key, note text);")
	runPsql(srcSock, "postgres", "INSERT INTO public.root_notes VALUES (1, 'kept-in-postgres-db');")
	runPsql(srcSock, "postgres", `CREATE DATABASE app_prod OWNER app_owner;`)
	runPsql(srcSock, "app_prod", `CREATE TABLE public.items (id serial primary key, payload text);`)
	runPsql(srcSock, "app_prod", `INSERT INTO public.items (payload) VALUES ('SET transaction_timeout = 0;'), ('hello-wdev');`)

	globalsFile := filepath.Join(tmp, "globals.sql")
	if out, err := exec.Command(filepath.Join(binDir, "pg_dumpall"), "-h", srcSock, "-U", "postgres", "--globals-only", "-f", globalsFile).CombinedOutput(); err != nil {
		t.Fatalf("pg_dumpall globals failed: %v\n%s", err, string(out))
	}
	pgDumpFile := filepath.Join(tmp, "db-postgres.sql")
	if out, err := exec.Command(filepath.Join(binDir, "pg_dump"), "-h", srcSock, "-U", "postgres", "--clean", "--if-exists", "--dbname=postgres", "-f", pgDumpFile).CombinedOutput(); err != nil {
		t.Fatalf("pg_dump postgres failed: %v\n%s", err, string(out))
	}
	appDumpFile := filepath.Join(tmp, "db-app.sql")
	if out, err := exec.Command(filepath.Join(binDir, "pg_dump"), "-h", srcSock, "-U", "postgres", "--dbname=app_prod", "-f", appDumpFile).CombinedOutput(); err != nil {
		t.Fatalf("pg_dump app_prod failed: %v\n%s", err, string(out))
	}

	stopDst := initAndStart(dstData, dstSock)
	defer stopDst()

	// Simulasi cluster target sudah punya database lama "legacy_only" + "app_prod" versi lama (downgrade round-trip)
	runPsql(dstSock, "postgres", `CREATE DATABASE legacy_only;`)
	runPsql(dstSock, "legacy_only", `CREATE TABLE public.legacy_tbl (v text); INSERT INTO public.legacy_tbl VALUES ('still-here');`)
	runPsql(dstSock, "postgres", `CREATE DATABASE app_prod;`)
	runPsql(dstSock, "app_prod", `CREATE TABLE public.stale (x int);`)

	// Simulasikan GUC PG 17/18 (transaction_timeout) di header dump saat downgrade ke cluster lebih tua
	injectDowngradeHeader := func(path string) {
		b, rErr := os.ReadFile(path)
		if rErr != nil {
			t.Fatal(rErr)
		}
		content := strings.Replace(string(b), "SET statement_timeout = 0;\n", "SET statement_timeout = 0;\nSET transaction_timeout = 0;\n", 1)
		if !strings.Contains(content, "SET transaction_timeout = 0;") {
			content = "SET transaction_timeout = 0;\n" + content
		}
		if wErr := os.WriteFile(path, []byte(content), 0600); wErr != nil {
			t.Fatal(wErr)
		}
	}
	injectDowngradeHeader(globalsFile)
	injectDowngradeHeader(pgDumpFile)
	injectDowngradeHeader(appDumpFile)

	// Uji transformasi dengan SupportedGUCs tanpa transaction_timeout
	opts := pgSQLTransformOpts{
		TargetMajor: 16,
		SupportedGUCs: map[string]bool{
			"statement_timeout":             true,
			"lock_timeout":                  true,
			"idle_in_transaction_session_timeout": true,
			"client_encoding":               true,
			"standard_conforming_strings":   true,
			"check_function_bodies":         true,
			"xmloption":                     true,
			"client_min_messages":           true,
			"row_security":                  true,
			"default_tablespace":            true,
			"default_table_access_method":   true,
			"default_toast_compression":     true,
			"default_transaction_read_only": true,
		},
	}

	transformFile := func(src, dst string, fn func(*os.File, *os.File) error) {
		in, e1 := os.Open(src)
		if e1 != nil {
			t.Fatal(e1)
		}
		defer in.Close()
		out, e2 := os.Create(dst)
		if e2 != nil {
			t.Fatal(e2)
		}
		defer out.Close()
		if err := fn(in, out); err != nil {
			t.Fatal(err)
		}
	}

	globalsImport := filepath.Join(tmp, "import-globals.sql")
	pgImport := filepath.Join(tmp, "import-postgres.sql")
	appImport := filepath.Join(tmp, "import-app.sql")
	transformFile(globalsFile, globalsImport, func(in, out *os.File) error {
		return transformGlobalsSQL(in, out, opts)
	})
	transformFile(pgDumpFile, pgImport, func(in, out *os.File) error {
		return transformDatabaseSQL(in, out, opts)
	})
	transformFile(appDumpFile, appImport, func(in, out *os.File) error {
		return transformDatabaseSQL(in, out, opts)
	})

	runPsqlFile := func(db, file string) {
		c := exec.Command(filepath.Join(binDir, "psql"), "-h", dstSock, "-U", "postgres", "-d", db, "-v", "ON_ERROR_STOP=1", "-f", file)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("psql -f %s (%s) failed: %v\n%s", file, db, err, string(out))
		}
	}

	// Restore globals, postgres DB, dan app_prod
	runPsqlFile("postgres", globalsImport)
	runPsqlFile("postgres", pgImport)
	runPsql(dstSock, "template1", `DROP DATABASE IF EXISTS "app_prod";`)
	runPsql(dstSock, "template1", `CREATE DATABASE "app_prod" OWNER "app_owner" TEMPLATE template0;`)
	runPsqlFile("app_prod", appImport)

	// Verifikasi: seluruh data dari sumber hadir, sekaligus database non-tabrakan (legacy_only) di target tetap terjaga!
	if got := runPsql(dstSock, "postgres", "SELECT note FROM public.root_notes WHERE id=1;"); got != "kept-in-postgres-db" {
		t.Fatalf("postgres DB data mismatch: got %q", got)
	}
	if got := runPsql(dstSock, "app_prod", "SELECT payload FROM public.items ORDER BY id;"); got != "SET transaction_timeout = 0;\nhello-wdev" {
		t.Fatalf("app_prod data mismatch: got %q", got)
	}
	if got := runPsql(dstSock, "legacy_only", "SELECT v FROM public.legacy_tbl;"); got != "still-here" {
		t.Fatalf("legacy_only DB should have been preserved, got %q", got)
	}
	// Pastikan timestamp test valid (avoid unused import)
	_ = time.Second
}
