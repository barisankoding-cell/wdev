package installer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const localCommitMessageHostKey = "local"

var (
	ansiEscapeRe     = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	cursorModelRowRe = regexp.MustCompile(`(?i)^(\S+)(?:\s+-\s+(.+?))?(?:\s+\(((?:current|default)(?:,\s*(?:current|default))?)\))?$`)
	bareModelIDRe    = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	agyLegacyRowRe   = regexp.MustCompile(`(?i)^.+ \((?:low|medium|high|thinking)\)$`)
)

// Test hook untuk simulasi output probe CLI model agent.
var runUserModelProbeFn = runUserCommandWithTimeout

// OrcaThinkingLevel merepresentasikan pilihan tingkat reasoning/effort sebuah model.
type OrcaThinkingLevel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// OrcaCommitAIModel merepresentasikan model AI untuk commit message generator Orca.
type OrcaCommitAIModel struct {
	ID                   string              `json:"id"`
	Label                string              `json:"label"`
	Description          string              `json:"description,omitempty"`
	ThinkingLevels       []OrcaThinkingLevel `json:"thinkingLevels,omitempty"`
	DefaultThinkingLevel string              `json:"defaultThinkingLevel,omitempty"`
	IsDefault            bool                `json:"isDefault,omitempty"`
	Source               string              `json:"source,omitempty"`
}

// OrcaCommitAIModelsResult adalah hasil daftar model untuk satu agent AI.
type OrcaCommitAIModelsResult struct {
	AgentID           string              `json:"agent_id"`
	DefaultModelID    string              `json:"default_model_id"`
	SupportsDiscovery bool                `json:"supports_discovery"`
	Models            []OrcaCommitAIModel `json:"models"`
	SelectedModel     string              `json:"selected_model"`
	ThinkingLevel     string              `json:"thinking_level,omitempty"`
	LiveDiscovered    bool                `json:"live_discovered"`
	DiscoveryError    string              `json:"discovery_error,omitempty"`
}

var (
	openAIThinkingLevels = []OrcaThinkingLevel{
		{ID: "low", Label: "Low"},
		{ID: "medium", Label: "Medium"},
		{ID: "high", Label: "High"},
		{ID: "xhigh", Label: "Extra High"},
	}
	claudeThinkingLevels = []OrcaThinkingLevel{
		{ID: "low", Label: "Low"},
		{ID: "medium", Label: "Medium"},
		{ID: "high", Label: "High"},
		{ID: "xhigh", Label: "Extra High"},
		{ID: "max", Label: "Max"},
	}
	piThinkingLevels = []OrcaThinkingLevel{
		{ID: "off", Label: "Off"},
		{ID: "low", Label: "Low"},
		{ID: "medium", Label: "Medium"},
		{ID: "high", Label: "High"},
		{ID: "xhigh", Label: "Extra High"},
	}
)

func defaultModelIDForAgent(agentID string) string {
	switch agentID {
	case "claude":
		return "sonnet"
	case "codex":
		return "gpt-5.6-terra"
	case "copilot", "cursor":
		return "auto"
	case "custom":
		return ""
	default:
		return "default"
	}
}

func agentSupportsDynamicModelDiscovery(agentID string) bool {
	switch agentID {
	case "opencode", "opencode2", "antigravity", "omp", "claude", "cursor", "pi", "jcode":
		return true
	default:
		return false
	}
}

