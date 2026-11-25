package installer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

var (
	// ErrOrcaCommitAIValidation menandakan input konfigurasi Orca Commit AI tidak valid.
	ErrOrcaCommitAIValidation = errors.New("validasi konfigurasi orca commit ai gagal")
	// ErrOrcaCommitAIConflict menandakan kondisi runtime/database Orca belum siap atau bentrok.
	ErrOrcaCommitAIConflict = errors.New("kondisi runtime orca tidak memungkinkan penyimpanan")
)

const (
	defaultOrcaProfileID       = "local-default"
	orcaBasePromptToken        = "{basePrompt}"
	defaultIndonesianPrompt    = "Gunakan format Conventional Commits (feat, fix, refactor, docs, chore, test). Judul maksimal 72 karakter. Tulis pesan commit dalam Bahasa Indonesia yang singkat, padat, dan jelas."
	defaultEnglishPrompt       = "Follow Conventional Commits (feat, fix, refactor, docs, chore, test). Keep title under 72 characters. Write in clear and concise English."
	defaultDetailedIndoPrompt  = "Gunakan format Conventional Commits (feat, fix, refactor, docs, chore, test) dengan judul maksimal 72 karakter, diikuti body bullet ringkas yang menjelaskan alasan dan dampak perubahan dalam Bahasa Indonesia."
	defaultGitmojiEnglishPromp = "Start with a relevant Gitmoji followed by Conventional Commits prefix (e.g. ✨ feat: ...). Keep the subject line under 72 characters and explain key changes concisely."
)

var (
	validProfileIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	runtimeHostKeyRe = regexp.MustCompile(`runtime:[A-Za-z0-9_.:-]+`)
)

// Test hooks for unit tests without touching the host's live Orca directory.
var (
	resolveOrcaCommitUserFn = ResolveOrcaUser
	orcaUserSvcActiveFn     = orcaServiceActive
	orcaSysSvcActiveFn      = isSystemOrcaServiceActive
	orcaProcessActiveFn     = isOrcaIDEProcessRunning
	orcaAnyProcessActiveFn  = isAnyOrcaProcessRunning
	stopOrcaForCommitAIFn             = stopOrcaService
	startOrcaForCommitAIFn            = startOrcaService
	forceQuitOrcaDesktopForCommitAIFn = forceQuitOrcaDesktopProcesses
)

type orcaAgentSpec struct {
	ID          string
	Label       string
	Binary      string
	SupportsTUI bool
}

// Katalog agent AI untuk Commit Message Generator sesuai COMMIT_MESSAGE_AGENT_SPECS di Orca ADE.
var orcaCommitAgentSpecs = []orcaAgentSpec{
	{ID: "opencode", Label: "OpenCode", Binary: "opencode", SupportsTUI: true},
	{ID: "antigravity", Label: "Antigravity (AGY)", Binary: "agy", SupportsTUI: true},
	{ID: "claude", Label: "Claude Code", Binary: "claude", SupportsTUI: true},
	{ID: "codex", Label: "OpenAI Codex", Binary: "codex", SupportsTUI: true},
	{ID: "copilot", Label: "GitHub Copilot", Binary: "copilot", SupportsTUI: true},
	{ID: "cursor", Label: "Cursor Agent", Binary: "cursor-agent", SupportsTUI: true},
	{ID: "omp", Label: "OMP", Binary: "omp", SupportsTUI: true},
	{ID: "pi", Label: "Pi", Binary: "pi", SupportsTUI: true},
	{ID: "kimi", Label: "Kimi Code", Binary: "kimi", SupportsTUI: true},
	{ID: "opencode2", Label: "OpenCode 2", Binary: "opencode2", SupportsTUI: true},
	{ID: "jcode", Label: "Jcode", Binary: "jcode", SupportsTUI: true},
	{ID: "dsh", Label: "DeepSeek Harness", Binary: "dsh", SupportsTUI: true},
	{ID: "custom", Label: "Custom Agent Command", Binary: "", SupportsTUI: false},
}

// OrcaCommitAIAgentOption mendeskripsikan opsi agent AI beserta status deteksi biner lokal.
type OrcaCommitAIAgentOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Binary      string `json:"binary,omitempty"`
	Installed   bool   `json:"installed"`
	Path        string `json:"path,omitempty"`
	SupportsTUI bool   `json:"supports_tui"`
}

// OrcaCommitTemplatePreset menyediakan pilihan template instruksi commit message siap pakai.
type OrcaCommitTemplatePreset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// OrcaCommitAISummary adalah ringkasan status Commit AI untuk kartu Orca ADE di halaman Workspace.
type OrcaCommitAISummary struct {
	Configured        bool   `json:"configured"`
	Enabled           bool   `json:"enabled"`
	AgentID           string `json:"agent_id"`
	AgentLabel        string `json:"agent_label"`
	ModelID           string `json:"model_id,omitempty"`
	ThinkingLevel     string `json:"thinking_level,omitempty"`
	AgentArgs         string `json:"agent_args,omitempty"`
	HasCustomPrompt   bool   `json:"has_custom_prompt"`
	RepoCount         int    `json:"repo_count"`
	RepoOverrideCount int    `json:"repo_override_count"`
}

