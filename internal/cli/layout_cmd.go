package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/linode/tui/internal/audit"
	"github.com/linode/tui/internal/config"
)

func layoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "layout",
		Usage: "Inspect or pipe saved TUI layouts",
		Commands: []*cli.Command{
			{
				Name:      "cat",
				Usage:     "Print a saved layout YAML to stdout",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "config",
						Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
					},
					&cli.BoolFlag{
						Name:  "json",
						Usage: "emit as JSON instead of YAML",
					},
					&cli.BoolFlag{
						Name:  "raw",
						Usage: "emit only the layout body (no layout_version/name wrapper)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui layout cat <name>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					name := c.Args().First()
					l, ok := cfg.Layouts[name]
					if !ok {
						return fmt.Errorf("no such layout %q", name)
					}
					if c.Bool("raw") {
						if c.Bool("json") {
							enc := json.NewEncoder(os.Stdout)
							enc.SetIndent("", "  ")
							return enc.Encode(l)
						}
						return yaml.NewEncoder(os.Stdout).Encode(l)
					}
					doc := struct {
						LayoutVersion int                `yaml:"layout_version" json:"layout_version"`
						Name          string             `yaml:"name" json:"name"`
						Layout        config.NamedLayout `yaml:"layout" json:"layout"`
					}{LayoutVersion: 1, Name: name, Layout: l}
					if c.Bool("json") {
						enc := json.NewEncoder(os.Stdout)
						enc.SetIndent("", "  ")
						return enc.Encode(doc)
					}
					return yaml.NewEncoder(os.Stdout).Encode(doc)
				},
			},
			{
				Name:  "list",
				Usage: "List saved layout names",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					for name := range cfg.Layouts {
						fmt.Println(name)
					}
					return nil
				},
			},
			{
				Name:      "import-all",
				Usage:     "Load every *.yaml from a directory into saved layouts",
				ArgsUsage: "<dir>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
					&cli.BoolFlag{Name: "overwrite", Usage: "overwrite existing layout names (default: skip)"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui layout import-all <dir>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					dir := c.Args().First()
					entries, err := os.ReadDir(dir)
					if err != nil {
						return err
					}
					if cfg.Layouts == nil {
						cfg.Layouts = map[string]config.NamedLayout{}
					}
					overwrite := c.Bool("overwrite")
					var imported, skipped int
					for _, e := range entries {
						if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
							continue
						}
						data, err := os.ReadFile(filepath.Join(dir, e.Name()))
						if err != nil {
							return err
						}
						var doc struct {
							LayoutVersion int                `yaml:"layout_version"`
							Name          string             `yaml:"name"`
							Layout        config.NamedLayout `yaml:"layout"`
						}
						if err := yaml.Unmarshal(data, &doc); err != nil {
							return fmt.Errorf("parse %s: %w", e.Name(), err)
						}
						if doc.LayoutVersion > 1 {
							return fmt.Errorf("%s: layout_version %d > 1", e.Name(), doc.LayoutVersion)
						}
						name := doc.Name
						if name == "" {
							name = strings.TrimSuffix(e.Name(), ".yaml")
						}
						if _, exists := cfg.Layouts[name]; exists && !overwrite {
							skipped++
							continue
						}
						cfg.Layouts[name] = doc.Layout
						imported++
					}
					if err := cfg.Save(); err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "imported %d layout(s), skipped %d (use --overwrite to replace)\n", imported, skipped)
					return nil
				},
			},
			{
				Name:      "export-all",
				Usage:     "Dump every saved layout to a directory (one YAML per layout)",
				ArgsUsage: "<dir>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui layout export-all <dir>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					dir := c.Args().First()
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return err
					}
					for name, l := range cfg.Layouts {
						data, err := yaml.Marshal(struct {
							LayoutVersion int                `yaml:"layout_version"`
							Name          string             `yaml:"name"`
							Layout        config.NamedLayout `yaml:"layout"`
						}{LayoutVersion: 1, Name: name, Layout: l})
						if err != nil {
							return err
						}
						if err := os.WriteFile(filepath.Join(dir, name+".yaml"), data, 0o644); err != nil {
							return err
						}
					}
					fmt.Fprintf(os.Stdout, "wrote %d layout(s) to %s\n", len(cfg.Layouts), dir)
					return nil
				},
			},
			{
				Name:      "rename",
				Usage:     "Rename a saved layout",
				ArgsUsage: "<old> <new>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 2 {
						return fmt.Errorf("usage: linode-tui layout rename <old> <new>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					oldName := c.Args().Get(0)
					newName := c.Args().Get(1)
					l, ok := cfg.Layouts[oldName]
					if !ok {
						return fmt.Errorf("no such layout %q", oldName)
					}
					if _, exists := cfg.Layouts[newName]; exists {
						return fmt.Errorf("layout %q already exists", newName)
					}
					delete(cfg.Layouts, oldName)
					cfg.Layouts[newName] = l
					if d, ok := cfg.LayoutDigests[oldName]; ok {
						if cfg.LayoutDigests == nil {
							cfg.LayoutDigests = map[string]string{}
						}
						cfg.LayoutDigests[newName] = d
						delete(cfg.LayoutDigests, oldName)
					}
					if err := cfg.Save(); err != nil {
						return err
					}
					audit.Append(audit.Entry{
						Action:  "layout-rename",
						Kind:    "layout",
						Account: cfg.DefaultAccount,
						ID:      oldName,
						Label:   newName,
					})
					fmt.Fprintf(os.Stdout, "renamed %s → %s\n", oldName, newName)
					return nil
				},
			},
			{
				Name:      "delete",
				Aliases:   []string{"rm"},
				Usage:     "Delete a saved layout",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui layout delete <name>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					name := c.Args().First()
					if _, ok := cfg.Layouts[name]; !ok {
						return fmt.Errorf("no such layout %q", name)
					}
					delete(cfg.Layouts, name)
					delete(cfg.LayoutDigests, name)
					if err := cfg.Save(); err != nil {
						return err
					}
					audit.Append(audit.Entry{
						Action:  "layout-delete",
						Kind:    "layout",
						Account: cfg.DefaultAccount,
						ID:      name,
					})
					fmt.Fprintf(os.Stdout, "deleted layout %q\n", name)
					return nil
				},
			},
			{
				Name:      "fingerprint",
				Usage:     "Print the sha256 of a saved layout's canonical YAML",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui layout fingerprint <name>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					name := c.Args().First()
					l, ok := cfg.Layouts[name]
					if !ok {
						return fmt.Errorf("no such layout %q", name)
					}
					data, err := yaml.Marshal(struct {
						LayoutVersion int                `yaml:"layout_version"`
						Name          string             `yaml:"name"`
						Layout        config.NamedLayout `yaml:"layout"`
					}{LayoutVersion: 1, Name: name, Layout: l})
					if err != nil {
						return err
					}
					sum := sha256.Sum256(data)
					fmt.Fprintln(os.Stdout, hex.EncodeToString(sum[:]))
					return nil
				},
			},
			{
				Name:      "pin",
				Usage:     "Print an import-from URL with the layout's sha256 appended",
				ArgsUsage: "<name> <base-url>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 2 {
						return fmt.Errorf("usage: linode-tui layout pin <name> <base-url>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					name := c.Args().Get(0)
					baseURL := c.Args().Get(1)
					l, ok := cfg.Layouts[name]
					if !ok {
						return fmt.Errorf("no such layout %q", name)
					}
					digest := cfg.ActiveLayoutDigest(name)
					if digest == "" {
						data, err := yaml.Marshal(struct {
							LayoutVersion int                `yaml:"layout_version"`
							Name          string             `yaml:"name"`
							Layout        config.NamedLayout `yaml:"layout"`
						}{LayoutVersion: 1, Name: name, Layout: l})
						if err != nil {
							return err
						}
						sum := sha256.Sum256(data)
						digest = hex.EncodeToString(sum[:])
					}
					parsed, err := neturl.Parse(baseURL)
					if err != nil {
						return fmt.Errorf("invalid url: %w", err)
					}
					q := parsed.Query()
					q.Set("sha256", digest)
					parsed.RawQuery = q.Encode()
					fmt.Fprintln(os.Stdout, parsed.String())
					return nil
				},
			},
			{
				Name:      "diff",
				Usage:     "Show pane-by-pane differences between two saved layouts",
				ArgsUsage: "<a> <b>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 2 {
						return fmt.Errorf("usage: linode-tui layout diff <a> <b>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					a, ok := cfg.Layouts[c.Args().Get(0)]
					if !ok {
						return fmt.Errorf("no such layout %q", c.Args().Get(0))
					}
					b, ok := cfg.Layouts[c.Args().Get(1)]
					if !ok {
						return fmt.Errorf("no such layout %q", c.Args().Get(1))
					}
					diffs := diffLayouts(a, b)
					if len(diffs) == 0 {
						fmt.Fprintf(os.Stdout, "%s == %s (identical)\n", c.Args().Get(0), c.Args().Get(1))
						return nil
					}
					fmt.Fprintf(os.Stdout, "%s → %s\n", c.Args().Get(0), c.Args().Get(1))
					for _, d := range diffs {
						fmt.Fprintf(os.Stdout, "  %-11s  %-20s → %s\n", d.field, d.a, d.b)
					}
					return nil
				},
			},
			{
				Name:      "import-from",
				Usage:     "Fetch a layout YAML over HTTP(S) and save it locally",
				ArgsUsage: "<url> [name]",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui layout import-from <url> [name]")
					}
					url := c.Args().First()
					return runLayoutImportFrom(ctx, c, url, c.Args().Get(1))
				},
			},
		},
	}
}