func seedModelsForAgent(agentID string) []OrcaCommitAIModel {
	switch agentID {
	case "opencode", "opencode2":
		return []OrcaCommitAIModel{
			{ID: "default", Label: "Config default", IsDefault: true, Source: "seed"},
			{
				ID:                   "opencode/gpt-5.4-mini",
				Label:                "OpenCode GPT 5.4 Mini",
				ThinkingLevels:       append([]OrcaThinkingLevel(nil), openAIThinkingLevels...),
				DefaultThinkingLevel: "low",
				Source:               "seed",
			},
		}
	case "claude":
		return []OrcaCommitAIModel{
			{ID: "haiku", Label: "Haiku", Source: "seed"},
			{
				ID:                   "sonnet",
				Label:                "Sonnet",
				ThinkingLevels:       append([]OrcaThinkingLevel(nil), claudeThinkingLevels...),
				DefaultThinkingLevel: "low",
				IsDefault:            true,
				Source:               "seed",
			},
			{
				ID:                   "opus",
				Label:                "Opus",
				ThinkingLevels:       append([]OrcaThinkingLevel(nil), claudeThinkingLevels...),
				DefaultThinkingLevel: "low",
				Source:               "seed",
			},
		}
	case "codex":
		ids := [][2]string{
			{"gpt-5.6-terra", "GPT-5.6 Terra"},
			{"gpt-5.5", "GPT-5.5"},
			{"gpt-5.4", "GPT-5.4"},
			{"gpt-5.4-mini", "GPT-5.4 Mini"},
			{"gpt-5.3-codex", "GPT-5.3 Codex"},
			{"gpt-5.3-codex-spark", "GPT-5.3 Codex Spark"},
			{"gpt-5.2", "GPT-5.2"},
		}
		models := make([]OrcaCommitAIModel, 0, len(ids))
		for i, pair := range ids {
			models = append(models, OrcaCommitAIModel{
				ID:                   pair[0],
				Label:                pair[1],
				ThinkingLevels:       append([]OrcaThinkingLevel(nil), openAIThinkingLevels...),
				DefaultThinkingLevel: "low",
				IsDefault:            i == 0,
				Source:               "seed",
			})
		}
		return models
	case "copilot":
		return []OrcaCommitAIModel{
			{ID: "auto", Label: "Auto", IsDefault: true, Source: "seed"},
			{ID: "claude-haiku-4.5", Label: "Claude Haiku 4.5", Source: "seed"},
			{ID: "claude-sonnet-4.5", Label: "Claude Sonnet 4.5", Source: "seed"},
			{ID: "claude-sonnet-4.6", Label: "Claude Sonnet 4.6", Source: "seed"},
			{ID: "claude-opus-4.5", Label: "Claude Opus 4.5", Source: "seed"},
			{ID: "claude-opus-4.6", Label: "Claude Opus 4.6", Source: "seed"},
			{ID: "claude-opus-4.6-fast", Label: "Claude Opus 4.6 Fast", Source: "seed"},
			{ID: "claude-opus-4.7", Label: "Claude Opus 4.7", Source: "seed"},
			{ID: "gpt-4.1", Label: "GPT-4.1", Source: "seed"},
			{ID: "gpt-5-mini", Label: "GPT-5 Mini", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
			{ID: "gpt-5.2", Label: "GPT-5.2", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
			{ID: "gpt-5.2-codex", Label: "GPT-5.2 Codex", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
			{ID: "gpt-5.3-codex", Label: "GPT-5.3 Codex", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
			{ID: "gpt-5.4", Label: "GPT-5.4", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
			{ID: "gpt-5.4-mini", Label: "GPT-5.4 Mini", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
			{ID: "gpt-5.5", Label: "GPT-5.5", ThinkingLevels: append([]OrcaThinkingLevel(nil), openAIThinkingLevels...), DefaultThinkingLevel: "low", Source: "seed"},
		}
	case "cursor":
		return []OrcaCommitAIModel{
			{ID: "auto", Label: "Auto", IsDefault: true, Source: "seed"},
		}
	case "kimi":
		return []OrcaCommitAIModel{
			{ID: "default", Label: "Config default", IsDefault: true, Source: "seed"},
			{ID: "kimi-code/kimi-for-coding", Label: "Kimi K2.6", Source: "seed"},
			{ID: "kimi-code/k3", Label: "Kimi K3", Source: "seed"},
		}
	case "omp", "pi", "antigravity", "jcode", "dsh":
		return []OrcaCommitAIModel{
			{ID: "default", Label: "Config default", IsDefault: true, Source: "seed"},
		}
	default:
		return nil
	}
}

func labelFromModelID(id string) string {
	// Gabungkan versi ber-dash seperti claude-opus-4-7 menjadi claude-opus-4.7 (kecuali tanggal 2 digit berulang seperti 2024-08-06).
	segments := strings.FieldsFunc(id, func(r rune) bool { return r == '/' || r == '-' })
	if len(segments) == 0 {
		return id
	}
	isVerNum := func(s string) bool {
		if len(s) == 0 || len(s) > 2 {
			return false
		}
		if len(s) == 2 && s[0] == '0' {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	isFourDigitYear := func(s string) bool {
		if len(s) != 4 {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	merged := make([]string, 0, len(segments))
	for i := 0; i < len(segments); i++ {
		if i+1 < len(segments) && isVerNum(segments[i]) && isVerNum(segments[i+1]) {
			// Jangan gabungkan pola tanggal MM-DD (dua segmen tepat 2 digit setelah tahun 4 digit)
			if !(i > 0 && isFourDigitYear(segments[i-1])) {
				ver := segments[i] + "." + segments[i+1]
				j := i + 2
				for j < len(segments) && isVerNum(segments[j]) {
					ver += "." + segments[j]
					j++
				}
				merged = append(merged, ver)
				i = j - 1
				continue
			}
		}
		merged = append(merged, segments[i])
	}

	out := make([]string, 0, len(merged))
	for _, seg := range merged {
		if strings.EqualFold(seg, "gpt") {
			out = append(out, "GPT")
			continue
		}
		if len(seg) <= 4 && seg[0] >= '0' && seg[0] <= '9' {
			out = append(out, strings.ToUpper(seg))
			continue
		}
		out = append(out, strings.ToUpper(seg[:1])+seg[1:])
	}
	return strings.Join(out, " ")
}

func withOpenAiThinking(m OrcaCommitAIModel) OrcaCommitAIModel {
	lower := strings.ToLower(m.ID)
	if strings.Contains(lower, "gpt-5") || strings.Contains(lower, "codex") {
		if len(m.ThinkingLevels) == 0 {
			m.ThinkingLevels = append([]OrcaThinkingLevel(nil), openAIThinkingLevels...)
		}
		if m.DefaultThinkingLevel == "" {
			m.DefaultThinkingLevel = "low"
		}
	}
	return m
}

func uniqueCommitModels(models []OrcaCommitAIModel) []OrcaCommitAIModel {
	seen := make(map[string]bool, len(models))
	out := make([]OrcaCommitAIModel, 0, len(models))
	for _, m := range models {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m.ID = id
		if strings.TrimSpace(m.Label) == "" {
			m.Label = id
		}
		out = append(out, m)
	}
	return out
}

func parseLineModels(stdout string) []OrcaCommitAIModel {
	cleaned := ansiEscapeRe.ReplaceAllString(stdout, "")
	var models []OrcaCommitAIModel
	for _, rawLine := range strings.Split(cleaned, "\n") {
		id := strings.TrimSpace(rawLine)
		if id == "" || strings.ContainsAny(id, " \t") {
			continue
		}
		models = append(models, withOpenAiThinking(OrcaCommitAIModel{
			ID:     id,
			Label:  labelFromModelID(id),
			Source: "discovered",
		}))
	}
	return uniqueCommitModels(models)
}

func parseAntigravityModels(stdout string) []OrcaCommitAIModel {
	cleaned := ansiEscapeRe.ReplaceAllString(stdout, "")
	var models []OrcaCommitAIModel
	for _, rawLine := range strings.Split(cleaned, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if sep := strings.IndexByte(line, '\t'); sep >= 0 {
			id := strings.TrimSpace(line[:sep])
			label := strings.TrimSpace(line[sep+1:])
			if id == "" || label == "" || strings.ContainsAny(id, " \t") || strings.EqualFold(id, "id") || strings.EqualFold(id, "model") {
				continue
			}
			models = append(models, OrcaCommitAIModel{
				ID:     id,
				Label:  label,
				Source: "discovered",
			})
			continue
		}
		if agyLegacyRowRe.MatchString(line) {
			models = append(models, OrcaCommitAIModel{
				ID:     line,
				Label:  line,
				Source: "discovered",
			})
		}
	}
	return uniqueCommitModels(models)
}

func parseOmpModels(stdout string) []OrcaCommitAIModel {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil
	}
	var payload struct {
		Models []struct {
			Provider      string   `json:"provider"`
			ID            string   `json:"id"`
			Selector      string   `json:"selector"`
			Name          string   `json:"name"`
			Thinking      []string `json:"thinking"`
			ContextWindow int      `json:"contextWindow"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		start := strings.IndexByte(trimmed, '{')
		end := strings.LastIndexByte(trimmed, '}')
		if start < 0 || end <= start {
			return nil
		}
		if err2 := json.Unmarshal([]byte(trimmed[start:end+1]), &payload); err2 != nil {
			return nil
		}
	}
	models := make([]OrcaCommitAIModel, 0, len(payload.Models))
	for _, row := range payload.Models {
		id := strings.TrimSpace(row.Selector)
		if id == "" && strings.TrimSpace(row.Provider) != "" && strings.TrimSpace(row.ID) != "" {
			id = strings.TrimSpace(row.Provider) + "/" + strings.TrimSpace(row.ID)
		}
		if id == "" {
			continue
		}
		label := strings.TrimSpace(row.Name)
		if label == "" {
			label = labelFromModelID(id)
		}
		var levels []OrcaThinkingLevel
		defaultThinking := ""
		for _, lvl := range row.Thinking {
			lvl = strings.TrimSpace(lvl)
			if lvl == "" {
				continue
			}
			levels = append(levels, OrcaThinkingLevel{
				ID:    lvl,
				Label: strings.ToUpper(lvl[:1]) + lvl[1:],
			})
			if lvl == "low" {
				defaultThinking = "low"
			}
		}
		if defaultThinking == "" && len(levels) > 0 {
			defaultThinking = levels[0].ID
		}
		models = append(models, OrcaCommitAIModel{
			ID:                   id,
			Label:                label,
			Description:          strings.TrimSpace(row.Provider),
			ThinkingLevels:       levels,
			DefaultThinkingLevel: defaultThinking,
			Source:               "discovered",
		})
	}
	return uniqueCommitModels(models)
}

func parseCursorModels(stdout string) []OrcaCommitAIModel {
	cleaned := ansiEscapeRe.ReplaceAllString(stdout, "")
	var models []OrcaCommitAIModel
	for _, rawLine := range strings.Split(cleaned, "\n") {
		line := strings.TrimSpace(rawLine)
		m := cursorModelRowRe.FindStringSubmatch(line)
		if len(m) < 4 {
			continue
		}
		id, label, flags := m[1], strings.TrimSpace(m[2]), strings.ToLower(m[3])
		if label == "" && !bareModelIDRe.MatchString(id) {
			continue
		}
		if label == "" {
			label = id
		}
		models = append(models, withOpenAiThinking(OrcaCommitAIModel{
			ID:        id,
			Label:     label,
			IsDefault: strings.Contains(flags, "default"),
			Source:    "discovered",
		}))
	}
	return uniqueCommitModels(models)
}

func parsePiModels(stdout string) []OrcaCommitAIModel {
	cleaned := ansiEscapeRe.ReplaceAllString(stdout, "")
	var models []OrcaCommitAIModel
	for _, rawLine := range strings.Split(cleaned, "\n") {
		parts := strings.Fields(rawLine)
		if len(parts) < 6 || parts[0] == "provider" {
			continue
		}
		id := parts[0] + "/" + parts[1]
		label := labelFromModelID(parts[0]) + " " + labelFromModelID(parts[1])
		m := OrcaCommitAIModel{
			ID:     id,
			Label:  label,
			Source: "discovered",
		}
		if parts[4] == "yes" {
			m.ThinkingLevels = append([]OrcaThinkingLevel(nil), piThinkingLevels...)
			m.DefaultThinkingLevel = "low"
		}
		models = append(models, m)
	}
	return uniqueCommitModels(models)
}

func parseClaudeModels(stdout string) []OrcaCommitAIModel {
	var models []OrcaCommitAIModel
	for _, rawLine := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "control_response") {
			continue
		}
		var env struct {
			Type     string `json:"type"`
			Response struct {
				Subtype  string `json:"subtype"`
				Response struct {
					Models []struct {
						Value                 string   `json:"value"`
						DisplayName           string   `json:"displayName"`
						Description           string   `json:"description"`
						Disabled              bool     `json:"disabled"`
						SupportsEffort        bool     `json:"supportsEffort"`
						SupportedEffortLevels []string `json:"supportedEffortLevels"`
						ResolvedModel         string   `json:"resolvedModel"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type != "control_response" || env.Response.Subtype != "success" {
			continue
		}
		type claudeEntry struct {
			Value                 string
			DisplayName           string
			Description           string
			Disabled              bool
			SupportsEffort        bool
			SupportedEffortLevels []string
			ResolvedModel         string
		}
		var nonDefault []claudeEntry
		var defaultEntries []claudeEntry
		defaultResolved := ""
		for _, raw := range env.Response.Response.Models {
			e := claudeEntry{
				Value:                 strings.TrimSpace(raw.Value),
				DisplayName:           strings.TrimSpace(raw.DisplayName),
				Description:           strings.TrimSpace(raw.Description),
				Disabled:              raw.Disabled,
				SupportsEffort:        raw.SupportsEffort,
				SupportedEffortLevels: raw.SupportedEffortLevels,
				ResolvedModel:         strings.TrimSpace(raw.ResolvedModel),
			}
			if e.Value == "default" {
				if e.ResolvedModel != "" {
					defaultResolved = e.ResolvedModel
				}
				defaultEntries = append(defaultEntries, e)
			} else {
				nonDefault = append(nonDefault, e)
			}
		}

		ordered := append(nonDefault, defaultEntries...)
		for _, entry := range ordered {
			if entry.Disabled {
				continue
			}
			id := entry.ResolvedModel
			if id == "" {
				id = entry.Value
			}
			if id == "" || id == "default" {
				continue
			}
			label := entry.DisplayName
			if label == "" || (entry.ResolvedModel != "" && entry.ResolvedModel != entry.Value) {
				label = labelFromModelID(id)
			}
			var levels []OrcaThinkingLevel
			defaultThinking := ""
			if entry.SupportsEffort && len(entry.SupportedEffortLevels) > 0 {
				allowed := map[string]bool{}
				for _, l := range entry.SupportedEffortLevels {
					allowed[l] = true
				}
				for _, cl := range claudeThinkingLevels {
					if allowed[cl.ID] {
						levels = append(levels, cl)
						if cl.ID == "low" {
							defaultThinking = "low"
						}
					}
				}
				if defaultThinking == "" && len(levels) > 0 {
					defaultThinking = levels[0].ID
				}
			}
			models = append(models, OrcaCommitAIModel{
				ID:                   id,
				Label:                label,
				Description:          entry.Description,
				ThinkingLevels:       levels,
				DefaultThinkingLevel: defaultThinking,
				IsDefault:            defaultResolved != "" && id == defaultResolved,
				Source:               "discovered",
			})
		}
		if len(models) > 0 {
			return uniqueCommitModels(models)
		}
	}
	return nil
}

func userCliPath(home string) string {
	var parts []string
	if home != "" {
		parts = append(parts,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}
	parts = append(parts,
		"/usr/local/lib/nodejs/current/bin",
		"/usr/local/go/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	)
	if cur := strings.TrimSpace(os.Getenv("PATH")); cur != "" {
		parts = append(parts, cur)
	}
	seen := make(map[string]bool, len(parts))
	dedup := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" && !seen[p] {
			seen[p] = true
			dedup = append(dedup, p)
		}
	}
	return strings.Join(dedup, ":")
}

func runUserCommandWithTimeout(ctx context.Context, user, home, binPath string, args []string, stdinData string, timeout time.Duration) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if os.Geteuid() == 0 && user != "" && user != "root" {
		runArgs := append([]string{"-u", user, "--", binPath}, args...)
		cmd = exec.CommandContext(probeCtx, "runuser", runArgs...)
	} else {
		cmd = exec.CommandContext(probeCtx, binPath, args...)
	}

	env := userSystemctlEnv(user, home)
	pathVal := "PATH=" + userCliPath(home)
	replaced := false
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = pathVal
			replaced = true
			break
		}
	}
	if !replaced {
		env = append(env, pathVal)
	}
	cmd.Env = env
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errOut := strings.TrimSpace(stderr.String())
		if errOut != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, errOut)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func probeDynamicModelsForAgent(ctx context.Context, user, home, agentID, binPath string) ([]OrcaCommitAIModel, error) {
	const probeTimeout = 12 * time.Second
	switch agentID {
	case "opencode", "opencode2":
		out, err := runUserModelProbeFn(ctx, user, home, binPath, []string{"models"}, "", probeTimeout)
		if err != nil {
			return nil, err
		}
		return parseLineModels(out), nil
	case "antigravity":
		out, err := runUserModelProbeFn(ctx, user, home, binPath, []string{"models"}, "", probeTimeout)
		if err != nil {
			return nil, err
		}
		return parseAntigravityModels(out), nil
	case "omp":
		out, err := runUserModelProbeFn(ctx, user, home, binPath, []string{"models", "--json"}, "", probeTimeout)
		if err != nil {
			return nil, err
		}
		return parseOmpModels(out), nil
	case "cursor":
		out, err := runUserModelProbeFn(ctx, user, home, binPath, []string{"--list-models"}, "", probeTimeout)
		if err != nil {
			return nil, err
		}
		return parseCursorModels(out), nil
	case "pi":
		out, err := runUserModelProbeFn(ctx, user, home, binPath, []string{"--list-models"}, "", probeTimeout)
		if err != nil {
			return nil, err
		}
		return parsePiModels(out), nil
	case "jcode":
		out, err := runUserModelProbeFn(ctx, user, home, binPath, []string{"--no-update", "--quiet", "model", "list"}, "", probeTimeout)
		if err != nil {
			return nil, err
		}
		return parseLineModels(out), nil
	case "claude":
		stdinPayload := `{"type":"control_request","request_id":"orca-model-discovery","request":{"subtype":"list_models"}}` + "\n"
		args := []string{
			"-p", "--input-format", "stream-json", "--output-format", "stream-json",
			"--verbose", "--strict-mcp-config", "--settings", `{"disableAllHooks":true}`,
		}
		out, err := runUserModelProbeFn(ctx, user, home, binPath, args, stdinPayload, probeTimeout)
		if err != nil {
			return nil, err
		}
		return parseClaudeModels(out), nil
	default:
		return nil, nil
	}
}

func decodeModelListValue(raw any, source string) []OrcaCommitAIModel {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var list []OrcaCommitAIModel
	if json.Unmarshal(b, &list) != nil {
		return nil
	}
	for i := range list {
		if list[i].Source == "" {
			list[i].Source = source
		}
	}
	return uniqueCommitModels(list)
}

func extractCachedModelsMapFromSettings(settings map[string]any) map[string][]OrcaCommitAIModel {
	out := map[string][]OrcaCommitAIModel{}
	sc := getMap(settings, "sourceControlAi")
	legacy := getMap(settings, "commitMessageAi")

	collectFrom := func(container map[string]any) {
		if container == nil {
			return
		}
		if byHost := getMap(container, "discoveredModelsByAgentByHost"); byHost != nil {
			if localMap := getMap(byHost, localCommitMessageHostKey); localMap != nil {
				for agentID, val := range localMap {
					if _, exists := out[agentID]; !exists {
						if models := decodeModelListValue(val, "cached"); len(models) > 0 {
							out[agentID] = models
						}
					}
				}
			}
		}
		if byAgent := getMap(container, "discoveredModelsByAgent"); byAgent != nil {
			for agentID, val := range byAgent {
				if _, exists := out[agentID]; !exists {
					if models := decodeModelListValue(val, "cached"); len(models) > 0 {
						out[agentID] = models
					}
				}
			}
		}
	}

	collectFrom(sc)
	collectFrom(legacy)
	return out
}

func extractSelectedModelsFromSettings(settings map[string]any) (selectedByAgent map[string]string, thinkingByModel map[string]string) {
	selectedByAgent = map[string]string{}
	thinkingByModel = map[string]string{}

	sc := getMap(settings, "sourceControlAi")
	legacy := getMap(settings, "commitMessageAi")
	opOverride := getMap(getMap(sc, "modelOverridesByOperation"), "commitMessage")

	readModels := func(choice map[string]any) {
		if choice == nil {
			return
		}
		if byHost := getMap(choice, "selectedModelByAgentByHost"); byHost != nil {
			if localMap := getMap(byHost, localCommitMessageHostKey); localMap != nil {
				for agentID, val := range localMap {
					if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
						if _, exists := selectedByAgent[agentID]; !exists {
							selectedByAgent[agentID] = s
						}
					}
				}
			}
		}
		if byAgent := getMap(choice, "selectedModelByAgent"); byAgent != nil {
			for agentID, val := range byAgent {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					if _, exists := selectedByAgent[agentID]; !exists {
						selectedByAgent[agentID] = s
					}
				}
			}
		}
		if thinkMap := getMap(choice, "selectedThinkingByModel"); thinkMap != nil {
			for modelID, val := range thinkMap {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					if _, exists := thinkingByModel[modelID]; !exists {
						thinkingByModel[modelID] = s
					}
				}
			}
		}
	}

	readModels(opOverride)
	readModels(sc)
	readModels(legacy)
	return selectedByAgent, thinkingByModel
}

func mergeSeedAndDiscoveredModels(agentID string, extra []OrcaCommitAIModel) []OrcaCommitAIModel {
	seeds := seedModelsForAgent(agentID)
	combined := make([]OrcaCommitAIModel, 0, len(seeds)+len(extra))
	combined = append(combined, seeds...)
	combined = append(combined, extra...)
	return uniqueCommitModels(combined)
}

func buildCachedModelsByAgentMap(settings map[string]any) map[string][]OrcaCommitAIModel {
	cached := extractCachedModelsMapFromSettings(settings)
	out := make(map[string][]OrcaCommitAIModel, len(orcaCommitAgentSpecs))
	for _, spec := range orcaCommitAgentSpecs {
		if spec.ID == "custom" {
			continue
		}
		out[spec.ID] = mergeSeedAndDiscoveredModels(spec.ID, cached[spec.ID])
	}
	return out
}

// DiscoverOrcaCommitAIModels mengambil daftar model untuk agent tertentu (seed + cache DB + probe CLI live).
func DiscoverOrcaCommitAIModels(ctx context.Context, agentID string, forceRefresh bool) (*OrcaCommitAIModelsResult, error) {
	agentID = strings.TrimSpace(agentID)
	spec, ok := lookupOrcaAgentSpec(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: agent AI tidak dikenal: %q", ErrOrcaCommitAIValidation, agentID)
	}

	user, home := resolveOrcaCommitUserFn()
	_, dbPath := ResolveOrcaProfileDBPath(home)

	var cachedModels []OrcaCommitAIModel
	selectedModel := ""
	thinkingByModel := map[string]string{}

	if dbPath != "" {
		if fi, err := os.Stat(dbPath); err == nil && !fi.IsDir() {
			dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(3000)", dbPath)
			if db, openErr := sql.Open("sqlite", dsn); openErr == nil {
				defer db.Close()
				var settingsPayload string
				if qErr := db.QueryRowContext(ctx, `SELECT payload FROM profile_state_documents WHERE domain = 'settings'`).Scan(&settingsPayload); qErr == nil {
					var settings map[string]any
					if json.Unmarshal([]byte(settingsPayload), &settings) == nil {
						cachedMap := extractCachedModelsMapFromSettings(settings)
						cachedModels = cachedMap[agentID]
						selMap, thinkMap := extractSelectedModelsFromSettings(settings)
						selectedModel = selMap[agentID]
						thinkingByModel = thinkMap
					}
				}
			}
		}
	}

	defaultModelID := defaultModelIDForAgent(agentID)
	if selectedModel == "" {
		selectedModel = defaultModelID
	}

	supportsDiscovery := agentSupportsDynamicModelDiscovery(agentID)
	liveDiscovered := false
	discoveryErr := ""
	extraModels := cachedModels

	if supportsDiscovery && (forceRefresh || len(cachedModels) == 0) {
		binPath := findUserBinary(home, spec.Binary)
		if binPath != "" {
			probed, pErr := probeDynamicModelsForAgent(ctx, user, home, agentID, binPath)
			if pErr != nil {
				discoveryErr = pErr.Error()
			} else if len(probed) > 0 {
				extraModels = probed
				liveDiscovered = true
			}
		} else if forceRefresh {
			discoveryErr = fmt.Sprintf("biner CLI %q tidak ditemukan untuk user aktif", spec.Binary)
		}
	}

	models := mergeSeedAndDiscoveredModels(agentID, extraModels)
	thinkingLevel := thinkingByModel[selectedModel]
	if thinkingLevel == "" {
		for _, m := range models {
			if m.ID == selectedModel && m.DefaultThinkingLevel != "" {
				thinkingLevel = m.DefaultThinkingLevel
				break
			}
		}
	}

	return &OrcaCommitAIModelsResult{
		AgentID:           agentID,
		DefaultModelID:    defaultModelID,
		SupportsDiscovery: supportsDiscovery,
		Models:            models,
		SelectedModel:     selectedModel,
		ThinkingLevel:     thinkingLevel,
		LiveDiscovered:    liveDiscovered,
		DiscoveryError:    discoveryErr,
	}, nil
}

func sanitizeDiscoveredModelsForDB(models []OrcaCommitAIModel) []map[string]any {
	clean := uniqueCommitModels(models)
	out := make([]map[string]any, 0, len(clean))
	for _, m := range clean {
		// Seed bawaan tidak perlu disimpan ulang sebagai discovered, namun model hasil CLI (termasuk opencode/gpt-5.4-mini bila muncul) aman disimpan.
		if m.Source == "seed" {
			continue
		}
		item := map[string]any{
			"id":    m.ID,
			"label": m.Label,
		}
		if len(m.ThinkingLevels) > 0 {
			levels := make([]map[string]string, 0, len(m.ThinkingLevels))
			for _, lvl := range m.ThinkingLevels {
				levels = append(levels, map[string]string{"id": lvl.ID, "label": lvl.Label})
			}
			item["thinkingLevels"] = levels
		}
		if m.DefaultThinkingLevel != "" {
			item["defaultThinkingLevel"] = m.DefaultThinkingLevel
		}
		out = append(out, item)
	}
	return out
}

func applyModelSelectionToContainer(container map[string]any, agentID, modelID, thinkingLevel string, keepHostKeys bool) {
	byAgent := ensureMap(container, "selectedModelByAgent")
	if modelID != "" {
		byAgent[agentID] = modelID
	} else {
		delete(byAgent, agentID)
	}

	if keepHostKeys {
		byHost := ensureMap(container, "selectedModelByAgentByHost")
		// Hapus key "local" agar host lokal membaca selectedModelByAgent (sama seperti writeChoice di Orca).
		if localMap := getMap(byHost, localCommitMessageHostKey); localMap != nil {
			delete(localMap, agentID)
			if len(localMap) == 0 {
				delete(byHost, localCommitMessageHostKey)
			}
		}
		for hostKey, raw := range byHost {
			if hostKey == localCommitMessageHostKey {
				continue
			}
			if hostMap, ok := raw.(map[string]any); ok && hostMap != nil {
				if modelID != "" {
					hostMap[agentID] = modelID
				} else {
					delete(hostMap, agentID)
				}
			}
		}
	}

	thinkMap := ensureMap(container, "selectedThinkingByModel")
	if modelID != "" && thinkingLevel != "" {
		thinkMap[modelID] = thinkingLevel
	}
}

func applyDiscoveredModelsToContainer(container map[string]any, agentID string, sanitized []map[string]any) {
	if len(sanitized) == 0 {
		return
	}
	byAgent := ensureMap(container, "discoveredModelsByAgent")
	byAgent[agentID] = sanitized

	byHost := ensureMap(container, "discoveredModelsByAgentByHost")
	localMap := ensureMap(byHost, localCommitMessageHostKey)
	localMap[agentID] = sanitized
}