// OrcaCommitAIConfig merepresentasikan konfigurasi lengkap AI Commit Generator Orca ADE.
type OrcaCommitAIConfig struct {
	DBExists                bool                           `json:"db_exists"`
	DBPath                  string                         `json:"db_path"`
	ProfileID               string                         `json:"profile_id"`
	OrcaInstalled           bool                           `json:"orca_installed"`
	OrcaRunning             bool                           `json:"orca_running"`
	OrcaMode                string                         `json:"orca_mode"`
	Enabled                 bool                           `json:"enabled"`
	AgentID                 string                         `json:"agent_id"`
	AgentLabel              string                         `json:"agent_label"`
	ModelID                 string                         `json:"model_id"`
	ThinkingLevel           string                         `json:"thinking_level"`
	AgentArgs               string                         `json:"agent_args"`
	CustomAgentCommand      string                         `json:"custom_agent_command"`
	CustomPrompt            string                         `json:"custom_prompt"`
	CommandInputTemplate    string                         `json:"command_input_template"`
	DefaultTUIAgent         string                         `json:"default_tui_agent"`
	RepoCount               int                            `json:"repo_count"`
	RepoOverrideCount       int                            `json:"repo_override_count"`
	SelectedModelsByAgent   map[string]string              `json:"selected_models_by_agent"`
	SelectedThinkingByModel map[string]string              `json:"selected_thinking_by_model"`
	CachedModelsByAgent     map[string][]OrcaCommitAIModel `json:"cached_models_by_agent"`
	AvailableAgents         []OrcaCommitAIAgentOption      `json:"available_agents"`
	TemplatePresets         []OrcaCommitTemplatePreset     `json:"template_presets"`
}

// OrcaCommitAISaveInput adalah payload penyimpanan konfigurasi AI Commit Orca.
type OrcaCommitAISaveInput struct {
	Enabled               bool                `json:"enabled"`
	AgentID               string              `json:"agent_id"`
	ModelID               string              `json:"model_id"`
	ThinkingLevel         string              `json:"thinking_level"`
	AgentArgs             string              `json:"agent_args"`
	DiscoveredModels      []OrcaCommitAIModel `json:"discovered_models,omitempty"`
	CustomAgentCommand    string              `json:"custom_agent_command"`
	CustomPrompt          string              `json:"custom_prompt"`
	UpdateDefaultTUIAgent bool                `json:"update_default_tui_agent"`
	ClearRepoOverrides    bool                `json:"clear_repo_overrides"`
}

func orcaCommitTemplatePresets() []OrcaCommitTemplatePreset {
	return []OrcaCommitTemplatePreset{
		{
			ID:          "conventional-id",
			Label:       "Conventional Commits (Indonesia)",
			Description: "Format Conventional Commits ringkas dalam Bahasa Indonesia (maks. 72 karakter).",
			Prompt:      defaultIndonesianPrompt,
		},
		{
			ID:          "conventional-en",
			Label:       "Conventional Commits (English)",
			Description: "Standard Conventional Commits in concise English (under 72 chars).",
			Prompt:      defaultEnglishPrompt,
		},
		{
			ID:          "detailed-id",
			Label:       "Conventional + Ringkasan (Indonesia)",
			Description: "Judul Conventional Commits + body bullet alasan dan dampak perubahan.",
			Prompt:      defaultDetailedIndoPrompt,
		},
		{
			ID:          "gitmoji",
			Label:       "Gitmoji + Conventional (English)",
			Description: "Prefiks emoji relevan diikuti tipe Conventional Commits.",
			Prompt:      defaultGitmojiEnglishPromp,
		},
		{
			ID:          "default",
			Label:       "Bawaan Orca ({basePrompt} saja)",
			Description: "Tanpa instruksi tambahan; memakai prompt dasar bawaan Orca.",
			Prompt:      "",
		},
	}
}

func lookupOrcaAgentSpec(id string) (orcaAgentSpec, bool) {
	for _, spec := range orcaCommitAgentSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return orcaAgentSpec{}, false
}

func findUserBinary(home, binName string) string {
	if binName == "" {
		return ""
	}
	var candidates []string
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", binName),
			filepath.Join(home, ".opencode", "bin", binName),
			filepath.Join(home, ".npm-global", "bin", binName),
			filepath.Join(home, ".bun", "bin", binName),
			filepath.Join(home, ".cargo", "bin", binName),
		)
	}
	candidates = append(candidates,
		filepath.Join("/usr/local/bin", binName),
		filepath.Join("/usr/local/lib/nodejs/current/bin", binName),
		filepath.Join("/usr/bin", binName),
	)
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	if p, err := exec.LookPath(binName); err == nil {
		return p
	}
	return ""
}

func detectAvailableCommitAgents(home string) []OrcaCommitAIAgentOption {
	opts := make([]OrcaCommitAIAgentOption, 0, len(orcaCommitAgentSpecs))
	for _, spec := range orcaCommitAgentSpecs {
		opt := OrcaCommitAIAgentOption{
			ID:          spec.ID,
			Label:       spec.Label,
			Binary:      spec.Binary,
			SupportsTUI: spec.SupportsTUI,
		}
		if spec.Binary != "" {
			if path := findUserBinary(home, spec.Binary); path != "" {
				opt.Installed = true
				opt.Path = path
			}
		} else if spec.ID == "custom" {
			opt.Installed = true
		}
		opts = append(opts, opt)
	}
	return opts
}

// ResolveOrcaProfileDBPath menemukan path profile-state.db untuk profil aktif Orca ADE.
func ResolveOrcaProfileDBPath(home string) (profileID string, dbPath string) {
	profileID = defaultOrcaProfileID
	if home == "" {
		return profileID, ""
	}
	orcaCfgDir := filepath.Join(home, ".config", "orca")
	indexPath := filepath.Join(orcaCfgDir, "orca-profile-index.json")
	if raw, err := os.ReadFile(indexPath); err == nil {
		var idx struct {
			ActiveProfileID string `json:"activeProfileId"`
		}
		if json.Unmarshal(raw, &idx) == nil {
			cand := strings.TrimSpace(idx.ActiveProfileID)
			if validProfileIDRe.MatchString(cand) {
				profileID = cand
			}
		}
	}
	return profileID, filepath.Join(orcaCfgDir, "profiles", profileID, "profile-state.db")
}

