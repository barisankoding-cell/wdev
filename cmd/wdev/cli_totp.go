package main

import (
	"fmt"
	"os"
	"strings"
	"wdev/internal/auth"
)

func handlePanel2FACLI(authMgr *auth.Manager, args []string) {
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch action {
	case "status", "info":
		st := authMgr.GetTOTPStatus()
		fmt.Println("=== STATUS TWO-FACTOR AUTHENTICATION (2FA) ===")
		fmt.Printf(" Status 2FA       : %v\n", st.Enabled)
		fmt.Printf(" Wajib di Terminal: %v\n", st.TerminalRequired)
		fmt.Printf(" Recovery Codes   : %d tersisa\n", st.RecoveryRemaining)
		fmt.Printf(" Akun / Issuer    : %s (%s)\n", st.Account, st.Issuer)
	case "disable", "off":
		if err := authMgr.DisableTOTP(); err != nil {
			fmt.Fprintf(os.Stderr, "Gagal menonaktifkan 2FA: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Two-Factor Authentication (2FA) panel dinonaktifkan.")
	case "terminal":
		if len(args) < 2 {
			fmt.Println("Penggunaan: wdev panel 2fa terminal <on|off>")
			os.Exit(1)
		}
		mode := strings.ToLower(strings.TrimSpace(args[1]))
		enable := mode == "on" || mode == "1" || mode == "true"
		if enable && !authMgr.IsTOTPEnabled() {
			fmt.Fprintln(os.Stderr, "Error: aktifkan 2FA terlebih dahulu melalui Web UI sebelum mewajibkannya di terminal.")
			os.Exit(1)
		}
		if err := authMgr.SetTOTPTerminalRequired(enable); err != nil {
			fmt.Fprintf(os.Stderr, "Gagal mengubah kebijakan 2FA terminal: %v\n", err)
			os.Exit(1)
		}
		if enable {
			fmt.Println("✓ Verifikasi 2FA sekarang wajib saat membuka sesi terminal root.")
		} else {
			fmt.Println("✓ Verifikasi 2FA tidak lagi diwajibkan untuk membuka sesi terminal root.")
		}
	default:
		fmt.Println("Penggunaan: wdev panel 2fa <status|disable|terminal <on|off>>")
		os.Exit(1)
	}
}
