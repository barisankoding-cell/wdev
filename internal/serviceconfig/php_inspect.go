package serviceconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PHPInspectResult berisi informasi runtime PHP CLI, PHP-FPM, berkas INI, limit efektif, dan daftar ekstensi aktif.
type PHPInspectResult struct {
	TargetVersion      string   `json:"target_version"`
	AvailableVersions  []string `json:"available_versions"`
	CLIBinary          string   `json:"cli_binary"`
	CLIVersion         string   `json:"cli_version"`
	DefaultCLIBinary   string   `json:"default_cli_binary"`
	DefaultCLIVersion  string   `json:"default_cli_version"`
	SAPI               string   `json:"sapi"`
	ZendVersion        string   `json:"zend_version"`
	FPMBinary          string   `json:"fpm_binary"`
	FPMVersion         string   `json:"fpm_version"`
	ComposerVersion    string   `json:"composer_version"`
	LoadedINI          string   `json:"loaded_ini"`
	AdditionalINIFiles []string `json:"additional_ini_files"`
	ExtensionDir       string   `json:"extension_dir"`
	IncludePath        string   `json:"include_path"`
	MemoryLimit        string   `json:"memory_limit"`
	MaxExecutionTime   string   `json:"max_execution_time"`
	UploadMaxFilesize  string   `json:"upload_max_filesize"`
	PostMaxSize        string   `json:"post_max_size"`
	Timezone           string   `json:"timezone"`
	OpcacheEnabled     bool     `json:"opcache_enabled"`
	OpcacheCLIEnabled  bool     `json:"opcache_cli_enabled"`
	DisabledFunctions  []string `json:"disabled_functions"`
	Extensions         []string `json:"extensions"`
}

const phpInspectSnippet = `
$ext = get_loaded_extensions();
sort($ext, SORT_NATURAL | SORT_FLAG_CASE);
$scanned = php_ini_scanned_files();
$scannedList = $scanned ? array_values(array_filter(array_map('trim', explode(",", $scanned)))) : [];
$disabledRaw = trim((string)ini_get('disable_functions'));
$disabled = $disabledRaw !== '' ? array_values(array_filter(array_map('trim', explode(',', $disabledRaw)))) : [];
echo json_encode([
	'cli_version' => (string)phpversion(),
	'sapi' => (string)php_sapi_name(),
	'zend_version' => (string)zend_version(),
	'loaded_ini' => (string)(php_ini_loaded_file() ?: ''),
	'additional_ini_files' => $scannedList,
	'extension_dir' => (string)ini_get('extension_dir'),
	'include_path' => (string)ini_get('include_path'),
	'memory_limit' => (string)ini_get('memory_limit'),
	'max_execution_time' => (string)ini_get('max_execution_time'),
	'upload_max_filesize' => (string)ini_get('upload_max_filesize'),
	'post_max_size' => (string)ini_get('post_max_size'),
	'timezone' => (string)(ini_get('date.timezone') ?: date_default_timezone_get()),
	'opcache_enabled' => extension_loaded('Zend OPcache'),
	'opcache_cli_enabled' => filter_var(ini_get('opcache.enable_cli'), FILTER_VALIDATE_BOOLEAN),
	'disabled_functions' => $disabled,
	'extensions' => $ext,
], JSON_UNESCAPED_SLASHES);
`

// ParsePHPInspectJSON mem-parse keluaran JSON dari php -r snippet ke PHPInspectResult.
func ParsePHPInspectJSON(raw string) (*PHPInspectResult, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("keluaran inspeksi PHP kosong")
	}
	var out PHPInspectResult
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("gagal mem-parse info PHP: %w", err)
	}
	if out.AdditionalINIFiles == nil {
		out.AdditionalINIFiles = []string{}
	}
	if out.DisabledFunctions == nil {
		out.DisabledFunctions = []string{}
	}
	if out.Extensions == nil {
		out.Extensions = []string{}
	}
	return &out, nil
}

