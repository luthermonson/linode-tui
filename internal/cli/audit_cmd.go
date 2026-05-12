package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/audit"
)

func auditCommand() *cli.Command {
	return &cli.Command{
		Name:  "audit",
		Usage: "Show the local audit log of mutating actions",
		Commands: []*cli.Command{
			auditTailCommand(),
			auditPurgeCommand(),
			auditClearCommand(),
			auditCountCommand(),
			auditGrepCommand(),
			auditRecentCommand(),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return auditTailCommand().Action(ctx, c)
		},
	}
}

func auditGrepCommand() *cli.Command {
	return &cli.Command{
		Name:      "grep",
		Usage:     "Filter audit entries by a case-insensitive substring (matches action/kind/id/label/err)",
		ArgsUsage: "<pattern>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "lines",
				Usage: "max entries to scan from the tail (default: 1000)",
				Value: 1000,
			},
			&cli.BoolFlag{Name: "json", Usage: "emit JSON-Lines"},
			&cli.BoolFlag{Name: "err", Usage: "only include entries with a non-empty Err field"},
			&cli.StringFlag{Name: "account", Usage: "only include entries tagged with this account"},
			&cli.DurationFlag{Name: "since", Usage: "only include entries newer than this duration (e.g. 24h)"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			if c.NArg() < 1 {
				return fmt.Errorf("usage: linode-tui audit grep <pattern>")
			}
			needle := strings.ToLower(c.Args().First())
			pool := audit.Tail(int(c.Int("lines")))
			asJSON := c.Bool("json")
			errOnly := c.Bool("err")
			account := c.String("account")
			var sinceCutoff time.Time
			if since := c.Duration("since"); since > 0 {
				sinceCutoff = time.Now().Add(-since)
			}
			matched := 0
			for _, e := range pool {
				if errOnly && e.Err == "" {
					continue
				}
				if account != "" && e.Account != account {
					continue
				}
				if !sinceCutoff.IsZero() && e.Timestamp.Before(sinceCutoff) {
					continue
				}
				blob := strings.ToLower(e.Action + " " + e.Kind + " " + e.ID + " " + e.Label + " " + e.Err)
				if !strings.Contains(blob, needle) {
					continue
				}
				matched++
				if asJSON {
					data, _ := json.Marshal(e)
					fmt.Fprintln(os.Stdout, string(data))
				} else {
					marker := "✓"
					if e.Err != "" {
						marker = "✗"
					}
					fmt.Fprintf(os.Stdout, "%s  %s  %-8s  %-12s  %-10s  %s",
						marker, e.Timestamp.Format("2006-01-02 15:04:05"),
						e.Action, e.Kind, e.ID, e.Label)
					if e.Err != "" {
						fmt.Fprintf(os.Stdout, "  err=%s", e.Err)
					}
					fmt.Fprintln(os.Stdout)
				}
			}
			if matched == 0 && !asJSON {
				fmt.Fprintln(os.Stdout, "(no matches)")
			}
			return nil
		},
	}
}

func auditRecentCommand() *cli.Command {
	return &cli.Command{
		Name:      "recent",
		Usage:     "Show the last n entries in the styled banner format (no color when piped)",
		ArgsUsage: "[n]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "err", Usage: "only show entries with non-empty Err"},
			&cli.BoolFlag{Name: "no-marker", Usage: "drop the leading colored dot"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			n := 10
			if c.NArg() >= 1 {
				if v, err := strconv.Atoi(c.Args().First()); err == nil && v > 0 {
					n = v
				}
			}
			useColor := stdoutIsTTY() && os.Getenv("NO_COLOR") == ""
			pool := n
			if c.Bool("err") {
				pool = n * 10
				if pool > 1000 {
					pool = 1000
				}
			}
			all := audit.Tail(pool)
			entries := all[:0]
			for _, e := range all {
				if c.Bool("err") && e.Err == "" {
					continue
				}
				entries = append(entries, e)
			}
			if len(entries) > n {
				entries = entries[:n]
			}
			today := time.Now().UTC().Truncate(24 * time.Hour)
			for _, e := range entries {
				label := e.Label
				if label == "" {
					label = e.ID
				}
				stamp := e.Timestamp.Format("01-02 15:04:05")
				line := fmt.Sprintf("%s  %-18s  %s/%s  %s", stamp, e.Action, e.Kind, e.ID, label)
				if e.Err != "" {
					line += "  err=" + e.Err
				}
				color, dot := "", "●"
				switch {
				case e.Err != "":
					color = "\x1b[31m"
				case e.Timestamp.UTC().After(today):
					color = "\x1b[32m"
				default:
					color = "\x1b[33m"
				}
				if !useColor {
					color = ""
				}
				reset := ""
				if useColor {
					reset = "\x1b[0m"
				}
				if c.Bool("no-marker") {
					fmt.Fprintln(os.Stdout, line)
				} else {
					fmt.Fprintf(os.Stdout, "%s%s%s %s\n", color, dot, reset, line)
				}
			}
			return nil
		},
	}
}

