package installer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	pgBackupBaseDir      = "/var/lib/wdev-pg-backups"
	pgActiveStatePath    = "/etc/wdev/pg-active-major"
	maxRetainedPGBackups = 10
	pgHbaManagedBegin    = "# BEGIN WDEV REMOTE ACCESS"
	pgHbaManagedEnd      = "# END WDEV REMOTE ACCESS"
)

type pgPlatformKind string

const (
	pgPlatformDebian pgPlatformKind = "debian"
	pgPlatformRPM    pgPlatformKind = "rpm"
)

type pgCluster struct {
	Major       string
	BinDir      string
	DataDir     string
	ConfPath    string
	HbaPath     string
	StartConf   string
	Unit        string
	Port        int
	Active      bool
	Initialized bool
	IsDistroRPM bool
}

type pgDatabaseMeta struct {
	Name    string
	Owner   string
	Enc     string
	Collate string
	Ctype   string
}

type pgDBSetting struct {
	Role  string
	Key   string
	Value string
}

type pgSQLTransformOpts struct {
	TargetMajor   int
	SupportedGUCs map[string]bool // nil = jangan filter GUC (untuk upgrade atau versi sama)
	TargetRoles   map[string]bool // nil = jangan filter role sistem pg_*
	StripRestrict bool
}

var pgdgAlternativeTools = []string{
	"psql",
	"pg_dump",
	"pg_dumpall",
	"pg_restore",
	"createdb",
	"dropdb",
	"createuser",
	"dropuser",
	"clusterdb",
	"pg_basebackup",
	"reindexdb",
	"vacuumdb",
}

var wdevCarriedPGConfKeys = []string{
	"listen_addresses",
	"max_connections",
	"shared_buffers",
	"effective_cache_size",
	"work_mem",
	"maintenance_work_mem",
	"password_encryption",
	"timezone",
	"log_min_duration_statement",
}

// IsValidPGMajor memvalidasi versi mayor PostgreSQL (mis. "14" .. "19").
func IsValidPGMajor(v string) bool {
	m, ok := parsePGMajorInt(v)
	return ok && m >= 10 && m <= 99
}

func parsePGMajorInt(v string) (int, bool) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if s == "" || len(s) > 3 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// psqlSupportsRestrict memeriksa apakah biner psql target mendukung meta-command \restrict / \unrestrict
// (diperkenalkan pada PostgreSQL 18.0, 17.6, 16.10, 15.14, 14.19).
func psqlSupportsRestrict(versionOutput string) bool {
	fields := strings.Fields(strings.TrimSpace(versionOutput))
	if len(fields) < 3 {
		return true // konservatif: jangan hapus \restrict bila output tidak dikenali
	}
	verStr := strings.TrimPrefix(fields[2], "v")
	parts := strings.Split(verStr, ".")
	if len(parts) < 2 {
		return true
	}
	maj, err1 := strconv.Atoi(digitsPrefix(parts[0]))
	min, err2 := strconv.Atoi(digitsPrefix(parts[1]))
	if err1 != nil || err2 != nil {
		return true
	}
	switch {
	case maj >= 18:
		return true
	case maj == 17:
		return min >= 6
	case maj == 16:
		return min >= 10
	case maj == 15:
		return min >= 14
	case maj == 14:
		return min >= 19
	default:
		return false
	}
}

func digitsPrefix(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			break
		}
	}
	return b.String()
}

func quotePGIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quotePGLiteral(val string) string {
	return `'` + strings.ReplaceAll(val, `'`, `''`) + `'`
}

// parsePGCreateRoleStmt mem-parse statement tunggal "CREATE ROLE <ident>;" dari pg_dumpall.
func parsePGCreateRoleStmt(trimmedLine string) (rawIdent, roleName string, ok bool) {
	if !strings.HasPrefix(trimmedLine, "CREATE ROLE ") || !strings.HasSuffix(trimmedLine, ";") {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmedLine, "CREATE ROLE "), ";"))
	if body == "" {
		return "", "", false
	}
	if strings.HasPrefix(body, `"`) {
		if len(body) < 2 || !strings.HasSuffix(body, `"`) {
			return "", "", false
		}
		inner := body[1 : len(body)-1]
		// Pastikan tidak ada kutip ganda tak ter-escape di tengah
		for i := 0; i < len(inner); i++ {
			if inner[i] == '"' {
				if i+1 >= len(inner) || inner[i+1] != '"' {
					return "", "", false
				}
				i++
			}
		}
		return body, strings.ReplaceAll(inner, `""`, `"`), true
	}
	if strings.ContainsAny(body, " \t\r\n") {
		return "", "", false
	}
	return body, strings.ToLower(body), true
}

func isPGIdentChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '.'
}

// extractSetGUCName mengekstrak nama GUC dari baris tunggal "SET [SESSION|LOCAL] <guc> = ...;" atau "TO ...;".
func extractSetGUCName(trimmedLine string) (string, bool) {
	if len(trimmedLine) < 6 || !strings.HasSuffix(trimmedLine, ";") {
		return "", false
	}
	if !strings.EqualFold(trimmedLine[:4], "SET ") && !strings.EqualFold(trimmedLine[:4], "SET\t") {
		return "", false
	}
	rest := strings.TrimSpace(trimmedLine[4:])
	upperRest := strings.ToUpper(rest)
	if strings.HasPrefix(upperRest, "SESSION ") || strings.HasPrefix(upperRest, "SESSION\t") {
		rest = strings.TrimSpace(rest[8:])
	} else if strings.HasPrefix(upperRest, "LOCAL ") || strings.HasPrefix(upperRest, "LOCAL\t") {
		rest = strings.TrimSpace(rest[6:])
	}
	if rest == "" {
		return "", false
	}
	i := 0
	for i < len(rest) && isPGIdentChar(rest[i]) {
		i++
	}
	if i == 0 {
		return "", false
	}
	guc := strings.ToLower(rest[:i])
	after := strings.TrimSpace(rest[i:])
	if strings.HasPrefix(after, "=") {
		return guc, true
	}
	if len(after) >= 3 && strings.EqualFold(after[:2], "TO") && (after[2] == ' ' || after[2] == '\t' || after[2] == '\'') {
		return guc, true
	}
	return "", false
}

func isRestrictMetaCmd(trimmedLine string) bool {
	return strings.HasPrefix(trimmedLine, `\restrict `) ||
		trimmedLine == `\restrict` ||
		strings.HasPrefix(trimmedLine, `\unrestrict `) ||
		trimmedLine == `\unrestrict`
}

// extractGrantBuiltinRole memeriksa baris "GRANT <role> TO ..." dari pg_dumpall --globals-only.
func extractGrantBuiltinRole(trimmedLine string) (string, bool) {
	if !strings.HasPrefix(trimmedLine, "GRANT ") || !strings.HasSuffix(trimmedLine, ";") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "GRANT "))
	toIdx := strings.Index(rest, " TO ")
	if toIdx <= 0 {
		return "", false
	}
	role := strings.TrimSpace(rest[:toIdx])
	role = strings.Trim(role, `"`)
	if strings.HasPrefix(role, "pg_") {
		return role, true
	}
	return "", false
}

// normalizeGrantRoleForPre16 menyesuaikan sintaks GRANT role PG 16+ ("WITH INHERIT ..., ADMIN ..., SET ...")
// agar tetap valid saat direstore ke PostgreSQL 14 atau 15.
func normalizeGrantRoleForPre16(trimmedLine string) string {
	if !strings.HasPrefix(trimmedLine, "GRANT ") || !strings.HasSuffix(trimmedLine, ";") {
		return trimmedLine
	}
	withIdx := strings.Index(trimmedLine, " WITH ")
	if withIdx < 0 {
		return trimmedLine
	}
	afterWith := trimmedLine[withIdx+6:]
	if !strings.Contains(afterWith, "INHERIT ") && !strings.Contains(afterWith, "SET ") {
		return trimmedLine
	}
	adminTrue := strings.Contains(afterWith, "ADMIN TRUE")
	grantedIdx := strings.Index(afterWith, " GRANTED BY ")
	suffix := ";"
	if grantedIdx >= 0 {
		suffix = afterWith[grantedIdx:]
	}
	prefix := trimmedLine[:withIdx]
	if adminTrue {
		return prefix + " WITH ADMIN OPTION" + suffix
	}
	return prefix + suffix
}

func shouldSkipGUC(trimmedLine string, supportedGUCs map[string]bool) bool {
	if supportedGUCs == nil {
		return false
	}
	guc, ok := extractSetGUCName(trimmedLine)
	if !ok {
		return false
	}
	// Biarkan GUC custom ber-prefix ekstensi (mengandung titik, mis. "myapp.setting")
	if strings.Contains(guc, ".") {
		return false
	}
	return !supportedGUCs[guc]
}

