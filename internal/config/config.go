// Package config holds clutch's runtime configuration: where to look for work
// and where the authoritative store lives.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config is the clutch runtime configuration.
type Config struct {
	// SearchRoots are filesystem roots scanned for repos/worktrees.
	SearchRoots []string `json:"search_roots"`
	// RepoMapping maps a durable repo identity to a local checkout path.
	RepoMapping map[string]string `json:"repo_mapping"`
	// StoreLocation is the out-of-repo path of the Task+Board store.
	StoreLocation string `json:"store_location"`
}

// Load reads and validates configuration from path.
//
// Resolution order:
//  1. If path is non-empty, that file is read (an error if it does not exist).
//  2. Otherwise the default config path is tried, in order: $CLUTCH_CONFIG,
//     ./clutch.json, then $HOME/.config/clutch/config.json. The first that
//     exists is read.
//  3. If no config file is found, defaults are used (NOT an error): SearchRoots
//     is the current working directory and StoreLocation is
//     $XDG_STATE_HOME/clutch (else $HOME/.local/state/clutch) — so a bare
//     `clutch scan` works without any config.
//
// Env overrides are then applied: CLUTCH_STORE overrides StoreLocation, and
// CLUTCH_SEARCH_ROOTS (split on os.PathListSeparator) overrides SearchRoots.
// Finally StoreLocation is validated to be non-empty.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	resolved, err := resolvePath(path)
	if err != nil {
		return nil, err
	}
	if resolved != "" {
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	if cfg.StoreLocation == "" {
		return nil, errors.New("config: store_location is empty after defaults and overrides")
	}
	return cfg, nil
}

// resolvePath returns the config file to read, or "" if none exists.
func resolvePath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	var candidates []string
	if v := os.Getenv("CLUTCH_CONFIG"); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, "clutch.json")
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "clutch", "config.json"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// applyDefaults fills empty fields with sensible defaults so a bare scan works.
func applyDefaults(cfg *Config) {
	if len(cfg.SearchRoots) == 0 {
		if wd, err := os.Getwd(); err == nil {
			cfg.SearchRoots = []string{wd}
		}
	}
	if cfg.StoreLocation == "" {
		if v := os.Getenv("XDG_STATE_HOME"); v != "" {
			cfg.StoreLocation = filepath.Join(v, "clutch")
		} else if home, err := os.UserHomeDir(); err == nil {
			cfg.StoreLocation = filepath.Join(home, ".local", "state", "clutch")
		}
	}
}

// applyEnvOverrides applies env overrides over file/default values.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CLUTCH_STORE"); v != "" {
		cfg.StoreLocation = v
	}
	if v := os.Getenv("CLUTCH_SEARCH_ROOTS"); v != "" {
		cfg.SearchRoots = strings.Split(v, string(os.PathListSeparator))
	}
}
