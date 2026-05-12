package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/tui/theme"
)

func validateConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "validate-config",
		Usage: "Strict-parse the config and report unknown fields or sketchy values",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit results as JSON",
			},
			&cli.BoolFlag{
				Name:  "strict",
				Usage: "exit non-zero when any warnings are present",
			},
			&cli.BoolFlag{
				Name:  "quiet",
				Usage: "suppress success output; still print warnings and errors",
			},
			&cli.StringSliceFlag{
				Name:  "check",
				Usage: "run only the named check(s): theme, account-token, account-theme, default-account (repeatable)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runValidateConfig(ctx, c, os.Stdout)
		},
	}
}

// validateCheckNames is the canonical list of warning classes the `--check`
// filter accepts. Surfaced via `--check ?`.
var validateCheckNames = []string{"theme", "account-token", "account-theme", "default-account"}

func runValidateConfig(_ context.Context, c *cli.Command, out io.Writer) error {
	for _, n := range c.StringSlice("check") {
		if n == "?" || n == "list" {
			if c.Bool("json") {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"checks": validateCheckNames})
			}
			for _, name := range validateCheckNames {
				fmt.Fprintln(out, name)
			}
			return nil
		}
	}
	// Resolve the path the same way config.Load does.
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	path := cfg.Path()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "no config at %s (defaults will be used)\n", path)
			return nil
		}
		return err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var strict config.Config
	if err := dec.Decode(&strict); err != nil {
		if c.Bool("json") {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]any{
				"ok":    false,
				"path":  path,
				"error": err.Error(),
			})
			return fmt.Errorf("config has unknown or invalid fields")
		}
		fmt.Fprintf(out, "✗ %s\n   %v\n", path, err)
		return fmt.Errorf("config has unknown or invalid fields")
	}

	// Sanity checks. Each warning is tagged with a check name so --check can
	// narrow the output.
	type warning struct {
		check string
		msg   string
	}
	var raw []warning
	if cfg.ActiveTheme != "" {
		if _, ok := theme.ByName(cfg.ActiveTheme); !ok {
			raw = append(raw, warning{"theme",
				fmt.Sprintf("active_theme %q is not one of: %v", cfg.ActiveTheme, theme.Names())})
		}
	}
	for name, acct := range cfg.Accounts {
		if acct.Token == "" && acct.OPRef == "" {
			raw = append(raw, warning{"account-token",
				fmt.Sprintf("account %q has no token and no op_ref", name)})
		}
		if acct.Theme != "" {
			if _, ok := theme.ByName(acct.Theme); !ok {
				raw = append(raw, warning{"account-theme",
					fmt.Sprintf("account %q theme %q is not one of: %v", name, acct.Theme, theme.Names())})
			}
		}
	}
	if cfg.DefaultAccount != "" {
		if _, ok := cfg.Accounts[cfg.DefaultAccount]; !ok {
			raw = append(raw, warning{"default-account",
				fmt.Sprintf("default_account %q is not in accounts", cfg.DefaultAccount)})
		}
	}
	wanted := map[string]bool{}
	for _, n := range c.StringSlice("check") {
		wanted[n] = true
	}
	warnings := make([]string, 0, len(raw))
	for _, w := range raw {
		if len(wanted) > 0 && !wanted[w.check] {
			continue
		}
		warnings = append(warnings, w.msg)
	}

	strictFail := c.Bool("strict") && len(warnings) > 0

	if c.Bool("json") {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"ok":       !strictFail,
			"path":     path,
			"warnings": warnings,
		})
		if strictFail {
			return fmt.Errorf("--strict: %d warning(s)", len(warnings))
		}
		return nil
	}

	if !c.Bool("quiet") {
		fmt.Fprintf(out, "✓ %s parses cleanly\n", path)
	}
	if len(warnings) > 0 {
		if !c.Bool("quiet") {
			fmt.Fprintln(out)
		}
		for _, w := range warnings {
			fmt.Fprintln(out, "~ "+w)
		}
		if !c.Bool("quiet") {
			fmt.Fprintf(out, "\n%d warning(s)\n", len(warnings))
		}
		if strictFail {
			return fmt.Errorf("--strict: %d warning(s)", len(warnings))
		}
	}
	return nil
}
