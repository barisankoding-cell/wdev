package monitor

import "testing"

func TestParsePostgresMajorFromVersion(t *testing.T) {
	cases := []struct {
		out  string
		want string
	}{
		{"postgres (PostgreSQL) 16.14", "16"},
		{"postgres (PostgreSQL) 16.16", "16"},
		{"psql (PostgreSQL) 17.9", "17"},
		{"psql (PostgreSQL) 18.3\n", "18"},
		{"", ""},
		{"unexpected", ""},
	}
	for _, c := range cases {
		if got := parsePostgresMajorFromVersion(c.out); got != c.want {
			t.Errorf("parsePostgresMajorFromVersion(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestPickPostgresSystemdUnit(t *testing.T) {
	t.Run("active PGDG unit wins over newer inactive unit", func(t *testing.T) {
		got := pickPostgresSystemdUnit(
			func(u string) bool { return u == "postgresql-16" },
			func(u string) bool { return u == "postgresql-16" || u == "postgresql-18" },
			"16",
		)
		if got != "postgresql-16" {
			t.Fatalf("got %q, want postgresql-16", got)
		}
	})

	t.Run("active Fedora distro unit wins when running", func(t *testing.T) {
		got := pickPostgresSystemdUnit(
			func(u string) bool { return u == "postgresql" },
			func(u string) bool { return u == "postgresql" },
			"16",
		)
		if got != "postgresql" {
			t.Fatalf("got %q, want postgresql", got)
		}
	})

	t.Run("stopped PGDG unit matches active major", func(t *testing.T) {
		got := pickPostgresSystemdUnit(
			func(string) bool { return false },
			func(u string) bool { return u == "postgresql-16" || u == "postgresql-18" },
			"16",
		)
		if got != "postgresql-16" {
			t.Fatalf("got %q, want postgresql-16", got)
		}
	})

	t.Run("fallback to postgresql", func(t *testing.T) {
		got := pickPostgresSystemdUnit(
			func(string) bool { return false },
			func(string) bool { return false },
			"16",
		)
		if got != "postgresql" {
			t.Fatalf("got %q, want postgresql", got)
		}
	})
}

func TestParsePHPVersionIgnoresWarnings(t *testing.T) {
	noisy := "PHP Warning:  PHP Startup: Unable to load dynamic library 'pgsql' (undefined symbol: PQsocketPoll, version RHPG_18) in Unknown on line 0\n" +
		"PHP 8.4.27 (fpm-fcgi) (built: Oct 19 2026 14:38:59)\n" +
		"Copyright (c) The PHP Group\n"
	if got := parsePHPVersionFromOutput(noisy); got != "v8.4.27" {
		t.Fatalf("parsePHPVersionFromOutput = %q, want v8.4.27", got)
	}

	noisyShort := "PHP Warning:  something\n8.4\n"
	if got := parsePHPMajorMinor(noisyShort); got != "8.4" {
		t.Fatalf("parsePHPMajorMinor = %q, want 8.4", got)
	}

	noisyPlain := "PHP Warning: something\n8.4.27\n"
	if got := parsePHPVersionFromOutput(noisyPlain); got != "v8.4.27" {
		t.Fatalf("parsePHPVersionFromOutput(plain) = %q, want v8.4.27", got)
	}
}

func TestResolveActivePostgresMajorFromProbes(t *testing.T) {
	t.Run("stopped downgraded cluster uses enabled unit instead of highest binary", func(t *testing.T) {
		got := resolveActivePostgresMajorFromProbes(
			func(string) bool { return false },
			func(u string) bool { return u == "postgresql-16" },
			func() string { return "" },
			func(p string) bool { return p == "/usr/pgsql-18/bin/postgres" || p == "/usr/pgsql-16/bin/postgres" },
			func(string, ...string) string { return "" },
		)
		if got != "16" {
			t.Fatalf("got %q, want 16", got)
		}
	})

	t.Run("stopped cluster uses state file when no enabled unit", func(t *testing.T) {
		got := resolveActivePostgresMajorFromProbes(
			func(string) bool { return false },
			func(string) bool { return false },
			func() string { return "16" },
			func(p string) bool { return p == "/usr/pgsql-18/bin/postgres" || p == "/usr/pgsql-16/bin/postgres" },
			func(string, ...string) string { return "" },
		)
		if got != "16" {
			t.Fatalf("got %q, want 16", got)
		}
	})
}
