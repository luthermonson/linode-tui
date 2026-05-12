package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/linode"
	"github.com/linode/tui/internal/tui"
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
				Usage:   "Linode API token (overrides config + 1Password)",
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
		},
		Action: func(ctx context.Context, c *cli.Command) error {
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
			token, err := linode.ResolveToken(ctx, cfg)
			if err != nil {
				return err
			}
			return tui.Run(ctx, cfg, linode.NewClient(token))
		},
		Commands: []*cli.Command{
			versionCommand(version, commit),
			clearAccountCommand(),
			openCommand(),
			listCommand(),
			doctorCommand(),
			importCLICommand(),
			installCompletionCommand(),
			auditCommand(),
			replayLastCommand(),
			replayFromCommand(),
			validateConfigCommand(),
			uiCommand(),
			layoutCommand(),
			defaultsCommand(),
			cacheCommand(),
			configCommand(),
			bookmarkCommand(),
		},
	}
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
