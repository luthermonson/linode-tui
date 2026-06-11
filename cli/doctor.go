package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"

	"github.com/luthermonson/linode-tui/config"
	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tui/views"
)

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Check the environment: external tools, config validity, token resolution",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit results as JSON (for CI / scripting)",
			},
			&cli.BoolFlag{
				Name:  "fix",
				Usage: "auto-remove detected stale .tmp files in the cache dir",
			},
			&cli.BoolFlag{
				Name:  "strict",
				Usage: "treat optional warnings as required failures",
			},
			&cli.BoolFlag{
				Name:  "quiet",
				Usage: "suppress success output; only print failures",
			},
			&cli.StringSliceFlag{
				Name:  "section",
				Usage: "run only the named check(s): config, read-only, stale-cache, op, k9s, lazysql, ssh, token (repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  "group",
				Usage: "include only checks tagged with these groups: config, tools, runtime, layout (repeatable)",
			},
			&cli.DurationFlag{
				Name:  "watch",
				Usage: "repeat checks at the given interval (e.g. 5s); ctrl+c to stop",
			},
			&cli.BoolFlag{
				Name:  "no-color",
				Usage: "disable ANSI color (also honored: NO_COLOR env var)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			interval := c.Duration("watch")
			if interval <= 0 {
				return runDoctor(ctx, c, os.Stdout)
			}
			watchCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()
			jsonStream := c.Bool("json")
			for {
				if !jsonStream {
					fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
				}
				if err := runDoctor(watchCtx, c, os.Stdout); err != nil && !jsonStream {
					fmt.Fprintf(os.Stdout, "\n%v\n", err)
				}
				if !jsonStream {
					fmt.Fprintf(os.Stdout, "\n(refreshing every %s — ctrl+c to stop)\n", interval)
				}
				select {
				case <-watchCtx.Done():
					if !jsonStream {
						fmt.Fprintln(os.Stdout, "\nstopped.")
					}
					return nil
				case <-time.After(interval):
				}
			}
		},
	}
}

type checkResult struct {
	name       string
	ok         bool
	optional   bool
	detail     string
	suggestion string
	group      string
}

// doctorGroupByName provides default groupings so call sites can stay terse;
// any result that doesn't set a group inline picks one up from here.
var doctorGroupByName = map[string]string{
	"config":         "config",
	"read-only":      "config",
	"stale-cache":    "runtime",
	"op":             "tools",
	"k9s":            "tools",
	"lazysql":        "tools",
	"ssh":            "tools",
	"token":          "runtime",
	"layout-digests": "layout",
	"refresh":        "runtime",
}

