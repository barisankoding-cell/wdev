package installer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wdev/internal/i18n"
)

// File status sinkronisasi Orca Commit AI milik wdev. Orca ADE adalah pemilik
// profile-state.db, jadi wdev menyimpan "keinginan" (desired state) di sini lalu
// memastikan payload Orca selalu cocok dengan keinginan tersebut.
const (
	orcaCommitAISyncSchemaVersion = 1
	orcaCommitAISyncFileName      = "orca-commit-ai.json"
	orcaCommitAIReconcileCooldown = 5 * time.Minute
)

// Jeda sebelum verifikasi ulang setelah Orca dihidupkan kembali.
// Dibuat var supaya bisa dipercepat di unit test.
var orcaCommitAIPostApplyVerifyWait = 2500 * time.Millisecond

// Status sinkronisasi yang ditampilkan ke panel wdev.
const (
	orcaCommitAISyncStateUnmanaged = "unmanaged"
	orcaCommitAISyncStateSynced    = "synced"
	orcaCommitAISyncStateDrift     = "drift"
	orcaCommitAISyncStateBlocked   = "blocked"
	orcaCommitAISyncStateConflict  = "conflict"
	orcaCommitAISyncStateError     = "error"
)

// OrcaCommitAIDesiredState adalah konfigurasi Commit AI yang diinginkan wdev.
type OrcaCommitAIDesiredState struct {
	SchemaVersion         int                 `json:"schema_version"`
	ProfileID             string              `json:"profile_id"`
	Enabled               bool                `json:"enabled"`
	AgentID               string              `json:"agent_id"`
	ModelID               string              `json:"model_id"`
	ThinkingLevel         string              `json:"thinking_level"`
	AgentArgs             string              `json:"agent_args"`
	CustomAgentCommand    string              `json:"custom_agent_command"`
	CustomPrompt          string              `json:"custom_prompt"`
	UpdateDefaultTUIAgent bool                `json:"update_default_tui_agent"`
	ClearRepoOverrides    bool                `json:"clear_repo_overrides"`
	DiscoveredModels      []OrcaCommitAIModel `json:"discovered_models,omitempty"`
}

// OrcaCommitAISyncState menyimpan desired state beserta riwayat penerapannya.
type OrcaCommitAISyncState struct {
	Desired             *OrcaCommitAIDesiredState `json:"desired"`
	UpdatedAt           int64                     `json:"updated_at_ms"`
	LastAppliedAt       int64                     `json:"last_applied_at_ms,omitempty"`
	LastAppliedRevision int64                     `json:"last_applied_revision,omitempty"`
	LastError           string                    `json:"last_error,omitempty"`
	Failures            int                       `json:"failures,omitempty"`
}

// OrcaCommitAISyncSide merangkum nilai effective di satu sisi perbandingan.
type OrcaCommitAISyncSide struct {
	Enabled            bool   `json:"enabled"`
	AgentID            string `json:"agent_id"`
	ModelID            string `json:"model_id"`
	ThinkingLevel      string `json:"thinking_level"`
	AgentArgs          string `json:"agent_args"`
	CustomPrompt       string `json:"custom_prompt"`
	CustomAgentCommand string `json:"custom_agent_command"`
	RepoOverrideCount  int    `json:"repo_override_count"`
}

// OrcaCommitAISyncStatus adalah hasil pemeriksaan sinkronisasi saat ini.
type OrcaCommitAISyncStatus struct {
	Managed           bool                  `json:"managed"`
	InSync            bool                  `json:"in_sync"`
	State             string                `json:"state"`
	Drift             []string              `json:"drift,omitempty"`
	Desired           *OrcaCommitAISyncSide `json:"desired,omitempty"`
	Actual            *OrcaCommitAISyncSide `json:"actual,omitempty"`
	LastAppliedAt     int64                 `json:"last_applied_at_ms,omitempty"`
	LastError         string                `json:"last_error,omitempty"`
	ProfileID         string                `json:"profile_id"`
	DBPath            string                `json:"db_path"`
	CloudLinked       bool                  `json:"cloud_linked"`
	OrcaRunning       bool                  `json:"orca_running"`
	OrcaDesktopOpen   bool                  `json:"orca_desktop_open"`
	SettingsRevision  int64                 `json:"settings_revision"`
	SettingsUpdatedAt int64                 `json:"settings_updated_at_ms"`
}

