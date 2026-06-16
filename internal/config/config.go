// Package config holds clutch's runtime configuration: where to look for work
// and where the authoritative store lives.
package config

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
func Load(path string) (*Config, error) {
	// TODO(wave1-d): read the config file and apply env overrides.
	panic("not implemented")
}