// transformGlobalsSQL memproses hasil pg_dumpall --globals-only agar idempoten
// terhadap role yang sudah ada (mis. postgres) dan kompatibel saat downgrade.
func transformGlobalsSQL(r io.Reader, w io.Writer, opts pgSQLTransformOpts) error {
	reader := bufio.NewReaderSize(r, 256*1024)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			hasNL := strings.HasSuffix(line, "\n")
			noNL := strings.TrimRight(line, "\r\n")
			trimmed := strings.TrimSpace(noNL)

			outLine := noNL
			skip := false

			switch {
			case opts.StripRestrict && isRestrictMetaCmd(trimmed):
				outLine = "-- [wdev] stripped restrict meta-command"
			case shouldSkipGUC(trimmed, opts.SupportedGUCs):
				outLine = "-- [wdev downgrade skipped unsupported GUC] " + noNL
			default:
				if rawIdent, roleName, ok := parsePGCreateRoleStmt(trimmed); ok {
					outLine = fmt.Sprintf(
						"DO $wdev_role$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN CREATE ROLE %s; END IF; END $wdev_role$;",
						quotePGLiteral(roleName), rawIdent,
					)
				} else if opts.TargetRoles != nil {
					if builtInRole, ok := extractGrantBuiltinRole(trimmed); ok && !opts.TargetRoles[builtInRole] {
						outLine = "-- [wdev downgrade skipped unknown built-in role] " + noNL
						skip = true
					}
				}
				if !skip && opts.TargetMajor > 0 && opts.TargetMajor < 16 {
					outLine = normalizeGrantRoleForPre16(outLine)
				}
			}

			if hasNL {
				outLine += "\n"
			}
			if _, wErr := io.WriteString(w, outLine); wErr != nil {
				return wErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	return nil
}

// updateDollarQuoteState memperbarui status tag dollar-quote PostgreSQL ($$ atau $tag$)
// di luar string literal tunggal dan komentar baris --.
func updateDollarQuoteState(line string, currentTag string) string {
	inSingleQuote := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if currentTag != "" {
			if ch == '$' && strings.HasPrefix(line[i:], currentTag) {
				i += len(currentTag) - 1
				currentTag = ""
			}
			continue
		}
		if inSingleQuote {
			if ch == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}
		if ch == '-' && i+1 < len(line) && line[i+1] == '-' {
			break
		}
		if ch == '\'' {
			inSingleQuote = true
			continue
		}
		if ch == '$' {
			if tag, ok := parseDollarTagAt(line[i:]); ok {
				currentTag = tag
				i += len(tag) - 1
			}
		}
	}
	return currentTag
}

func parseDollarTagAt(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	if s[1] == '$' {
		return "$$", true
	}
	if !((s[1] >= 'a' && s[1] <= 'z') || (s[1] >= 'A' && s[1] <= 'Z') || s[1] == '_') {
		return "", false
	}
	for j := 2; j < len(s); j++ {
		c := s[j]
		if c == '$' {
			return s[:j+1], true
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return "", false
		}
	}
	return "", false
}

// transformDatabaseSQL menyaring baris SQL level-atas yang tidak kompatibel saat downgrade
// (seperti SET transaction_timeout pada PG <= 16) secara streaming tanpa memuat seluruh file ke memori
// dan tanpa menyentuh blok COPY FROM stdin maupun isi dollar-quoted function.
func transformDatabaseSQL(r io.Reader, w io.Writer, opts pgSQLTransformOpts) error {
	reader := bufio.NewReaderSize(r, 256*1024)
	writer := bufio.NewWriterSize(w, 256*1024)
	inCopy := false
	dollarTag := ""

	for {
		lineBytes, err := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			if inCopy {
				if _, wErr := writer.Write(lineBytes); wErr != nil {
					return wErr
				}
				trimmed := strings.TrimRight(string(lineBytes), "\r\n")
				if trimmed == `\.` {
					inCopy = false
				}
			} else {
				lineStr := string(lineBytes)
				hasNL := strings.HasSuffix(lineStr, "\n")
				noNL := strings.TrimRight(lineStr, "\r\n")
				trimmed := strings.TrimSpace(noNL)

				if dollarTag == "" && len(noNL) > 0 && noNL[0] != ' ' && noNL[0] != '\t' {
					if strings.HasPrefix(trimmed, "COPY ") && strings.HasSuffix(trimmed, " FROM stdin;") {
						inCopy = true
						if _, wErr := writer.Write(lineBytes); wErr != nil {
							return wErr
						}
						goto nextRead
					}
					if opts.StripRestrict && isRestrictMetaCmd(trimmed) {
						out := "-- [wdev] stripped restrict meta-command"
						if hasNL {
							out += "\n"
						}
						if _, wErr := writer.WriteString(out); wErr != nil {
							return wErr
						}
						goto nextRead
					}
					if shouldSkipGUC(trimmed, opts.SupportedGUCs) {
						out := "-- [wdev downgrade skipped unsupported GUC] " + noNL
						if hasNL {
							out += "\n"
						}
						if _, wErr := writer.WriteString(out); wErr != nil {
							return wErr
						}
						goto nextRead
					}
				}

				dollarTag = updateDollarQuoteState(noNL, dollarTag)
				if _, wErr := writer.Write(lineBytes); wErr != nil {
					return wErr
				}
			}
		}
	nextRead:
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	return writer.Flush()
}

// setPGConfPortContent memperbarui atau menyisipkan baris `port = <N>` pada postgresql.conf.
func setPGConfPortContent(content string, port int) string {
	hadTrailingNL := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" && content == "" {
		lines = nil
	}
	wanted := fmt.Sprintf("port = %d", port)
	activeIdx := -1
	commentIdx := -1
	out := make([]string, 0, len(lines)+1)

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			uncommented := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if k, _, ok := strings.Cut(uncommented, "="); ok && strings.TrimSpace(k) == "port" && commentIdx < 0 {
				commentIdx = len(out)
			}
			out = append(out, l)
			continue
		}
		if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == "port" {
			if activeIdx >= 0 {
				continue
			}
			activeIdx = len(out)
			out = append(out, wanted)
			continue
		}
		out = append(out, l)
	}

	if activeIdx < 0 {
		if commentIdx >= 0 {
			withInsert := make([]string, 0, len(out)+1)
			withInsert = append(withInsert, out[:commentIdx+1]...)
			withInsert = append(withInsert, wanted)
			withInsert = append(withInsert, out[commentIdx+1:]...)
			out = withInsert
		} else {
			out = append(out, wanted)
		}
	}

	res := strings.Join(out, "\n")
	if hadTrailingNL || res != "" {
		res += "\n"
	}
	return res
}

func parsePGConfActiveKey(content, key string) string {
	var val string
	for _, l := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		val = stripPGConfInlineComment(v)
	}
	return val
}

func stripPGConfInlineComment(raw string) string {
	inQuotes := false
	var sb strings.Builder
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch == '\'' {
			inQuotes = !inQuotes
			sb.WriteByte(ch)
			continue
		}
		if ch == '#' && !inQuotes {
			break
		}
		sb.WriteByte(ch)
	}
	return strings.TrimSpace(sb.String())
}