// orcaCommitAIActualState adalah keadaan effective yang dibaca langsung dari DB Orca.
type orcaCommitAIActualState struct {
	Enabled            bool
	AgentID            string
	ModelID            string
	ThinkingLevel      string
	AgentArgs          string
	CustomPrompt       string
	CustomAgentCommand string
	DefaultTUIAgent    string
	RepoOverrideCount  int
	SettingsRevision   int64
	SettingsUpdatedAt  int64
	MirrorDrift        []string
	HostModelDrift     []string
}

func orcaCommitAIStatePath(home string) string {
	return filepath.Join(home, ".config", "wdev", orcaCommitAISyncFileName)
}

// LoadOrcaCommitAISyncState membaca desired state. Mengembalikan nil bila wdev
// belum pernah menyimpan konfigurasi Commit AI untuk user tersebut.
func LoadOrcaCommitAISyncState(home string) (*OrcaCommitAISyncState, error) {
	if strings.TrimSpace(home) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(orcaCommitAIStatePath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gagal membaca status sinkron Orca Commit AI: %w", err)
	}
	var st OrcaCommitAISyncState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("format status sinkron Orca Commit AI rusak: %w", err)
	}
	if st.Desired == nil {
		return nil, nil
	}
	if st.Desired.SchemaVersion == 0 {
		st.Desired.SchemaVersion = orcaCommitAISyncSchemaVersion
	}
	return &st, nil
}

