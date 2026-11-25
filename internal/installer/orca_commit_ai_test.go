package installer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestOrcaProfileDB(t *testing.T, home, profileID string, rev int64, settings map[string]any, repos, setups []map[string]any) string {
	t.Helper()
	orcaDir := filepath.Join(home, ".config", "orca")
	dbDir := filepath.Join(orcaDir, "profiles", profileID)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idxBytes, _ := json.Marshal(map[string]any{"schemaVersion": 1, "activeProfileId": profileID})
	if err := os.WriteFile(filepath.Join(orcaDir, "orca-profile-index.json"), idxBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dbDir, "profile-state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schema := `
CREATE TABLE profile_state_meta (key TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL);
CREATE TABLE profile_state_documents (
	domain TEXT PRIMARY KEY NOT NULL, payload TEXT NOT NULL,
	domain_version INTEGER NOT NULL, revision INTEGER NOT NULL,
	updated_at INTEGER NOT NULL, content_hash TEXT NOT NULL
);
CREATE TABLE profile_state_automation_runs_meta (
	domain TEXT PRIMARY KEY NOT NULL, presence TEXT NOT NULL,
	domain_version INTEGER NOT NULL, revision INTEGER NOT NULL,
	updated_at INTEGER NOT NULL, content_hash TEXT NOT NULL
);
CREATE TABLE profile_state_document_parts_meta (
	domain TEXT PRIMARY KEY NOT NULL, domain_version INTEGER NOT NULL,
	revision INTEGER NOT NULL, updated_at INTEGER NOT NULL,
	content_hash TEXT NOT NULL, legacy_revision INTEGER
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO profile_state_meta (key, value) VALUES ('profile_id', ?), ('revision', ?)`, profileID, rev); err != nil {
		t.Fatal(err)
	}

	insertDoc := func(domain string, doc any) {
		b, mErr := json.Marshal(doc)
		if mErr != nil {
			t.Fatal(mErr)
		}
		payload := string(b)
		if _, execErr := db.Exec(
			`INSERT INTO profile_state_documents (domain, payload, domain_version, revision, updated_at, content_hash) VALUES (?, ?, 1, ?, 1000, ?)`,
			domain, payload, rev, sha256Hex(payload),
		); execErr != nil {
			t.Fatal(execErr)
		}
	}
	insertDoc("settings", settings)
	insertDoc("repos", repos)
	insertDoc("projectHostSetups", setups)
	return dbPath
}

func TestOrcaCommitAIReadAndSaveClearsStaleRepoOverrides(t *testing.T) {
	home := t.TempDir()

	// Buat biner mock untuk claude agar deteksi agent bekerja
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeBin := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	settings := map[string]any{
		"defaultTuiAgent": "opencode",
		"uiLanguage":      "en",
		"commitMessageAi": map[string]any{
			"enabled":      true,
			"agentId":      "opencode",
			"customPrompt": "old prompt",
		},
		"sourceControlAi": map[string]any{
			"enabled": true,
			"agentId": "opencode",
			"actions": map[string]any{
				"commitMessage": map[string]any{
					"agentId":              "opencode",
					"commandInputTemplate": "{basePrompt}\n\nold prompt",
					"launchPreferences":    map[string]any{"agentId": "opencode"},
				},
				"pullRequest": map[string]any{
					"agentId":              "opencode",
					"commandInputTemplate": "{basePrompt}",
				},
			},
			"instructionsByOperation": map[string]any{
				"commitMessage": "old prompt",
				"pullRequest":   "keep pr prompt",
			},
		},
	}
	repos := []map[string]any{
		{
			"id":   "repo-1",
			"path": "/tmp/repo-1",
			"commitMessageAi": map[string]any{
				"enabled":      true,
				"agentId":      "opencode",
				"customPrompt": "old prompt",
			},
			"sourceControlAi": map[string]any{
				"enabled": true,
				"actionOverrides": map[string]any{
					"commitMessage": map[string]any{
						"agentId":              nil,
						"commandInputTemplate": "{basePrompt}",
					},
					"pullRequest": map[string]any{
						"agentId":              "claude",
						"commandInputTemplate": "{basePrompt}",
					},
				},
				"instructionsByOperation": map[string]any{
					"commitMessage": "stale repo prompt",
				},
			},
		},
	}
	setups := []map[string]any{
		{
			"id":   "repo-1",
			"path": "/tmp/repo-1",
			"sourceControlAi": map[string]any{
				"enabled": true,
				"actionOverrides": map[string]any{
					"commitMessage": map[string]any{
						"agentId":              nil,
						"commandInputTemplate": "{basePrompt}",
					},
				},
			},
		},
	}

	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 42, settings, repos, setups)

	origResolve := resolveOrcaCommitUserFn
	origUserSvc := orcaUserSvcActiveFn
	origSysSvc := orcaSysSvcActiveFn
	origProc := orcaProcessActiveFn
	origStop := stopOrcaForCommitAIFn
	origStart := startOrcaForCommitAIFn
	t.Cleanup(func() {
		resolveOrcaCommitUserFn = origResolve
		orcaUserSvcActiveFn = origUserSvc
		orcaSysSvcActiveFn = origSysSvc
		orcaProcessActiveFn = origProc
		stopOrcaForCommitAIFn = origStop
		startOrcaForCommitAIFn = origStart
	})

	stopped := false
	started := false
	resolveOrcaCommitUserFn = func() (string, string) { return "", home }
	orcaUserSvcActiveFn = func(context.Context, string, string) bool { return true }
	orcaSysSvcActiveFn = func(context.Context) bool { return false }
	orcaProcessActiveFn = func(context.Context, string) bool { return false }
	stopOrcaForCommitAIFn = func(context.Context, LogCallback, string, string) { stopped = true }
	startOrcaForCommitAIFn = func(_ context.Context, _ LogCallback, _, _ string, useUser bool) error {
		if !useUser {
			t.Fatalf("expected user systemd unit restart")
		}
		started = true
		return nil
	}

	ctx := context.Background()
	cur, err := GetOrcaCommitAIConfig(ctx)
	if err != nil {
		t.Fatalf("GetOrcaCommitAIConfig error: %v", err)
	}
	if !cur.DBExists || cur.AgentID != "opencode" || cur.RepoOverrideCount != 1 {
		t.Fatalf("unexpected initial config: %+v", cur)
	}

	var claudeOpt *OrcaCommitAIAgentOption
	for i := range cur.AvailableAgents {
		if cur.AvailableAgents[i].ID == "claude" {
			claudeOpt = &cur.AvailableAgents[i]
			break
		}
	}
	if claudeOpt == nil || !claudeOpt.Installed || claudeOpt.Path != claudeBin {
		t.Fatalf("expected claude detected at %s, got %+v", claudeBin, claudeOpt)
	}

	saved, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled:               true,
		AgentID:               "claude",
		CustomPrompt:          defaultEnglishPrompt,
		UpdateDefaultTUIAgent: true,
		ClearRepoOverrides:    true,
	})
	if err != nil {
		t.Fatalf("SaveOrcaCommitAIConfig error: %v", err)
	}
	if !stopped || !started {
		t.Fatalf("expected orca service stop (%v) and restart (%v)", stopped, started)
	}
	if saved.AgentID != "claude" || saved.CustomPrompt != defaultEnglishPrompt || saved.RepoOverrideCount != 0 {
		t.Fatalf("unexpected saved config: %+v", saved)
	}
	wantTpl := "{basePrompt}\n\n" + defaultEnglishPrompt
	if saved.CommandInputTemplate != wantTpl {
		t.Fatalf("CommandInputTemplate = %q, want %q", saved.CommandInputTemplate, wantTpl)
	}

	// Verifikasi langsung ke SQLite: hash cocok, revision naik ke 43, override pullRequest tetap ada
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var metaRev string
	if err := db.QueryRow(`SELECT value FROM profile_state_meta WHERE key = 'revision'`).Scan(&metaRev); err != nil {
		t.Fatal(err)
	}
	if metaRev != "43" {
		t.Fatalf("meta revision = %q, want 43", metaRev)
	}

	for _, domain := range []string{"settings", "repos", "projectHostSetups"} {
		var payload, chash string
		var rev int64
		if err := db.QueryRow(`SELECT payload, revision, content_hash FROM profile_state_documents WHERE domain = ?`, domain).Scan(&payload, &rev, &chash); err != nil {
			t.Fatal(err)
		}
		if rev != 43 {
			t.Fatalf("%s revision = %d, want 43", domain, rev)
		}
		if chash != sha256Hex(payload) {
			t.Fatalf("%s content_hash mismatch", domain)
		}
		if domain == "settings" {
			var parsed map[string]any
			_ = json.Unmarshal([]byte(payload), &parsed)
			if parsed["uiLanguage"] != "en" || parsed["defaultTuiAgent"] != "claude" {
				t.Fatalf("unexpected settings after save: %v", parsed)
			}
			sc := getMap(parsed, "sourceControlAi")
			commitAct := getMap(getMap(sc, "actions"), "commitMessage")
			if _, hasLaunch := commitAct["launchPreferences"]; hasLaunch {
				t.Fatalf("expected stale launchPreferences cleared on agent switch")
			}
			if getMap(getMap(sc, "actions"), "pullRequest")["agentId"] != "opencode" {
				t.Fatalf("expected pullRequest action preserved")
			}
		}
		if domain == "repos" {
			var parsed []map[string]any
			_ = json.Unmarshal([]byte(payload), &parsed)
			ao := getMap(getMap(parsed[0], "sourceControlAi"), "actionOverrides")
			if _, hasCommit := ao["commitMessage"]; hasCommit {
				t.Fatalf("expected repo commitMessage override cleared")
			}
			if getMap(ao, "pullRequest")["agentId"] != "claude" {
				t.Fatalf("expected unrelated pullRequest repo override preserved")
			}
		}
	}
}

func TestOrcaCommitAIValidationAndDesktopConflict(t *testing.T) {
	home := t.TempDir()
	setupTestOrcaProfileDB(t, home, "local-default", 1, map[string]any{"defaultTuiAgent": "opencode"}, nil, nil)

	origResolve := resolveOrcaCommitUserFn
	origUserSvc := orcaUserSvcActiveFn
	origSysSvc := orcaSysSvcActiveFn
	origProc := orcaProcessActiveFn
	t.Cleanup(func() {
		resolveOrcaCommitUserFn = origResolve
		orcaUserSvcActiveFn = origUserSvc
		orcaSysSvcActiveFn = origSysSvc
		orcaProcessActiveFn = origProc
	})

	resolveOrcaCommitUserFn = func() (string, string) { return "", home }
	orcaUserSvcActiveFn = func(context.Context, string, string) bool { return false }
	orcaSysSvcActiveFn = func(context.Context) bool { return false }
	orcaProcessActiveFn = func(context.Context, string) bool { return false }

	ctx := context.Background()

	// Unknown agent
	if _, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{AgentID: "unknown-agent"}); !errors.Is(err, ErrOrcaCommitAIValidation) {
		t.Fatalf("expected validation error for unknown agent, got %v", err)
	}

	// Custom agent without command
	if _, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{AgentID: "custom"}); !errors.Is(err, ErrOrcaCommitAIValidation) {
		t.Fatalf("expected validation error for empty custom command, got %v", err)
	}

	// Valid custom agent does NOT overwrite defaultTuiAgent with "custom"
	saved, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled:               true,
		AgentID:               "custom",
		CustomAgentCommand:    "my-ai-cli --commit",
		CustomPrompt:          "Custom rule",
		UpdateDefaultTUIAgent: true,
	})
	if err != nil {
		t.Fatalf("unexpected error saving custom agent: %v", err)
	}
	if saved.DefaultTUIAgent != "opencode" || saved.AgentID != "custom" || saved.CustomAgentCommand != "my-ai-cli --commit" {
		t.Fatalf("unexpected custom agent state: %+v", saved)
	}

	// Desktop GUI running outside systemd must block write with conflict error
	orcaProcessActiveFn = func(context.Context, string) bool { return true }
	if _, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled: true,
		AgentID: "opencode",
	}); !errors.Is(err, ErrOrcaCommitAIConflict) {
		t.Fatalf("expected conflict error when Desktop GUI is running, got %v", err)
	}
}

func TestOrcaCommitAIModelParsersAndPersistence(t *testing.T) {
	// 1. Parser outputs
	oc := parseLineModels("opencode/beam-free\nreikurouter/reiku/gpt-5.4-mini\n")
	if len(oc) != 2 || oc[0].ID != "opencode/beam-free" || len(oc[1].ThinkingLevels) == 0 {
		t.Fatalf("unexpected opencode parsed models: %+v", oc)
	}

	agy := parseAntigravityModels("Fetching available models...\ngemini-4-hydrogen-xhigh\tGemini 4 Hydrogen (XHigh)\nclaude-sonnet-5-5-low\tClaude Sonnet 5.5 (Low)\n")
	if len(agy) != 2 || agy[0].ID != "gemini-4-hydrogen-xhigh" || agy[0].Label != "Gemini 4 Hydrogen (XHigh)" {
		t.Fatalf("unexpected antigravity parsed models: %+v", agy)
	}

	omp := parseOmpModels(`notice before json
{"models":[{"provider":"reikurouter","id":"reiku/gpt-5","selector":"reikurouter/reiku/gpt-5","name":"GPT-5","thinking":["low","high","xhigh"]}]}`)
	if len(omp) != 1 || omp[0].ID != "reikurouter/reiku/gpt-5" || omp[0].Description != "reikurouter" || len(omp[0].ThinkingLevels) != 3 {
		t.Fatalf("unexpected omp parsed models: %+v", omp)
	}

	claudeOut := `{"type":"control_response","response":{"subtype":"success","response":{"models":[{"value":"default","resolvedModel":"claude-sonnet-4-6","displayName":"Default"},{"value":"opus","resolvedModel":"claude-opus-4-7","displayName":"Opus","supportsEffort":true,"supportedEffortLevels":["low","high","max"]}]}}}`
	cm := parseClaudeModels(claudeOut)
	if len(cm) != 2 || cm[0].ID != "claude-opus-4-7" || cm[0].Label != "Claude Opus 4.7" || len(cm[0].ThinkingLevels) != 3 || cm[1].ID != "claude-sonnet-4-6" || !cm[1].IsDefault {
		t.Fatalf("unexpected claude parsed models: %+v", cm)
	}

	cur := parseCursorModels("auto - Auto (current, default)\nsonnet-4 - Claude Sonnet 4\n")
	if len(cur) != 2 || !cur[0].IsDefault || cur[1].Label != "Claude Sonnet 4" {
		t.Fatalf("unexpected cursor parsed models: %+v", cur)
	}

	pi := parsePiModels("provider model ctx max thinking\nopenai gpt-5 128000 16000 yes notes\n")
	if len(pi) != 1 || pi[0].ID != "openai/gpt-5" || len(pi[0].ThinkingLevels) == 0 {
		t.Fatalf("unexpected pi parsed models: %+v", pi)
	}

	// 2. Live discovery + persistence into profile-state.db
	home := t.TempDir()
	binDir := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 10, map[string]any{}, nil, nil)

	origResolve := resolveOrcaCommitUserFn
	origUserSvc := orcaUserSvcActiveFn
	origSysSvc := orcaSysSvcActiveFn
	origProc := orcaProcessActiveFn
	origProbe := runUserModelProbeFn
	t.Cleanup(func() {
		resolveOrcaCommitUserFn = origResolve
		orcaUserSvcActiveFn = origUserSvc
		orcaSysSvcActiveFn = origSysSvc
		orcaProcessActiveFn = origProc
		runUserModelProbeFn = origProbe
	})

	resolveOrcaCommitUserFn = func() (string, string) { return "", home }
	orcaUserSvcActiveFn = func(context.Context, string, string) bool { return false }
	orcaSysSvcActiveFn = func(context.Context) bool { return false }
	orcaProcessActiveFn = func(context.Context, string) bool { return false }
	runUserModelProbeFn = func(_ context.Context, _, _, _ string, args []string, _ string, _ time.Duration) (string, error) {
		if len(args) > 0 && args[0] == "models" {
			return "reikurouter/reiku/gpt-5.4\nopencode/big-pickle\n", nil
		}
		return "", nil
	}

	ctx := context.Background()
	disc, err := DiscoverOrcaCommitAIModels(ctx, "opencode", true)
	if err != nil {
		t.Fatalf("DiscoverOrcaCommitAIModels error: %v", err)
	}
	if !disc.LiveDiscovered || len(disc.Models) < 4 {
		t.Fatalf("expected live discovered models merged with seeds, got %+v", disc)
	}

	saved, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled:          true,
		AgentID:          "opencode",
		ModelID:          "reikurouter/reiku/gpt-5.4",
		ThinkingLevel:    "high",
		DiscoveredModels: disc.Models,
		CustomPrompt:     defaultIndonesianPrompt,
	})
	if err != nil {
		t.Fatalf("SaveOrcaCommitAIConfig with model error: %v", err)
	}
	if saved.ModelID != "reikurouter/reiku/gpt-5.4" || saved.ThinkingLevel != "high" {
		t.Fatalf("expected saved model and thinking level, got model=%q thinking=%q", saved.ModelID, saved.ThinkingLevel)
	}

	// Pastikan discoveredModelsByAgent dan modelOverridesByOperation tersimpan di DB
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var payload string
	if err := db.QueryRow(`SELECT payload FROM profile_state_documents WHERE domain = 'settings'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	_ = json.Unmarshal([]byte(payload), &settings)
	sc := getMap(settings, "sourceControlAi")
	opModel := getMap(getMap(sc, "modelOverridesByOperation"), "commitMessage")
	if getMap(opModel, "selectedModelByAgent")["opencode"] != "reikurouter/reiku/gpt-5.4" {
		t.Fatalf("expected selectedModelByAgent saved in modelOverridesByOperation, got %v", opModel)
	}
	if getMap(opModel, "selectedThinkingByModel")["reikurouter/reiku/gpt-5.4"] != "high" {
		t.Fatalf("expected selectedThinkingByModel saved, got %v", opModel)
	}
	if len(extractCachedModelsMapFromSettings(settings)["opencode"]) == 0 {
		t.Fatalf("expected discoveredModelsByAgent persisted in settings")
	}
}

func TestOrcaProcessDetectionSeparatesHeadlessAndDesktop(t *testing.T) {
	headlessOut := strings.Join([]string{
		"1572807 /home/dietpi/.local/lib/orca-ide/orca-ide /home/dietpi/.config/orca/serve-supervisor/abc/out/cli/index.js serve --supervise --force",
		"1572986 /home/dietpi/.config/orca/orcad-artifacts/bin/node /home/dietpi/.config/orca/orcad-artifacts/0.1.0/orcad-server.js --bind 0.0.0.0 --web-client-root /home/dietpi/.local/lib/orca-ide/resources/app.asar.unpacked/out/renderer",
		"1573346 /usr/lib/chromium/chromium --type=renderer --ozone-platform=headless --user-data-dir=/home/dietpi/.config/orca/browser-chromium",
		"1579000 bash -c pgrep -af orca",
	}, "\n")
	if hasOrcaDesktopProcess(headlessOut) {
		t.Fatalf("headless serve/orcad must not be detected as Orca Desktop")
	}
	if !hasAnyOrcaProcess(headlessOut) {
		t.Fatalf("expected hasAnyOrcaProcess to detect headless serve/orcad")
	}

	desktopOut := "210001 /home/dietpi/.local/lib/orca-ide/orca-ide --enable-crashpad\n"
	if !hasOrcaDesktopProcess(desktopOut) {
		t.Fatalf("expected hasOrcaDesktopProcess to detect Orca Desktop GUI")
	}
	if !hasAnyOrcaProcess(desktopOut) {
		t.Fatalf("expected hasAnyOrcaProcess to detect Orca Desktop GUI")
	}

	mixedOut := headlessOut + "\n210001 /home/dietpi/.local/lib/orca-ide/orca-ide --enable-crashpad\n210002 /home/dietpi/.local/lib/orca-ide/orca-ide --type=renderer\n210001 /home/dietpi/.local/lib/orca-ide/orca-ide --enable-crashpad\n"
	pids := extractOrcaDesktopPIDs(mixedOut)
	if len(pids) != 2 || pids[0] != 210001 || pids[1] != 210002 {
		t.Fatalf("unexpected desktop PIDs extracted: %v", pids)
	}
}

func TestOrcaCommitAIAgentArgsAndValidation(t *testing.T) {
	home := t.TempDir()
	dbPath := setupTestOrcaProfileDB(t, home, "local-default", 5, map[string]any{
		"sourceControlAi": map[string]any{
			"enabled": true,
			"agentId": "omp",
			"actions": map[string]any{
				"commitMessage": map[string]any{
					"agentId":              "omp",
					"commandInputTemplate": "{basePrompt}",
					"argumentChoices":      map[string]any{"syntaxAgent": "omp", "nodes": []any{}},
				},
			},
		},
	}, nil, nil)

	origResolve := resolveOrcaCommitUserFn
	origUserSvc := orcaUserSvcActiveFn
	origSysSvc := orcaSysSvcActiveFn
	origProc := orcaProcessActiveFn
	origAnyProc := orcaAnyProcessActiveFn
	t.Cleanup(func() {
		resolveOrcaCommitUserFn = origResolve
		orcaUserSvcActiveFn = origUserSvc
		orcaSysSvcActiveFn = origSysSvc
		orcaProcessActiveFn = origProc
		orcaAnyProcessActiveFn = origAnyProc
	})

	resolveOrcaCommitUserFn = func() (string, string) { return "", home }
	orcaUserSvcActiveFn = func(context.Context, string, string) bool { return false }
	orcaSysSvcActiveFn = func(context.Context) bool { return false }
	orcaProcessActiveFn = func(context.Context, string) bool { return false }
	orcaAnyProcessActiveFn = func(context.Context, string) bool { return false }

	ctx := context.Background()

	// Kutip tidak tertutup harus ditolak
	if _, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled:   true,
		AgentID:   "omp",
		ModelID:   "reikurouter/reiku/gpt-6-luna",
		AgentArgs: `--model "unclosed`,
	}); !errors.Is(err, ErrOrcaCommitAIValidation) {
		t.Fatalf("expected validation error for unclosed quote in agent_args, got %v", err)
	}

	// Simpan agent_args bersama model dan thinking level
	saved, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled:       true,
		AgentID:       "omp",
		ModelID:       "reikurouter/reiku/gpt-6-luna",
		ThinkingLevel: "medium",
		AgentArgs:     "--model reikurouter/reiku/gpt-6-luna --thinking medium",
		CustomPrompt:  defaultIndonesianPrompt,
	})
	if err != nil {
		t.Fatalf("SaveOrcaCommitAIConfig error: %v", err)
	}
	if saved.AgentArgs != "--model reikurouter/reiku/gpt-6-luna --thinking medium" {
		t.Fatalf("unexpected AgentArgs: %q", saved.AgentArgs)
	}

	// Pastikan argumentChoices dibersihkan dan agentArgs tersimpan di DB
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var payload string
	if err := db.QueryRow(`SELECT payload FROM profile_state_documents WHERE domain = 'settings'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	_ = json.Unmarshal([]byte(payload), &parsed)
	commitAct := getMap(getMap(getMap(parsed, "sourceControlAi"), "actions"), "commitMessage")
	if commitAct["agentArgs"] != "--model reikurouter/reiku/gpt-6-luna --thinking medium" {
		t.Fatalf("expected agentArgs in DB, got %v", commitAct["agentArgs"])
	}
	if _, hasChoices := commitAct["argumentChoices"]; hasChoices {
		t.Fatalf("expected argumentChoices removed from commitMessage action")
	}

	// Mengosongkan agent_args dan thinking_level harus menghapus field di DB
	cleared, err := SaveOrcaCommitAIConfig(ctx, nil, OrcaCommitAISaveInput{
		Enabled:      true,
		AgentID:      "omp",
		ModelID:      "reikurouter/reiku/gpt-6-luna",
		AgentArgs:    "",
		CustomPrompt: defaultIndonesianPrompt,
	})
	if err != nil {
		t.Fatalf("SaveOrcaCommitAIConfig clear error: %v", err)
	}
	if cleared.AgentArgs != "" || cleared.ThinkingLevel != "" {
		t.Fatalf("expected cleared AgentArgs and ThinkingLevel, got args=%q thinking=%q", cleared.AgentArgs, cleared.ThinkingLevel)
	}
}