func setPGConfRawKey(content, key, rawVal string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	wanted := fmt.Sprintf("%s = %s", key, rawVal)
	activeIdx := -1
	commentIdx := -1
	out := make([]string, 0, len(lines)+1)

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			uncommented := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if k, _, ok := strings.Cut(uncommented, "="); ok && strings.TrimSpace(k) == key && commentIdx < 0 {
				commentIdx = len(out)
			}
			out = append(out, l)
			continue
		}
		if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
			if activeIdx >= 0 {
				continue
			}
			activeIdx = len(out)
			out = append(out, wanted)
			continue
		}
		out = append(out, l)
	}

	if activeIdx < 0 {
		if commentIdx >= 0 {
			withInsert := make([]string, 0, len(out)+1)
			withInsert = append(withInsert, out[:commentIdx+1]...)
			withInsert = append(withInsert, wanted)
			withInsert = append(withInsert, out[commentIdx+1:]...)
			out = withInsert
		} else {
			out = append(out, wanted)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func extractWdevPGHbaBlock(hba string) []string {
	var block []string
	inBlock := false
	for _, l := range strings.Split(hba, "\n") {
		t := strings.TrimSpace(l)
		if t == pgHbaManagedBegin {
			inBlock = true
			block = []string{pgHbaManagedBegin}
			continue
		}
		if inBlock {
			block = append(block, l)
			if t == pgHbaManagedEnd {
				return block
			}
		}
	}
	return nil
}

func applyWdevPGHbaBlock(targetHba string, block []string) string {
	if len(block) == 0 {
		return targetHba
	}
	lines := strings.Split(strings.TrimRight(targetHba, "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	inBlock := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == pgHbaManagedBegin {
			inBlock = true
			continue
		}
		if t == pgHbaManagedEnd {
			inBlock = false
			continue
		}
		if !inBlock {
			cleaned = append(cleaned, l)
		}
	}
	for len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	if len(cleaned) > 0 {
		cleaned = append(cleaned, "")
	}
	cleaned = append(cleaned, block...)
	return strings.Join(cleaned, "\n") + "\n"
}

// applyWdevPGConfigOverlay menyalin parameter aman (jaringan/tuning/auth) dan blok pg_hba
// terkelola wdev dari cluster sumber ke cluster target yang baru diinisialisasi, tanpa menyalin port/data_directory.
func applyWdevPGConfigOverlay(srcConf, srcHba, dstConf, dstHba string) (newConf, newHba string) {
	newConf = dstConf
	for _, key := range wdevCarriedPGConfKeys {
		if val := parsePGConfActiveKey(srcConf, key); val != "" {
			newConf = setPGConfRawKey(newConf, key, val)
		}
	}
	newHba = applyWdevPGHbaBlock(dstHba, extractWdevPGHbaBlock(srcHba))
	return newConf, newHba
}

func (inst *Installer) detectPGPlatform() pgPlatformKind {
	if _, err := exec.LookPath("pg_createcluster"); err == nil {
		return pgPlatformDebian
	}
	if inst.isDebian {
		return pgPlatformDebian
	}
	return pgPlatformRPM
}

func (inst *Installer) privilegedReadFile(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if os.IsNotExist(err) || os.Geteuid() == 0 {
		return "", err
	}
	return inst.runQuietSudoOutput(ctx, "cat", path)
}

func (inst *Installer) writePostgresSystemFile(ctx context.Context, cb LogCallback, dstPath string, content []byte, mode string) error {
	if err := inst.writeSystemFile(ctx, cb, dstPath, content, mode); err != nil {
		return err
	}
	_ = inst.runCmd(ctx, cb, "chown", "postgres:postgres", dstPath)
	if _, err := exec.LookPath("restorecon"); err == nil {
		_ = inst.runCmd(ctx, cb, "restorecon", dstPath)
	}
	return nil
}

func (inst *Installer) readPGConfPort(ctx context.Context, confPath string) int {
	raw, err := inst.privilegedReadFile(ctx, confPath)
	if err != nil {
		return 5432
	}
	val := strings.Trim(parsePGConfActiveKey(raw, "port"), "'")
	if p, pErr := strconv.Atoi(val); pErr == nil && p > 0 && p <= 65535 {
		return p
	}
	return 5432
}

func (inst *Installer) setPGConfPort(ctx context.Context, cb LogCallback, confPath string, port int) error {
	raw, err := inst.privilegedReadFile(ctx, confPath)
	if err != nil {
		return fmt.Errorf("gagal membaca %s: %w", confPath, err)
	}
	updated := setPGConfPortContent(raw, port)
	if updated == raw {
		return nil
	}
	return inst.writePostgresSystemFile(ctx, cb, confPath, []byte(updated), "644")
}

func (inst *Installer) setDebianStartConf(ctx context.Context, cb LogCallback, startConfPath, mode string) {
	raw, err := inst.privilegedReadFile(ctx, startConfPath)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	replaced := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if t == "auto" || t == "manual" || t == "disabled" {
			lines[i] = mode
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, mode)
	}
	updated := strings.Join(lines, "\n") + "\n"
	if updated != raw {
		_ = inst.writePostgresSystemFile(ctx, cb, startConfPath, []byte(updated), "644")
	}
}

func inactiveDebianPortForMajor(ver string) int {
	m, ok := parsePGMajorInt(ver)
	if !ok {
		return 5433
	}
	p := 5400 + m
	if p == 5432 {
		return 5433
	}
	return p
}

func (inst *Installer) resolvePGCluster(ctx context.Context, platform pgPlatformKind, ver string) pgCluster {
	if platform == pgPlatformDebian {
		c := pgCluster{
			Major:     ver,
			BinDir:    fmt.Sprintf("/usr/lib/postgresql/%s/bin", ver),
			DataDir:   fmt.Sprintf("/var/lib/postgresql/%s/main", ver),
			ConfPath:  fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", ver),
			HbaPath:   fmt.Sprintf("/etc/postgresql/%s/main/pg_hba.conf", ver),
			StartConf: fmt.Sprintf("/etc/postgresql/%s/main/start.conf", ver),
			Unit:      fmt.Sprintf("postgresql@%s-main", ver),
			Port:      5432,
		}
		if out, err := exec.CommandContext(ctx, "pg_lsclusters", "-h").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 6 && fields[0] == ver && fields[1] == "main" {
					if p, pErr := strconv.Atoi(fields[2]); pErr == nil && p > 0 {
						c.Port = p
					}
					c.Active = fields[3] == "online"
					c.DataDir = fields[5]
					c.Initialized = true
					break
				}
			}
		}
		if !c.Active && exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", c.Unit).Run() == nil {
			c.Active = true
		}
		if !c.Initialized {
			c.Initialized = inst.privilegedFileExists(ctx, filepath.Join(c.DataDir, "PG_VERSION")) &&
				inst.privilegedFileExists(ctx, c.ConfPath)
		}
		if c.Initialized && c.Port == 5432 {
			c.Port = inst.readPGConfPort(ctx, c.ConfPath)
		}
		return c
	}

	// RPM PGDG layout
	pgdgDataDir := fmt.Sprintf("/var/lib/pgsql/%s/data", ver)
	pgdgMarker := filepath.Join(pgdgDataDir, "PG_VERSION")
	pgdgBinDir := fmt.Sprintf("/usr/pgsql-%s/bin", ver)
	pgdgBin := filepath.Join(pgdgBinDir, "postgres")
	_, hasPGDGBin := os.Stat(pgdgBin)
	hasPGDGData := inst.privilegedFileExists(ctx, pgdgMarker)

	if hasPGDGBin == nil || hasPGDGData {
		c := pgCluster{
			Major:       ver,
			BinDir:      pgdgBinDir,
			DataDir:     pgdgDataDir,
			ConfPath:    filepath.Join(pgdgDataDir, "postgresql.conf"),
			HbaPath:     filepath.Join(pgdgDataDir, "pg_hba.conf"),
			Unit:        "postgresql-" + ver,
			Port:        5432,
			Active:      exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "postgresql-"+ver).Run() == nil,
			Initialized: hasPGDGData,
		}
		if c.Initialized {
			c.Port = inst.readPGConfPort(ctx, c.ConfPath)
		}
		return c
	}

	// RPM distro default layout (/var/lib/pgsql/data + postgresql.service)
	legacyDataDir := "/var/lib/pgsql/data"
	legacyVer := inst.privilegedReadFileTrimmed(ctx, filepath.Join(legacyDataDir, "PG_VERSION"))
	if legacyVer == ver {
		c := pgCluster{
			Major:       ver,
			BinDir:      "/usr/bin",
			DataDir:     legacyDataDir,
			ConfPath:    filepath.Join(legacyDataDir, "postgresql.conf"),
			HbaPath:     filepath.Join(legacyDataDir, "pg_hba.conf"),
			Unit:        "postgresql",
			Port:        5432,
			Active:      exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "postgresql").Run() == nil,
			Initialized: true,
			IsDistroRPM: true,
		}
		c.Port = inst.readPGConfPort(ctx, c.ConfPath)
		return c
	}

	return pgCluster{
		Major:    ver,
		BinDir:   pgdgBinDir,
		DataDir:  pgdgDataDir,
		ConfPath: filepath.Join(pgdgDataDir, "postgresql.conf"),
		HbaPath:  filepath.Join(pgdgDataDir, "pg_hba.conf"),
		Unit:     "postgresql-" + ver,
		Port:     5432,
	}
}