func isOrcaInstalledForHome(home string) bool {
	return IsOrcaInstalled(home)
}

func listOrcaProcesses(ctx context.Context, user string) string {
	var cmd *exec.Cmd
	if user != "" {
		cmd = exec.CommandContext(ctx, "pgrep", "-u", user, "-af", "orca")
	} else {
		cmd = exec.CommandContext(ctx, "pgrep", "-af", "orca")
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func stripPgrepPID(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if sp := strings.IndexAny(trimmed, " \t"); sp > 0 {
		prefix := trimmed[:sp]
		allDigits := true
		for _, r := range prefix {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return strings.TrimSpace(trimmed[sp+1:])
		}
	}
	return trimmed
}

func isShellOrProbeCmd(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	exe := filepath.Base(fields[0])
	switch exe {
	case "pgrep", "pkill", "ps", "grep", "bash", "sh", "zsh":
		return true
	}
	return false
}

// isOrcaDesktopCmdline membedakan proses Electron Orca Desktop GUI dari daemon
// Headless (`orca-ide ... serve --supervise --force`, `orcad-server.js`, dan
// Chromium headless milik Orca).
func isOrcaDesktopCmdline(line string) bool {
	cmd := stripPgrepPID(line)
	if cmd == "" {
		return false
	}
	fields := strings.Fields(cmd)
	if isShellOrProbeCmd(fields) {
		return false
	}
	exeBase := filepath.Base(fields[0])
	isOrcaBinary := exeBase == "orca-ide" || exeBase == "orca.AppImage" || strings.HasSuffix(fields[0], "/orca-ide")
	if !isOrcaBinary {
		return false
	}
	for _, tok := range fields[1:] {
		if tok == "serve" || tok == "pair" || tok == "status" || tok == "switch" || tok == "mode" || tok == "update" ||
			tok == "--version" || tok == "-v" || tok == "--help" || tok == "-h" ||
			strings.Contains(tok, "serve-supervisor") ||
			strings.Contains(tok, "out/cli/index.js") ||
			strings.Contains(tok, "orcad-server.js") ||
			strings.Contains(tok, "--web-client-root") ||
			strings.Contains(tok, "--ozone-platform=headless") {
			return false
		}
	}
	return true
}

func isAnyOrcaCmdline(line string) bool {
	cmd := stripPgrepPID(line)
	if cmd == "" {
		return false
	}
	fields := strings.Fields(cmd)
	if isShellOrProbeCmd(fields) {
		return false
	}
	exeBase := filepath.Base(fields[0])
	if exeBase == "orca-ide" || exeBase == "orca.AppImage" || strings.HasSuffix(fields[0], "/orca-ide") {
		for _, tok := range fields[1:] {
			if tok == "--version" || tok == "-v" || tok == "--help" || tok == "-h" {
				return false
			}
		}
		return true
	}
	for _, tok := range fields[1:] {
		if strings.HasSuffix(tok, "/orcad-server.js") || tok == "orcad-server.js" {
			return true
		}
	}
	return false
}

func hasOrcaDesktopProcess(pgrepOutput string) bool {
	for _, line := range strings.Split(pgrepOutput, "\n") {
		if isOrcaDesktopCmdline(line) {
			return true
		}
	}
	return false
}

func hasAnyOrcaProcess(pgrepOutput string) bool {
	for _, line := range strings.Split(pgrepOutput, "\n") {
		if isAnyOrcaCmdline(line) {
			return true
		}
	}
	return false
}

func isOrcaIDEProcessRunning(ctx context.Context, user string) bool {
	return hasOrcaDesktopProcess(listOrcaProcesses(ctx, user))
}

func isAnyOrcaProcessRunning(ctx context.Context, user string) bool {
	return hasAnyOrcaProcess(listOrcaProcesses(ctx, user))
}

// IsOrcaServiceActive memeriksa apakah layanan Orca Headless (user/system systemd)
// atau proses server/desktop Orca sedang berjalan.
func IsOrcaServiceActive(ctx context.Context, user, home string) bool {
	if home != "" && orcaUserSvcActiveFn(ctx, user, home) {
		return true
	}
	if orcaSysSvcActiveFn(ctx) {
		return true
	}
	return orcaProcessActiveFn(ctx, user) || orcaAnyProcessActiveFn(ctx, user)
}

func commandTemplateFromPrompt(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return orcaBasePromptToken
	}
	if strings.Contains(trimmed, orcaBasePromptToken) || strings.Contains(trimmed, "{{basePrompt}}") {
		return trimmed
	}
	return orcaBasePromptToken + "\n\n" + trimmed
}

func legacyPromptFromTemplate(template, fallback string) string {
	trimmed := strings.TrimSpace(template)
	if trimmed == "" || trimmed == orcaBasePromptToken {
		return strings.TrimSpace(fallback)
	}
	if strings.HasPrefix(trimmed, orcaBasePromptToken) {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, orcaBasePromptToken))
	}
	return trimmed
}

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if sub, ok := m[key].(map[string]any); ok {
		return sub
	}
	return nil
}

