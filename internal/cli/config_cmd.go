package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/linode/tui/internal/config"
)

func configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Inspect the resolved configuration",
		Commands: []*cli.Command{
			{
				Name:  "show",
				Usage: "Dump the effective config to stdout (token values redacted)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "explicit config path"},
					&cli.BoolFlag{Name: "no-redact", Usage: "show real token values (DANGEROUS)"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					if !c.Bool("no-redact") {
						redacted := map[string]config.Account{}
						for name, a := range cfg.Accounts {
							if a.Token != "" {
								a.Token = "***redacted***"
							}
							redacted[name] = a
						}
						cfg.Accounts = redacted
					}
					data, err := yaml.Marshal(cfg)
					if err != nil {
						return err
					}
					_, err = os.Stdout.Write(data)
					return err
				},
			},
			{
				Name:  "edit",
				Usage: "Open the config file in $VISUAL or $EDITOR (defaults to `vi`)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "explicit config path"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					editor := os.Getenv("VISUAL")
					if editor == "" {
						editor = os.Getenv("EDITOR")
					}
					if editor == "" {
						editor = "vi"
					}
					cmd := exec.Command(editor, cfg.Path())
					cmd.Stdin = os.Stdin
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					return cmd.Run()
				},
			},
			{
				Name:  "path",
				Usage: "Print the resolved config file path and exit",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config", Usage: "explicit config path (overrides default discovery)"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stdout, cfg.Path())
					return nil
				},
			},
		},
	}
}