func findPHPCLIBinaryForVersion(ver string) string {
	ver = strings.TrimSpace(ver)
	if ver != "" && ver != "system" {
		for _, candidate := range []string{
			"/usr/bin/php" + ver,
			"/usr/local/bin/php" + ver,
		} {
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				return candidate
			}
		}
		if p, err := exec.LookPath("php" + ver); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("php"); err == nil {
		return p
	}
	return ""
}

func probeCommandLineVersion(ctx context.Context, bin string, args ...string) string {
	if bin == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func parseVersionBanner(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		for idx, f := range fields {
			if strings.EqualFold(f, "PHP") && idx+1 < len(fields) {
				return strings.TrimRight(fields[idx+1], ",")
			}
		}
	}
	return ""
}

// InspectPHP mengambil info PHP CLI, versi default sistem, FPM engine, INI efektif, serta daftar ekstensi aktif.
func (m *Manager) InspectPHP(ctx context.Context, requestedVer string) (*PHPInspectResult, error) {
	if !isValidPHPVersion(strings.TrimSpace(requestedVer)) {
		return nil, fmt.Errorf("versi PHP tidak valid: %q", requestedVer)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	avail := DetectPHPVersions()
	targetVer := strings.TrimSpace(requestedVer)
	fpmBin := ""
	if paths, err := resolvePHPPaths(targetVer); err == nil {
		targetVer = paths.version
		avail = paths.avail
		fpmBin = paths.fpmBin
	} else if len(avail) > 0 && targetVer == "" {
		targetVer = avail[0]
	}
	if avail == nil {
		avail = []string{}
	}

	cliBin := findPHPCLIBinaryForVersion(targetVer)
	if cliBin == "" {
		return nil, fmt.Errorf("biner PHP CLI tidak ditemukan di sistem")
	}

	rawJSON := probeCommandLineVersion(probeCtx, cliBin, "-r", phpInspectSnippet)
	var res *PHPInspectResult
	if rawJSON != "" {
		if parsed, err := ParsePHPInspectJSON(rawJSON); err == nil {
			res = parsed
		}
	}
	if res == nil {
		// Fallback bila json_encode tidak tersedia: pakai php -v & php -m
		res = &PHPInspectResult{
			CLIVersion:         parseVersionBanner(probeCommandLineVersion(probeCtx, cliBin, "-v")),
			AdditionalINIFiles: []string{},
			DisabledFunctions:  []string{},
			Extensions:         parsePHPModulesOutput(probeCommandLineVersion(probeCtx, cliBin, "-m")),
		}
	}

	res.TargetVersion = targetVer
	res.AvailableVersions = avail
	res.CLIBinary = cliBin
	if targetVer == "" && res.CLIVersion != "" {
		if parts := strings.Split(res.CLIVersion, "."); len(parts) >= 2 {
			res.TargetVersion = parts[0] + "." + parts[1]
		}
	}

	if defBin, err := exec.LookPath("php"); err == nil {
		if resolved, rErr := filepath.EvalSymlinks(defBin); rErr == nil {
			res.DefaultCLIBinary = resolved
		} else {
			res.DefaultCLIBinary = defBin
		}
		res.DefaultCLIVersion = probeCommandLineVersion(probeCtx, defBin, "-r", "echo PHP_VERSION;")
		if res.DefaultCLIVersion == "" {
			res.DefaultCLIVersion = parseVersionBanner(probeCommandLineVersion(probeCtx, defBin, "-v"))
		}
	}

	res.FPMBinary = fpmBin
	if fpmBin != "" {
		res.FPMVersion = parseVersionBanner(probeCommandLineVersion(probeCtx, fpmBin, "-v"))
	}

	if compBin, err := exec.LookPath("composer"); err == nil {
		if out := probeCommandLineVersion(probeCtx, compBin, "--version"); out != "" {
			fields := strings.Fields(out)
			if len(fields) >= 3 && fields[0] == "Composer" {
				res.ComposerVersion = fields[2]
			}
		}
	}

	return res, nil
}

func parsePHPModulesOutput(raw string) []string {
	var list []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "[") {
			continue
		}
		if !seen[trimmed] {
			seen[trimmed] = true
			list = append(list, trimmed)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i]) < strings.ToLower(list[j])
	})
	if list == nil {
		return []string{}
	}
	return list
}
