package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/linode"
)

func listCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "Headless list of a resource type (table/csv/json output, no TUI)",
		ArgsUsage: "<resource> [id]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Usage:   "output format: table | csv | json",
				Value:   "table",
			},
			&cli.DurationFlag{
				Name:  "list-refresh",
				Usage: "tail-like loop: re-fetch + re-render every duration (0 = one-shot)",
			},
			&cli.BoolFlag{
				Name:  "watch",
				Usage: "shortcut for --list-refresh 2s (requires <id>)",
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.NArg() < 1 {
				return fmt.Errorf("usage: linode-tui list <resource> [id]")
			}
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			cfg.ApplyOverrides(config.Overrides{
				Token:   c.String("token"),
				Account: c.String("account"),
			})
			token, err := linode.ResolveToken(ctx, cfg)
			if err != nil {
				return err
			}
			client := linode.NewClient(token)
			resource := c.Args().First()
			id := c.Args().Get(1)

			format := strings.ToLower(c.String("format"))
			tty := isatty.IsTerminal(os.Stdout.Fd())
			// If the user didn't specify and stdout is piped (not a TTY),
			// default to CSV so scripts get a clean parseable feed.
			if !c.IsSet("format") && !tty {
				format = "csv"
			}
			dump := func() error {
				switch format {
				case "json":
					return jsonDump(ctx, client, resource, id, os.Stdout)
				case "csv":
					return csvDump(ctx, client, resource, id, os.Stdout)
				case "table", "":
					return tableDump(ctx, client, resource, id, os.Stdout)
				default:
					return fmt.Errorf("unknown format %q (want table | csv | json)", format)
				}
			}
			interval := c.Duration("list-refresh")
			if c.Bool("watch") && interval == 0 {
				interval = 2 * time.Second
			}
			if interval <= 0 {
				return dump()
			}
			watchCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()
			for {
				if tty {
					// ANSI: clear screen + home cursor
					fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
				}
				if err := dump(); err != nil {
					return err
				}
				if tty {
					fmt.Fprintf(os.Stdout, "\n(refreshing every %s — ctrl+c to stop)\n", interval)
				}
				select {
				case <-watchCtx.Done():
					if tty {
						fmt.Fprintln(os.Stdout, "\nstopped.")
					}
					return nil
				case <-time.After(interval):
				}
			}
		},
	}
}

// tableDump uses csvDump's shape (header + rows) and renders it as an aligned
// table. Reuses csv internally via a small intercepting writer.
func tableDump(ctx context.Context, c *linode.Client, resource, id string, out io.Writer) error {
	var buf strings.Builder
	if err := csvDump(ctx, c, resource, id, &buf); err != nil {
		return err
	}
	return renderTable(buf.String(), out)
}

// renderTable parses a CSV string and prints it as a 2-space-padded aligned
// table. First line is the header.
func renderTable(csvStr string, out io.Writer) error {
	lines := strings.Split(strings.TrimRight(csvStr, "\n"), "\n")
	if len(lines) == 0 {
		return nil
	}
	// Parse CSV by splitting — values may contain commas if quoted, so use
	// encoding/csv for correctness.
	rows := make([][]string, 0, len(lines))
	cr := newCsvReader(csvStr)
	for {
		rec, err := cr.Read()
		if err != nil {
			break
		}
		rows = append(rows, rec)
	}
	if len(rows) == 0 {
		return nil
	}

	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, cell := range r {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	for i, r := range rows {
		var b strings.Builder
		for j, cell := range r {
			if j > 0 {
				b.WriteString("  ")
			}
			b.WriteString(cell)
			if j < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-len(cell)))
			}
		}
		fmt.Fprintln(out, b.String())
		if i == 0 {
			sep := strings.Builder{}
			for j, w := range widths {
				if j > 0 {
					sep.WriteString("  ")
				}
				sep.WriteString(strings.Repeat("─", w))
			}
			fmt.Fprintln(out, sep.String())
		}
	}
	return nil
}