func (inst *Installer) listPGClusters(ctx context.Context, platform pgPlatformKind) []pgCluster {
	seen := make(map[string]bool)
	var majors []string
	addMajor := func(v string) {
		v = strings.TrimSpace(v)
		if IsValidPGMajor(v) && !seen[v] {
			seen[v] = true
			majors = append(majors, v)
		}
	}

	for _, v := range supportedPGMajors {
		addMajor(v)
	}
	if platform == pgPlatformDebian {
		if entries, err := os.ReadDir("/etc/postgresql"); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					addMajor(e.Name())
				}
			}
		}
	} else {
		if entries, err := os.ReadDir("/usr"); err == nil {
			for _, e := range entries {
				if e.IsDir() && strings.HasPrefix(e.Name(), "pgsql-") {
					addMajor(strings.TrimPrefix(e.Name(), "pgsql-"))
				}
			}
		}
		if legacyVer := inst.privilegedReadFileTrimmed(ctx, "/var/lib/pgsql/data/PG_VERSION"); legacyVer != "" {
			addMajor(legacyVer)
		}
	}

	// Urutkan versi mayor dari besar ke kecil agar fallback deterministik
	sort.Slice(majors, func(i, j int) bool {
		mi, _ := parsePGMajorInt(majors[i])
		mj, _ := parsePGMajorInt(majors[j])
		return mi > mj
	})

	clusters := make([]pgCluster, 0, len(majors))
	for _, m := range majors {
		c := inst.resolvePGCluster(ctx, platform, m)
		if c.Initialized || c.Active {
			clusters = append(clusters, c)
		}
	}
	return clusters
}

// pickSourcePGCluster memilih cluster sumber paling otoritatif untuk dimigrasikan ke targetVer.
func pickSourcePGCluster(clusters []pgCluster, targetVer, stateActiveMajor string, isEnabledFn func(string) bool) *pgCluster {
	var candidates []pgCluster
	for _, c := range clusters {
		if c.Major == targetVer {
			continue
		}
		if !c.Initialized && !c.Active {
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return nil
	}

	// 1. Cluster aktif di port 5432
	for i := range candidates {
		if candidates[i].Active && candidates[i].Port == 5432 {
			return &candidates[i]
		}
	}
	// 2. Cluster aktif mana pun
	for i := range candidates {
		if candidates[i].Active {
			return &candidates[i]
		}
	}
	// 3. State terakhir yang dicatat wdev
	if stateActiveMajor != "" {
		for i := range candidates {
			if candidates[i].Major == stateActiveMajor && candidates[i].Initialized {
				return &candidates[i]
			}
		}
	}
	// 4. Unit systemd yang enabled
	if isEnabledFn != nil {
		for i := range candidates {
			if candidates[i].Initialized && isEnabledFn(candidates[i].Unit) {
				return &candidates[i]
			}
		}
	}
	// 5. Cluster yang dikonfigurasi pada port 5432
	for i := range candidates {
		if candidates[i].Initialized && candidates[i].Port == 5432 {
			return &candidates[i]
		}
	}
	// 6. Cluster terinisialisasi pertama (terurut mayor tertinggi)
	for i := range candidates {
		if candidates[i].Initialized {
			return &candidates[i]
		}
	}
	return &candidates[0]
}

func (inst *Installer) readPGActiveState() string {
	data, err := os.ReadFile(pgActiveStatePath)
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(data))
	if IsValidPGMajor(v) {
		return v
	}
	return ""
}

func (inst *Installer) writePGActiveState(ctx context.Context, cb LogCallback, major string) {
	if !IsValidPGMajor(major) {
		return
	}
	_ = inst.writeSystemFile(ctx, cb, pgActiveStatePath, []byte(major+"\n"), "644")
}

func (inst *Installer) setPGDGAlternatives(ctx context.Context, cb LogCallback, ver string) {
	for _, tool := range pgdgAlternativeTools {
		bin := fmt.Sprintf("/usr/pgsql-%s/bin/%s", ver, tool)
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		altName := "pgsql-" + tool
		if exec.CommandContext(ctx, "update-alternatives", "--display", altName).Run() == nil {
			_ = inst.runCmd(ctx, cb, "update-alternatives", "--set", altName, bin)
		}
	}
}

func (inst *Installer) waitPostgresReadyPort(ctx context.Context, ver, binDir string, port int) bool {
	if port <= 0 {
		port = 5432
	}
	bin := filepath.Join(binDir, "pg_isready")
	if _, err := os.Stat(bin); err != nil {
		bin = fmt.Sprintf("/usr/pgsql-%s/bin/pg_isready", ver)
		if _, err := os.Stat(bin); err != nil {
			bin = fmt.Sprintf("/usr/lib/postgresql/%s/bin/pg_isready", ver)
			if _, err := os.Stat(bin); err != nil {
				bin = "pg_isready"
			}
		}
	}
	portStr := strconv.Itoa(port)
	for i := 0; i < 15; i++ {
		if exec.CommandContext(ctx, bin, "-p", portStr).Run() == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return exec.CommandContext(ctx, bin, "-p", portStr).Run() == nil
}

func (inst *Installer) queryAsPostgres(ctx context.Context, binDir string, port int, dbName, sqlQuery string) (string, error) {
	if port <= 0 {
		port = 5432
	}
	if dbName == "" {
		dbName = "postgres"
	}
	psqlBin := filepath.Join(binDir, "psql")
	if _, err := os.Stat(psqlBin); err != nil {
		psqlBin = "psql"
	}
	return inst.runQuietSudoOutput(
		ctx,
		"runuser", "-u", "postgres", "--",
		psqlBin, "-p", strconv.Itoa(port), "-d", dbName, "-At", "-F", "\t", "-v", "ON_ERROR_STOP=1", "-c", sqlQuery,
	)
}

func (inst *Installer) execAsPostgresFile(ctx context.Context, cb LogCallback, binDir string, port int, dbName, sqlFile string) error {
	if port <= 0 {
		port = 5432
	}
	if dbName == "" {
		dbName = "postgres"
	}
	psqlBin := filepath.Join(binDir, "psql")
	if _, err := os.Stat(psqlBin); err != nil {
		psqlBin = "psql"
	}
	return inst.runCmd(
		ctx, cb,
		"runuser", "-u", "postgres", "--",
		psqlBin, "-p", strconv.Itoa(port), "-d", dbName, "-v", "ON_ERROR_STOP=1", "-f", sqlFile,
	)
}

func (inst *Installer) dumpGlobalsAsPostgres(ctx context.Context, cb LogCallback, binDir string, port int, outFile string) error {
	bin := filepath.Join(binDir, "pg_dumpall")
	return inst.runCmd(
		ctx, cb,
		"runuser", "-u", "postgres", "--",
		bin, "-p", strconv.Itoa(port), "--globals-only", "-f", outFile,
	)
}

func (inst *Installer) dumpAllAsPostgres(ctx context.Context, cb LogCallback, binDir string, port int, outFile string) error {
	bin := filepath.Join(binDir, "pg_dumpall")
	return inst.runCmd(
		ctx, cb,
		"runuser", "-u", "postgres", "--",
		bin, "-p", strconv.Itoa(port), "--clean", "--if-exists", "-f", outFile,
	)
}

func (inst *Installer) dumpDBAsPostgres(ctx context.Context, cb LogCallback, binDir string, port int, dbName, outFile string, clean bool) error {
	bin := filepath.Join(binDir, "pg_dump")
	args := []string{"-u", "postgres", "--", bin, "-p", strconv.Itoa(port)}
	if clean {
		args = append(args, "--clean", "--if-exists")
	}
	args = append(args, "--dbname="+dbName, "-f", outFile)
	return inst.runCmd(ctx, cb, "runuser", args...)
}

func (inst *Installer) verifyNonEmptyFilePrivileged(ctx context.Context, path string) error {
	if fi, err := os.Stat(path); err == nil {
		if fi.Size() == 0 {
			return fmt.Errorf("berkas dump %s kosong (0 byte)", path)
		}
		return nil
	}
	out, err := inst.runQuietSudoOutput(ctx, "stat", "-c", "%s", path)
	if err != nil {
		return fmt.Errorf("berkas dump %s tidak ditemukan: %w", path, err)
	}
	sz, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil || sz <= 0 {
		return fmt.Errorf("berkas dump %s tidak valid atau kosong (%q)", path, strings.TrimSpace(out))
	}
	return nil
}

func (inst *Installer) queryPGDatabases(ctx context.Context, binDir string, port int) ([]pgDatabaseMeta, error) {
	q := `SELECT d.datname, pg_get_userbyid(d.datdba), pg_encoding_to_char(d.encoding), d.datcollate, d.datctype ` +
		`FROM pg_database d WHERE d.datistemplate = false ORDER BY (d.datname = 'postgres') DESC, d.datname ASC;`
	out, err := inst.queryAsPostgres(ctx, binDir, port, "postgres", q)
	if err != nil {
		return nil, err
	}
	var res []pgDatabaseMeta
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) >= 5 {
			res = append(res, pgDatabaseMeta{
				Name:    cols[0],
				Owner:   cols[1],
				Enc:     cols[2],
				Collate: cols[3],
				Ctype:   cols[4],
			})
		}
	}
	return res, nil
}

