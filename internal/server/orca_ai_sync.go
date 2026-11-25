package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"wdev/internal/i18n"
	"wdev/internal/installer"
)

const orcaCommitAISyncInterval = 30 * time.Second

// startOrcaCommitAISyncLoop menjaga agar konfigurasi Commit AI Orca selalu
// sesuai dengan pengaturan yang dibuat lewat panel wdev, termasuk ketika Orca
// menimpanya sendiri setelah dihidupkan ulang atau setelah cloud sync.
func (s *Server) startOrcaCommitAISyncLoop() {
	if s.orcaSyncCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.orcaSyncCancel = cancel

	go func() {
		ticker := time.NewTicker(orcaCommitAISyncInterval)
		defer ticker.Stop()

		// Beri jeda saat startup agar tidak berebut dengan inisialisasi Orca.
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}

		for {
			if ctx.Err() != nil {
				return
			}
			if _, err := installer.ReconcileOrcaCommitAI(ctx, nil, false); err != nil {
				fmt.Printf("[wdev] gagal menyinkronkan Orca Commit AI: %v\n", err)
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Server) handleWorkspaceOrcaAISync(w http.ResponseWriter, r *http.Request) {
	lang := i18n.FromRequest(r)
	switch r.Method {
	case http.MethodGet:
		status, err := installer.GetOrcaCommitAISyncStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodPost:
		// Body opsional: {"force": true} (default tetap paksa terapkan ulang).
		force := true
		if r.Body != nil {
			var payload struct {
				Force *bool `json:"force"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload.Force != nil {
				force = *payload.Force
			}
		}
		status, err := installer.ReconcileOrcaCommitAILocale(r.Context(), nil, force, lang)
		if err != nil {
			switch {
			case errors.Is(err, installer.ErrOrcaCommitAIValidation):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, installer.ErrOrcaCommitAIConflict):
				writeError(w, http.StatusConflict, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		message := i18n.Pick(lang, "Orca Commit AI sync succeeded.", "Sinkronisasi Orca Commit AI berhasil.")
		if status != nil && !status.InSync {
			message = status.LastError
			if message == "" && len(status.Drift) > 0 {
				message = i18n.Pick(lang, "Remaining differences: ", "Masih ada perbedaan: ") + strings.Join(status.Drift, ", ")
			}
			if message == "" {
				message = i18n.Pick(lang, "Sync did not complete yet.", "Sinkronisasi belum berhasil.")
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  statusString(status),
			"message": message,
			"sync":    status,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, i18n.Pick(lang, "Method not allowed", "Method tidak diizinkan"))
	}
}

func statusString(status *installer.OrcaCommitAISyncStatus) string {
	if status == nil {
		return "unknown"
	}
	return status.State
}
