package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultFlutterDir    = "/usr/local/flutter"
	defaultAndroidSDKDir = "/usr/local/android-sdk"
)

var resolveSDKUserFn = ResolveOrcaUser

type sdkDirInspectResult struct {
	WrongOwner       bool
	BadPerm          bool
	StaleTempFiles   []string
	RootPubCacheConf bool
}

func (r sdkDirInspectResult) NeedsRepair() bool {
	return r.WrongOwner || r.BadPerm || len(r.StaleTempFiles) > 0 || r.RootPubCacheConf
}

// resolveSDKOwner menentukan user non-root pemilik Flutter & Android SDK.
// Pada VPS headless yang hanya memiliki root, mengembalikan user="" dan uid=0.
func resolveSDKOwner() (username, home string, uid int) {
	uName, uHome := resolveSDKUserFn()
	uName = strings.TrimSpace(uName)
	if uName == "" || uName == "root" {
		return "", "", 0
	}
	u, err := user.Lookup(uName)
	if err != nil {
		return "", "", 0
	}
	parsedUID, err := strconv.Atoi(u.Uid)
	if err != nil || parsedUID <= 0 {
		return "", "", 0
	}
	if uHome == "" {
		uHome = u.HomeDir
	}
	return u.Username, uHome, parsedUID
}

func resolveFlutterDirFromBin(binPath string) string {
	if binPath != "" {
		if real, err := filepath.EvalSymlinks(binPath); err == nil {
			root := filepath.Dir(filepath.Dir(real))
			if dirExists(root) {
				return root
			}
		}
		root := filepath.Dir(filepath.Dir(binPath))
		if dirExists(root) {
			return root
		}
	}
	return defaultFlutterDir
}

// readFlutterVersionMetadata membaca versi Flutter & Dart secara read-only
// tanpa mengeksekusi skrip bin/flutter atau bin/dart (yang memicu update_engine_version.sh).
func readFlutterVersionMetadata(ctx context.Context, flutterDir string) (flutterVer, dartVer string) {
	if flutterDir == "" {
		return "", ""
	}

	versionJSON := filepath.Join(flutterDir, "bin", "cache", "flutter.version.json")
	if data, err := os.ReadFile(versionJSON); err == nil {
		var meta struct {
			FlutterVersion   string `json:"flutterVersion"`
			FrameworkVersion string `json:"frameworkVersion"`
			DartSDKVersion   string `json:"dartSdkVersion"`
		}
		if json.Unmarshal(data, &meta) == nil {
			flutterVer = strings.TrimSpace(meta.FlutterVersion)
			if flutterVer == "" {
				flutterVer = strings.TrimSpace(meta.FrameworkVersion)
			}
			dartVer = strings.TrimSpace(meta.DartSDKVersion)
		}
	}

	if flutterVer == "" {
		if data, err := os.ReadFile(filepath.Join(flutterDir, "version")); err == nil {
			flutterVer = strings.TrimSpace(string(data))
		}
	}

	if flutterVer == "" && dirExists(filepath.Join(flutterDir, ".git")) {
		gitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(gitCtx, "git", "-c", "safe.directory="+flutterDir, "-C", flutterDir, "describe", "--tags").Output(); err == nil {
			flutterVer = strings.TrimSpace(string(out))
		}
	}

	if dartVer == "" {
		if data, err := os.ReadFile(filepath.Join(flutterDir, "bin", "cache", "dart-sdk", "version")); err == nil {
			dartVer = strings.TrimSpace(string(data))
		}
	}

	if dartVer == "" {
		realDart := filepath.Join(flutterDir, "bin", "cache", "dart-sdk", "bin", "dart")
		if fi, err := os.Stat(realDart); err == nil && !fi.IsDir() {
			dartCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(dartCtx, realDart, "--version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				for i, f := range fields {
					if f == "version:" && i+1 < len(fields) {
						dartVer = fields[i+1]
						break
					}
				}
			}
		}
	}

	return flutterVer, dartVer
}

