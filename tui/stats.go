package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// StatsPath returns the file backing persisted usage stats. Lives under the
// user's cache dir so it's distinct from config secrets.
func StatsPath() (string, error) { return statsPath() }

// statsPath is the package-internal version (exported wrapper above for any
// future callers).
func statsPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "linode-tui", "stats.json"), nil
}

func loadStats() map[string]int {
	path, err := statsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]int{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func saveStats(stats map[string]int) {
	path, err := statsPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
