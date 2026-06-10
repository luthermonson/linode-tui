package views

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// snapshotsKept caps how many timestamped snapshot files we keep per
// (kind, id). On each save, older files beyond this count are pruned.
const snapshotsKept = 10

// snapshotDir returns the directory used to store per-resource bookmark
// snapshots. Exported helpers in the tui package read from the same path.
func snapshotDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "linode-tui", "snapshots"), nil
}

// SnapshotPath returns the directory holding versioned snapshots for
// (kind, id). The dir contains one file per save, named by UTC timestamp.
func SnapshotPath(kind, id string) (string, error) {
	d, err := snapshotDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, kind, id), nil
}

// saveSnapshot writes a fresh timestamped file for (kind, id) and prunes the
// directory to the most recent snapshotsKept entries.
func saveSnapshot(kind, id string, body []byte) {
	dir, err := SnapshotPath(kind, id)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(dir, stamp+".json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return
	}
	pruneVersions(dir, snapshotsKept)
}

// removeSnapshot deletes every version for (kind, id).
func removeSnapshot(kind, id string) {
	dir, err := SnapshotPath(kind, id)
	if err != nil {
		return
	}
	_ = os.RemoveAll(dir)
}

// listSnapshotFiles returns version filenames for (kind, id) newest first.
func listSnapshotFiles(kind, id string) []string {
	dir, err := SnapshotPath(kind, id)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > 5 && n[len(n)-5:] == ".json" {
			out = append(out, n)
		}
	}
	// Filenames are ISO-ish so a reverse string sort puts newest first.
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// LoadSnapshot returns the most recent snapshot body for (kind, id).
func LoadSnapshot(kind, id string) ([]byte, error) {
	return LoadSnapshotAt(kind, id, 0)
}

// LoadSnapshotAt returns the body at index n (0 = latest, 1 = previous, …).
func LoadSnapshotAt(kind, id string, n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative snapshot index: %d", n)
	}
	files := listSnapshotFiles(kind, id)
	if len(files) == 0 {
		return nil, fmt.Errorf("no snapshots for %s/%s", kind, id)
	}
	if n >= len(files) {
		return nil, fmt.Errorf("only %d snapshots for %s/%s (asked for @%d)", len(files), kind, id, n)
	}
	dir, _ := SnapshotPath(kind, id)
	return os.ReadFile(filepath.Join(dir, files[n]))
}

// ListSnapshots returns timestamp labels (newest first) for the saved
// snapshots of (kind, id). Used by future :snapshots viewers.
func ListSnapshots(kind, id string) []string {
	out := listSnapshotFiles(kind, id)
	for i, n := range out {
		out[i] = n[:len(n)-len(".json")]
	}
	return out
}

// SnapshotAge returns the time since the latest snapshot for (kind, id) was
// written. Returns 0 + false when none exists.
func SnapshotAge(kind, id string) (time.Duration, bool) {
	files := listSnapshotFiles(kind, id)
	if len(files) == 0 {
		return 0, false
	}
	dir, _ := SnapshotPath(kind, id)
	info, err := os.Stat(filepath.Join(dir, files[0]))
	if err != nil {
		return 0, false
	}
	return time.Since(info.ModTime()), true
}

// pruneVersions trims the dir to the keep newest .json files.
func pruneVersions(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > 5 && n[len(n)-5:] == ".json" {
			files = append(files, n)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for i, name := range files {
		if i < keep {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// PruneSnapshots removes snapshot directories whose (kind, id) tuple isn't
// present in the supplied bookmarks map. Returns the count removed.
func PruneSnapshots(bookmarks map[string][]string) int {
	root, err := snapshotDir()
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	kept := map[string]map[string]bool{}
	for kind, ids := range bookmarks {
		kept[kind] = map[string]bool{}
		for _, id := range ids {
			kept[kind][id] = true
		}
	}
	removed := 0
	for _, kindEntry := range entries {
		if !kindEntry.IsDir() {
			continue
		}
		kind := kindEntry.Name()
		kindPath := filepath.Join(root, kind)
		idEntries, err := os.ReadDir(kindPath)
		if err != nil {
			continue
		}
		for _, idEntry := range idEntries {
			id := idEntry.Name()
			// Strip ".json" if legacy flat file format from a prior install.
			if !idEntry.IsDir() && len(id) > 5 && id[len(id)-5:] == ".json" {
				id = id[:len(id)-5]
			}
			if !kept[kind][id] {
				_ = os.RemoveAll(filepath.Join(kindPath, idEntry.Name()))
				removed++
			}
		}
	}
	return removed
}
