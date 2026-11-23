package installer

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func withOrcaCommitAISyncHooks(t *testing.T, home string, svcActive, procActive *bool, stopCount, startCount *int) {
	t.Helper()
	origResolve := resolveOrcaCommitUserFn
	origUserSvc := orcaUserSvcActiveFn
	origSysSvc := orcaSysSvcActiveFn
	origProc := orcaProcessActiveFn
	origAnyProc := orcaAnyProcessActiveFn
	origStop := stopOrcaForCommitAIFn
	origStart := startOrcaForCommitAIFn
	origForceQuit := forceQuitOrcaDesktopForCommitAIFn
	t.Cleanup(func() {
		resolveOrcaCommitUserFn = origResolve
		orcaUserSvcActiveFn = origUserSvc
		orcaSysSvcActiveFn = origSysSvc
		orcaProcessActiveFn = origProc
		orcaAnyProcessActiveFn = origAnyProc
		stopOrcaForCommitAIFn = origStop
		startOrcaForCommitAIFn = origStart
		forceQuitOrcaDesktopForCommitAIFn = origForceQuit
		orcaCommitAIPostApplyVerifyWait = 2500 * time.Millisecond
	})

	resolveOrcaCommitUserFn = func() (string, string) { return "", home }
	orcaUserSvcActiveFn = func(context.Context, string, string) bool { return *svcActive }
	orcaSysSvcActiveFn = func(context.Context) bool { return false }
	orcaProcessActiveFn = func(context.Context, string) bool { return *procActive }
	orcaAnyProcessActiveFn = func(context.Context, string) bool { return *svcActive || *procActive }
	stopOrcaForCommitAIFn = func(context.Context, LogCallback, string, string) { *stopCount++ }
	startOrcaForCommitAIFn = func(context.Context, LogCallback, string, string, bool) error {
		*startCount++
		return nil
	}
	forceQuitOrcaDesktopForCommitAIFn = func(context.Context, LogCallback, string) {
		*procActive = false
	}
	orcaCommitAIPostApplyVerifyWait = time.Millisecond
}

func testOrcaCommitAISettings() map[string]any {
	return map[string]any{
		"defaultTuiAgent": "opencode",
		"commitMessageAi": map[string]any{
			"enabled":      true,
			"agentId":      "opencode",
			"customPrompt": "",
		},
		"sourceControlAi": map[string]any{
			"enabled": true,
			"agentId": "opencode",
			"actions": map[string]any{
				"commitMessage": map[string]any{
					"agentId":              "opencode",
					"commandInputTemplate": "{basePrompt}",
				},
			},
			"instructionsByOperation": map[string]any{"commitMessage": ""},
		},
	}
}