func (inst *Installer) queryPGPostgresHasUserObjects(ctx context.Context, binDir string, port int) (bool, error) {
	q := `SELECT (` +
		`(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace ` +
		`WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_%' ` +
		`AND c.relkind IN ('r','v','m','S','f','p')) + ` +
		`(SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace ` +
		`WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_%' ` +
		`AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')) + ` +
		`(SELECT count(*) FROM pg_extension WHERE extname <> 'plpgsql') + ` +
		`(SELECT count(*) FROM pg_namespace WHERE nspname NOT IN ('public', 'pg_catalog', 'information_schema') AND nspname NOT LIKE 'pg_%')` +
		`);`
	out, err := inst.queryAsPostgres(ctx, binDir, port, "postgres", q)
	if err != nil {
		return false, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n > 0, nil
}

func (inst *Installer) queryPGCustomRoleCount(ctx context.Context, binDir string, port int) int {
	out, err := inst.queryAsPostgres(ctx, binDir, port, "postgres",
		`SELECT count(*) FROM pg_roles WHERE rolname NOT LIKE 'pg_%' AND rolname <> 'postgres';`)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

func (inst *Installer) queryPGSupportedGUCs(ctx context.Context, binDir string, port int) (map[string]bool, error) {
	out, err := inst.queryAsPostgres(ctx, binDir, port, "postgres", `SELECT lower(name) FROM pg_settings;`)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			m[name] = true
		}
	}
	return m, nil
}

func (inst *Installer) queryPGRoles(ctx context.Context, binDir string, port int) (map[string]bool, error) {
	out, err := inst.queryAsPostgres(ctx, binDir, port, "postgres", `SELECT rolname FROM pg_roles;`)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			m[name] = true
		}
	}
	return m, nil
}

func (inst *Installer) queryPGDBSettings(ctx context.Context, binDir string, port int, dbName string) []pgDBSetting {
	q := fmt.Sprintf(
		`SELECT COALESCE(r.rolname, ''), cfg FROM pg_db_role_setting s `+
			`JOIN pg_database d ON d.oid = s.setdatabase `+
			`LEFT JOIN pg_roles r ON r.oid = s.setrole, `+
			`LATERAL unnest(s.setconfig) AS cfg WHERE d.datname = %s;`,
		quotePGLiteral(dbName),
	)
	out, err := inst.queryAsPostgres(ctx, binDir, port, "postgres", q)
	if err != nil {
		return nil
	}
	var res []pgDBSetting
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.SplitN(line, "\t", 2)
		if len(cols) != 2 {
			continue
		}
		k, v, ok := strings.Cut(cols[1], "=")
		if !ok {
			continue
		}
		res = append(res, pgDBSetting{
			Role:  cols[0],
			Key:   strings.ToLower(strings.TrimSpace(k)),
			Value: v,
		})
	}
	return res
}

// transformPrivilegedDumpFile menjalankan transformFn secara streaming pada berkas dump yang dimiliki postgres:postgres.
func (inst *Installer) transformPrivilegedDumpFile(ctx context.Context, cb LogCallback, srcPath, dstPath string, transformFn func(io.Reader, io.Writer) error) error {
	if os.Geteuid() == 0 {
		in, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		if tErr := transformFn(in, out); tErr != nil {
			out.Close()
			_ = os.Remove(dstPath)
			return tErr
		}
		if cErr := out.Close(); cErr != nil {
			_ = os.Remove(dstPath)
			return cErr
		}
		return inst.runCmd(ctx, cb, "chown", "postgres:postgres", dstPath)
	}

	stageDir, err := os.MkdirTemp("/var/tmp", "wdev-pg-stage-*")
	if err != nil {
		stageDir, err = os.MkdirTemp("", "wdev-pg-stage-*")
		if err != nil {
			return err
		}
	}
	defer os.RemoveAll(stageDir)
	_ = os.Chmod(stageDir, 0700)

	stageOutPath := filepath.Join(stageDir, "transformed.sql")
	stageOut, err := os.OpenFile(stageOutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	pass := inst.GetSudoPassword()
	var cmd *exec.Cmd
	if pass != "" {
		cmd = exec.CommandContext(ctx, "sudo", "-S", "-p", "", "cat", srcPath)
		cmd.Stdin = strings.NewReader(pass + "\n")
	} else {
		cmd = exec.CommandContext(ctx, "sudo", "-n", "cat", srcPath)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stageOut.Close()
		return err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		stageOut.Close()
		return err
	}

	tErr := transformFn(stdout, stageOut)
	cErr := stageOut.Close()
	wErr := cmd.Wait()
	if tErr != nil {
		return tErr
	}
	if cErr != nil {
		return cErr
	}
	if wErr != nil {
		return fmt.Errorf("gagal membaca %s: %v (%s)", srcPath, wErr, strings.TrimSpace(errBuf.String()))
	}

	if err := inst.runCmd(ctx, cb, "cp", "-f", stageOutPath, dstPath); err != nil {
		return err
	}
	if err := inst.runCmd(ctx, cb, "chown", "postgres:postgres", dstPath); err != nil {
		return err
	}
	return inst.runCmd(ctx, cb, "chmod", "600", dstPath)
}

func (inst *Installer) installTargetPGPackagesAndInit(ctx context.Context, cb LogCallback, platform pgPlatformKind, ver string) error {
	if platform == pgPlatformDebian {
		binPath := fmt.Sprintf("/usr/lib/postgresql/%s/bin/postgres", ver)
		if _, err := os.Stat(binPath); os.IsNotExist(err) {
			cb(fmt.Sprintf("Memasang paket postgresql-%s...", ver))
			_ = inst.ensurePGDGRepo(ctx, cb)
			if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", "postgresql-"+ver); err != nil {
				return fmt.Errorf("gagal memasang postgresql-%s: %w", ver, err)
			}
		}
		confPath := fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", ver)
		if !inst.privilegedFileExists(ctx, confPath) {
			cb(fmt.Sprintf("Menginisialisasi cluster Debian PostgreSQL %s...", ver))
			if err := inst.runCmd(ctx, cb, "pg_createcluster", ver, "main"); err != nil {
				return fmt.Errorf("pg_createcluster %s main gagal: %w", ver, err)
			}
		}
		return nil
	}

	// RPM PGDG
	_ = inst.ensurePGDGRepo(ctx, cb)
	pkgServer := fmt.Sprintf("postgresql%s-server", ver)
	pkgClient := fmt.Sprintf("postgresql%s", ver)
	if err := inst.installOrUpgradeRPM(ctx, cb, pkgServer, pkgClient); err != nil {
		return fmt.Errorf("gagal memasang/memperbarui %s: %w", pkgServer, err)
	}
	if err := inst.EnsureSystemLibpqPriority(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Penyelarasan prioritas libpq sistem: %v", err))
	}

	pgBin := fmt.Sprintf("/usr/pgsql-%s/bin/postgres", ver)
	if _, err := os.Stat(pgBin); err != nil {
		return fmt.Errorf("biner PostgreSQL %s tidak ditemukan di %s setelah instalasi", ver, pgBin)
	}

	pgDataMarker := fmt.Sprintf("/var/lib/pgsql/%s/data/PG_VERSION", ver)
	if !inst.privilegedFileExists(ctx, pgDataMarker) {
		pgdgSetup := fmt.Sprintf("/usr/pgsql-%s/bin/postgresql-%s-setup", ver, ver)
		cb(fmt.Sprintf("Menginisialisasi cluster data baru PostgreSQL %s...", ver))
		if err := inst.runCmd(ctx, cb, pgdgSetup, "initdb"); err != nil {
			return fmt.Errorf("inisialisasi cluster PostgreSQL %s gagal: %w", ver, err)
		}
	}
	return nil
}

func (inst *Installer) stopDisableUnitIfPresent(ctx context.Context, cb LogCallback, unit string) {
	if exec.CommandContext(ctx, "systemctl", "cat", unit).Run() != nil {
		return
	}
	if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).Run() == nil {
		_ = inst.runCmd(ctx, cb, "systemctl", "stop", unit)
	}
	if exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", unit).Run() == nil {
		_ = inst.runCmd(ctx, cb, "systemctl", "disable", unit)
	}
}