func ensureMap(m map[string]any, key string) map[string]any {
	if sub, ok := m[key].(map[string]any); ok && sub != nil {
		return sub
	}
	sub := map[string]any{}
	m[key] = sub
	return sub
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func getBoolDefault(m map[string]any, key string, def bool) bool {
	if m == nil {
		return def
	}
	if b, ok := m[key].(bool); ok {
		return b
	}
	return def
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func repoHasCommitMessageOverride(repo map[string]any) bool {
	sc := getMap(repo, "sourceControlAi")
	if sc != nil {
		if strings.TrimSpace(getString(sc, "customAgentCommand")) != "" {
			return true
		}
		if ao := getMap(sc, "actionOverrides"); ao != nil {
			if _, exists := ao["commitMessage"]; exists {
				return true
			}
		}
		if instr := getMap(sc, "instructionsByOperation"); instr != nil {
			if val, exists := instr["commitMessage"]; exists {
				if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
					return true
				}
			}
		}
		if modOv := getMap(sc, "modelOverridesByOperation"); modOv != nil {
			if _, exists := modOv["commitMessage"]; exists {
				return true
			}
		}
	}
	if legacy := getMap(repo, "commitMessageAi"); legacy != nil {
		if strings.TrimSpace(getString(legacy, "customAgentCommand")) != "" {
			return true
		}
	}
	return false
}

// GetOrcaCommitAIConfig membaca konfigurasi Orca Commit AI saat ini beserta daftar agent lokal.
func GetOrcaCommitAIConfig(ctx context.Context) (*OrcaCommitAIConfig, error) {
	user, home := resolveOrcaCommitUserFn()
	profileID, dbPath := ResolveOrcaProfileDBPath(home)
	agents := detectAvailableCommitAgents(home)
	presets := orcaCommitTemplatePresets()

	userSvcActive := home != "" && orcaUserSvcActiveFn(ctx, user, home)
	sysSvcActive := orcaSysSvcActiveFn(ctx)
	procActive := orcaProcessActiveFn(ctx, user)
	anyProcActive := orcaAnyProcessActiveFn(ctx, user)

	cfg := &OrcaCommitAIConfig{
		DBPath:                  dbPath,
		ProfileID:               profileID,
		OrcaInstalled:           isOrcaInstalledForHome(home),
		OrcaRunning:             userSvcActive || sysSvcActive || procActive || anyProcActive,
		OrcaMode:                readOrcaMode(home),
		Enabled:                 true,
		CommandInputTemplate:    orcaBasePromptToken,
		SelectedModelsByAgent:   map[string]string{},
		SelectedThinkingByModel: map[string]string{},
		CachedModelsByAgent:     buildCachedModelsByAgentMap(nil),
		AvailableAgents:         agents,
		TemplatePresets:         presets,
	}

	// Default agent fallback berdasarkan biner yang tersedia bila belum dikonfigurasi
	for _, a := range agents {
		if a.ID != "custom" && a.Installed {
			cfg.AgentID = a.ID
			cfg.AgentLabel = a.Label
			break
		}
	}
	if cfg.AgentID == "" {
		cfg.AgentID = "opencode"
		cfg.AgentLabel = "OpenCode"
	}

	if dbPath == "" {
		return cfg, nil
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return cfg, nil
	}
	cfg.DBExists = true

	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(3000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return cfg, fmt.Errorf("gagal membuka profile-state.db: %w", err)
	}
	defer db.Close()

	var settingsPayload string
	if err := db.QueryRowContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain = 'settings'`).Scan(&settingsPayload); err == nil {
		var settings map[string]any
		if json.Unmarshal([]byte(settingsPayload), &settings) == nil {
			cfg.DefaultTUIAgent = getString(settings, "defaultTuiAgent")
			sc := getMap(settings, "sourceControlAi")
			legacy := getMap(settings, "commitMessageAi")

			if sc != nil {
				cfg.Enabled = getBoolDefault(sc, "enabled", true)
			} else if legacy != nil {
				cfg.Enabled = getBoolDefault(legacy, "enabled", true)
			}

			actions := getMap(sc, "actions")
			commitAction := getMap(actions, "commitMessage")
			instrMap := getMap(sc, "instructionsByOperation")

			agentID := getString(commitAction, "agentId")
			if agentID == "" {
				agentID = getString(sc, "agentId")
			}
			if agentID == "" {
				agentID = getString(legacy, "agentId")
			}
			if agentID == "" && cfg.DefaultTUIAgent != "" && cfg.DefaultTUIAgent != "blank" {
				agentID = cfg.DefaultTUIAgent
			}
			if agentID != "" {
				cfg.AgentID = agentID
			}

			customCmd := getString(sc, "customAgentCommand")
			if customCmd == "" {
				customCmd = getString(legacy, "customAgentCommand")
			}
			cfg.CustomAgentCommand = customCmd

			cfg.AgentArgs = strings.TrimSpace(getString(commitAction, "agentArgs"))

			tpl := getString(commitAction, "commandInputTemplate")
			instr := getString(instrMap, "commitMessage")
			legacyPrompt := getString(legacy, "customPrompt")

			if tpl != "" {
				cfg.CommandInputTemplate = tpl
				cfg.CustomPrompt = legacyPromptFromTemplate(tpl, instr)
			} else if instr != "" {
				cfg.CustomPrompt = instr
				cfg.CommandInputTemplate = commandTemplateFromPrompt(instr)
			} else if legacyPrompt != "" {
				cfg.CustomPrompt = legacyPrompt
				cfg.CommandInputTemplate = commandTemplateFromPrompt(legacyPrompt)
			}

			selMap, thinkMap := extractSelectedModelsFromSettings(settings)
			cfg.SelectedModelsByAgent = selMap
			cfg.SelectedThinkingByModel = thinkMap
			cfg.CachedModelsByAgent = buildCachedModelsByAgentMap(settings)
		}
	}

	if spec, ok := lookupOrcaAgentSpec(cfg.AgentID); ok {
		cfg.AgentLabel = spec.Label
	} else if cfg.AgentID != "" {
		cfg.AgentLabel = cfg.AgentID
	}

	if mID := cfg.SelectedModelsByAgent[cfg.AgentID]; mID != "" {
		cfg.ModelID = mID
	} else {
		cfg.ModelID = defaultModelIDForAgent(cfg.AgentID)
	}
	if cfg.ModelID != "" {
		cfg.ThinkingLevel = cfg.SelectedThinkingByModel[cfg.ModelID]
	}

	var reposPayload string
	if err := db.QueryRowContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain = 'repos'`).Scan(&reposPayload); err == nil {
		var repos []map[string]any
		if json.Unmarshal([]byte(reposPayload), &repos) == nil {
			cfg.RepoCount = len(repos)
			for _, repo := range repos {
				if repoHasCommitMessageOverride(repo) {
					cfg.RepoOverrideCount++
				}
			}
		}
	}

	return cfg, nil
}

