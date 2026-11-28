package main

import (
	"testing"
	"wdev/internal/auth"
	"wdev/internal/config"
	"wdev/internal/store"
)

func TestTruncateAndFormatBytesHelpers(t *testing.T) {
	if got := truncateStr("abcdef", 10); got != "abcdef" {
		t.Fatalf("truncateStr short = %q", got)
	}
	if got := truncateStr("abcdefghijkl", 8); got != "abcde..." {
		t.Fatalf("truncateStr long = %q", got)
	}
	if got := formatBytesCompact(1536); got != "1.5 KiB" {
		t.Fatalf("formatBytesCompact(1536) = %q", got)
	}
	if got := dashIfEmpty("  "); got != "-" {
		t.Fatalf("dashIfEmpty blank = %q", got)
	}
}

func TestHandlePanel2FACLIStatusAndDisable(t *testing.T) {
	db, err := store.New(t.TempDir() + "/cli_2fa.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mgr := auth.New(&config.Config{HTTPPort: "3200"}, db)
	enr, err := mgr.BeginTOTPEnrollment("localhost:3200")
	if err != nil {
		t.Fatal(err)
	}
	_ = enr
	// Disable via CLI helper
	handlePanel2FACLI(mgr, []string{"disable"})
	if mgr.IsTOTPEnabled() || mgr.GetTOTPStatus().Pending {
		t.Fatalf("expected handlePanel2FACLI disable to clear TOTP state")
	}
}