func (inst *Installer) activatePGCluster(ctx context.Context, cb LogCallback, platform pgPlatformKind, target pgCluster, allClusters []pgCluster) error {
	if platform == pgPlatformDebian {
		for _, c := range allClusters {
			if c.Major != target.Major {
				inst.stopDisableUnitIfPresent(ctx, cb, c.Unit)
				if c.Port == 5432 {
					_ = inst.setPGConfPort(ctx, cb, c.ConfPath, inactiveDebianPortForMajor(c.Major))
				}
				inst.setDebianStartConf(ctx, cb, c.StartConf, "manual")
			}
		}
		if err := inst.setPGConfPort(ctx, cb, target.ConfPath, 5432); err != nil {
			return fmt.Errorf("gagal menyetel port 5432 untuk PostgreSQL %s: %w", target.Major, err)
		}
		inst.setDebianStartConf(ctx, cb, target.StartConf, "auto")
		if err := inst.runCmd(ctx, cb, "systemctl", "restart", target.Unit); err != nil {
			return fmt.Errorf("gagal menjalankan service %s: %w", target.Unit, err)
		}
		if !inst.waitPostgresReadyPort(ctx, target.Major, target.BinDir, 5432) {
			return fmt.Errorf("PostgreSQL %s belum merespons pg_isready di port 5432", target.Major)
		}
		return nil
	}

	// RPM
	for _, other := range supportedPGMajors {
		if other != target.Major {
			inst.stopDisableUnitIfPresent(ctx, cb, "postgresql-"+other)
		}
	}
	if !target.IsDistroRPM {
		inst.stopDisableUnitIfPresent(ctx, cb, "postgresql")
	}
	if err := inst.setPGConfPort(ctx, cb, target.ConfPath, 5432); err != nil {
		return fmt.Errorf("gagal menyetel port 5432 untuk PostgreSQL %s: %w", target.Major, err)
	}
	_ = inst.runCmd(ctx, cb, "systemd-tmpfiles", "--create")
	if _, err := exec.LookPath("restorecon"); err == nil {
		_ = inst.runCmd(ctx, cb, "restorecon", "-RF", filepath.Dir(target.DataDir))
	}
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
	if err := inst.runCmd(ctx, cb, "systemctl", "enable", target.Unit); err != nil {
		return fmt.Errorf("gagal mengaktifkan service %s: %w", target.Unit, err)
	}
	if err := inst.runCmd(ctx, cb, "systemctl", "restart", target.Unit); err != nil {
		return fmt.Errorf("gagal menjalankan service %s: %w", target.Unit, err)
	}
	if !inst.waitPostgresReadyPort(ctx, target.Major, target.BinDir, 5432) {
		return fmt.Errorf("PostgreSQL %s aktif tetapi belum merespons pg_isready di port 5432", target.Major)
	}
	return nil
}