// GetOrcaCommitAISummary mengembalikan ringkasan singkat konfigurasi Commit AI Orca.
func GetOrcaCommitAISummary(ctx context.Context) *OrcaCommitAISummary {
	cfg, err := GetOrcaCommitAIConfig(ctx)
	if err != nil || cfg == nil || !cfg.DBExists {
		return nil
	}
	return &OrcaCommitAISummary{
		Configured:        cfg.AgentID != "",
		Enabled:           cfg.Enabled,
		AgentID:           cfg.AgentID,
		AgentLabel:        cfg.AgentLabel,
		ModelID:           cfg.ModelID,
		ThinkingLevel:     cfg.ThinkingLevel,
		AgentArgs:         cfg.AgentArgs,
		HasCustomPrompt:   strings.TrimSpace(cfg.CustomPrompt) != "",
		RepoCount:         cfg.RepoCount,
		RepoOverrideCount: cfg.RepoOverrideCount,
	}
}

func parsePgrepPID(line string) int {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0
	}
	sp := strings.IndexAny(trimmed, " \t")
	if sp <= 0 {
		return 0
	}
	pid, err := strconv.Atoi(trimmed[:sp])
	if err != nil || pid <= 1 {
		return 0
	}
	return pid
}

func extractOrcaDesktopPIDs(pgrepOutput string) []int {
	selfPID := os.Getpid()
	seen := map[int]bool{}
	var pids []int
	for _, line := range strings.Split(pgrepOutput, "\n") {
		if !isOrcaDesktopCmdline(line) {
			continue
		}
		pid := parsePgrepPID(line)
		if pid <= 1 || pid == selfPID || seen[pid] {
			continue
		}
		seen[pid] = true
		pids = append(pids, pid)
	}
	return pids
}

func signalOrcaPIDs(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, sig)
	}
}

func waitForOrcaDesktopExit(ctx context.Context, user string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !orcaProcessActiveFn(ctx, user) {
			return true
		}
		select {
		case <-ctx.Done():
			return !orcaProcessActiveFn(ctx, user)
		case <-time.After(150 * time.Millisecond):
		}
	}
	return !orcaProcessActiveFn(ctx, user)
}

func forceQuitOrcaDesktopProcesses(ctx context.Context, cb LogCallback, user string) {
	pids := extractOrcaDesktopPIDs(listOrcaProcesses(ctx, user))
	if len(pids) == 0 {
		return
	}
	if cb != nil {
		cb("==> Menghentikan Orca ADE Desktop GUI agar konfigurasi Commit AI tersimpan permanen...")
	}
	signalOrcaPIDs(pids, syscall.SIGTERM)
	if waitForOrcaDesktopExit(ctx, user, 3*time.Second) {
		return
	}
	remaining := extractOrcaDesktopPIDs(listOrcaProcesses(ctx, user))
	if len(remaining) == 0 {
		remaining = pids
	}
	if cb != nil {
		cb("==> Orca ADE Desktop masih tertahan; memaksa keluar (SIGKILL)...")
	}
	signalOrcaPIDs(remaining, syscall.SIGKILL)
	_ = waitForOrcaDesktopExit(ctx, user, 3*time.Second)
}

func waitForOrcaProcessExit(ctx context.Context, user string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !orcaProcessActiveFn(ctx, user) && !orcaAnyProcessActiveFn(ctx, user) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func maxProfileRevision(ctx context.Context, tx *sql.Conn) (int64, error) {
	var metaRevStr string
	var maxRev int64
	if err := tx.QueryRowContext(ctx, `SELECT value FROM profile_state_meta WHERE key = 'revision'`).Scan(&metaRevStr); err == nil {
		if v, pErr := strconv.ParseInt(strings.TrimSpace(metaRevStr), 10, 64); pErr == nil && v > maxRev {
			maxRev = v
		}
	}

	var docRev sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(revision) FROM profile_state_documents`).Scan(&docRev); err == nil && docRev.Valid && docRev.Int64 > maxRev {
		maxRev = docRev.Int64
	}

	for _, table := range []string{"profile_state_automation_runs_meta", "profile_state_document_parts_meta"} {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err == nil && exists > 0 {
			var tRev sql.NullInt64
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT MAX(revision) FROM %s`, table)).Scan(&tRev); err == nil && tRev.Valid && tRev.Int64 > maxRev {
				maxRev = tRev.Int64
			}
		}
	}

	return maxRev, nil
}

