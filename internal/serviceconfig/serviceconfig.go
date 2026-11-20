package serviceconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrSudoRequired menandakan operasi membutuhkan password sudo dari pengguna.
var ErrSudoRequired = errors.New("akses administrator (sudo) diperlukan")

// Manager mengelola pembacaan dan penulisan konfigurasi layanan inti (Redis, PostgreSQL, PHP-FPM).
type Manager struct {
	sudoPassFn func() string
}

// New membuat instance serviceconfig.Manager.
func New(sudoPassFn func() string) *Manager {
	if sudoPassFn == nil {
		sudoPassFn = func() string { return "" }
	}
	return &Manager{sudoPassFn: sudoPassFn}
}

func (m *Manager) buildSudoCmd(ctx context.Context, stdinData, name string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, name, args...)
		if stdinData != "" {
			cmd.Stdin = strings.NewReader(stdinData)
		}
		return cmd
	}
	pass := m.sudoPassFn()
	if pass != "" {
		sudoArgs := append([]string{"-S", "-p", "", name}, args...)
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
		cmd.Stdin = strings.NewReader(pass + "\n" + stdinData)
		return cmd
	}
	sudoArgs := append([]string{"-n", name}, args...)
	cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	return cmd
}

func isSudoAuthFailure(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "a password is required") ||
		strings.Contains(lower, "incorrect password") ||
		strings.Contains(lower, "sorry, try again") ||
		strings.Contains(lower, "no tty present")
}

func (m *Manager) runPrivilegedCmd(ctx context.Context, name string, args ...string) (string, error) {
	return m.runPrivilegedCmdStdin(ctx, "", name, args...)
}

func (m *Manager) runPrivilegedCmdStdin(ctx context.Context, stdinData, name string, args ...string) (string, error) {
	cmd := m.buildSudoCmd(ctx, stdinData, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	outStr := strings.TrimSpace(buf.String())
	if err != nil {
		if isSudoAuthFailure(outStr) {
			return outStr, ErrSudoRequired
		}
		if outStr != "" {
			return outStr, fmt.Errorf("%s %s gagal: %s", name, strings.Join(args, " "), outStr)
		}
		return outStr, fmt.Errorf("%s %s gagal: %w", name, strings.Join(args, " "), err)
	}
	return outStr, nil
}

func (m *Manager) fileExistsPrivileged(ctx context.Context, path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	}
	if os.Geteuid() == 0 {
		return false, nil
	}
	_, err := m.runPrivilegedCmd(ctx, "test", "-f", path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrSudoRequired) {
		return false, ErrSudoRequired
	}
	return false, nil
}

func (m *Manager) readPrivilegedFile(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if os.IsNotExist(err) {
		return "", err
	}
	if os.Geteuid() == 0 {
		return "", err
	}
	out, sErr := m.runPrivilegedCmd(ctx, "cat", path)
	if sErr != nil {
		return "", sErr
	}
	return out, nil
}

// writePrivilegedFile menulis file sistem secara atomik melalui temp file,
// sambil mempertahankan kepemilikan (uid:gid) dan permission file asal bila sudah ada.
func (m *Manager) writePrivilegedFile(ctx context.Context, dstPath string, content []byte, defaultMode string) error {
	if defaultMode == "" {
		defaultMode = "644"
	}

	var ownerSpec, modeSpec string
	exists, err := m.fileExistsPrivileged(ctx, dstPath)
	if err != nil {
		return err
	}
	if exists {
		if statOut, sErr := m.runPrivilegedCmd(ctx, "stat", "-c", "%u:%g %a", dstPath); sErr == nil {
			parts := strings.Fields(statOut)
			if len(parts) == 2 {
				ownerSpec = parts[0]
				modeSpec = parts[1]
			}
		}
	}
	if modeSpec == "" {
		modeSpec = defaultMode
	}

	if _, err := m.runPrivilegedCmd(ctx, "mkdir", "-p", filepath.Dir(dstPath)); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "wdev-svc-cfg-*")
	if err != nil {
		return fmt.Errorf("gagal membuat file sementara: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("gagal menulis file sementara: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("gagal menutup file sementara: %w", err)
	}

	if _, err := m.runPrivilegedCmd(ctx, "cp", "-f", tmpPath, dstPath); err != nil {
		return err
	}
	if ownerSpec != "" {
		if _, err := m.runPrivilegedCmd(ctx, "chown", ownerSpec, dstPath); err != nil {
			return err
		}
	}
	if _, err := m.runPrivilegedCmd(ctx, "chmod", modeSpec, dstPath); err != nil {
		return err
	}
	return nil
}

func (m *Manager) restartService(ctx context.Context, unit string) error {
	if unit == "" {
		return fmt.Errorf("unit systemd tidak ditemukan")
	}
	_, err := m.runPrivilegedCmd(ctx, "systemctl", "restart", unit)
	return err
}

func (m *Manager) isServiceActive(ctx context.Context, unit string) bool {
	if unit == "" {
		return false
	}
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).Run() == nil
}

// Validation helpers

var memSizeRegex = regexp.MustCompile(`^(0|[1-9][0-9]*\s*(b|k|kb|m|mb|g|gb|t|tb)?)$`)
var timezoneRegex = regexp.MustCompile(`^[A-Za-z0-9_+-]+(?:/[A-Za-z0-9_+-]+)*$`)

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port harus di antara 1 sampai 65535")
	}
	return nil
}

func validateSingleLine(val, fieldName string) error {
	if strings.ContainsAny(val, "\r\n\x00") {
		return fmt.Errorf("%s tidak boleh mengandung karakter baris baru", fieldName)
	}
	return nil
}

func validateMemSize(val, fieldName string, allowZero bool) error {
	v := strings.TrimSpace(val)
	if v == "" {
		return fmt.Errorf("%s wajib diisi", fieldName)
	}
	if !allowZero && v == "0" {
		return fmt.Errorf("%s tidak boleh 0", fieldName)
	}
	if !memSizeRegex.MatchString(strings.ToLower(v)) {
		return fmt.Errorf("format %s tidak valid (contoh: 256MB, 512M, 1G)", fieldName)
	}
	return nil
}

func validateTimezone(tz string) error {
	v := strings.TrimSpace(tz)
	if v == "" {
		return fmt.Errorf("timezone wajib diisi")
	}
	if !timezoneRegex.MatchString(v) {
		return fmt.Errorf("format timezone tidak valid (contoh: Asia/Jakarta atau UTC)")
	}
	return nil
}
