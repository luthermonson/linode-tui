package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/linode/tui/config"
	"github.com/linode/tui/linode"
	"github.com/linode/tui/tui"
)

func NewApp(version, commit string) *cli.Command {
	return &cli.Command{
		Name:                  "linode-tui",
		Usage:                 "k9s-style TUI for the Linode API",
		Version:               version,
		EnableShellCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "token",
				Usage:   "Linode API token (skips account token / op_ref resolution from config)",
				Sources: cli.EnvVars("LINODE_TOKEN"),
			},
			&cli.StringFlag{
				Name:  "account",
				Usage: "named account from config to use",
			},
			&cli.DurationFlag{
				Name:  "refresh",
				Usage: "auto-refresh interval for resource views",
				Value: 2 * time.Second,
			},
			&cli.StringFlag{
				Name:  "theme",
				Usage: "theme name: dark | light | dracula | solarized-light",
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "verbose logging to ~/.cache/linode-tui/debug.log",
			},
			&cli.StringFlag{
				Name:  "view",
				Usage: "open directly into a resource view (e.g. linodes, volumes, lke)",
			},
			&cli.StringFlag{
				Name:  "focus",
				Usage: "focus a specific resource id in the --view (e.g. --view linodes --focus 12345)",
			},
			&cli.StringFlag{
				Name:  "layout",
				Usage: "saved-layout name to load (default: 'default' if it exists)",
			},
			&cli.StringFlag{
				Name:  "layout-file",
				Usage: "apply an ad-hoc layout YAML file without saving it to config",
			},
			&cli.BoolFlag{
				Name:  "no-layout",
				Usage: "skip the implicit default-layout load",
			},
			&cli.StringSliceFlag{
				Name:  "pane",
				Usage: "ad-hoc pane assignment, e.g. --pane primary=instances --pane secondary=events (slots: primary|secondary|tertiary|quaternary; repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  "ratio",
				Usage: "set split ratios, e.g. --ratio primary=0.5 --ratio quat=0.3 (repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  "refresh-view",
				Usage: "per-view refresh override, e.g. --refresh-view events=5s (repeatable)",
			},
			&cli.BoolFlag{
				Name:  "read-only",
				Usage: "disable every mutating action for this session (creates, deletes, configure, install)",
			},
			&cli.StringFlag{
				Name:  "fold-char",
				Usage: "one-shot override of config.fold_char (default '+')",
			},
			&cli.BoolFlag{
				Name:  "print-only",
				Usage: "validate layout + token resolution then exit (CI smoke)",
			},
		},
		Action: runTUI,
		Commands: []*cli.Command{
			versionCommand(version, commit),
			doctorCommand(),
			installCompletionCommand(),
		},
	}
}

