// Package persist provides simple JSON config persistence for the launcher.
package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config holds user-facing settings that survive process restarts.
type Config struct {
	JavaHome      string `json:"javaHome,omitempty"`
	WorkspacePath string `json:"workspacePath,omitempty"`
	SkipTests     bool   `json:"skipTests"`
	TomcatPath    string `json:"tomcatPath,omitempty"`
	VSCodePath    string `json:"vscodePath,omitempty"`
	RSPTomcatPort int    `json:"rspTomcatPort,omitempty"`
}

var (
	mu       sync.Mutex
	filePath string
	current  Config
	loaded   bool
)

// Init sets the config file path (call once at startup).
func Init(path string) {
	mu.Lock()
	defer mu.Unlock()
	filePath = path
	loaded = false
}

// Load reads the config file from disk. Returns zero Config on error.
func Load() Config {
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return current
	}
	if filePath == "" {
		filePath = defaultPath()
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		loaded = true
		return current
	}
	_ = json.Unmarshal(b, &current)
	loaded = true
	return current
}

// Save persists the config to disk.
func Save(c Config) error {
	mu.Lock()
	defer mu.Unlock()
	if filePath == "" {
		filePath = defaultPath()
	}
	current = c
	_ = os.MkdirAll(filepath.Dir(filePath), 0o755)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, b, 0o644)
}

// Update applies a partial update via a mutator function and saves.
func Update(fn func(c *Config)) error {
	mu.Lock()
	cfg := current
	mu.Unlock()
	fn(&cfg)
	return Save(cfg)
}

// defaultPath returns ~/.egov-launcher.json
func defaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".egov-launcher.json")
}