func staleFlutterTempFiles(cacheDir string) []string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	var stale []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "engine.stamp.tmp.") {
			continue
		}
		pidPart := strings.TrimPrefix(name, "engine.stamp.tmp.")
		if pid, err := strconv.Atoi(pidPart); err == nil && pid > 0 {
			if _, statErr := os.Stat(fmt.Sprintf("/proc/%d", pid)); statErr == nil {
				continue
			}
		}
		stale = append(stale, filepath.Join(cacheDir, name))
	}
	return stale
}

func packageConfigHasRootPubCache(pkgConfigPath string) bool {
	data, err := os.ReadFile(pkgConfigPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "file:///root/.pub-cache")
}

func isGitObjectRelPath(rel string) bool {
	slashed := filepath.ToSlash(rel)
	return strings.HasPrefix(slashed, ".git/objects/")
}

func inspectSDKDirState(dir string, wantUID int, wantHome string) sdkDirInspectResult {
	var res sdkDirInspectResult
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return res
	}

	res.StaleTempFiles = staleFlutterTempFiles(filepath.Join(dir, "bin", "cache"))
	if wantUID > 0 && wantHome != "" {
		pkgConf := filepath.Join(dir, "packages", "flutter_tools", ".dart_tool", "package_config.json")
		res.RootPubCacheConf = packageConfigHasRootPubCache(pkgConf)
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if wantUID > 0 {
			if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != wantUID {
				res.WrongOwner = true
			}
		}
		if d.Type()&os.ModeSymlink == 0 {
			perm := info.Mode().Perm()
			if (perm & 0o022) != 0 {
				res.BadPerm = true
			}
			if d.IsDir() {
				if (perm & 0o005) != 0o005 {
					res.BadPerm = true
				}
			} else if wantUID > 0 {
				rel, relErr := filepath.Rel(dir, path)
				if relErr == nil && !isGitObjectRelPath(rel) && (perm&0o200) == 0 {
					res.BadPerm = true
				}
			}
		}
		if (!res.WrongOwner && wantUID > 0) || !res.BadPerm {
			return nil
		}
		return fs.SkipAll
	})

	return res
}

func sdkOwnershipAndCacheFinding(dirs ...string) *Finding {
	wantUser, wantHome, wantUID := resolveSDKOwner()

	var paths []string
	var reasons []string
	hasStale := false
	hasRootPub := false

	for _, dir := range dirs {
		st := inspectSDKDirState(dir, wantUID, wantHome)
		if !st.NeedsRepair() {
			continue
		}
		paths = append(paths, dir)
		var parts []string
		if st.WrongOwner && wantUser != "" {
			parts = append(parts, "kepemilikan bukan "+wantUser)
		}
		if st.BadPerm {
			parts = append(parts, "izin file/direktori")
		}
		if len(st.StaleTempFiles) > 0 {
			hasStale = true
			parts = append(parts, fmt.Sprintf("%d file temp engine.stamp.tmp.*", len(st.StaleTempFiles)))
		}
		if st.RootPubCacheConf {
			hasRootPub = true
			parts = append(parts, "package_config menunjuk /root/.pub-cache")
		}
		reasons = append(reasons, fmt.Sprintf("%s (%s)", dir, strings.Join(parts, ", ")))
	}

	if len(paths) == 0 {
		return nil
	}

	detail := "izin & kepemilikan SDK perlu diselaraskan"
	if wantUser != "" {
		detail = fmt.Sprintf("SDK perlu dimiliki user %s tanpa berkas terkunci root: %s", wantUser, strings.Join(reasons, "; "))
	} else if hasStale || hasRootPub {
		detail = "cache SDK perlu dibersihkan: " + strings.Join(reasons, "; ")
	} else {
		detail = "izin SDK perlu dinormalkan: " + strings.Join(reasons, "; ")
	}

	return &Finding{
		Severity: SeverityWarning,
		Kind:     "sdk-ownership",
		Tool:     "flutter",
		Detail:   detail,
		Fixable:  true,
		Paths:    paths,
	}
}