type layoutDiff struct {
	field, a, b string
}

func diffLayouts(a, b config.NamedLayout) []layoutDiff {
	var out []layoutDiff
	cmp := func(field, av, bv string) {
		if av != bv {
			out = append(out, layoutDiff{field: field, a: emptyDash(av), b: emptyDash(bv)})
		}
	}
	cmp("primary", a.Primary, b.Primary)
	cmp("secondary", a.Secondary, b.Secondary)
	cmp("tertiary", a.Tertiary, b.Tertiary)
	cmp("quaternary", a.Quaternary, b.Quaternary)
	if a.Ratio != b.Ratio {
		out = append(out, layoutDiff{field: "ratio", a: fmt.Sprintf("%g", a.Ratio), b: fmt.Sprintf("%g", b.Ratio)})
	}
	if a.QuatRatio != b.QuatRatio {
		out = append(out, layoutDiff{field: "quat_ratio", a: fmt.Sprintf("%g", a.QuatRatio), b: fmt.Sprintf("%g", b.QuatRatio)})
	}
	return out
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func runLayoutImportFrom(ctx context.Context, c *cli.Command, url, override string) error {
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return err
	}

	parsed, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	expectedSum := parsed.Query().Get("sha256")

	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	gotSum := sha256.Sum256(data)
	gotDigest := hex.EncodeToString(gotSum[:])
	if expectedSum != "" && gotDigest != expectedSum {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", gotDigest, expectedSum)
	}

	var doc struct {
		LayoutVersion int                `yaml:"layout_version"`
		Name          string             `yaml:"name"`
		Layout        config.NamedLayout `yaml:"layout"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if doc.LayoutVersion > 1 {
		return fmt.Errorf("file uses layout_version %d; this build understands 1", doc.LayoutVersion)
	}
	name := doc.Name
	if override != "" {
		name = override
	}
	if name == "" {
		return fmt.Errorf("missing name (set in file or pass as 2nd arg)")
	}

	// Cache the raw download for offline reuse.
	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "linode-tui", "layouts")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(dir, name+".yaml"), data, 0o644)
		}
	}

	if cfg.Layouts == nil {
		cfg.Layouts = map[string]config.NamedLayout{}
	}
	cfg.Layouts[name] = doc.Layout

	// Hash-pin: surface drift when re-fetching an unpinned URL.
	if expectedSum == "" {
		if prev := cfg.ActiveLayoutDigest(name); prev != "" && prev != gotDigest {
			fmt.Fprintf(os.Stderr,
				"warning: layout %q digest changed since last fetch (%s → %s); pin in URL with ?sha256=%s to enforce\n",
				name, prev, gotDigest, gotDigest)
		}
	}
	cfg.RecordLayoutDigest(name, gotDigest)

	if err := cfg.Save(); err != nil {
		return err
	}
	audit.Append(audit.Entry{
		Action:  "layout-import-from",
		Kind:    "layout",
		Account: cfg.DefaultAccount,
		ID:      name,
		Label:   url,
	})
	fmt.Fprintf(os.Stdout, "imported layout %q from %s\n", name, url)
	return nil
}