func cleanRepoSourceControlCommitOverride(item map[string]any, enabled bool, syncLegacyCommitAI bool, agentID, prompt string) bool {
	changed := false
	if sc := getMap(item, "sourceControlAi"); sc != nil {
		if _, hasLegacyActions := sc["actions"]; hasLegacyActions {
			delete(sc, "actions")
			changed = true
		}
		if _, hasLegacyAgent := sc["agentId"]; hasLegacyAgent {
			delete(sc, "agentId")
			changed = true
		}
		if _, hasCustomCmd := sc["customAgentCommand"]; hasCustomCmd {
			delete(sc, "customAgentCommand")
			changed = true
		}
		if ao := getMap(sc, "actionOverrides"); ao != nil {
			if _, exists := ao["commitMessage"]; exists {
				delete(ao, "commitMessage")
				changed = true
			}
			if len(ao) == 0 {
				delete(sc, "actionOverrides")
				changed = true
			}
		}
		if instr := getMap(sc, "instructionsByOperation"); instr != nil {
			if _, exists := instr["commitMessage"]; exists {
				delete(instr, "commitMessage")
				changed = true
			}
			if len(instr) == 0 {
				delete(sc, "instructionsByOperation")
				changed = true
			}
		}
		if modOv := getMap(sc, "modelOverridesByOperation"); modOv != nil {
			if _, exists := modOv["commitMessage"]; exists {
				delete(modOv, "commitMessage")
				changed = true
			}
			if len(modOv) == 0 {
				delete(sc, "modelOverridesByOperation")
				changed = true
			}
		}
		if curEnabled, ok := sc["enabled"].(bool); !ok || curEnabled != enabled {
			sc["enabled"] = enabled
			changed = true
		}
	} else if !enabled {
		item["sourceControlAi"] = map[string]any{"enabled": false}
		changed = true
	}

	if syncLegacyCommitAI {
		if legacy := getMap(item, "commitMessageAi"); legacy != nil {
			if curEnabled, ok := legacy["enabled"].(bool); !ok || curEnabled != enabled {
				legacy["enabled"] = enabled
				changed = true
			}
			if getString(legacy, "agentId") != agentID {
				legacy["agentId"] = agentID
				changed = true
			}
			if getString(legacy, "customPrompt") != prompt {
				legacy["customPrompt"] = prompt
				changed = true
			}
			if _, hasCustomCmd := legacy["customAgentCommand"]; hasCustomCmd {
				delete(legacy, "customAgentCommand")
				changed = true
			}
		}
	}

	return changed
}

func updateDomainJSONDocument(ctx context.Context, conn *sql.Conn, domain, payload string, rev, nowMs int64) error {
	hash := sha256Hex(payload)
	res, err := conn.ExecContext(ctx,
		`UPDATE profile_state_documents SET payload = ?, revision = ?, updated_at = ?, content_hash = ? WHERE domain = ?`,
		payload, rev, nowMs, hash, domain,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_, err = conn.ExecContext(ctx,
			`INSERT INTO profile_state_documents (domain, payload, domain_version, revision, updated_at, content_hash) VALUES (?, ?, 1, ?, ?, ?)`,
			domain, payload, rev, nowMs, hash,
		)
		return err
	}
	return nil
}

// SaveOrcaCommitAIConfig menyimpan pengaturan AI Commit Message Generator ke profile-state.db Orca ADE.
func SaveOrcaCommitAIConfig(ctx context.Context, cb LogCallback, input OrcaCommitAISaveInput) (*OrcaCommitAIConfig, error) {
	return saveOrcaCommitAIConfig(ctx, cb, input, false)
}

func hasBalancedAgentArgQuotes(s string) bool {
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == '\\' && quote == '"' && i+1 < len(s) {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '\\' && i+1 < len(s) {
			i++
			continue
		}
	}
	return quote == 0
}

func validateOrcaCommitAISaveInput(input OrcaCommitAISaveInput) (orcaAgentSpec, string, string, error) {
	agentID := strings.TrimSpace(input.AgentID)
	spec, ok := lookupOrcaAgentSpec(agentID)
	if !ok {
		return orcaAgentSpec{}, "", "", fmt.Errorf("%w: agent AI tidak dikenal: %q", ErrOrcaCommitAIValidation, agentID)
	}
	customCmd := strings.TrimSpace(input.CustomAgentCommand)
	if agentID == "custom" && customCmd == "" {
		return orcaAgentSpec{}, "", "", fmt.Errorf("%w: perintah custom agent wajib diisi saat memilih Custom Agent Command", ErrOrcaCommitAIValidation)
	}
	agentArgs := strings.TrimSpace(input.AgentArgs)
	if !hasBalancedAgentArgQuotes(agentArgs) {
		return orcaAgentSpec{}, "", "", fmt.Errorf("%w: argumen CLI memiliki tanda kutip yang belum ditutup", ErrOrcaCommitAIValidation)
	}
	return spec, customCmd, agentArgs, nil
}

func saveOrcaCommitAIConfig(ctx context.Context, cb LogCallback, input OrcaCommitAISaveInput, forceQuitDesktop bool) (*OrcaCommitAIConfig, error) {
	spec, customCmd, agentArgs, err := validateOrcaCommitAISaveInput(input)
	if err != nil {
		return nil, err
	}
	input.AgentArgs = agentArgs

	user, home := resolveOrcaCommitUserFn()
	if home == "" {
		return nil, fmt.Errorf("%w: direktori home user Orca tidak ditemukan", ErrOrcaCommitAIConflict)
	}
	_, dbPath := ResolveOrcaProfileDBPath(home)
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return nil, fmt.Errorf("%w: database profil Orca tidak ditemukan di %s; pastikan Orca ADE sudah terpasang dan pernah dijalankan", ErrOrcaCommitAIConflict, dbPath)
	}

	wasUserSvc := orcaUserSvcActiveFn(ctx, user, home)
	wasSysSvc := orcaSysSvcActiveFn(ctx)
	wasSvcActive := wasUserSvc || wasSysSvc
	if !wasSvcActive && orcaProcessActiveFn(ctx, user) {
		if forceQuitDesktop {
			forceQuitOrcaDesktopForCommitAIFn(ctx, cb, user)
		}
		if orcaProcessActiveFn(ctx, user) {
			return nil, fmt.Errorf("%w: aplikasi Orca ADE Desktop sedang berjalan; tutup Orca terlebih dahulu sebelum menyimpan agar konfigurasi tidak tertimpa", ErrOrcaCommitAIConflict)
		}
	}

	if wasSvcActive {
		if cb != nil {
			cb("==> Menghentikan layanan Orca sementara agar database tersimpan permanen...")
		}
		stopOrcaForCommitAIFn(ctx, cb, user, home)
		waitForOrcaProcessExit(ctx, user, 8*time.Second)

		defer func() {
			if cb != nil {
				cb("==> Menjalankan kembali layanan Orca Server...")
			}
			restartCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := startOrcaForCommitAIFn(restartCtx, cb, user, home, wasUserSvc); err != nil && cb != nil {
				cb(fmt.Sprintf("[WARN] Gagal memulai ulang layanan Orca: %v", err))
			}
		}()
	}

	cmdTemplate := commandTemplateFromPrompt(input.CustomPrompt)
	plainPrompt := legacyPromptFromTemplate(cmdTemplate, input.CustomPrompt)

	if err := applyOrcaCommitAIToDB(ctx, dbPath, user, spec, customCmd, plainPrompt, cmdTemplate, input); err != nil {
		return nil, err
	}

	if cb != nil {
		cb(fmt.Sprintf("✓ Konfigurasi Orca Commit AI (%s) berhasil disimpan permanen.", spec.Label))
	}
	return GetOrcaCommitAIConfig(ctx)
}

