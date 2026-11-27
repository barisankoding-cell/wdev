package tui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRenderMenuDisplaysTenCategories(t *testing.T) {
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	var tuiApp TUI
	tuiApp.renderMenu()

	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	out := buf.String()
	if !strings.Contains(out, "MENU UTAMA (10 KATEGORI):") {
		t.Fatalf("expected menu header with 10 categories, got:\n%s", out)
	}
	if !strings.Contains(out, "[10]") || !strings.Contains(out, "Docker & Compose v5") {
		t.Fatalf("expected [10] Docker & Compose v5 in menu, got:\n%s", out)
	}
	if !strings.Contains(out, "(22 Item)") {
		t.Fatalf("expected (22 Item) in modular install menu label, got:\n%s", out)
	}
}
