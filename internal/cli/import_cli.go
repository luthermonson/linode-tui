package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"gopkg.in/ini.v1"

	"github.com/linode/tui/internal/config"
)

func importCLICommand() *cli.Command {
	return &cli.Command{
		Name:  "import-cli",
		Usage: "Seed accounts from an existing linode-cli config (~/.config/linode-cli)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "from",
				Usage: "path to linode-cli config file (default ~/.config/linode-cli)",
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to linode-tui config (default ~/.config/linode-tui/config.yaml)",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "show what would be imported without writing",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runImportCLI(ctx, c, os.Stdout)
		},
	}
}

func runImportCLI(_ context.Context, c *cli.Command, out io.Writer) error {
	src := c.String("from")
	if src == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		src = filepath.Join(home, ".config", "linode-cli")
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("linode-cli config not found at %s", src)
	}

	f, err := ini.Load(src)
	if err != nil {
		return fmt.Errorf("parse %s: %w", src, err)
	}

	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return fmt.Errorf("load tui config: %w", err)
	}
	if cfg.Accounts == nil {
		cfg.Accounts = map[string]config.Account{}
	}

	defaultUser := ""
	if d, err := f.GetSection("DEFAULT"); err == nil {
		defaultUser = d.Key("default-user").String()
	}

	imported := 0
	for _, section := range f.Sections() {
		name := section.Name()
		if name == "DEFAULT" || name == ini.DefaultSection {
			continue
		}
		token := section.Key("token").String()
		if token == "" {
			fmt.Fprintf(out, "[skip] %s: no token\n", name)
			continue
		}
		acct := cfg.Accounts[name]
		acct.Token = token
		// Pre-fill the LastCreate so new linodes default to what the cli
		// was last configured with.
		if r := section.Key("region").String(); r != "" {
			acct.LastCreate.Region = r
		}
		if t := section.Key("type").String(); t != "" {
			acct.LastCreate.Type = t
		}
		if img := section.Key("image").String(); img != "" {
			acct.LastCreate.Image = img
		}
		cfg.Accounts[name] = acct
		fmt.Fprintf(out, "[%s] %s\n", actionFor(c.Bool("dry-run")), name)
		imported++
	}

	if defaultUser != "" && cfg.DefaultAccount == "" {
		cfg.DefaultAccount = defaultUser
		fmt.Fprintf(out, "default account set to %q\n", defaultUser)
	}

	if c.Bool("dry-run") {
		fmt.Fprintf(out, "\ndry-run: would import %d accounts (re-run without --dry-run to write)\n", imported)
		return nil
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save tui config: %w", err)
	}
	fmt.Fprintf(out, "\nimported %d accounts to %s\n", imported, cfg.Path())
	return nil
}

func actionFor(dry bool) string {
	if dry {
		return "would import"
	}
	return "import"
}