type sqlQueryContexter interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryKnownOrcaRuntimeHostKeys(ctx context.Context, q sqlQueryContexter) []string {
	rows, err := q.QueryContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain IN ('settings', 'ui', 'repos', 'projectHostSetups', 'workspaceSession')`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := map[string]bool{}
	var keys []string
	for rows.Next() {
		var payload string
		if rows.Scan(&payload) != nil {
			continue
		}
		for _, m := range runtimeHostKeyRe.FindAllString(payload, -1) {
			if !seen[m] {
				seen[m] = true
				keys = append(keys, m)
			}
		}
	}
	return keys
}

func collectKnownOrcaRuntimeHostKeysFromDB(ctx context.Context, db *sql.DB) []string {
	return queryKnownOrcaRuntimeHostKeys(ctx, db)
}

func collectKnownOrcaRuntimeHostKeys(ctx context.Context, conn *sql.Conn) []string {
	return queryKnownOrcaRuntimeHostKeys(ctx, conn)
}

func applyOrcaCommitAIToDB(
	ctx context.Context,
	dbPath, user string,
	spec orcaAgentSpec,
	customCmd, plainPrompt, cmdTemplate string,
	input OrcaCommitAISaveInput,
) error {
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("gagal membuka profile-state.db: %w", err)
	}
	defer func() {
		_ = db.Close()
		chownPath(user, dbPath)
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, statErr := os.Stat(dbPath + suffix); statErr == nil {
				chownPath(user, dbPath+suffix)
			}
		}
	}()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("gagal mengambil koneksi sqlite: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("gagal memulai transaksi database Orca: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	curRev, err := maxProfileRevision(ctx, conn)
	if err != nil {
		return err
	}
	newRev := curRev + 1
	nowMs := time.Now().UnixMilli()

	// 1. Update domain settings
	var settingsPayload string
	err = conn.QueryRowContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain = 'settings'`).Scan(&settingsPayload)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("gagal membaca domain settings Orca: %w", err)
	}
	settings := map[string]any{}
	if strings.TrimSpace(settingsPayload) != "" {
		if uErr := json.Unmarshal([]byte(settingsPayload), &settings); uErr != nil {
			return fmt.Errorf("format settings Orca tidak valid: %w", uErr)
		}
	}

	if input.UpdateDefaultTUIAgent && spec.SupportsTUI {
		settings["defaultTuiAgent"] = spec.ID
	}

	// Legacy commitMessageAi (masih dipertahankan untuk sinkronisasi kompatibilitas)
	legacy := ensureMap(settings, "commitMessageAi")
	legacy["enabled"] = input.Enabled
	legacy["agentId"] = spec.ID
	legacy["customPrompt"] = plainPrompt
	legacy["customAgentCommand"] = customCmd
	for _, mKey := range []string{"selectedModelByAgent", "selectedModelByAgentByHost", "discoveredModelsByAgent", "discoveredModelsByAgentByHost", "selectedThinkingByModel"} {
		ensureMap(legacy, mKey)
	}

	// Primary sourceControlAi (Orca v1.4+)
	sc := ensureMap(settings, "sourceControlAi")
	sc["enabled"] = input.Enabled
	sc["agentId"] = spec.ID
	sc["customAgentCommand"] = customCmd
	for _, mKey := range []string{"selectedModelByAgent", "selectedModelByAgentByHost", "discoveredModelsByAgent", "discoveredModelsByAgentByHost", "selectedThinkingByModel", "launchActionDefaults", "modelOverridesByOperation"} {
		ensureMap(sc, mKey)
	}
	instr := ensureMap(sc, "instructionsByOperation")
	instr["commitMessage"] = plainPrompt

	actions := ensureMap(sc, "actions")
	commitAction := ensureMap(actions, "commitMessage")
	prevActionAgent := getString(commitAction, "agentId")
	if prevActionAgent != "" && prevActionAgent != spec.ID {
		// Pilihan model/permission milik agent sebelumnya tidak valid untuk agent baru.
		delete(commitAction, "launchPreferences")
		delete(commitAction, "argumentChoices")
	}
	commitAction["agentId"] = spec.ID
	commitAction["commandInputTemplate"] = cmdTemplate
	if agentArgs := strings.TrimSpace(input.AgentArgs); agentArgs != "" {
		commitAction["agentArgs"] = agentArgs
		// commitMessage adalah text action; jangan biarkan argumentChoices sisa launch action bertahan.
		delete(commitAction, "argumentChoices")
	} else {
		delete(commitAction, "agentArgs")
	}

	// Simpan pilihan model, thinking level, dan hasil discovery model untuk agent aktif.
	if spec.ID != "custom" {
		modelID := strings.TrimSpace(input.ModelID)
		thinkingLevel := strings.TrimSpace(input.ThinkingLevel)
		sanitizedDiscovered := sanitizeDiscoveredModelsForDB(input.DiscoveredModels)
		runtimeHostKeys := collectKnownOrcaRuntimeHostKeys(ctx, conn)

		modelOverrides := ensureMap(sc, "modelOverridesByOperation")
		commitModelChoice := ensureMap(modelOverrides, "commitMessage")
		applyModelSelectionToContainer(commitModelChoice, spec.ID, modelID, thinkingLevel, true, runtimeHostKeys...)
		applyModelSelectionToContainer(sc, spec.ID, modelID, thinkingLevel, true, runtimeHostKeys...)
		applyModelSelectionToContainer(legacy, spec.ID, modelID, thinkingLevel, true, runtimeHostKeys...)

		applyDiscoveredModelsToContainer(sc, spec.ID, sanitizedDiscovered, runtimeHostKeys...)
		applyDiscoveredModelsToContainer(legacy, spec.ID, sanitizedDiscovered, runtimeHostKeys...)
	}

	newSettingsBytes, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := updateDomainJSONDocument(ctx, conn, "settings", string(newSettingsBytes), newRev, nowMs); err != nil {
		return fmt.Errorf("gagal menyimpan domain settings Orca: %w", err)
	}

	// 2. Bersihkan override per-repo agar seluruh repositori mewarisi pengaturan global terbaru
	if input.ClearRepoOverrides {
		var reposPayload string
		if err := conn.QueryRowContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain = 'repos'`).Scan(&reposPayload); err == nil && strings.TrimSpace(reposPayload) != "" {
			var repos []map[string]any
			if json.Unmarshal([]byte(reposPayload), &repos) == nil {
				reposChanged := false
				for _, repo := range repos {
					if cleanRepoSourceControlCommitOverride(repo, input.Enabled, true, spec.ID, plainPrompt) {
						reposChanged = true
					}
				}
				if reposChanged {
					if b, mErr := json.Marshal(repos); mErr == nil {
						if uErr := updateDomainJSONDocument(ctx, conn, "repos", string(b), newRev, nowMs); uErr != nil {
							return fmt.Errorf("gagal memperbarui domain repos Orca: %w", uErr)
						}
					}
				}
			}
		}

		var setupsPayload string
		if err := conn.QueryRowContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain = 'projectHostSetups'`).Scan(&setupsPayload); err == nil && strings.TrimSpace(setupsPayload) != "" {
			var setups []map[string]any
			if json.Unmarshal([]byte(setupsPayload), &setups) == nil {
				setupsChanged := false
				for _, setup := range setups {
					if cleanRepoSourceControlCommitOverride(setup, input.Enabled, false, spec.ID, plainPrompt) {
						setupsChanged = true
					}
				}
				if setupsChanged {
					if b, mErr := json.Marshal(setups); mErr == nil {
						if uErr := updateDomainJSONDocument(ctx, conn, "projectHostSetups", string(b), newRev, nowMs); uErr != nil {
							return fmt.Errorf("gagal memperbarui domain projectHostSetups Orca: %w", uErr)
						}
					}
				}
			}
		}
	}

	// 3. Sinkronkan revisi global profile_state_meta
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO profile_state_meta (key, value) VALUES ('revision', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		strconv.FormatInt(newRev, 10),
	); err != nil {
		return fmt.Errorf("gagal memperbarui revisi profil Orca: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("gagal commit perubahan database Orca: %w", err)
	}
	committed = true

	_, _ = conn.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return nil
}