func mutateTestOrcaSettings(t *testing.T, dbPath string, mutate func(map[string]any)) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var payload string
	if err := db.QueryRowContext(context.Background(),
		`SELECT payload FROM profile_state_documents WHERE domain = 'settings'`,
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		t.Fatal(err)
	}
	mutate(settings)
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE profile_state_documents SET payload = ?, revision = revision + 1, updated_at = ?, content_hash = ? WHERE domain = 'settings'`,
		string(raw), time.Now().UnixMilli(), sha256Hex(string(raw))); err != nil {
		t.Fatal(err)
	}
}

func TestOrcaCommitAISyncSetThenStatusSynced(t *testing.T) {
	home := t.TempDir()
	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)
	svcActive, procActive := true, false
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	ctx := context.Background()
	status, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled:               true,
		AgentID:               "opencode",
		ModelID:               "default",
		CustomPrompt:          "Tulis commit Bahasa Indonesia",
		UpdateDefaultTUIAgent: true,
		ClearRepoOverrides:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Managed || !status.InSync || status.State != orcaCommitAISyncStateSynced {
		t.Fatalf("expected managed+synced, got %+v", status)
	}
	if stopCount != 1 || startCount != 1 {
		t.Fatalf("expected orca stop/start once, got stop=%d start=%d", stopCount, startCount)
	}
	if _, err := os.Stat(orcaCommitAIStatePath(home)); err != nil {
		t.Fatalf("desired state file not written: %v", err)
	}

	mutateTestOrcaSettings(t, dbPath, func(settings map[string]any) {
		sc := settings["sourceControlAi"].(map[string]any)
		tpl := sc["actions"].(map[string]any)["commitMessage"].(map[string]any)["commandInputTemplate"].(string)
		if !strings.Contains(tpl, "Tulis commit Bahasa Indonesia") {
			t.Fatalf("prompt tidak tersimpan di template: %q", tpl)
		}
	})

	again, err := GetOrcaCommitAISyncStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !again.InSync || again.State != orcaCommitAISyncStateSynced {
		t.Fatalf("second status should stay synced, got %+v", again)
	}
}

func TestOrcaCommitAISyncDetectsDriftAndReconciles(t *testing.T) {
	home := t.TempDir()
	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)
	svcActive, procActive := true, false
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	ctx := context.Background()
	if _, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled:               true,
		AgentID:               "opencode",
		ModelID:               "default",
		CustomPrompt:          "Pesan asli dari wdev",
		UpdateDefaultTUIAgent: true,
		ClearRepoOverrides:    true,
	}); err != nil {
		t.Fatal(err)
	}

	// Simulasikan Orca ADE menimpa pengaturan (agent + prompt berubah).
	mutateTestOrcaSettings(t, dbPath, func(settings map[string]any) {
		settings["defaultTuiAgent"] = "claude"
		settings["commitMessageAi"].(map[string]any)["agentId"] = "claude"
		sc := settings["sourceControlAi"].(map[string]any)
		sc["agentId"] = "claude"
		sc["actions"].(map[string]any)["commitMessage"].(map[string]any)["agentId"] = "claude"
		sc["actions"].(map[string]any)["commitMessage"].(map[string]any)["commandInputTemplate"] = "{basePrompt}\n\nDitimpa Orca"
	})

	status, err := GetOrcaCommitAISyncStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.InSync || status.State != orcaCommitAISyncStateDrift {
		t.Fatalf("expected drift, got %+v", status)
	}
	if len(status.Drift) == 0 {
		t.Fatal("expected drift details")
	}

	reconciled, err := ReconcileOrcaCommitAI(ctx, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.InSync || reconciled.State != orcaCommitAISyncStateSynced {
		t.Fatalf("expected reconciled back to synced, got %+v", reconciled)
	}
	if stopCount != 2 || startCount != 2 {
		t.Fatalf("expected second stop/start, got stop=%d start=%d", stopCount, startCount)
	}

	mutateTestOrcaSettings(t, dbPath, func(settings map[string]any) {
		sc := settings["sourceControlAi"].(map[string]any)
		tpl := sc["actions"].(map[string]any)["commitMessage"].(map[string]any)["commandInputTemplate"].(string)
		if strings.Contains(tpl, "Ditimpa Orca") {
			t.Fatalf("drift tidak diperbaiki: %q", tpl)
		}
	})
}

func TestOrcaCommitAISyncBlockedWhenOrcaDesktopOpen(t *testing.T) {
	home := t.TempDir()
	setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)
	svcActive, procActive := false, false
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	ctx := context.Background()
	// Simpan saat Orca Desktop belum berjalan.
	if _, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled: true, AgentID: "opencode", ModelID: "default", CustomPrompt: "Asli", ClearRepoOverrides: true,
	}); err != nil {
		t.Fatal(err)
	}
	svcActive, procActive = false, true

	status, err := GetOrcaCommitAISyncStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.OrcaDesktopOpen {
		t.Fatal("expected OrcaDesktopOpen true")
	}

	// Tidak ada drift, jadi reconcile tidak boleh menyentuh Orca sama sekali.
	if _, err := ReconcileOrcaCommitAI(ctx, nil, false); err != nil {
		t.Fatal(err)
	}
	if stopCount != 0 || startCount != 0 {
		t.Fatalf("reconcile tanpa drift tidak boleh stop/start Orca: stop=%d start=%d", stopCount, startCount)
	}
}

func TestOrcaCommitAISyncDetectsMirrorInconsistency(t *testing.T) {
	home := t.TempDir()
	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)
	svcActive, procActive := false, false
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	ctx := context.Background()
	if _, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled: true, AgentID: "opencode", ModelID: "default", CustomPrompt: "Asli", ClearRepoOverrides: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Orca menulis key level action saja, tapi lupa menyamakan key top-level.
	mutateTestOrcaSettings(t, dbPath, func(settings map[string]any) {
		settings["commitMessageAi"].(map[string]any)["agentId"] = "omp"
	})

	status, err := GetOrcaCommitAISyncStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range status.Drift {
		if strings.Contains(d, "agentId tidak konsisten") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mirror drift, got %v", status.Drift)
	}
}

func TestOrcaCommitAISyncPopulatesRuntimeHostKeys(t *testing.T) {
	home := t.TempDir()
	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)

	// Simulasikan dokumen UI dari Web Client mode Headless yang mencatat host runtime:web-123.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	uiPayload := `{"setupScriptPromptDismissedRepoIds":["generation-v1:runtime:web-123\u0000repo-1"]}`
	if _, err := db.Exec(`INSERT INTO profile_state_documents (domain, payload, domain_version, revision, updated_at, content_hash) VALUES ('ui', ?, 1, 10, 1000, ?)`, uiPayload, sha256Hex(uiPayload)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	svcActive, procActive := true, false
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	ctx := context.Background()
	status, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled:            true,
		AgentID:            "omp",
		ModelID:            "reikurouter/reiku/gpt-6-luna",
		ThinkingLevel:      "medium",
		CustomPrompt:       "Bahasa Indonesia",
		ClearRepoOverrides: true,
		DiscoveredModels: []OrcaCommitAIModel{
			{ID: "reikurouter/reiku/gpt-6-luna", Label: "GPT-6 Luna", Source: "discovered"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.InSync || status.OrcaDesktopOpen {
		t.Fatalf("expected synced and OrcaDesktopOpen=false in headless mode, got %+v", status)
	}

	mutateTestOrcaSettings(t, dbPath, func(settings map[string]any) {
		sc := settings["sourceControlAi"].(map[string]any)
		commitModels := sc["modelOverridesByOperation"].(map[string]any)["commitMessage"].(map[string]any)
		byHost := commitModels["selectedModelByAgentByHost"].(map[string]any)
		if byHost["runtime:web-123"].(map[string]any)["omp"] != "reikurouter/reiku/gpt-6-luna" {
			t.Fatalf("expected runtime:web-123 host model synced, got %v", byHost)
		}
		if _, hasLocal := byHost["local"]; hasLocal {
			t.Fatalf("local key should stay removed from selectedModelByAgentByHost, got %v", byHost)
		}
		discByHost := sc["discoveredModelsByAgentByHost"].(map[string]any)
		if _, hasRuntimeDisc := discByHost["runtime:web-123"]; !hasRuntimeDisc {
			t.Fatalf("expected discoveredModelsByAgentByHost populated for runtime:web-123")
		}
	})
}

func TestOrcaCommitAISaveForceQuitsOpenOrcaDesktop(t *testing.T) {
	home := t.TempDir()
	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)
	svcActive, procActive := false, true
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	forceQuitCalls := 0
	forceQuitOrcaDesktopForCommitAIFn = func(context.Context, LogCallback, string) {
		forceQuitCalls++
		procActive = false
	}

	ctx := context.Background()
	status, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled:               true,
		AgentID:               "claude",
		ModelID:               "default",
		CustomPrompt:          "Simpan sambil paksa keluar Orca Desktop",
		UpdateDefaultTUIAgent: true,
		ClearRepoOverrides:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if forceQuitCalls != 1 {
		t.Fatalf("expected forceQuitCalls=1, got %d", forceQuitCalls)
	}
	if procActive {
		t.Fatalf("expected Orca Desktop process to be closed")
	}
	if !status.InSync || status.State != orcaCommitAISyncStateSynced || status.OrcaDesktopOpen {
		t.Fatalf("expected synced and OrcaDesktopOpen=false, got %+v", status)
	}
	if stopCount != 0 || startCount != 0 {
		t.Fatalf("desktop mode should not invoke systemd stop/start, got stop=%d start=%d", stopCount, startCount)
	}

	mutateTestOrcaSettings(t, dbPath, func(settings map[string]any) {
		sc := settings["sourceControlAi"].(map[string]any)
		if sc["agentId"] != "claude" {
			t.Fatalf("expected agentId claude saved, got %v", sc["agentId"])
		}
	})
}

func TestOrcaCommitAISaveRemainsBlockedIfForceQuitFails(t *testing.T) {
	home := t.TempDir()
	setupTestOrcaProfileDB(t, home, "local-default", 10, testOrcaCommitAISettings(), nil, nil)
	svcActive, procActive := false, true
	stopCount, startCount := 0, 0
	withOrcaCommitAISyncHooks(t, home, &svcActive, &procActive, &stopCount, &startCount)

	forceQuitCalls := 0
	forceQuitOrcaDesktopForCommitAIFn = func(context.Context, LogCallback, string) {
		forceQuitCalls++
		// Simulasikan proses tetap aktif walau sudah dipaksa keluar.
	}

	ctx := context.Background()
	status, err := SetOrcaCommitAIDesiredState(ctx, nil, OrcaCommitAISaveInput{
		Enabled: true, AgentID: "claude", ModelID: "default", CustomPrompt: "Gagal keluar", ClearRepoOverrides: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if forceQuitCalls != 1 {
		t.Fatalf("expected forceQuitCalls=1, got %d", forceQuitCalls)
	}
	if status.InSync || status.State != orcaCommitAISyncStateBlocked {
		t.Fatalf("expected blocked state when desktop remains open, got %+v", status)
	}
}