func runDoctor(ctx context.Context, c *cli.Command, out io.Writer) error {
	var results []checkResult

	sections := map[string]bool{}
	for _, s := range c.StringSlice("section") {
		sections[strings.ToLower(strings.TrimSpace(s))] = true
	}

	// Config
	path := c.String("config")
	if path == "" {
		ud, err := os.UserConfigDir()
		if err == nil {
			path = filepath.Join(ud, "linode-tui", "config.yaml")
		}
	}
	cfg, cfgErr := config.Load(path)
	if cfgErr != nil {
		results = append(results, checkResult{name: "config", ok: false, detail: fmt.Sprintf("%s: %v", path, cfgErr)})
	} else {
		results = append(results, checkResult{name: "config", ok: true, detail: path})
	}

	// Read-only mode is informational but surface it so CI / scripts can
	// detect a sticky persisted setting.
	if cfg != nil && cfg.ReadOnly {
		results = append(results, checkResult{
			name:     "read-only",
			ok:       false,
			optional: true,
			detail:   "config.read_only is set — TUI will block mutations (toggle with :read-only)",
		})
	}

	// Stale lock / tmp files in the cache dir — surface as warnings, or
	// remove them when --fix is set.
	if cache, err := os.UserCacheDir(); err == nil {
		stale := findStaleCacheFiles(filepath.Join(cache, "linode-tui"))
		if len(stale) > 0 {
			detail := fmt.Sprintf("%d orphan .tmp file(s): %s", len(stale), strings.Join(stale, ", "))
			if c.Bool("fix") {
				removed := 0
				for _, p := range stale {
					if err := os.Remove(p); err == nil {
						removed++
					}
				}
				detail = fmt.Sprintf("removed %d orphan .tmp file(s)", removed)
				results = append(results, checkResult{name: "stale-cache", ok: true, detail: detail})
			} else {
				results = append(results, checkResult{
					name:     "stale-cache",
					ok:       false,
					optional: true,
					detail:   detail + " (run with --fix to remove)",
				})
			}
		}
	}

	// External binaries — all optional; missing is a warning, not a failure.
	binSuggest := map[string]string{
		"op":      "brew install 1password-cli",
		"k9s":     "brew install k9s  (or :tools upgrade inside the TUI)",
		"lazysql": "brew install jorgerojas26/tap/lazysql  (or :tools upgrade)",
		"ssh":     "install OpenSSH client",
	}
	for _, bin := range []string{"op", "k9s", "lazysql", "ssh"} {
		if p, err := exec.LookPath(bin); err == nil {
			results = append(results, checkResult{name: bin, ok: true, detail: p})
		} else {
			results = append(results, checkResult{
				name:       bin,
				ok:         false,
				optional:   true,
				detail:     "not in PATH (optional)",
				suggestion: binSuggest[bin],
			})
		}
	}

	// Token resolution (best-effort; non-fatal)
	if cfgErr == nil && cfg != nil {
		cfg.ApplyOverrides(config.Overrides{
			Token:   c.String("token"),
			Account: c.String("account"),
		})
		if cfg.DefaultAccount == "" {
			results = append(results, checkResult{
				name:       "token",
				ok:         false,
				detail:     "no active account; set LINODE_TOKEN or configure one",
				suggestion: "export LINODE_TOKEN=…",
			})
		} else {
			tok, err := linode.ResolveTokenForAccount(ctx, cfg, cfg.DefaultAccount)
			switch {
			case err != nil:
				results = append(results, checkResult{name: "token", ok: false, detail: err.Error()})
			case tok == "":
				results = append(results, checkResult{name: "token", ok: false, detail: "resolved to empty"})
			default:
				masked := tok
				if len(masked) > 6 {
					masked = masked[:3] + "…" + masked[len(masked)-3:]
				}
				results = append(results, checkResult{name: "token", ok: true, detail: fmt.Sprintf("account=%s token=%s", cfg.DefaultAccount, masked)})
			}
		}
	}

	// Per-account layout digest drift: account-pinned digest disagrees
	// with the global map for the same name. Mirrors internal/health.
	if cfg != nil && cfg.DefaultAccount != "" {
		if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok {
			var drifts []string
			for name, acctDigest := range acct.LayoutDigests {
				if g, ok := cfg.LayoutDigests[name]; ok && g != acctDigest {
					drifts = append(drifts, name)
				}
			}
			if len(drifts) > 0 {
				sort.Strings(drifts)
				results = append(results, checkResult{
					name:     "layout-digests",
					ok:       false,
					optional: true,
					detail: fmt.Sprintf("active account %q has different digest than global for: %s",
						cfg.DefaultAccount, strings.Join(drifts, ", ")),
					suggestion: "re-run `:layout import-from <url>` to refresh the global pin",
				})
			}
		}
	}

	// Refresh-overrides sanity: warn when an override targets a name that
	// isn't a registered view.
	if cfg != nil && len(cfg.RefreshOverrides) > 0 {
		known := map[string]bool{}
		for _, n := range views.Names() {
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
			results = append(results, checkResult{
				name:     "refresh",
				ok:       false,
				optional: true,
				detail:   "refresh_overrides keys not in registry: " + strings.Join(unknown, ", "),
			})
		} else {
			results = append(results, checkResult{
				name:   "refresh",
				ok:     true,
				detail: fmt.Sprintf("%d override(s) resolve to known views", len(cfg.RefreshOverrides)),
			})
		}
	}

	if len(sections) > 0 {
		filtered := results[:0]
		for _, r := range results {
			if sections[r.name] {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}
	if groups := c.StringSlice("group"); len(groups) > 0 {
		wanted := map[string]bool{}
		for _, g := range groups {
			wanted[g] = true
		}
		filtered := results[:0]
		for _, r := range results {
			g := r.group
			if g == "" {
				g = doctorGroupByName[r.name]
			}
			if g == "" {
				g = "other"
			}
			if wanted[g] {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	required := 0
	for _, r := range results {
		if !r.ok && (!r.optional || c.Bool("strict")) {
			required++
		}
	}

	if c.Bool("json") {
		type jsonRow struct {
			Name       string `json:"name"`
			OK         bool   `json:"ok"`
			Optional   bool   `json:"optional"`
			Detail     string `json:"detail"`
			Suggestion string `json:"suggestion,omitempty"`
		}
		jrows := make([]jsonRow, 0, len(results))
		for _, r := range results {
			jrows = append(jrows, jsonRow{r.name, r.ok, r.optional, r.detail, r.suggestion})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{
			"ok":              required == 0,
			"required_failed": required,
			"checks":          jrows,
		}); err != nil {
			return err
		}
		if required > 0 {
			return fmt.Errorf("%d required check(s) failed", required)
		}
		return nil
	}

	quiet := c.Bool("quiet")
	useColor := stdoutIsTTY() && !c.Bool("json") && !c.Bool("no-color") && os.Getenv("NO_COLOR") == ""
	// Group results so related checks render together with blank-line
	// separators. Untagged rows fall through to "other".
	groupOrder := []string{"config", "tools", "runtime", "layout", "other"}
	byGroup := map[string][]checkResult{}
	for _, r := range results {
		g := r.group
		if g == "" {
			g = doctorGroupByName[r.name]
		}
		if g == "" {
			g = "other"
		}
		byGroup[g] = append(byGroup[g], r)
	}
	firstGroup := true
	for _, g := range groupOrder {
		rows := byGroup[g]
		if len(rows) == 0 {
			continue
		}
		printed := false
		for _, r := range rows {
			mark := "✓"
			color := ""
			switch {
			case r.ok:
				color = "\x1b[32m" // green
				if quiet {
					continue
				}
			case r.optional:
				mark = "~"
				color = "\x1b[33m" // yellow
				if quiet && !c.Bool("strict") {
					continue
				}
			default:
				mark = "✗"
				color = "\x1b[31m" // red
			}
			if !useColor {
				color = ""
			}
			reset := ""
			if useColor {
				reset = "\x1b[0m"
			}
			if !printed && !firstGroup && !quiet {
				fmt.Fprintln(out)
			}
			printed = true
			fmt.Fprintf(out, "%s%s%s  %-10s  %s\n", color, mark, reset, r.name, r.detail)
			if !r.ok && r.suggestion != "" {
				fmt.Fprintf(out, "              → %s\n", r.suggestion)
			}
		}
		if printed {
			firstGroup = false
		}
	}
	if !quiet {
		fmt.Fprintln(out)
	}
	if required == 0 {
		if !quiet {
			fmt.Fprintln(out, "ok — required checks passed.")
		}
		return nil
	}
	if required == 1 {
		return fmt.Errorf("%d required check failed", required)
	}
	return fmt.Errorf("%d required checks failed", required)
}

// findStaleCacheFiles walks the cache dir for orphan .tmp files left behind
// stdoutIsTTY reports whether stdout is a terminal (controls ANSI coloring).
func stdoutIsTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

// by a crashed write. Returns paths suitable for human display.
func findStaleCacheFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tmp") {
			out = append(out, path)
		}
		return nil
	})
	return out
}