// ConfigureDefaultOrcaCommitAI dipakai oleh runner non-interaktif (`orca-ai`) untuk
// menerapkan default AI Commit Generator berdasarkan agent CLI yang terpasang.
func ConfigureDefaultOrcaCommitAI(ctx context.Context, cb LogCallback) error {
	if cb != nil {
		cb("==> Memeriksa konfigurasi dan agent AI lokal untuk Orca Commit AI...")
	}
	cur, err := GetOrcaCommitAIConfig(ctx)
	if err != nil {
		return err
	}
	if !cur.DBExists {
		return fmt.Errorf("database profil Orca tidak ditemukan di %s; jalankan Orca ADE terlebih dahulu", cur.DBPath)
	}

	chosenAgent := cur.AgentID
	if chosenAgent == "" {
		for _, a := range cur.AvailableAgents {
			if a.ID != "custom" && a.Installed {
				chosenAgent = a.ID
				break
			}
		}
	}
	if chosenAgent == "" {
		chosenAgent = "opencode"
	}

	prompt := strings.TrimSpace(cur.CustomPrompt)
	if prompt == "" {
		prompt = defaultIndonesianPrompt
	}

	if cb != nil {
		cb(fmt.Sprintf("  Agent aktif : %s", chosenAgent))
		cb(fmt.Sprintf("  Prompt      : %s", prompt))
	}

	_, err = SetOrcaCommitAIDesiredState(ctx, cb, OrcaCommitAISaveInput{
		Enabled:               true,
		AgentID:               chosenAgent,
		ModelID:               cur.ModelID,
		ThinkingLevel:         cur.ThinkingLevel,
		AgentArgs:             cur.AgentArgs,
		CustomAgentCommand:    cur.CustomAgentCommand,
		CustomPrompt:          prompt,
		UpdateDefaultTUIAgent: chosenAgent != "custom",
		ClearRepoOverrides:    true,
	})
	return err
}
