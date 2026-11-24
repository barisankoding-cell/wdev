package siteapp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"wdev/internal/site"
)

const (
	RuntimeNodeJS = "nodejs"
	RuntimeGo     = "go"
	RuntimeRust   = "rust"
)

// ScriptCandidate menyimpan preset build & start command yang terdeteksi dari proyek.
type ScriptCandidate struct {
	Label        string `json:"label"`
	BuildCommand string `json:"build_command,omitempty"`
	StartCommand string `json:"start_command"`
}

// InspectResult merangkum hasil deteksi proyek aplikasi pada suatu direktori.
type InspectResult struct {
	WorkingDir         string            `json:"working_dir"`
	Detected           bool              `json:"detected"`
	Runtime            string            `json:"runtime,omitempty"`
	AppName            string            `json:"app_name,omitempty"`
	PackageManager     string            `json:"package_manager,omitempty"`
	Scripts            []string          `json:"scripts,omitempty"`
	Candidates         []ScriptCandidate `json:"candidates,omitempty"`
	RecommendedBuild   string            `json:"recommended_build,omitempty"`
	RecommendedStart   string            `json:"recommended_start,omitempty"`
	HasDotEnv          bool              `json:"has_dot_env"`
	DotEnvVarCount     int               `json:"dot_env_var_count"`
	DetectedPortHint   int               `json:"detected_port_hint,omitempty"`
	ToolchainInstalled bool              `json:"toolchain_installed"`
	ToolchainBinary    string            `json:"toolchain_binary,omitempty"`
}

// canonicalToolDirs mencerminkan direktori instalasi toolchain resmi wdev.
var canonicalToolDirs = []string{
	"/usr/local/bin",
	"/usr/local/go/bin",
	"/usr/local/cargo/bin",
	"/usr/local/lib/nodejs/current/bin",
	"/usr/bin",
	"/bin",
}

