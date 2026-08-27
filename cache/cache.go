// Package cache provides small helpers for inspecting the
// ~/.cache/linode-tui directory: byte sizes per subdir and a human-readable
// byte formatter. Backs the in-TUI `:cache size` / `:cache prune` verbs and
// the `cache` check in `linode-tui doctor`.
package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Root returns the OS-specific path to the linode-tui cache directory.
func Root() (string, error) {
	c, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "linode-tui"), nil
}

// SubdirSizes returns the byte size of every immediate subdirectory of root
// (and "_" for files at the root). Missing root is not an error — returns an
// empty map. Returns the total across all entries.
//
// Per-entry stat/walk failures (permission errors, races with concurrent
// writers, etc.) are collected and returned via errors.Join rather than
// discarded, so a caller like `:cache size` can surface that its total is a
// lower bound instead of silently under-reporting.
func SubdirSizes(root string) (map[string]int64, int64, error) {
	out := map[string]int64{}
	var total int64
	var walkErrs []error
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return out, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		if !e.IsDir() {
			info, err := e.Info()
			if err != nil {
				walkErrs = append(walkErrs, fmt.Errorf("%s: %w", path, err))
				continue
			}
			out["_"] += info.Size()
			total += info.Size()
			continue
		}
		var sz int64
		walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				walkErrs = append(walkErrs, fmt.Errorf("%s: %w", p, err))
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				walkErrs = append(walkErrs, fmt.Errorf("%s: %w", p, err))
				return nil
			}
			sz += info.Size()
			return nil
		})
		if walkErr != nil {
			walkErrs = append(walkErrs, walkErr)
		}
		out[e.Name()] = sz
		total += sz
	}
	return out, total, errors.Join(walkErrs...)
}

// Total returns the byte size of everything under root. Missing root is not
// an error — returns 0. Use this when you only need the total and the
// per-subdir map from SubdirSizes would be wasted work.
func Total(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

// FormatBytes renders n as a human-readable string (B, KiB, MiB, …).
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
