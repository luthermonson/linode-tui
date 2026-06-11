// Package audit appends one-line JSON records of every mutating action
// performed through the TUI or CLI. Lives under the user's cache dir; never
// transmitted off-host.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry is one audit record.
type Entry struct {
	Timestamp time.Time `json:"ts"`
	Account   string    `json:"account,omitempty"`
	Action    string    `json:"action"`
	Kind      string    `json:"kind,omitempty"`
	ID        string    `json:"id,omitempty"`
	Label     string    `json:"label,omitempty"`
	Err       string    `json:"err,omitempty"`
}

var mu sync.Mutex

// Path returns the audit log file path.
func Path() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "linode-tui", "audit.log"), nil
}

// maxLogBytes is the rotation threshold. When the active log file exceeds
// this size, it's renamed to <path>.1 (replacing any previous .1). Single
// generation kept — keeps the cache footprint bounded.
const maxLogBytes = 2 * 1024 * 1024 // 2 MB

// Append writes one entry as a JSON line. Errors are swallowed — auditing
// must never block a real operation. Rotates the log when it exceeds
// maxLogBytes.
func Append(e Entry) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	mu.Lock()
	defer mu.Unlock()
	p, err := Path()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	rotateIfBigLocked(p)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(f, string(data))
}

func rotateIfBigLocked(p string) {
	info, err := os.Stat(p)
	if err != nil || info.Size() < maxLogBytes {
		return
	}
	// Replace any prior .1 — only one generation kept.
	_ = os.Remove(p + ".1")
	_ = os.Rename(p, p+".1")
}

// PruneOlderThan removes entries whose Timestamp is before `cutoff`. Rewrites
// the log file in place. Returns the count removed.
func PruneOlderThan(cutoff time.Time) int {
	return PruneOlderThanKind(cutoff, "")
}

// PruneOlderThanKind drops entries older than cutoff. When kind is non-empty,
// only matching entries are pruned — others survive even if older.
func PruneOlderThanKind(cutoff time.Time, kind string) int {
	mu.Lock()
	defer mu.Unlock()
	p, err := Path()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	var kept []byte
	removed := 0
	start := 0
	for start < len(data) {
		end := start
		for end < len(data) && data[end] != '\n' {
			end++
		}
		line := data[start:end]
		var e Entry
		if err := json.Unmarshal(line, &e); err == nil {
			drop := e.Timestamp.Before(cutoff) || e.Timestamp.Equal(cutoff)
			if kind != "" && e.Kind != kind {
				drop = false
			}
			if drop {
				removed++
			} else {
				kept = append(kept, line...)
				kept = append(kept, '\n')
			}
		}
		start = end + 1
	}
	if removed == 0 {
		return 0
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, kept, 0o644); err != nil {
		return 0
	}
	_ = os.Rename(tmp, p)
	return removed
}

// Tail returns the most recent n entries (newest first). Returns fewer if the
// log is shorter, or empty if it doesn't exist.
// Filter narrows an entry slice by the common predicates used across audit
// CLI subcommands. Zero-value fields are ignored.
type Filter struct {
	Account string
	ErrOnly bool
	// SinceCutoff drops entries strictly before this time. Zero means no
	// cutoff.
	SinceCutoff time.Time
	// Substring is a case-insensitive substring matched against
	// action+kind+id+label+err. Empty means no substring filter.
	Substring string
}

// Matches reports whether e passes f. A nil-value f.Matches is a permissive
// match (returns true for everything).
func (f Filter) Matches(e Entry) bool {
	if f.Account != "" && e.Account != f.Account {
		return false
	}
	if f.ErrOnly && e.Err == "" {
		return false
	}
	if !f.SinceCutoff.IsZero() && e.Timestamp.Before(f.SinceCutoff) {
		return false
	}
	if f.Substring != "" {
		blob := strings.ToLower(e.Action + " " + e.Kind + " " + e.ID + " " + e.Label + " " + e.Err)
		if !strings.Contains(blob, strings.ToLower(f.Substring)) {
			return false
		}
	}
	return true
}

// Count returns the total number of audit entries on disk. Cheaper than
// Tail when callers only need a size.
func Count() int {
	mu.Lock()
	defer mu.Unlock()
	p, err := Path()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

func Tail(n int) []Entry {
	mu.Lock()
	defer mu.Unlock()
	p, err := Path()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	// Walk lines from end.
	var out []Entry
	start := len(data)
	for start > 0 && len(out) < n {
		end := start
		start--
		for start > 0 && data[start-1] != '\n' {
			start--
		}
		line := data[start:end]
		// Trim trailing \n
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err == nil {
			out = append(out, e)
		}
	}
	return out
}