// FindTool mencari binary toolchain dengan memprioritaskan direktori instalasi wdev.
func FindTool(names ...string) string {
	for _, name := range names {
		for _, dir := range canonicalToolDirs {
			candidate := filepath.Join(dir, name)
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				return candidate
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// InspectDirectory memeriksa isi direktori proyek yang tervalidasi dan menyarankan
// runtime, package manager, serta perintah build/start.
func InspectDirectory(dir string) (*InspectResult, error) {
	cleanDir, err := site.ValidateRootPath(dir)
	if err != nil {
		return nil, err
	}
	return inspectDirectoryPath(cleanDir)
}

func inspectDirectoryPath(cleanDir string) (*InspectResult, error) {
	fi, err := os.Stat(cleanDir)
	if err != nil {
		return nil, fmt.Errorf("direktori tidak dapat diakses: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("path harus berupa direktori: %s", cleanDir)
	}

	res := &InspectResult{
		WorkingDir: cleanDir,
	}

	if entries, _, dotErr := ReadEnvFile(filepath.Join(cleanDir, ".env")); dotErr == nil {
		res.HasDotEnv = true
		res.DotEnvVarCount = len(entries)
		for _, e := range entries {
			if e.Key == "PORT" {
				if p, pErr := strconv.Atoi(strings.TrimSpace(e.Value)); pErr == nil && p > 0 && p <= 65535 {
					res.DetectedPortHint = p
				}
			}
		}
	}

	switch {
	case fileExists(filepath.Join(cleanDir, "package.json")):
		inspectNodeProject(cleanDir, res)
	case fileExists(filepath.Join(cleanDir, "go.mod")):
		inspectGoProject(cleanDir, res)
	case fileExists(filepath.Join(cleanDir, "Cargo.toml")):
		inspectRustProject(cleanDir, res)
	}

	return res, nil
}

func inspectNodeProject(dir string, res *InspectResult) {
	res.Detected = true
	res.Runtime = RuntimeNodeJS

	pm := detectNodePackageManager(dir)
	res.PackageManager = pm

	if bin := FindTool(pm, "node"); bin != "" {
		res.ToolchainInstalled = true
		res.ToolchainBinary = bin
	}

	var pkg struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		if jsonErr := json.Unmarshal(raw, &pkg); jsonErr == nil {
			res.AppName = strings.TrimSpace(pkg.Name)
		}
	}

	if len(pkg.Scripts) > 0 {
		keys := make([]string, 0, len(pkg.Scripts))
		for k := range pkg.Scripts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		res.Scripts = keys
	}

	hasBuild := pkg.Scripts["build"] != ""
	buildCmd := ""
	if hasBuild {
		buildCmd = fmt.Sprintf("%s install && %s run build", pm, pm)
	} else {
		buildCmd = fmt.Sprintf("%s install", pm)
	}
	res.RecommendedBuild = buildCmd

	if pkg.Scripts["start"] != "" {
		res.RecommendedStart = fmt.Sprintf("%s run start", pm)
		res.Candidates = append(res.Candidates, ScriptCandidate{
			Label:        fmt.Sprintf("Production (%s run start)", pm),
			BuildCommand: buildCmd,
			StartCommand: fmt.Sprintf("%s run start", pm),
		})
	}
	if pkg.Scripts["dev"] != "" {
		if res.RecommendedStart == "" {
			res.RecommendedStart = fmt.Sprintf("%s run dev", pm)
			res.RecommendedBuild = ""
		}
		res.Candidates = append(res.Candidates, ScriptCandidate{
			Label:        fmt.Sprintf("Development (%s run dev)", pm),
			BuildCommand: "",
			StartCommand: fmt.Sprintf("%s run dev", pm),
		})
	}
	if pkg.Scripts["preview"] != "" {
		res.Candidates = append(res.Candidates, ScriptCandidate{
			Label:        fmt.Sprintf("Preview (%s run preview)", pm),
			BuildCommand: buildCmd,
			StartCommand: fmt.Sprintf("%s run preview", pm),
		})
	}
	if res.RecommendedStart == "" {
		res.RecommendedStart = fmt.Sprintf("%s run start", pm)
		res.Candidates = append(res.Candidates, ScriptCandidate{
			Label:        fmt.Sprintf("Default (%s run start)", pm),
			BuildCommand: buildCmd,
			StartCommand: res.RecommendedStart,
		})
	}
}

func detectNodePackageManager(dir string) string {
	switch {
	case fileExists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case fileExists(filepath.Join(dir, "bun.lockb")) || fileExists(filepath.Join(dir, "bun.lock")):
		return "bun"
	case fileExists(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	case fileExists(filepath.Join(dir, "package-lock.json")):
		return "npm"
	case FindTool("pnpm") != "":
		return "pnpm"
	default:
		return "npm"
	}
}

func inspectGoProject(dir string, res *InspectResult) {
	res.Detected = true
	res.Runtime = RuntimeGo
	if bin := FindTool("go"); bin != "" {
		res.ToolchainInstalled = true
		res.ToolchainBinary = bin
	}

	if f, err := os.Open(filepath.Join(dir, "go.mod")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "module ") {
				res.AppName = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
	}

	targetPkg := "."
	if !fileExists(filepath.Join(dir, "main.go")) {
		if entries, err := os.ReadDir(filepath.Join(dir, "cmd")); err == nil {
			var cmdDirs []string
			for _, e := range entries {
				if e.IsDir() && fileExists(filepath.Join(dir, "cmd", e.Name(), "main.go")) {
					cmdDirs = append(cmdDirs, e.Name())
				}
			}
			sort.Strings(cmdDirs)
			if len(cmdDirs) > 0 {
				targetPkg = "./cmd/" + cmdDirs[0]
				for _, name := range cmdDirs {
					res.Candidates = append(res.Candidates, ScriptCandidate{
						Label:        fmt.Sprintf("Build ./cmd/%s", name),
						BuildCommand: fmt.Sprintf("go build -o ./bin/%s ./cmd/%s", name, name),
						StartCommand: fmt.Sprintf("./bin/%s", name),
					})
				}
			}
		}
	}

	res.RecommendedBuild = fmt.Sprintf("go build -o ./bin/app %s", targetPkg)
	res.RecommendedStart = "./bin/app"
	if len(res.Candidates) == 0 {
		res.Candidates = append(res.Candidates, ScriptCandidate{
			Label:        "Go Build Binary",
			BuildCommand: res.RecommendedBuild,
			StartCommand: res.RecommendedStart,
		})
	}
}

func inspectRustProject(dir string, res *InspectResult) {
	res.Detected = true
	res.Runtime = RuntimeRust
	if bin := FindTool("cargo"); bin != "" {
		res.ToolchainInstalled = true
		res.ToolchainBinary = bin
	}

	pkgName := parseCargoPackageName(filepath.Join(dir, "Cargo.toml"))
	res.AppName = pkgName
	if pkgName == "" {
		pkgName = "app"
	}

	res.RecommendedBuild = "cargo build --release"
	res.RecommendedStart = "./target/release/" + pkgName
	res.Candidates = append(res.Candidates,
		ScriptCandidate{
			Label:        "Release Binary (cargo build --release)",
			BuildCommand: res.RecommendedBuild,
			StartCommand: res.RecommendedStart,
		},
		ScriptCandidate{
			Label:        "Cargo Run (Debug)",
			BuildCommand: "",
			StartCommand: "cargo run",
		},
	)
}

func parseCargoPackageName(cargoTomlPath string) string {
	f, err := os.Open(cargoTomlPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	inPackageSection := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			inPackageSection = (section == "package")
			continue
		}
		if inPackageSection && strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "name" {
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"'`)
				return strings.TrimSpace(val)
			}
		}
	}
	return ""
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
