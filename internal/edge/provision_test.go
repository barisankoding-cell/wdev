package edge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAPIKeyGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "api_key.secret")

	key, err := resolveAPIKeyAt("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) < 16 {
		t.Fatalf("API key terlalu pendek: %q", key)
	}

	data, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("API key tidak ditulis: %v", err)
	}
	if strings.TrimSpace(string(data)) != key {
		t.Fatalf("isi file berbeda dengan key: %q vs %q", data, key)
	}

	// Idempoten: panggilan kedua harus memakai kunci yang sama.
	again, err := resolveAPIKeyAt("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if again != key {
		t.Fatalf("kunci berubah: %q -> %q", key, again)
	}

	// Kunci eksplisit harus menang.
	explicit, err := resolveAPIKeyAt("kunci-dari-operator", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if explicit != "kunci-dari-operator" {
		t.Fatalf("kunci eksplisit diabaikan: %q", explicit)
	}
}

func TestResolveAPIKeyIgnoresEmptyFile(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "api_key.secret")
	if err := os.WriteFile(keyFile, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := resolveAPIKeyAt("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(key) == "" {
		t.Fatal("file kosong seharusnya memicu pembuatan kunci baru")
	}
}

func TestEdgeConfRoundTrip(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "edge.conf")
	conf := "PUBLIC_IP=203.0.113.10\nNETBIRD_IP=100.64.0.9\nPDNS_API_PORT=8081\nAGENT_PORT=8082\nAPI_KEY=abc123\n"
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "API_KEY=abc123") {
		t.Fatalf("API_KEY tidak tersimpan:\n%s", data)
	}
}
