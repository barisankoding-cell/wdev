package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileCargoConfigFreshAndIdempotent(t *testing.T) {
	got := reconcileCargoConfig("", "/usr/local/bin/sccache", true)
	for _, want := range []string{
		"[build]",
		`rustc-wrapper = "/usr/local/bin/sccache"`,
		"[target.x86_64-unknown-linux-gnu]",
		"[target.aarch64-unknown-linux-gnu]",
		`linker = "clang"`,
		`rustflags = ["-C", "link-arg=-fuse-ld=mold"]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("konfigurasi Cargo baru kurang %q:\n%s", want, got)
		}
	}

	again := reconcileCargoConfig(got, "/usr/local/bin/sccache", true)
	if again != got {
		t.Fatalf("reconcileCargoConfig tidak idempoten:\nfirst:\n%s\nsecond:\n%s", got, again)
	}
}

func TestReconcileCargoConfigMergesExistingWithoutClobbering(t *testing.T) {
	existing := `[build]
jobs = 8

[net]
git-fetch-with-cli = true

[target.x86_64-unknown-linux-gnu]
rustflags = ["-C", "target-cpu=native"]
`
	got := reconcileCargoConfig(existing, "/usr/local/bin/sccache", true)
	if !strings.Contains(got, "jobs = 8") {
		t.Fatalf("opsi jobs user hilang:\n%s", got)
	}
	if !strings.Contains(got, `git-fetch-with-cli = true`) {
		t.Fatalf("blok [net] user hilang:\n%s", got)
	}
	if !strings.Contains(got, `rustc-wrapper = "/usr/local/bin/sccache"`) {
		t.Fatalf("rustc-wrapper tidak disisipkan ke [build]:\n%s", got)
	}
	if !strings.Contains(got, `rustflags = ["-C", "target-cpu=native"]`) {
		t.Fatalf("rustflags kustom user tertimpa:\n%s", got)
	}
	if strings.Count(got, "rustflags =") != 2 {
		t.Fatalf("rustflags x86_64 tidak boleh diduplikasi (harus 1 untuk x86_64 + 1 untuk aarch64):\n%s", got)
	}
	if !strings.Contains(got, `linker = "clang"`) {
		t.Fatalf("linker clang tidak disisipkan ke target:\n%s", got)
	}

	// Custom rustc-wrapper milik user harus dipertahankan.
	custom := "[build]\nrustc-wrapper = \"/custom/wrapper\"\n"
	gotCustom := reconcileCargoConfig(custom, "/usr/local/bin/sccache", false)
	if gotCustom != custom {
		t.Fatalf("rustc-wrapper kustom tertimpa:\n%s", gotCustom)
	}
}

func TestSyncUserCargoConfigAndMissingCheck(t *testing.T) {
	home := t.TempDir()
	changed, cfgPath, err := syncUserCargoConfig("testuser", home, "/usr/local/bin/sccache", true)
	if err != nil {
		t.Fatalf("syncUserCargoConfig error: %v", err)
	}
	if !changed {
		t.Fatal("syncUserCargoConfig harus menandai perubahan pada inisialisasi pertama")
	}
	if cfgPath != filepath.Join(home, ".cargo", "config.toml") {
		t.Fatalf("path config tidak sesuai: %s", cfgPath)
	}
	if cargoConfigMissingSccache(cfgPath) {
		t.Fatal("cargoConfigMissingSccache harus false setelah disinkronkan")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `rustc-wrapper = "/usr/local/bin/sccache"`) {
		t.Fatalf("isi config user tidak memuat sccache:\n%s", string(data))
	}

	// Panggilan kedua harus no-op.
	changed2, _, err := syncUserCargoConfig("testuser", home, "/usr/local/bin/sccache", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Fatal("panggilan kedua syncUserCargoConfig harus idempoten (changed=false)")
	}
}
func TestParseSccacheStatsJSON(t *testing.T) {
	sample := []byte(`{
		"version": "0.19.0",
		"cache_location": "Local disk: \"/home/rhee/.cache/sccache\"",
		"cache_size": null,
		"max_cache_size": 10737418240,
		"use_preprocessor_cache_mode": true,
		"stats": {
			"compile_requests": 24,
			"requests_unsupported_compiler": 1,
			"requests_not_compile": 4,
			"requests_not_cacheable": 16,
			"requests_executed": 3,
			"cache_errors": {"counts": {"Rust": 0}, "adv_counts": {}},
			"cache_hits": {"counts": {"Rust": 2, "C/C++": 1}, "adv_counts": {}},
			"cache_misses": {"counts": {"Rust": 1}, "adv_counts": {}},
			"cache_timeouts": 0,
			"non_cacheable_compilations": 0,
			"compile_fails": 0,
			"cache_write_duration": {"secs": 0, "nanos": 5000000},
			"cache_read_hit_duration": {"secs": 0, "nanos": 1200000},
			"compiler_write_duration": {"secs": 0, "nanos": 36000000}
		}
	}`)

	var rep SccacheStatsReport
	if err := parseSccacheStatsJSON(sample, &rep); err != nil {
		t.Fatalf("parseSccacheStatsJSON error: %v", err)
	}
	if rep.Version != "0.19.0" || !rep.PreprocessorMode {
		t.Fatalf("metadata tidak sesuai: %+v", rep)
	}
	if rep.CompileRequests != 24 || rep.RequestsExecuted != 3 {
		t.Fatalf("requests tidak sesuai: %+v", rep)
	}
	if rep.CacheHits != 3 || rep.CacheMisses != 1 {
		t.Fatalf("hits/misses tidak sesuai: hits=%d misses=%d", rep.CacheHits, rep.CacheMisses)
	}
	if rep.CacheHitRate != 75.0 {
		t.Fatalf("CacheHitRate = %.2f, want 75.0", rep.CacheHitRate)
	}
	if rep.NonCacheableCalls != 20 || rep.UnsupportedCompiler != 1 {
		t.Fatalf("non-cacheable/unsupported tidak sesuai: %+v", rep)
	}
	if rep.AvgCompilerMs != 36.0 {
		t.Fatalf("AvgCompilerMs = %.2f, want 36.0", rep.AvgCompilerMs)
	}
}
