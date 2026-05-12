package cli

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/tui/theme"
)

func defaultsCommand() *cli.Command {
	return &cli.Command{
		Name:  "defaults",
		Usage: "Apply opinionated config presets without launching the TUI",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Usage: "path to config file"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "config:           %s\n", cfg.Path())
			active := cfg.ActiveTheme
			if active == "" {
				active = "(unset; falls back to dark)"
			}
			fmt.Fprintf(os.Stdout, "active_theme:     %s\n", active)
			refresh := cfg.Refresh
			if refresh == 0 {
				fmt.Fprintln(os.Stdout, "refresh:          (unset; 2s default)")
			} else {
				fmt.Fprintf(os.Stdout, "refresh:          %s\n", refresh)
			}
			fmt.Fprintf(os.Stdout, "refresh_overrides: %d\n", len(cfg.RefreshOverrides))
			fmt.Fprintf(os.Stdout, "saved_layouts:    %d\n", len(cfg.Layouts))
			fmt.Fprintln(os.Stdout, "\nsubcommands: theme <name>  |  refresh [--dry-run]")
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:      "theme",
				Usage:     "Set the global active theme (dark | light | dracula | solarized-light)",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui defaults theme <name>")
					}
					name := c.Args().First()
					if _, ok := theme.ByName(name); !ok {
						return fmt.Errorf("unknown theme %q (try one of: %v)", name, theme.Names())
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					cfg.ActiveTheme = name
					if err := cfg.Save(); err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "active_theme = %s (in %s)\n", name, cfg.Path())
					return nil
				},
			},
			{
				Name:  "refresh",
				Usage: "Write the per-view refresh preset to config (events=2s, instances=5s, images=60s, …)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
					&cli.BoolFlag{Name: "dry-run", Usage: "print the preset without writing"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					preset := config.RefreshDefaults()
					if c.Bool("dry-run") {
						names := make([]string, 0, len(preset))
						for n := range preset {
							names = append(names, n)
						}
						sort.Strings(names)
						for _, n := range names {
							fmt.Fprintf(os.Stdout, "%s: %s\n", n, preset[n])
						}
						return nil
					}
					cfg.RefreshOverrides = preset
					if err := cfg.Save(); err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "wrote %d refresh override(s) to %s\n", len(preset), cfg.Path())
					return nil
				},
			},
		},
	}
}
