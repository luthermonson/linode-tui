package cli

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/linode/tui/internal/config"
)

func bookmarkCommand() *cli.Command {
	return &cli.Command{
		Name:  "bookmark",
		Usage: "Inspect / export / import the local bookmark set",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "Print counts per resource kind",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					if len(cfg.Bookmarks) == 0 {
						fmt.Fprintln(os.Stdout, "(no bookmarks)")
						return nil
					}
					kinds := make([]string, 0, len(cfg.Bookmarks))
					for k := range cfg.Bookmarks {
						kinds = append(kinds, k)
					}
					sort.Strings(kinds)
					for _, k := range kinds {
						fmt.Fprintf(os.Stdout, "%-16s %d\n", k, len(cfg.Bookmarks[k]))
					}
					return nil
				},
			},
			{
				Name:      "export",
				Usage:     "Dump the bookmark map to a YAML file",
				ArgsUsage: "<path>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui bookmark export <path>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					data, err := yaml.Marshal(struct {
						Version   int                 `yaml:"version"`
						Bookmarks map[string][]string `yaml:"bookmarks"`
					}{Version: 1, Bookmarks: cfg.Bookmarks})
					if err != nil {
						return err
					}
					path := c.Args().First()
					if err := os.WriteFile(path, data, 0o644); err != nil {
						return err
					}
					n := 0
					for _, ids := range cfg.Bookmarks {
						n += len(ids)
					}
					fmt.Fprintf(os.Stdout, "exported %d bookmark(s) → %s\n", n, path)
					return nil
				},
			},
			{
				Name:      "scope",
				Usage:     "Show or switch bookmark scope (global | account)",
				ArgsUsage: "[global|account]",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					cur := "global"
					if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok && acct.Bookmarks != nil {
						cur = "account=" + cfg.DefaultAccount
					}
					if c.NArg() == 0 {
						fmt.Fprintln(os.Stdout, cur)
						return nil
					}
					target := c.Args().First()
					switch target {
					case "account":
						if cfg.DefaultAccount == "" {
							return fmt.Errorf("no active account; set default_account first")
						}
						acct, ok := cfg.Accounts[cfg.DefaultAccount]
						if !ok {
							return fmt.Errorf("active account %q not in accounts", cfg.DefaultAccount)
						}
						if acct.Bookmarks == nil {
							acct.Bookmarks = map[string][]string{}
							cfg.Accounts[cfg.DefaultAccount] = acct
							if err := cfg.Save(); err != nil {
								return err
							}
						}
						fmt.Fprintf(os.Stdout, "bookmark scope = account %s\n", cfg.DefaultAccount)
						return nil
					case "global":
						if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok && acct.Bookmarks != nil {
							acct.Bookmarks = nil
							cfg.Accounts[cfg.DefaultAccount] = acct
							if err := cfg.Save(); err != nil {
								return err
							}
						}
						fmt.Fprintln(os.Stdout, "bookmark scope = global")
						return nil
					default:
						return fmt.Errorf("expected global|account, got %q", target)
					}
				},
			},
			{
				Name:  "migrate",
				Usage: "Move global bookmarks into the active account's slot",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
					&cli.StringFlag{Name: "account", Usage: "target account (default: cfg.default_account)"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					account := c.String("account")
					if account == "" {
						account = cfg.DefaultAccount
					}
					if account == "" {
						return fmt.Errorf("no active account; pass --account <name>")
					}
					acct, ok := cfg.Accounts[account]
					if !ok {
						return fmt.Errorf("no such account %q", account)
					}
					if len(cfg.Bookmarks) == 0 {
						fmt.Fprintln(os.Stdout, "no global bookmarks to move")
						return nil
					}
					if acct.Bookmarks == nil {
						acct.Bookmarks = map[string][]string{}
					}
					moved := 0
					for kind, ids := range cfg.Bookmarks {
						existing := map[string]bool{}
						for _, id := range acct.Bookmarks[kind] {
							existing[id] = true
						}
						for _, id := range ids {
							if !existing[id] {
								acct.Bookmarks[kind] = append(acct.Bookmarks[kind], id)
								existing[id] = true
								moved++
							}
						}
					}
					cfg.Accounts[account] = acct
					cfg.Bookmarks = nil
					if err := cfg.Save(); err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "moved %d bookmark(s) into account %s\n", moved, account)
					return nil
				},
			},
			{
				Name:      "import",
				Usage:     "Read a bookmark YAML and write to config",
				ArgsUsage: "<path>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "path to config file"},
					&cli.BoolFlag{Name: "merge", Usage: "union with existing bookmarks (default: replace)"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui bookmark import <path>")
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					data, err := os.ReadFile(c.Args().First())
					if err != nil {
						return err
					}
					var doc struct {
						Version   int                 `yaml:"version"`
						Bookmarks map[string][]string `yaml:"bookmarks"`
					}
					if err := yaml.Unmarshal(data, &doc); err != nil {
						return fmt.Errorf("parse: %w", err)
					}
					if doc.Version > 1 {
						return fmt.Errorf("file version %d > 1", doc.Version)
					}
					merge := c.Bool("merge")
					if !merge || cfg.Bookmarks == nil {
						cfg.Bookmarks = map[string][]string{}
					}
					added := 0
					for kind, ids := range doc.Bookmarks {
						existing := map[string]bool{}
						for _, id := range cfg.Bookmarks[kind] {
							existing[id] = true
						}
						for _, id := range ids {
							if !existing[id] {
								cfg.Bookmarks[kind] = append(cfg.Bookmarks[kind], id)
								existing[id] = true
								added++
							}
						}
					}
					if err := cfg.Save(); err != nil {
						return err
					}
					mode := "replace"
					if merge {
						mode = "merge"
					}
					fmt.Fprintf(os.Stdout, "bookmark import (%s): +%d new bookmark(s)\n", mode, added)
					return nil
				},
			},
		},
	}
}
