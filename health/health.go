// Package health runs the checks shared by `linode-tui doctor` and the
// in-TUI `:doctor` verb. Keeping the logic in a neutral package avoids a
// cli ↔ tui import cycle.
package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/linode/tui/audit"
	"github.com/linode/tui/cache"
	"github.com/linode/tui/config"
	"github.com/linode/tui/linode"
)

// Result is one health check outcome.
type Result struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Optional bool   `json:"optional"`
	Detail   string `json:"detail"`
	// Suggestion is a short copy-pasteable hint, only set when OK is false
	// and a clear remediation exists ("install op", "set LINODE_TOKEN", …).
	Suggestion string `json:"suggestion,omitempty"`
	// Group buckets results for rendering: "config", "tools", "runtime",
	// "layout". Optional — empty Group renders ungrouped.
	Group string `json:"group,omitempty"`
}

// Options shapes which checks run and how they resolve their state.
type Options struct {
	ConfigPath string
	// KnownViews is consulted by the refresh-overrides sanity check.
	KnownViews []string
	// SkipToken skips the token resolution check (useful when caller has
	// already resolved a token and doesn't want a second op shell-out).
	SkipToken bool
}

// Run executes the standard suite of health checks against cfg. Callers
// can also pass a non-nil cfg to skip the load step; otherwise opts.ConfigPath
// is consulted.
func Run(ctx context.Context, cfg *config.Config, opts Options) []Result {
	var out []Result

	if cfg != nil {
		out = append(out, Result{Name: "config", OK: true, Detail: cfg.Path(), Group: "config"})
	} else {
		c, err := config.Load(opts.ConfigPath)
		if err != nil {
			out = append(out, Result{Name: "config", Detail: fmt.Sprintf("%s: %v", opts.ConfigPath, err), Group: "config"})
		} else {
			cfg = c
			out = append(out, Result{Name: "config", OK: true, Detail: cfg.Path(), Group: "config"})
		}
	}

	// Audit log size: warn at >80% of the 2 MB rotation threshold so users
	// know rotation is imminent (and history is about to be lost).
	if p, err := audit.Path(); err == nil {
		if info, err := os.Stat(p); err == nil {
			const rotateAt = 2 * 1024 * 1024
			const warnAt = rotateAt * 8 / 10 // 1.6 MB
			switch {
			case info.Size() >= warnAt:
				out = append(out, Result{
					Name:       "audit-log",
					Optional:   true,
					Detail:     fmt.Sprintf("%d bytes (>%dB; rotates at 2MiB)", info.Size(), warnAt),
					Suggestion: "linode-tui audit purge --older-than 720h  (or :audit clear in the TUI)",
				})
			default:
				out = append(out, Result{
					Name:   "audit-log",
					OK:     true,
					Detail: fmt.Sprintf("%d bytes", info.Size()),
				})
			}
		}
	}

	// Cache writability — we drop layout caches, debug logs, audit trail
	// here. A read-only FS makes a lot of UX silently lossy.
	if udir, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(udir, "linode-tui")
		switch err := checkWritable(dir); {
		case err != nil:
			out = append(out, Result{Name: "cache", Optional: true, Detail: dir + ": " + err.Error()})
		default:
			total, _ := cache.Total(dir)
			out = append(out, Result{Name: "cache", OK: true, Detail: fmt.Sprintf("%s (%s)", dir, cache.FormatBytes(total))})
		}
	}

	if cfg != nil && cfg.ReadOnly {
		out = append(out, Result{
			Name:     "read-only",
			Optional: true,
			Detail:   "config.read_only is set — TUI will block mutations (toggle with :read-only)",
		})
	}

	bins := []string{"op", "k9s", "lazysql", "ssh"}
	binSuggestions := map[string]string{
		"op":      "brew install 1password-cli  (or download from 1password.com/downloads/command-line)",
		"k9s":     "brew install k9s  (or :tools upgrade to fetch the pinned release)",
		"lazysql": ":tools upgrade  (or brew install jorgerojas26/tap/lazysql)",
		"ssh":     "install OpenSSH client",
	}
	binResults := make([]Result, len(bins))
	var wg sync.WaitGroup
	for i, bin := range bins {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			if p, err := exec.LookPath(name); err == nil {
				binResults[idx] = Result{Name: name, OK: true, Detail: p}
			} else {
				binResults[idx] = Result{
					Name: name, Optional: true,
					Detail:     "not in PATH (optional)",
					Suggestion: binSuggestions[name],
				}
			}
		}(i, bin)
	}
	wg.Wait()
	out = append(out, binResults...)

	if !opts.SkipToken && cfg != nil {
		if cfg.DefaultAccount == "" {
			out = append(out, Result{
				Name:       "token",
				Detail:     "no active account; set LINODE_TOKEN or configure one",
				Suggestion: "export LINODE_TOKEN=… (or add an account to ~/.config/linode-tui/config.yaml)",
			})
		} else {
			tok, err := linode.ResolveTokenForAccount(ctx, cfg, cfg.DefaultAccount)
			switch {
			case err != nil:
				detail := err.Error()
				// If this account depends on 1Password but `op` is missing,
				// the underlying error is hard to read. Surface the actual
				// cause so users know which dependency to install.
				if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok && acct.OPRef != "" {
					if _, lookErr := exec.LookPath("op"); lookErr != nil {
						detail = fmt.Sprintf("account %s uses 1Password (op_ref=%s) but `op` is not in PATH — install 1Password CLI or set Account.Token instead", cfg.DefaultAccount, acct.OPRef)
					}
				}
				out = append(out, Result{Name: "token", Detail: detail})
			case tok == "":
				out = append(out, Result{Name: "token", Detail: "resolved to empty"})
			default:
				masked := tok
				if len(masked) > 6 {
					masked = masked[:3] + "…" + masked[len(masked)-3:]
				}
				out = append(out, Result{Name: "token", OK: true, Detail: fmt.Sprintf("account=%s token=%s", cfg.DefaultAccount, masked)})
			}
		}
	}

	// Per-account layout digest drift: compare each known layout's effective
	// digest against the active account's stored digest. Distinguishes
	// drift (both sides exist, differ) from divergence (only one side set).
	if cfg != nil && cfg.DefaultAccount != "" {
		if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok {
			seen := map[string]bool{}
			for n := range acct.LayoutDigests {
				seen[n] = true
			}
			for n := range cfg.LayoutDigests {
				seen[n] = true
			}
			var drifts, accountOnly, globalOnly []string
			for name := range seen {
				a := acct.LayoutDigests[name]
				g := cfg.LayoutDigests[name]
				switch {
				case a != "" && g != "" && a != g:
					drifts = append(drifts, name)
				case a != "" && g == "":
					accountOnly = append(accountOnly, name)
				case a == "" && g != "":
					globalOnly = append(globalOnly, name)
				}
			}
			sort.Strings(drifts)
			sort.Strings(accountOnly)
			sort.Strings(globalOnly)
			switch {
			case len(drifts) > 0:
				out = append(out, Result{
					Name:     "layout-digests",
					Optional: true,
					Detail: fmt.Sprintf("active account %q digest differs from global for: %s",
						cfg.DefaultAccount, strings.Join(drifts, ", ")),
					Suggestion: "re-run `:layout import-from <url>` to refresh the global pin",
				})
			case len(accountOnly) > 0 || len(globalOnly) > 0:
				detail := ""
				if len(accountOnly) > 0 {
					detail = "only in account: " + strings.Join(accountOnly, ", ")
				}
				if len(globalOnly) > 0 {
					if detail != "" {
						detail += " · "
					}
					detail += "only global: " + strings.Join(globalOnly, ", ")
				}
				out = append(out, Result{
					Name:     "layout-digests",
					Optional: true,
					Detail:   detail,
				})
			}
		}
	}

	// Bookmark split-brain: both global Bookmarks and active account's
	// Bookmarks are non-empty. The active layer wins, but the global set
	// is then invisible until the user runs `:bookmark migrate` or flips
	// scope. Surface it so the dormant set isn't a surprise.
	if cfg != nil && cfg.DefaultAccount != "" {
		if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok &&
			len(acct.Bookmarks) > 0 && len(cfg.Bookmarks) > 0 {
			out = append(out, Result{
				Name:       "bookmark-scope",
				Optional:   true,
				Detail:     fmt.Sprintf("global has %d kind(s), account %q has %d (account wins; global is shadowed)", len(cfg.Bookmarks), cfg.DefaultAccount, len(acct.Bookmarks)),
				Suggestion: "run `:bookmark migrate` (or `linode-tui bookmark migrate`) to consolidate",
				Group:      "runtime",
			})
		}
	}

	// Refresh interval default: cfg.Refresh==0 silently picks up the 2s
	// fallback in listView.tick. Surface this so config tinkerers know
	// the actual interval in use.
	if cfg != nil && cfg.Refresh == 0 {
		out = append(out, Result{
			Name:     "refresh-default",
			Optional: true,
			Detail:   "cfg.refresh is 0; views fall back to 2s",
			Group:    "runtime",
		})
	}

	// Refresh-override collisions: when the active account sets the same
	// view as the global map but with a different duration, the account
	// wins silently. Surface this so the duplicate isn't a surprise.
	if cfg != nil && cfg.DefaultAccount != "" {
		if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok {
			var collisions []string
			for name, acctDur := range acct.RefreshOverrides {
				if gDur, ok := cfg.RefreshOverrides[name]; ok && gDur != acctDur {
					collisions = append(collisions,
						fmt.Sprintf("%s: account=%s global=%s (account wins)", name, acctDur, gDur))
				}
			}
			if len(collisions) > 0 {
				sort.Strings(collisions)
				out = append(out, Result{
					Name:     "refresh-collision",
					Optional: true,
					Detail:   strings.Join(collisions, " · "),
				})
			}
		}
	}

	if cfg != nil && len(cfg.RefreshOverrides) > 0 {
		known := map[string]bool{}
		for _, n := range opts.KnownViews {
			known[n] = true
		}
		var unknown []string
		for name := range cfg.RefreshOverrides {
			if !known[name] {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			out = append(out, Result{
				Name:     "refresh",
				Optional: true,
				Detail:   "refresh_overrides keys not in registry: " + strings.Join(unknown, ", "),
			})
		} else {
			out = append(out, Result{
				Name:   "refresh",
				OK:     true,
				Detail: fmt.Sprintf("%d override(s) resolve to known views", len(cfg.RefreshOverrides)),
			})
		}
	}

	// Tag groups for any Result that didn't set one inline.
	groupByName := map[string]string{
		"config":    "config",
		"read-only": "config",
		"op":        "tools",
		"k9s":       "tools",
		"lazysql":   "tools",
		"ssh":       "tools",
		"token":     "runtime",
		"cache":     "runtime",
		"audit-log": "runtime",
		"refresh":           "runtime",
		"refresh-collision": "runtime",

		"layout-digests": "layout",
	}
	for i := range out {
		if out[i].Group == "" {
			if g, ok := groupByName[out[i].Name]; ok {
				out[i].Group = g
			}
		}
	}
	return out
}

func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".healthcheck-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
