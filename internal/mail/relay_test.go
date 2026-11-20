package mail

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleStrategy() map[string]any {
	return map[string]any{
		"route": map[string]any{
			"match": map[string]any{
				"0": map[string]any{"if": "sender_domain == 'b.example'", "then": "'relay_b_example'"},
				"1": map[string]any{"if": "sender_domain == 'a.example'", "then": "'relay_a_example'"},
				"2": map[string]any{"if": "rcpt_domain == 'keep.example'", "then": "'mx'", "extra": "keep-me"},
			},
			"else": "'relay_default'",
			"note": "jangan dibuang",
		},
	}
}

func TestParseOutboundRouteSortsBranches(t *testing.T) {
	route, err := parseOutboundRoute(sampleStrategy())
	if err != nil {
		t.Fatalf("parse gagal: %v", err)
	}
	if route.Else != "relay_default" {
		t.Fatalf("else = %q, want relay_default", route.Else)
	}
	if len(route.Branches) != 3 {
		t.Fatalf("jumlah branch = %d, want 3", len(route.Branches))
	}
	if route.Branches[0].Then != "relay_b_example" {
		t.Fatalf("urutan branch tidak mengikuti key numerik: %+v", route.Branches)
	}
	if route.Branches[2].Extra["extra"] != "keep-me" {
		t.Fatalf("field tambahan hilang: %+v", route.Branches[2])
	}
}

func TestUpsertDomainReplacesExistingBranch(t *testing.T) {
	route, err := parseOutboundRoute(sampleStrategy())
	if err != nil {
		t.Fatal(err)
	}
	route.upsertDomain("A.Example.", "relay_new")
	if len(route.Branches) != 3 {
		t.Fatalf("upsert menambah branch duplikat: %d", len(route.Branches))
	}
	if !route.hasDomain("a.example") {
		t.Fatal("domain a.example tidak terdeteksi")
	}
	for _, b := range route.Branches {
		if strings.TrimSpace(b.If) == relayMatchCondition("a.example") && b.Then != "relay_new" {
			t.Fatalf("branch tidak diperbarui: %+v", b)
		}
	}
}

func TestRemoveDomainKeepsOtherBranches(t *testing.T) {
	route, err := parseOutboundRoute(sampleStrategy())
	if err != nil {
		t.Fatal(err)
	}
	if !route.removeDomain("a.example") {
		t.Fatal("removeDomain tidak menghapus branch")
	}
	if route.hasDomain("a.example") {
		t.Fatal("branch a.example masih ada")
	}
	if !route.hasDomain("b.example") {
		t.Fatal("branch lain ikut terhapus")
	}
	if route.removeDomain("a.example") {
		t.Fatal("removeDomain dua kali harus false")
	}
}

func TestMarshalKeepsRouteMetadataAndIsDeterministic(t *testing.T) {
	route, err := parseOutboundRoute(sampleStrategy())
	if err != nil {
		t.Fatal(err)
	}
	route.upsertDomain("c.example", "relay_c_example")
	first, err := json.Marshal(route.marshal())
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(route.marshal())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("marshal tidak deterministik")
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["note"] != "jangan dibuang" {
		t.Fatalf("metadata route hilang: %v", decoded)
	}
	match, _ := decoded["match"].(map[string]any)
	if len(match) != 4 {
		t.Fatalf("jumlah match = %d, want 4", len(match))
	}
	for i := 0; i < len(match); i++ {
		if _, ok := match[itoa(i)]; !ok {
			t.Fatalf("index match tidak berurutan: %v", match)
		}
	}
}

func TestMarshalQuotesThenAndElse(t *testing.T) {
	route := &outboundRoute{Else: "relay_default", Raw: map[string]any{}}
	route.upsertDomain("a.example", "relay_a")
	out := route.marshal()
	if out["else"] != "'relay_default'" {
		t.Fatalf("else tidak dikutip: %#v", out["else"])
	}
	match := out["match"].(map[string]any)
	branch := match["0"].(map[string]any)
	if branch["then"] != "'relay_a'" {
		t.Fatalf("then tidak dikutip: %#v", branch["then"])
	}
}

func TestVerifyRejectsMismatch(t *testing.T) {
	want, err := parseOutboundRoute(sampleStrategy())
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseOutboundRoute(sampleStrategy())
	if err != nil {
		t.Fatal(err)
	}
	got.upsertDomain("a.example", "relay_lain")
	if err := want.verify(got); err == nil {
		t.Fatal("verify harus gagal saat route berbeda")
	}

	got2, _ := parseOutboundRoute(sampleStrategy())
	got2.removeDomain("b.example")
	if err := want.verify(got2); err == nil {
		t.Fatal("verify harus gagal saat branch hilang")
	}
}

func TestDomainFromCondition(t *testing.T) {
	cases := map[string]string{
		"sender_domain == 'a.example'":  "a.example",
		`sender_domain == "b.example"`:  "b.example",
		"sender_domain == 'A.Example.'": "a.example",
	}
	for cond, want := range cases {
		got, ok := domainFromCondition(cond)
		if !ok || got != want {
			t.Fatalf("domainFromCondition(%q) = %q,%v want %q", cond, got, ok, want)
		}
	}
	if _, ok := domainFromCondition("rcpt_domain == 'x'"); ok {
		t.Fatal("kondisi selain sender_domain dianggap valid")
	}
}

func TestRelayRouteName(t *testing.T) {
	if relayRouteName("") != DefaultRelayRoute || relayRouteName("global") != DefaultRelayRoute {
		t.Fatal("scope global harus memakai route default")
	}
	if got := relayRouteName("Mail.Example."); got != "relay_mail_example" {
		t.Fatalf("relayRouteName = %q", got)
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}
