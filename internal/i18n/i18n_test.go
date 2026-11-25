package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", LocaleEN},
		{"en", LocaleEN},
		{"en-US,en;q=0.9", LocaleEN},
		{"id", LocaleID},
		{"id-ID,id;q=0.9,en-US;q=0.8", LocaleID},
		{"en;q=0.5, id;q=0.9", LocaleID},
		{"id;q=0.4, en-US;q=0.9", LocaleEN},
		{"id;q=0", LocaleEN},
		{"fr-FR,de;q=0.8", LocaleEN},
		{"ID_id", LocaleID},
	}
	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFromRequest(t *testing.T) {
	reqEmpty := httptest.NewRequest(http.MethodGet, "/api/ports", nil)
	if got := FromRequest(reqEmpty); got != LocaleEN {
		t.Fatalf("empty request locale = %q, want en", got)
	}

	reqHeader := httptest.NewRequest(http.MethodGet, "/api/ports", nil)
	reqHeader.Header.Set("Accept-Language", "id-ID,id;q=0.9,en;q=0.8")
	if got := FromRequest(reqHeader); got != LocaleID {
		t.Fatalf("Accept-Language id request = %q, want id", got)
	}

	// Query ?lang= overrides Accept-Language
	reqQuery := httptest.NewRequest(http.MethodGet, "/api/ports?lang=en", nil)
	reqQuery.Header.Set("Accept-Language", "id")
	if got := FromRequest(reqQuery); got != LocaleEN {
		t.Fatalf("?lang=en override = %q, want en", got)
	}

	reqQueryID := httptest.NewRequest(http.MethodGet, "/api/ports?lang=id", nil)
	if got := FromRequest(reqQueryID); got != LocaleID {
		t.Fatalf("?lang=id = %q, want id", got)
	}
}

func TestPick(t *testing.T) {
	if got := Pick("en", "hello", "halo"); got != "hello" {
		t.Fatalf("Pick(en) = %q, want hello", got)
	}
	if got := Pick("id-ID", "hello", "halo"); got != "halo" {
		t.Fatalf("Pick(id-ID) = %q, want halo", got)
	}
}