func runTUI(ctx context.Context, c *cli.Command) error {
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.ApplyOverrides(config.Overrides{
		Token:   c.String("token"),
		Account: c.String("account"),
		Refresh: c.Duration("refresh"),
		Theme:   c.String("theme"),
		Debug:   c.Bool("debug"),
	})

	var primary string
	loadLayout := func(l config.NamedLayout) {
		cfg.LastSplit = config.SplitState{
			View:    l.Secondary,
			Ratio:   l.Ratio,
			Right:   l.Tertiary,
			Down:    l.Quaternary,
			Focused: l.Primary,
		}
		primary = l.Primary
	}

	switch {
	case c.String("layout-file") != "":
		data, err := os.ReadFile(c.String("layout-file"))
		if err != nil {
			return fmt.Errorf("read layout-file: %w", err)
		}
		var doc struct {
			Layout config.NamedLayout `yaml:"layout"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse layout-file: %w", err)
		}
		loadLayout(doc.Layout)
		fmt.Fprintf(os.Stderr, "loaded ad-hoc layout from %s\n", c.String("layout-file"))
	case len(c.StringSlice("pane")) > 0:
		var l config.NamedLayout
		for _, pair := range c.StringSlice("pane") {
			slot, view, ok := strings.Cut(pair, "=")
			if !ok || slot == "" || view == "" {
				return fmt.Errorf("invalid --pane %q (want slot=view, e.g. primary=instances)", pair)
			}
			switch strings.ToLower(strings.TrimSpace(slot)) {
			case "primary", "p":
				l.Primary = view
			case "secondary", "s":
				l.Secondary = view
			case "tertiary", "t":
				l.Tertiary = view
			case "quaternary", "q":
				l.Quaternary = view
			default:
				return fmt.Errorf("invalid --pane slot %q (want primary|secondary|tertiary|quaternary)", slot)
			}
		}
		if l.Primary == "" {
			return fmt.Errorf("--pane requires at least primary=<view>")
		}
		loadLayout(l)
		fmt.Fprintf(os.Stderr, "loaded ad-hoc layout from --pane flags\n")
	case c.Bool("no-layout"):
		cfg.LastSplit = config.SplitState{}
	default:
		name := c.String("layout")
		if name == "" {
			name = "default"
		}
		if l, ok := cfg.Layouts[name]; ok {
			loadLayout(l)
			fmt.Fprintf(os.Stderr, "loaded layout %q\n", name)
		} else if c.IsSet("layout") {
			return fmt.Errorf("no such layout %q (saved layouts: see :layout list)", name)
		}
	}

	if v := c.String("fold-char"); v != "" {
		cfg.FoldChar = v
	}
	for _, pair := range c.StringSlice("ratio") {
		slot, val, ok := strings.Cut(pair, "=")
		if !ok || slot == "" || val == "" {
			return fmt.Errorf("invalid --ratio %q (want primary=0.5 or quat=0.3)", pair)
		}
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("invalid --ratio %q: %w", pair, err)
		}
		if f <= 0 || f >= 1 {
			return fmt.Errorf("invalid --ratio %q: must be in (0, 1)", pair)
		}
		switch strings.ToLower(strings.TrimSpace(slot)) {
		case "primary", "p":
			cfg.LastSplit.Ratio = f
		case "quat", "quaternary", "q":
			cfg.LastSplit.QuatRatio = f
		default:
			return fmt.Errorf("invalid --ratio slot %q (want primary|quat)", slot)
		}
	}
	for _, pair := range c.StringSlice("refresh-view") {
		name, dur, ok := strings.Cut(pair, "=")
		if !ok || name == "" || dur == "" {
			return fmt.Errorf("invalid --refresh-view %q (want name=duration, e.g. events=5s)", pair)
		}
		d, err := time.ParseDuration(dur)
		if err != nil {
			return fmt.Errorf("invalid --refresh-view %q: %w", pair, err)
		}
		if cfg.RefreshOverrides == nil {
			cfg.RefreshOverrides = map[string]time.Duration{}
		}
		cfg.RefreshOverrides[strings.ToLower(strings.TrimSpace(name))] = d
	}

	// --view overrides the layout's primary pane.
	var initialCtx map[string]any
	if v := c.String("view"); v != "" {
		primary = v
		if id := c.String("focus"); id != "" {
			initialCtx = map[string]any{"focus_id": id}
		}
	} else if c.String("focus") != "" {
		return fmt.Errorf("--focus requires --view")
	}

	token, err := linode.ResolveToken(ctx, cfg)
	if err != nil {
		if imported := maybeImportLinodeCLI(cfg); imported {
			token, err = linode.ResolveToken(ctx, cfg)
		}
		if err != nil {
			return err
		}
	}

	if c.Bool("print-only") {
		fmt.Fprintf(os.Stderr, "ok: account=%s primary=%s secondary=%s tertiary=%s quaternary=%s read_only=%v\n",
			cfg.DefaultAccount, primary,
			cfg.LastSplit.View, cfg.LastSplit.Right, cfg.LastSplit.Down,
			c.Bool("read-only"))
		return nil
	}
	client, err := linode.NewClient(token)
	if err != nil {
		return fmt.Errorf("init linode client: %w", err)
	}
	return tui.RunFull(ctx, cfg, client, primary, initialCtx, c.Bool("read-only"))
}

func versionCommand(version, commit string) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print version info",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Printf("linode-tui %s (%s)\n", version, commit)
			return nil
		},
	}
}