func saveOrcaCommitAISyncState(user, home string, st *OrcaCommitAISyncState) error {
	if strings.TrimSpace(home) == "" {
		return fmt.Errorf("%w: direktori home user Orca tidak ditemukan", ErrOrcaCommitAIConflict)
	}
	dir := filepath.Dir(orcaCommitAIStatePath(home))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("gagal membuat direktori status sinkron wdev: %w", err)
	}
	chownPath(user, dir)

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".orca-commit-ai-*.tmp")
	if err != nil {
		return fmt.Errorf("gagal membuat file status sementara: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	chownPath(user, tmpName)
	if err := os.Rename(tmpName, orcaCommitAIStatePath(home)); err != nil {
		return fmt.Errorf("gagal menyimpan status sinkron wdev: %w", err)
	}
	return nil
}

func desiredFromSaveInput(input OrcaCommitAISaveInput, profileID string) OrcaCommitAIDesiredState {
	// Override per-repo selalu dibersihkan supaya pengaturan global dari wdev
	// benar-benar berlaku di semua repositori.
	return OrcaCommitAIDesiredState{
		SchemaVersion:         orcaCommitAISyncSchemaVersion,
		ProfileID:             strings.TrimSpace(profileID),
		Enabled:               input.Enabled,
		AgentID:               strings.TrimSpace(input.AgentID),
		ModelID:               strings.TrimSpace(input.ModelID),
		ThinkingLevel:         strings.TrimSpace(input.ThinkingLevel),
		AgentArgs:             strings.TrimSpace(input.AgentArgs),
		CustomAgentCommand:    strings.TrimSpace(input.CustomAgentCommand),
		CustomPrompt:          strings.TrimSpace(input.CustomPrompt),
		UpdateDefaultTUIAgent: input.UpdateDefaultTUIAgent,
		ClearRepoOverrides:    true,
		DiscoveredModels:      input.DiscoveredModels,
	}
}

func (d *OrcaCommitAIDesiredState) toSaveInput() OrcaCommitAISaveInput {
	return OrcaCommitAISaveInput{
		Enabled:               d.Enabled,
		AgentID:               d.AgentID,
		ModelID:               d.ModelID,
		ThinkingLevel:         d.ThinkingLevel,
		AgentArgs:             d.AgentArgs,
		CustomAgentCommand:    d.CustomAgentCommand,
		CustomPrompt:          d.CustomPrompt,
		UpdateDefaultTUIAgent: d.UpdateDefaultTUIAgent,
		ClearRepoOverrides:    true,
		DiscoveredModels:      d.DiscoveredModels,
	}
}

func (d *OrcaCommitAIDesiredState) side() *OrcaCommitAISyncSide {
	return &OrcaCommitAISyncSide{
		Enabled:            d.Enabled,
		AgentID:            d.AgentID,
		ModelID:            d.ModelID,
		ThinkingLevel:      d.ThinkingLevel,
		AgentArgs:          strings.TrimSpace(d.AgentArgs),
		CustomPrompt:       strings.TrimSpace(d.CustomPrompt),
		CustomAgentCommand: strings.TrimSpace(d.CustomAgentCommand),
	}
}

func (a *orcaCommitAIActualState) side() *OrcaCommitAISyncSide {
	return &OrcaCommitAISyncSide{
		Enabled:            a.Enabled,
		AgentID:            a.AgentID,
		ModelID:            a.ModelID,
		ThinkingLevel:      a.ThinkingLevel,
		AgentArgs:          strings.TrimSpace(a.AgentArgs),
		CustomPrompt:       strings.TrimSpace(a.CustomPrompt),
		CustomAgentCommand: strings.TrimSpace(a.CustomAgentCommand),
		RepoOverrideCount:  a.RepoOverrideCount,
	}
}

// orcaProfileIsCloudLinked membaca jenis profil aktif dari indeks profil Orca.
func orcaProfileIsCloudLinked(home string) bool {
	if strings.TrimSpace(home) == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "orca", "orca-profile-index.json"))
	if err != nil {
		return false
	}
	var idx struct {
		Profiles []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"profiles"`
	}
	if json.Unmarshal(raw, &idx) != nil {
		return false
	}
	activeID, _ := ResolveOrcaProfileDBPath(home)
	for _, p := range idx.Profiles {
		if p.ID == activeID {
			return strings.EqualFold(strings.TrimSpace(p.Kind), "cloud-linked")
		}
	}
	return false
}

func readOrcaCommitAIActualState(ctx context.Context) (*orcaCommitAIActualState, error) {
	_, home := resolveOrcaCommitUserFn()
	_, dbPath := ResolveOrcaProfileDBPath(home)
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("%w: path database profil Orca tidak tersedia", ErrOrcaCommitAIConflict)
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return nil, fmt.Errorf("%w: database profil Orca tidak ditemukan di %s", ErrOrcaCommitAIConflict, dbPath)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(3000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka profile-state.db: %w", err)
	}
	defer db.Close()

	var payload string
	var rev int64
	var updatedAt int64
	if err := db.QueryRowContext(ctx,
		`SELECT payload, revision, updated_at FROM profile_state_documents WHERE domain = 'settings'`,
	).Scan(&payload, &rev, &updatedAt); err != nil {
		return nil, fmt.Errorf("gagal membaca domain settings Orca: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return nil, fmt.Errorf("format settings Orca tidak valid: %w", err)
	}

	sc := getMap(settings, "sourceControlAi")
	legacy := getMap(settings, "commitMessageAi")
	actions := getMap(sc, "actions")
	commitAction := getMap(actions, "commitMessage")
	instrMap := getMap(sc, "instructionsByOperation")

	actual := &orcaCommitAIActualState{
		Enabled:            getBoolDefault(sc, "enabled", true),
		SettingsRevision:   rev,
		SettingsUpdatedAt:  updatedAt,
		DefaultTUIAgent:    getString(settings, "defaultTuiAgent"),
		AgentArgs:          strings.TrimSpace(getString(commitAction, "agentArgs")),
		CustomAgentCommand: getString(sc, "customAgentCommand"),
	}
	if sc == nil {
		actual.Enabled = getBoolDefault(legacy, "enabled", true)
	}
	if actual.CustomAgentCommand == "" {
		actual.CustomAgentCommand = getString(legacy, "customAgentCommand")
	}

	agentMirrors := map[string]string{
		"sourceControlAi":          getString(sc, "agentId"),
		"actions.commitMessage":    getString(commitAction, "agentId"),
		"commitMessageAi (legacy)": getString(legacy, "agentId"),
	}
	// Urutan prioritas harus deterministik (iterasi map di Go acak), mengikuti
	// fallback yang dipakai GetOrcaCommitAIConfig.
	for _, v := range []string{
		getString(commitAction, "agentId"),
		getString(sc, "agentId"),
		getString(legacy, "agentId"),
	} {
		if v != "" {
			actual.AgentID = v
			break
		}
	}
	if actual.AgentID == "" && actual.DefaultTUIAgent != "" && actual.DefaultTUIAgent != "blank" {
		actual.AgentID = actual.DefaultTUIAgent
	}

	tpl := getString(commitAction, "commandInputTemplate")
	instr := getString(instrMap, "commitMessage")
	legacyPrompt := getString(legacy, "customPrompt")
	if tpl != "" {
		actual.CustomPrompt = legacyPromptFromTemplate(tpl, instr)
	} else if instr != "" {
		actual.CustomPrompt = instr
	} else {
		actual.CustomPrompt = legacyPrompt
	}

	promptMirrors := map[string]string{
		"actions.commitMessage":    strings.TrimSpace(legacyPromptFromTemplate(tpl, instr)),
		"instructionsByOperation":  strings.TrimSpace(instr),
		"commitMessageAi (legacy)": strings.TrimSpace(legacyPrompt),
	}
	actual.MirrorDrift = append(actual.MirrorDrift, distinctMirrorDrift("agentId", agentMirrors)...)
	actual.MirrorDrift = append(actual.MirrorDrift, distinctMirrorDrift("prompt", promptMirrors)...)

	selectedByAgent, thinkingByModel := extractSelectedModelsFromSettings(settings)
	if mID := selectedByAgent[actual.AgentID]; mID != "" {
		actual.ModelID = mID
	} else {
		actual.ModelID = defaultModelIDForAgent(actual.AgentID)
	}
	actual.ThinkingLevel = thinkingByModel[actual.ModelID]

	if actual.AgentID != "" && actual.AgentID != "custom" && actual.ModelID != "" {
		runtimeHostKeys := collectKnownOrcaRuntimeHostKeysFromDB(ctx, db)
		if len(runtimeHostKeys) > 0 {
			commitChoice := getMap(getMap(sc, "modelOverridesByOperation"), "commitMessage")
			byHost := getMap(commitChoice, "selectedModelByAgentByHost")
			for _, hostKey := range runtimeHostKeys {
				hostMap := getMap(byHost, hostKey)
				if getString(hostMap, actual.AgentID) != actual.ModelID {
					actual.HostModelDrift = append(actual.HostModelDrift, fmt.Sprintf("model host %s belum sinkron", hostKey))
				}
			}
		}
	}

	var reposPayload string
	if err := db.QueryRowContext(ctx,
		`SELECT payload FROM profile_state_documents WHERE domain = 'repos'`,
	).Scan(&reposPayload); err == nil {
		var repos []map[string]any
		if json.Unmarshal([]byte(reposPayload), &repos) == nil {
			for _, repo := range repos {
				if repoHasCommitMessageOverride(repo) {
					actual.RepoOverrideCount++
				}
			}
		}
	}

	return actual, nil
}

func distinctMirrorDrift(name string, mirrors map[string]string) []string {
	seen := map[string]bool{}
	var values []string
	for _, v := range mirrors {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		values = append(values, v)
	}
	if len(values) <= 1 {
		return nil
	}
	return []string{fmt.Sprintf("%s tidak konsisten di %s", name, strings.Join(values, " / "))}
}

func compareOrcaCommitAIDesired(d *OrcaCommitAIDesiredState, a *orcaCommitAIActualState) []string {
	var drift []string
	if d.Enabled != a.Enabled {
		drift = append(drift, "enabled")
	}
	if strings.TrimSpace(d.AgentID) != strings.TrimSpace(a.AgentID) {
		drift = append(drift, "agent")
	}
	if d.AgentID != "custom" {
		if strings.TrimSpace(d.ModelID) != strings.TrimSpace(a.ModelID) {
			drift = append(drift, "model")
		}
		if strings.TrimSpace(d.ThinkingLevel) != strings.TrimSpace(a.ThinkingLevel) {
			drift = append(drift, "thinking level")
		}
	}
	if strings.TrimSpace(d.AgentArgs) != strings.TrimSpace(a.AgentArgs) {
		drift = append(drift, "agent args")
	}
	if strings.TrimSpace(d.CustomPrompt) != strings.TrimSpace(a.CustomPrompt) {
		drift = append(drift, "prompt")
	}
	if strings.TrimSpace(d.CustomAgentCommand) != strings.TrimSpace(a.CustomAgentCommand) {
		drift = append(drift, "custom agent command")
	}
	if d.UpdateDefaultTUIAgent && strings.TrimSpace(d.AgentID) != strings.TrimSpace(a.DefaultTUIAgent) {
		drift = append(drift, "default TUI agent")
	}
	if d.ClearRepoOverrides && a.RepoOverrideCount > 0 {
		drift = append(drift, fmt.Sprintf("override per-repo (%d)", a.RepoOverrideCount))
	}
	drift = append(drift, a.HostModelDrift...)
	return append(drift, a.MirrorDrift...)
}

// GetOrcaCommitAISyncStatus membandingkan desired state wdev dengan keadaan DB Orca (default locale en).
func GetOrcaCommitAISyncStatus(ctx context.Context) (*OrcaCommitAISyncStatus, error) {
	return GetOrcaCommitAISyncStatusLocale(ctx, i18n.DefaultLocale)
}

// GetOrcaCommitAISyncStatusLocale compares wdev desired state against Orca DB state using the requested locale.
func GetOrcaCommitAISyncStatusLocale(ctx context.Context, lang string) (*OrcaCommitAISyncStatus, error) {
	lang = i18n.Normalize(lang)
	user, home := resolveOrcaCommitUserFn()
	profileID, dbPath := ResolveOrcaProfileDBPath(home)
	st, loadErr := LoadOrcaCommitAISyncState(home)

	desktopOpen := orcaProcessActiveFn(ctx, user)
	status := &OrcaCommitAISyncStatus{
		ProfileID:       profileID,
		DBPath:          dbPath,
		CloudLinked:     orcaProfileIsCloudLinked(home),
		OrcaRunning:     (home != "" && orcaUserSvcActiveFn(ctx, user, home)) || orcaSysSvcActiveFn(ctx) || desktopOpen || orcaAnyProcessActiveFn(ctx, user),
		OrcaDesktopOpen: desktopOpen,
		State:           orcaCommitAISyncStateUnmanaged,
		InSync:          true,
	}
	if loadErr != nil {
		status.State = orcaCommitAISyncStateError
		status.InSync = false
		status.LastError = loadErr.Error()
		return status, nil
	}
	if st == nil || st.Desired == nil {
		return status, nil
	}

	status.Managed = true
	status.InSync = false
	status.LastAppliedAt = st.LastAppliedAt
	status.LastError = st.LastError
	status.Desired = st.Desired.side()

	if st.Desired.ProfileID != "" && st.Desired.ProfileID != profileID {
		status.State = orcaCommitAISyncStateConflict
		status.Drift = []string{fmt.Sprintf(i18n.Pick(lang, "active Orca profile changed from %s to %s", "profil Orca aktif berubah dari %s menjadi %s"), st.Desired.ProfileID, profileID)}
		return status, nil
	}

	actual, err := readOrcaCommitAIActualState(ctx)
	if err != nil {
		status.State = orcaCommitAISyncStateError
		status.Drift = []string{err.Error()}
		return status, nil
	}
	status.Actual = actual.side()
	status.SettingsRevision = actual.SettingsRevision
	status.SettingsUpdatedAt = actual.SettingsUpdatedAt

	drift := compareOrcaCommitAIDesired(st.Desired, actual)
	if len(drift) > 0 {
		status.State = orcaCommitAISyncStateDrift
		status.Drift = drift
		if status.OrcaDesktopOpen && !(home != "" && orcaUserSvcActiveFn(ctx, user, home)) && !orcaSysSvcActiveFn(ctx) {
			status.State = orcaCommitAISyncStateBlocked
		}
		return status, nil
	}

	status.State = orcaCommitAISyncStateSynced
	status.InSync = true
	return status, nil
}

// SetOrcaCommitAIDesiredState menyimpan keinginan wdev lalu langsung menerapkannya (default locale en).
func SetOrcaCommitAIDesiredState(ctx context.Context, cb LogCallback, input OrcaCommitAISaveInput) (*OrcaCommitAISyncStatus, error) {
	return SetOrcaCommitAIDesiredStateLocale(ctx, cb, input, i18n.DefaultLocale)
}

// SetOrcaCommitAIDesiredStateLocale stores wdev's desired Orca Commit AI state and reconciles it in the requested locale.
func SetOrcaCommitAIDesiredStateLocale(ctx context.Context, cb LogCallback, input OrcaCommitAISaveInput, lang string) (*OrcaCommitAISyncStatus, error) {
	lang = i18n.Normalize(lang)
	// Validasi dulu: jangan sampai input buruk tersimpan sebagai desired state
	// dan terus-menerus gagal disinkronkan oleh reconciler.
	if _, _, agentArgs, err := validateOrcaCommitAISaveInputLocale(input, lang); err != nil {
		return nil, err
	} else {
		input.AgentArgs = agentArgs
	}

	user, home := resolveOrcaCommitUserFn()
	profileID, _ := ResolveOrcaProfileDBPath(home)
	desired := desiredFromSaveInput(input, profileID)

	st, err := LoadOrcaCommitAISyncState(home)
	if err != nil {
		return nil, err
	}
	if st == nil {
		st = &OrcaCommitAISyncState{}
	}
	st.Desired = &desired
	st.UpdatedAt = time.Now().UnixMilli()
	st.LastError = ""
	st.Failures = 0
	if err := saveOrcaCommitAISyncState(user, home, st); err != nil {
		return nil, err
	}
	return ReconcileOrcaCommitAILocale(ctx, cb, true, lang)
}

// ReconcileOrcaCommitAI menerapkan desired state bila terjadi drift (default locale en).
// Dipanggil setelah save, saat startup, dan oleh ticker periodik server.
func ReconcileOrcaCommitAI(ctx context.Context, cb LogCallback, force bool) (*OrcaCommitAISyncStatus, error) {
	return ReconcileOrcaCommitAILocale(ctx, cb, force, i18n.DefaultLocale)
}

// ReconcileOrcaCommitAILocale applies desired state on drift using localized diagnostic/log messages.
func ReconcileOrcaCommitAILocale(ctx context.Context, cb LogCallback, force bool, lang string) (*OrcaCommitAISyncStatus, error) {
	lang = i18n.Normalize(lang)
	status, err := GetOrcaCommitAISyncStatusLocale(ctx, lang)
	if err != nil && status == nil {
		return nil, err
	}
	if status == nil || status.State == orcaCommitAISyncStateUnmanaged {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	if !force && status.InSync {
		return status, nil
	}
	if !force && (status.State == orcaCommitAISyncStateBlocked || status.State == orcaCommitAISyncStateConflict || status.State == orcaCommitAISyncStateError) {
		_, home := resolveOrcaCommitUserFn()
		st, loadErr := LoadOrcaCommitAISyncState(home)
		if loadErr == nil && st != nil && st.LastAppliedAt > 0 && time.Since(time.UnixMilli(st.LastAppliedAt)) < orcaCommitAIReconcileCooldown {
			return status, nil
		}
	}

	user, home := resolveOrcaCommitUserFn()
	st, err := LoadOrcaCommitAISyncState(home)
	if err != nil || st == nil || st.Desired == nil {
		return status, err
	}

	if cb != nil {
		cb(i18n.Pick(lang, "==> Checking Orca Commit AI synchronization...", "==> Memeriksa sinkronisasi Orca Commit AI..."))
	}
	if _, err := saveOrcaCommitAIConfigLocale(ctx, cb, st.Desired.toSaveInput(), force, lang); err != nil {
		st.LastError = err.Error()
		st.Failures++
		if saveErr := saveOrcaCommitAISyncState(user, home, st); saveErr != nil && cb != nil {
			cb(fmt.Sprintf(i18n.Pick(lang, "[WARN] Failed to save sync failure status: %v", "[WARN] Gagal menyimpan status kegagalan sinkron: %v"), saveErr))
		}
		status.State = orcaCommitAISyncStateBlocked
		if !errors.Is(err, ErrOrcaCommitAIConflict) {
			status.State = orcaCommitAISyncStateError
		}
		status.InSync = false
		status.LastError = err.Error()
		return status, nil
	}

	st.LastAppliedAt = time.Now().UnixMilli()
	st.LastError = ""
	st.Failures = 0
	if saveErr := saveOrcaCommitAISyncState(user, home, st); saveErr != nil && cb != nil {
		cb(fmt.Sprintf(i18n.Pick(lang, "[WARN] Failed to save last sync timestamp: %v", "[WARN] Gagal menyimpan waktu sinkron terakhir: %v"), saveErr))
	}

	// Jika Orca dimulai ulang setelah save, beri jeda singkat lalu pastikan payload
	// tidak ditimpa saat aplikasi memuat ulang pengaturannya.
	if (home != "" && orcaUserSvcActiveFn(ctx, user, home)) || orcaSysSvcActiveFn(ctx) || orcaAnyProcessActiveFn(ctx, user) {
		select {
		case <-time.After(orcaCommitAIPostApplyVerifyWait):
		case <-ctx.Done():
		}
	}

	verify, verifyErr := GetOrcaCommitAISyncStatusLocale(ctx, lang)
	if verifyErr != nil || verify == nil {
		return status, verifyErr
	}
	if verify.State == orcaCommitAISyncStateUnmanaged {
		return verify, nil
	}
	if !verify.InSync {
		msg := i18n.Pick(lang, "Orca overwrote configuration after apply", "Orca menimpa konfigurasi setelah diterapkan")
		if len(verify.Drift) > 0 {
			msg += ": " + strings.Join(verify.Drift, ", ")
		}
		st.LastError = msg
		st.Failures++
		if saveErr := saveOrcaCommitAISyncState(user, home, st); saveErr != nil && cb != nil {
			cb(fmt.Sprintf(i18n.Pick(lang, "[WARN] Failed to save sync conflict status: %v", "[WARN] Gagal menyimpan status konflik sinkron: %v"), saveErr))
		}
		verify.State = orcaCommitAISyncStateConflict
		verify.InSync = false
		verify.LastError = msg
	}
	return verify, nil
}
