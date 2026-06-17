package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clutch.json")
	body := `{"search_roots":["/a","/b"],"store_location":"/store","repo_mapping":{"x":"/x"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SearchRoots) != 2 || cfg.SearchRoots[0] != "/a" {
		t.Fatalf("SearchRoots = %v", cfg.SearchRoots)
	}
	if cfg.StoreLocation != "/store" {
		t.Fatalf("StoreLocation = %q", cfg.StoreLocation)
	}
	if cfg.RepoMapping["x"] != "/x" {
		t.Fatalf("RepoMapping = %v", cfg.RepoMapping)
	}
}

func TestLoadDefaultsNoFile(t *testing.T) {
	// Isolate from any ambient env or on-disk config.
	t.Setenv("CLUTCH_CONFIG", "")
	t.Setenv("CLUTCH_STORE", "")
	t.Setenv("CLUTCH_SEARCH_ROOTS", "")
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd := t.TempDir()
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// os.Getwd may resolve symlinks (e.g. /var -> /private/var on macOS), so
	// compare resolved forms.
	gotRoot := cfg.SearchRoots[0]
	wantRoot, _ := filepath.EvalSymlinks(wd)
	if got, _ := filepath.EvalSymlinks(gotRoot); got != wantRoot {
		t.Fatalf("SearchRoots[0] = %q, want cwd %q", gotRoot, wd)
	}
	want := filepath.Join(home, ".local", "state", "clutch")
	if cfg.StoreLocation != want {
		t.Fatalf("StoreLocation = %q, want %q", cfg.StoreLocation, want)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clutch.json")
	body := `{"search_roots":["/from/file"],"store_location":"/from/file/store"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLUTCH_STORE", "/env/store")
	roots := strings.Join([]string{"/env/a", "/env/b"}, string(os.PathListSeparator))
	t.Setenv("CLUTCH_SEARCH_ROOTS", roots)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StoreLocation != "/env/store" {
		t.Fatalf("StoreLocation = %q, want env override", cfg.StoreLocation)
	}
	if len(cfg.SearchRoots) != 2 || cfg.SearchRoots[0] != "/env/a" || cfg.SearchRoots[1] != "/env/b" {
		t.Fatalf("SearchRoots = %v, want env override", cfg.SearchRoots)
	}
}