func (inst *Installer) createTargetDatabase(ctx context.Context, cb LogCallback, targetBinDir string, meta pgDatabaseMeta) error {
	dbIdent := quotePGIdent(meta.Name)
	ownerIdent := quotePGIdent(meta.Owner)

	// Gunakan template1 sebagai database kontrol agar aman walau meta.Name == "postgres"
	termSQL := fmt.Sprintf(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();`,
		quotePGLiteral(meta.Name),
	)
	_, _ = inst.queryAsPostgres(ctx, targetBinDir, 5432, "template1", termSQL)

	if _, err := inst.queryAsPostgres(ctx, targetBinDir, 5432, "template1", fmt.Sprintf(`DROP DATABASE IF EXISTS %s;`, dbIdent)); err != nil {
		return fmt.Errorf("gagal menyiapkan ulang database %s: %w", meta.Name, err)
	}

	var attempts []string
	if meta.Enc != "" && meta.Collate != "" && meta.Ctype != "" {
		attempts = append(attempts, fmt.Sprintf(
			`CREATE DATABASE %s WITH OWNER = %s ENCODING = %s LC_COLLATE = %s LC_CTYPE = %s TEMPLATE = template0;`,
			dbIdent, ownerIdent, quotePGLiteral(meta.Enc), quotePGLiteral(meta.Collate), quotePGLiteral(meta.Ctype),
		))
	}
	if meta.Enc != "" {
		attempts = append(attempts, fmt.Sprintf(
			`CREATE DATABASE %s WITH OWNER = %s ENCODING = %s TEMPLATE = template0;`,
			dbIdent, ownerIdent, quotePGLiteral(meta.Enc),
		))
	}
	attempts = append(attempts, fmt.Sprintf(
		`CREATE DATABASE %s WITH OWNER = %s TEMPLATE = template0;`,
		dbIdent, ownerIdent,
	))

	var lastErr error
	for _, stmt := range attempts {
		if _, err := inst.queryAsPostgres(ctx, targetBinDir, 5432, "template1", stmt); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	cb(fmt.Sprintf("[ERROR] Gagal membuat database %s: %v", meta.Name, lastErr))
	return lastErr
}

func (inst *Installer) pruneOldPGBackups(ctx context.Context, cb LogCallback, baseDir string, keep int) {
	if keep <= 0 {
		return
	}
	out, err := inst.runQuietSudoOutput(ctx, "ls", "-1t", baseDir)
	if err != nil {
		return
	}
	var dirs []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
			continue
		}
		if strings.HasPrefix(name, "switch-pg") {
			dirs = append(dirs, name)
		}
	}
	if len(dirs) <= keep {
		return
	}
	for _, name := range dirs[keep:] {
		_ = inst.runCmd(ctx, cb, "rm", "-rf", filepath.Join(baseDir, name))
	}
}

// SwitchPostgres mengalihkan cluster PostgreSQL aktif ke targetVer secara aman
// (mendukung upgrade maupun downgrade) dengan backup otomatis, migrasi penuh role &
// seluruh database, serta rollback otomatis ke cluster asal bila terjadi kegagalan.
func (inst *Installer) SwitchPostgres(ctx context.Context, cb LogCallback, targetVer string) error {
	if cb == nil {
		cb = func(string) {}
	}
	cleanVer := strings.TrimSpace(targetVer)
	if !IsValidPGMajor(cleanVer) {
		return fmt.Errorf("versi mayor PostgreSQL tidak valid: %q", targetVer)
	}

	cb(fmt.Sprintf("=== PENGALIHAN POSTGRESQL KE VERSI %s (DENGAN PROTEKSI DATA) ===", cleanVer))
	platform := inst.detectPGPlatform()

	// Kasus khusus Fedora/RHEL: cluster distro (/var/lib/pgsql/data) bermigrasi langsung ke PGDG pada versi mayor yang sama.
	if platform == pgPlatformRPM {
		pgdgMarker := fmt.Sprintf("/var/lib/pgsql/%s/data/PG_VERSION", cleanVer)
		legacyVer := inst.privilegedReadFileTrimmed(ctx, "/var/lib/pgsql/data/PG_VERSION")
		if legacyVer == cleanVer && !inst.privilegedFileExists(ctx, pgdgMarker) {
			cb(fmt.Sprintf("[INFO] Mendeteksi data PostgreSQL %s bawaan distro di /var/lib/pgsql/data; memigrasikan langsung ke PGDG %s...", cleanVer, cleanVer))
			return inst.InstallPostgres(ctx, cb, cleanVer)
		}
	}

	clusters := inst.listPGClusters(ctx, platform)
	stateMajor := inst.readPGActiveState()
	source := pickSourcePGCluster(clusters, cleanVer, stateMajor, func(unit string) bool {
		return exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", unit).Run() == nil
	})

	targetBefore := inst.resolvePGCluster(ctx, platform, cleanVer)
	targetPreExisted := targetBefore.Initialized

	// Jika tidak ada cluster lain di mesin (atau sudah aktif di versi target), cukup pastikan aktif di 5432.
	if source == nil {
		cb(fmt.Sprintf("[1/2] Memastikan PostgreSQL %s terpasang dan siap...", cleanVer))
		if err := inst.installTargetPGPackagesAndInit(ctx, cb, platform, cleanVer); err != nil {
			return err
		}
		target := inst.resolvePGCluster(ctx, platform, cleanVer)
		allClusters := inst.listPGClusters(ctx, platform)
		cb(fmt.Sprintf("[2/2] Mengaktifkan PostgreSQL %s pada port 5432...", cleanVer))
		if err := inst.activatePGCluster(ctx, cb, platform, target, allClusters); err != nil {
			return err
		}
		if platform == pgPlatformRPM {
			inst.setPGDGAlternatives(ctx, cb, cleanVer)
			_ = inst.EnsureSystemLibpqPriority(ctx, cb)
		}
		inst.writePGActiveState(ctx, cb, cleanVer)
		cb(fmt.Sprintf("✓ [SELESAI] PostgreSQL %s aktif pada port 5432.", cleanVer))
		return nil
	}

	srcMajorInt, _ := parsePGMajorInt(source.Major)
	tgtMajorInt, _ := parsePGMajorInt(cleanVer)
	modeLabel := "MIGRASI"
	if tgtMajorInt > srcMajorInt {
		modeLabel = "UPGRADE"
	} else if tgtMajorInt < srcMajorInt {
		modeLabel = "DOWNGRADE"
	}
	cb(fmt.Sprintf("[INFO] Mode %s: PostgreSQL %s -> PostgreSQL %s", modeLabel, source.Major, cleanVer))

	// 1. Pasang & inisialisasi target terlebih dahulu TANPA menyentuh cluster sumber yang sedang aktif.
	cb(fmt.Sprintf("[1/6] Memasang & menyiapkan biner PostgreSQL %s (cluster asal tetap aman)...", cleanVer))
	if err := inst.installTargetPGPackagesAndInit(ctx, cb, platform, cleanVer); err != nil {
		return err
	}
	target := inst.resolvePGCluster(ctx, platform, cleanVer)
	if !target.Initialized {
		return fmt.Errorf("cluster target PostgreSQL %s gagal diinisialisasi di %s", cleanVer, target.DataDir)
	}

	// 2. Pastikan sumber aktif agar database dapat diinspeksi & di-dump.
	sourceStartedTemp := false
	if !source.Active {
		cb(fmt.Sprintf("[2/6] Menyalakan sementara PostgreSQL %s (port %d) untuk mengekspor database...", source.Major, source.Port))
		if err := inst.runCmd(ctx, cb, "systemctl", "start", source.Unit); err != nil {
			return fmt.Errorf("gagal menyalakan cluster sumber %s untuk ekspor: %w", source.Unit, err)
		}
		if !inst.waitPostgresReadyPort(ctx, source.Major, source.BinDir, source.Port) {
			_ = inst.runCmd(ctx, cb, "systemctl", "stop", source.Unit)
			return fmt.Errorf("cluster sumber %s tidak merespons pg_isready pada port %d", source.Unit, source.Port)
		}
		sourceStartedTemp = true
	} else {
		cb(fmt.Sprintf("[2/6] Cluster sumber PostgreSQL %s aktif di port %d.", source.Major, source.Port))
	}

	stopTempSource := func() {
		if sourceStartedTemp {
			_ = inst.runCmd(ctx, cb, "systemctl", "stop", source.Unit)
		}
	}

	// 3. Kumpulkan metadata database & konfigurasi sumber.
	srcDBs, err := inst.queryPGDatabases(ctx, source.BinDir, source.Port)
	if err != nil {
		stopTempSource()
		return fmt.Errorf("gagal membaca daftar database dari PostgreSQL %s: %w", source.Major, err)
	}
	pgHasObjs, err := inst.queryPGPostgresHasUserObjects(ctx, source.BinDir, source.Port)
	if err != nil {
		stopTempSource()
		return fmt.Errorf("gagal memeriksa objek database postgres pada PostgreSQL %s: %w", source.Major, err)
	}

	dbSettings := make(map[string][]pgDBSetting, len(srcDBs))
	for _, db := range srcDBs {
		if s := inst.queryPGDBSettings(ctx, source.BinDir, source.Port, db.Name); len(s) > 0 {
			dbSettings[db.Name] = s
		}
	}

	var srcConfRaw, srcHbaRaw string
	if !targetPreExisted {
		srcConfRaw, _ = inst.privilegedReadFile(ctx, source.ConfPath)
		srcHbaRaw, _ = inst.privilegedReadFile(ctx, source.HbaPath)
	}

	// 4. Dump seluruh roles/globals dan database dari sumber.
	// Aturan PostgreSQL:
	//   - Upgrade (target >= source): gunakan pg_dump / pg_dumpall milik TARGET (lebih baru).
	//   - Downgrade (target < source): gunakan pg_dump / pg_dumpall milik SUMBER (lebih baru) lalu sanitasi saat restore.
	dumpBinDir := target.BinDir
	if tgtMajorInt < srcMajorInt {
		dumpBinDir = source.BinDir
	}

	runDir := filepath.Join(pgBackupBaseDir, fmt.Sprintf("switch-pg%s-to-pg%s-%s", source.Major, cleanVer, time.Now().Format("20060102-150405")))
	if err := inst.runCmd(ctx, cb, "mkdir", "-p", runDir); err != nil {
		stopTempSource()
		return fmt.Errorf("gagal membuat direktori backup %s: %w", runDir, err)
	}
	_ = inst.runCmd(ctx, cb, "chown", "postgres:postgres", pgBackupBaseDir, runDir)
	_ = inst.runCmd(ctx, cb, "chmod", "700", pgBackupBaseDir, runDir)

	cb(fmt.Sprintf("[3/6] Mengekspor roles & %d database dari PostgreSQL %s ke %s...", len(srcDBs), source.Major, runDir))
	globalsDumpPath := filepath.Join(runDir, "source-globals.sql")
	if err := inst.dumpGlobalsAsPostgres(ctx, cb, dumpBinDir, source.Port, globalsDumpPath); err != nil {
		stopTempSource()
		return fmt.Errorf("gagal dump globals/roles dari PostgreSQL %s: %w", source.Major, err)
	}
	if err := inst.verifyNonEmptyFilePrivileged(ctx, globalsDumpPath); err != nil {
		stopTempSource()
		return err
	}

	type dumpedDB struct {
		meta     pgDatabaseMeta
		dumpPath string
		isSystem bool
	}
	var dumpedDBs []dumpedDB
	for i, db := range srcDBs {
		if db.Name == "postgres" {
			if !pgHasObjs {
				continue
			}
			outPath := filepath.Join(runDir, "db-postgres.sql")
			if err := inst.dumpDBAsPostgres(ctx, cb, dumpBinDir, source.Port, "postgres", outPath, true); err != nil {
				stopTempSource()
				return fmt.Errorf("gagal dump database postgres: %w", err)
			}
			if err := inst.verifyNonEmptyFilePrivileged(ctx, outPath); err != nil {
				stopTempSource()
				return err
			}
			dumpedDBs = append(dumpedDBs, dumpedDB{meta: db, dumpPath: outPath, isSystem: true})
			continue
		}

		outPath := filepath.Join(runDir, fmt.Sprintf("db-%d.sql", i))
		cb(fmt.Sprintf("  -> Dump database: %s (owner: %s)", db.Name, db.Owner))
		if err := inst.dumpDBAsPostgres(ctx, cb, dumpBinDir, source.Port, db.Name, outPath, false); err != nil {
			stopTempSource()
			return fmt.Errorf("gagal dump database %s: %w", db.Name, err)
		}
		if err := inst.verifyNonEmptyFilePrivileged(ctx, outPath); err != nil {
			stopTempSource()
			return err
		}
		dumpedDBs = append(dumpedDBs, dumpedDB{meta: db, dumpPath: outPath})
	}

	// Fungsi rollback: mengembalikan port 5432 ke cluster sumber tanpa mengubah datanya.
	allClusters := inst.listPGClusters(ctx, platform)
	rollbackToSource := func(cause error) error {
		cb(fmt.Sprintf("[ROLLBACK] Terjadi kesalahan saat migrasi ke PostgreSQL %s: %v", cleanVer, cause))
		cb(fmt.Sprintf("[ROLLBACK] Mengaktifkan kembali PostgreSQL %s pada port 5432 agar database tetap utuh...", source.Major))
		_ = inst.runCmd(ctx, cb, "systemctl", "stop", target.Unit)
		_ = inst.runCmd(ctx, cb, "systemctl", "disable", target.Unit)
		if platform == pgPlatformDebian {
			_ = inst.setPGConfPort(ctx, cb, target.ConfPath, inactiveDebianPortForMajor(cleanVer))
			inst.setDebianStartConf(ctx, cb, target.StartConf, "manual")
		}
		if rbErr := inst.activatePGCluster(ctx, cb, platform, *source, allClusters); rbErr != nil {
			cb(fmt.Sprintf("[ERROR] Rollback otomatis gagal (%v); backup SQL tetap tersimpan di %s", rbErr, runDir))
		} else {
			if platform == pgPlatformRPM && !source.IsDistroRPM {
				inst.setPGDGAlternatives(ctx, cb, source.Major)
				_ = inst.EnsureSystemLibpqPriority(ctx, cb)
			}
			inst.writePGActiveState(ctx, cb, source.Major)
			cb(fmt.Sprintf("✓ [ROLLBACK SELESAI] PostgreSQL %s kembali aktif; seluruh data asal aman. Backup SQL: %s", source.Major, runDir))
		}
		return fmt.Errorf("pengalihan ke PostgreSQL %s dibatalkan dan dikembalikan ke PostgreSQL %s: %w", cleanVer, source.Major, cause)
	}

	// 5. Terapkan overlay config wdev bila target baru diinisialisasi, lalu aktifkan target di port 5432.
	cb(fmt.Sprintf("[4/6] Menghentikan PostgreSQL %s (data tetap disimpan utuh) dan menyalakan PostgreSQL %s di port 5432...", source.Major, cleanVer))
	if !targetPreExisted && srcConfRaw != "" {
		dstConfRaw, cErr := inst.privilegedReadFile(ctx, target.ConfPath)
		dstHbaRaw, hErr := inst.privilegedReadFile(ctx, target.HbaPath)
		if cErr == nil && hErr == nil {
			newConf, newHba := applyWdevPGConfigOverlay(srcConfRaw, srcHbaRaw, dstConfRaw, dstHbaRaw)
			if newConf != dstConfRaw {
				_ = inst.writePostgresSystemFile(ctx, cb, target.ConfPath, []byte(newConf), "644")
			}
			if newHba != dstHbaRaw {
				_ = inst.writePostgresSystemFile(ctx, cb, target.HbaPath, []byte(newHba), "640")
			}
		}
	}

	if err := inst.activatePGCluster(ctx, cb, platform, target, allClusters); err != nil {
		return rollbackToSource(err)
	}

	// 6. Bila target sebelumnya sudah punya data (mis. pernah dipakai lalu downgrade kembali), buat backup penuh target dulu!
	targetPGObjs := false
	if targetPreExisted {
		tgtDBs, _ := inst.queryPGDatabases(ctx, target.BinDir, 5432)
		targetPGObjs, _ = inst.queryPGPostgresHasUserObjects(ctx, target.BinDir, 5432)
		tgtRoles := inst.queryPGCustomRoleCount(ctx, target.BinDir, 5432)
		if len(tgtDBs) > 1 || targetPGObjs || tgtRoles > 0 {
			preTargetBackup := filepath.Join(runDir, fmt.Sprintf("backup-target-before-pg%s.sql", cleanVer))
			cb(fmt.Sprintf("  -> Menyimpan backup pengaman kondisi awal PostgreSQL %s ke %s...", cleanVer, preTargetBackup))
			if err := inst.dumpAllAsPostgres(ctx, cb, target.BinDir, 5432, preTargetBackup); err != nil {
				return rollbackToSource(fmt.Errorf("gagal membuat backup pengaman target PostgreSQL %s: %w", cleanVer, err))
			}
		}
	}

	// 7. Periksa kapabilitas psql & GUC target, lalu restore globals & database.
	verOut, _ := exec.CommandContext(ctx, filepath.Join(target.BinDir, "psql"), "--version").Output()
	stripRestrict := !psqlSupportsRestrict(string(verOut))

	var supportedGUCs map[string]bool
	var targetRoles map[string]bool
	if tgtMajorInt < srcMajorInt {
		supportedGUCs, err = inst.queryPGSupportedGUCs(ctx, target.BinDir, 5432)
		if err != nil {
			return rollbackToSource(fmt.Errorf("gagal membaca pg_settings PostgreSQL %s: %w", cleanVer, err))
		}
		targetRoles, _ = inst.queryPGRoles(ctx, target.BinDir, 5432)
	}

	transformOpts := pgSQLTransformOpts{
		TargetMajor:   tgtMajorInt,
		SupportedGUCs: supportedGUCs,
		TargetRoles:   targetRoles,
		StripRestrict: stripRestrict,
	}

	cb(fmt.Sprintf("[5/6] Memulihkan roles & %d database ke PostgreSQL %s...", len(dumpedDBs), cleanVer))
	importGlobalsPath := filepath.Join(runDir, "import-globals.sql")
	if err := inst.transformPrivilegedDumpFile(ctx, cb, globalsDumpPath, importGlobalsPath, func(r io.Reader, w io.Writer) error {
		return transformGlobalsSQL(r, w, transformOpts)
	}); err != nil {
		return rollbackToSource(fmt.Errorf("gagal menyiapkan file import globals: %w", err))
	}
	if err := inst.execAsPostgresFile(ctx, cb, target.BinDir, 5432, "postgres", importGlobalsPath); err != nil {
		return rollbackToSource(fmt.Errorf("gagal mengimpor roles/globals ke PostgreSQL %s: %w", cleanVer, err))
	}

	needsDBTransform := supportedGUCs != nil || stripRestrict
	for idx, item := range dumpedDBs {
		importPath := item.dumpPath
		if needsDBTransform {
			importPath = filepath.Join(runDir, fmt.Sprintf("import-db-%d.sql", idx))
			if err := inst.transformPrivilegedDumpFile(ctx, cb, item.dumpPath, importPath, func(r io.Reader, w io.Writer) error {
				return transformDatabaseSQL(r, w, transformOpts)
			}); err != nil {
				return rollbackToSource(fmt.Errorf("gagal memproses SQL database %s: %w", item.meta.Name, err))
			}
		}

		if item.isSystem {
			cb("  -> Memulihkan objek pada database: postgres")
			if targetPGObjs {
				// Target postgres DB sudah berisi objek lama; bersihkan lewat template1 agar bebas konflik FK/view.
				if err := inst.createTargetDatabase(ctx, cb, target.BinDir, item.meta); err != nil {
					return rollbackToSource(err)
				}
			}
			if err := inst.execAsPostgresFile(ctx, cb, target.BinDir, 5432, "postgres", importPath); err != nil {
				return rollbackToSource(fmt.Errorf("gagal memulihkan database postgres: %w", err))
			}
		} else {
			cb(fmt.Sprintf("  -> Mengimpor database: %s", item.meta.Name))
			if err := inst.createTargetDatabase(ctx, cb, target.BinDir, item.meta); err != nil {
				return rollbackToSource(err)
			}
			if err := inst.execAsPostgresFile(ctx, cb, target.BinDir, 5432, item.meta.Name, importPath); err != nil {
				return rollbackToSource(fmt.Errorf("gagal memulihkan database %s ke PostgreSQL %s: %w", item.meta.Name, cleanVer, err))
			}
		}

		// Pulihkan konfigurasi ALTER DATABASE / ALTER ROLE IN DATABASE SET bila ada
		for _, st := range dbSettings[item.meta.Name] {
			if supportedGUCs != nil && !strings.Contains(st.Key, ".") && !supportedGUCs[st.Key] {
				continue
			}
			var alterStmt string
			if st.Role == "" {
				alterStmt = fmt.Sprintf(`ALTER DATABASE %s SET %s TO %s;`, quotePGIdent(item.meta.Name), st.Key, quotePGLiteral(st.Value))
			} else {
				alterStmt = fmt.Sprintf(`ALTER ROLE %s IN DATABASE %s SET %s TO %s;`, quotePGIdent(st.Role), quotePGIdent(item.meta.Name), st.Key, quotePGLiteral(st.Value))
			}
			if _, aErr := inst.queryAsPostgres(ctx, target.BinDir, 5432, "postgres", alterStmt); aErr != nil {
				cb(fmt.Sprintf("[WARN] Melewati setting %s pada %s: %v", st.Key, item.meta.Name, aErr))
			}
		}
	}

	// 8. Verifikasi seluruh database dari sumber benar-benar hadir di target!
	cb(fmt.Sprintf("[6/6] Memverifikasi integritas daftar database di PostgreSQL %s...", cleanVer))
	finalDBs, err := inst.queryPGDatabases(ctx, target.BinDir, 5432)
	if err != nil {
		return rollbackToSource(fmt.Errorf("gagal memverifikasi daftar database di PostgreSQL %s: %w", cleanVer, err))
	}
	present := make(map[string]bool, len(finalDBs))
	for _, d := range finalDBs {
		present[d.Name] = true
	}
	for _, want := range srcDBs {
		if !present[want.Name] {
			return rollbackToSource(fmt.Errorf("verifikasi gagal: database %q tidak ditemukan di PostgreSQL %s setelah restore", want.Name, cleanVer))
		}
	}

	if platform == pgPlatformRPM {
		inst.setPGDGAlternatives(ctx, cb, cleanVer)
		_ = inst.EnsureSystemLibpqPriority(ctx, cb)
	}
	inst.writePGActiveState(ctx, cb, cleanVer)
	inst.pruneOldPGBackups(ctx, cb, pgBackupBaseDir, maxRetainedPGBackups)

	psqlBin := filepath.Join(target.BinDir, "psql")
	if out, vErr := exec.CommandContext(ctx, psqlBin, "--version").CombinedOutput(); vErr == nil {
		cb(fmt.Sprintf("✓ %s", strings.TrimSpace(string(out))))
	}
	cb(fmt.Sprintf("✓ [SELESAI] PostgreSQL %s aktif di port 5432 (%d database termigrasi utuh).", cleanVer, len(srcDBs)))
	cb(fmt.Sprintf("✓ Cluster lama PostgreSQL %s tetap tersimpan di %s.", source.Major, source.DataDir))
	cb(fmt.Sprintf("✓ Backup migrasi SQL tersimpan di %s.", runDir))
	return nil
}