func auditCountCommand() *cli.Command {
	return &cli.Command{
		Name:  "count",
		Usage: "Print just the total number of audit entries on disk",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Fprintln(os.Stdout, audit.Count())
			return nil
		},
	}
}

func auditClearCommand() *cli.Command {
	return &cli.Command{
		Name:  "clear",
		Usage: "Wipe the entire local audit log (irreversible)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:     "i-know-what-im-doing",
				Usage:    "required confirmation flag; clear refuses without it",
				Required: true,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			removed := audit.PruneOlderThan(time.Now())
			fmt.Fprintf(os.Stdout, "audit clear: removed %d entries\n", removed)
			return nil
		},
	}
}

func auditPurgeCommand() *cli.Command {
	return &cli.Command{
		Name:  "purge",
		Usage: "Drop entries older than the given duration",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:     "older-than",
				Usage:    "duration cutoff (e.g. 30d uses 720h)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "kind",
				Usage: "limit purge to a specific resource kind (e.g. instances)",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit result as JSON",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			cutoff := time.Now().UTC().Add(-c.Duration("older-than"))
			removed := audit.PruneOlderThanKind(cutoff, c.String("kind"))
			if c.Bool("json") {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"removed": removed,
					"cutoff":  cutoff.Format(time.RFC3339),
				})
			}
			fmt.Fprintf(os.Stdout, "purged %d entries older than %s\n", removed, cutoff.Format(time.RFC3339))
			return nil
		},
	}
}

func auditTailCommand() *cli.Command {
	return &cli.Command{
		Name:  "tail",
		Usage: "Print recent audit entries (and optionally follow new ones)",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "n",
				Aliases: []string{"lines"},
				Usage:   "number of entries to print",
				Value:   50,
			},
			&cli.BoolFlag{
				Name:    "follow",
				Aliases: []string{"f"},
				Usage:   "poll the log and stream new entries",
			},
			&cli.DurationFlag{
				Name:  "poll-interval",
				Usage: "follow poll interval",
				Value: 1 * time.Second,
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit each entry as a single JSON line",
			},
			&cli.StringFlag{
				Name:  "account",
				Usage: "only include entries tagged with this account",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runAuditTail(ctx, c, os.Stdout)
		},
	}
}

func runAuditTail(ctx context.Context, c *cli.Command, out io.Writer) error {
	n := int(c.Int("n"))
	if n <= 0 {
		n = 50
	}
	account := c.String("account")
	match := func(e audit.Entry) bool { return account == "" || e.Account == account }
	// When an account filter is set, pull a wider pool so we still print
	// up to n matching rows.
	pool := n
	if account != "" {
		pool = n * 10
		if pool > 1000 {
			pool = 1000
		}
	}
	entries := audit.Tail(pool)
	if account != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if match(e) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
		if len(entries) > n {
			entries = entries[:n]
		}
	}
	// Tail returns newest-first; flip for normal "older→newer" reading.
	for i := len(entries) - 1; i >= 0; i-- {
		printAuditEntry(out, entries[i], c.Bool("json"))
	}

	if !c.Bool("follow") {
		return nil
	}

	// Follow: poll for entries newer than the last printed timestamp.
	var since time.Time
	if len(entries) > 0 {
		since = entries[0].Timestamp
	}
	interval := c.Duration("poll-interval")
	if interval <= 0 {
		interval = time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
		fresh := audit.Tail(200)
		// Reverse, then filter to entries strictly newer than `since`.
		newer := make([]audit.Entry, 0, len(fresh))
		for i := len(fresh) - 1; i >= 0; i-- {
			if !fresh[i].Timestamp.After(since) {
				continue
			}
			if !match(fresh[i]) {
				continue
			}
			newer = append(newer, fresh[i])
		}
		for _, e := range newer {
			printAuditEntry(out, e, c.Bool("json"))
			since = e.Timestamp
		}
	}
}

func printAuditEntry(out io.Writer, e audit.Entry, asJSON bool) {
	if asJSON {
		b, err := json.Marshal(e)
		if err == nil {
			fmt.Fprintln(out, string(b))
		}
		return
	}
	marker := "✓"
	if e.Err != "" {
		marker = "✗"
	}
	fmt.Fprintf(out, "%s  %s  %-10s  %-14s  %-12s  %s",
		marker, e.Timestamp.Format("2006-01-02 15:04:05"),
		e.Action, e.Kind, e.ID, e.Label)
	if e.Err != "" {
		fmt.Fprintf(out, "  err=%s", e.Err)
	}
	fmt.Fprintln(out)
}