func (inst *Installer) runAsSDKUser(ctx context.Context, cb LogCallback, targetUser, userHome, name string, args ...string) error {
	if targetUser == "" || targetUser == "root" {
		return inst.runCmd(ctx, cb, name, args...)
	}

	envArgs := []string{
		"FLUTTER_ROOT=" + defaultFlutterDir,
		"ANDROID_HOME=" + defaultAndroidSDKDir,
		"ANDROID_SDK_ROOT=" + defaultAndroidSDKDir,
		"CI=true",
	}
	if userHome != "" {
		envArgs = append(envArgs,
			"HOME="+userHome,
			"PUB_CACHE="+filepath.Join(userHome, ".pub-cache"),
		)
	}

	if os.Geteuid() != 0 {
		if cur, err := user.Current(); err == nil && cur.Username == targetUser {
			if cb != nil {
				cb(fmt.Sprintf("==> [%s] %s %s", targetUser, name, strings.Join(args, " ")))
			}
			cmd := exec.CommandContext(ctx, name, args...)
			overrides := make([][2]string, 0, len(envArgs))
			for _, kv := range envArgs {
				if k, v, ok := strings.Cut(kv, "="); ok {
					overrides = append(overrides, [2]string{k, v})
				}
			}
			cmd.Env = withEnvOverrides(os.Environ(), overrides)
			out, err := cmd.CombinedOutput()
			for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				if line != "" && cb != nil {
					cb(line)
				}
			}
			if err != nil && cb != nil {
				cb(fmt.Sprintf("[ERROR] Perintah keluar dengan kode error: %v", err))
			}
			return err
		}
	}

	runArgs := append([]string{"-u", targetUser, "--", "env"}, envArgs...)
	runArgs = append(runArgs, name)
	runArgs = append(runArgs, args...)
	return inst.runCmd(ctx, cb, "runuser", runArgs...)
}

func (inst *Installer) cleanFlutterStaleTempFiles(ctx context.Context, cb LogCallback, flutterDir string) {
	stale := staleFlutterTempFiles(filepath.Join(flutterDir, "bin", "cache"))
	if len(stale) == 0 {
		return
	}
	args := append([]string{"-f"}, stale...)
	_ = inst.runCmd(ctx, cb, "rm", args...)
}

func (inst *Installer) finalizeSDKDirOwnership(ctx context.Context, cb LogCallback, dir string) {
	if !dirExists(dir) {
		return
	}
	inst.cleanFlutterStaleTempFiles(ctx, cb, dir)
	_ = inst.runCmd(ctx, cb, "chmod", "-R", "u+rwX,go-w,a+rX", dir)
	targetUser, _, wantUID := resolveSDKOwner()
	if wantUID > 0 && targetUser != "" {
		_ = inst.runCmd(ctx, cb, "chown", "-R", targetUser+":", dir)
	}
}

func (inst *Installer) repairFlutterPackageConfig(ctx context.Context, cb LogCallback, flutterDir, targetUser, userHome string) {
	if targetUser == "" || targetUser == "root" || userHome == "" {
		return
	}
	pkgConf := filepath.Join(flutterDir, "packages", "flutter_tools", ".dart_tool", "package_config.json")
	if !packageConfigHasRootPubCache(pkgConf) {
		return
	}
	dartBin := filepath.Join(flutterDir, "bin", "cache", "dart-sdk", "bin", "dart")
	if fi, err := os.Stat(dartBin); err != nil || fi.IsDir() {
		return
	}
	toolsDir := filepath.Join(flutterDir, "packages", "flutter_tools")
	if cb != nil {
		cb("Menyelaraskan package_config flutter_tools ke pub-cache milik " + targetUser + "...")
	}
	if err := inst.runAsSDKUser(ctx, cb, targetUser, userHome, dartBin, "pub", "get", "--directory="+toolsDir, "--offline"); err != nil {
		_ = inst.runAsSDKUser(ctx, cb, targetUser, userHome, dartBin, "pub", "get", "--directory="+toolsDir)
	}
}

func (inst *Installer) repairSDKDirAndCache(ctx context.Context, cb LogCallback, dir string) {
	inst.finalizeSDKDirOwnership(ctx, cb, dir)
	targetUser, userHome, wantUID := resolveSDKOwner()
	if wantUID > 0 {
		inst.repairFlutterPackageConfig(ctx, cb, dir, targetUser, userHome)
		inst.finalizeSDKDirOwnership(ctx, cb, dir)
	}
}
