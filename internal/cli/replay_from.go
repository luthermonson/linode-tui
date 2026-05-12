package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/audit"
	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/linode"
)

func replayFromCommand() *cli.Command {
	return &cli.Command{
		Name:      "replay-from",
		Usage:     "Replay every idempotent action recorded since <date>. Default is dry-run.",
		ArgsUsage: "<YYYY-MM-DD>   (or use --since for relative durations)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "execute",
				Usage: "actually replay (only safe for delete actions)",
			},
			&cli.StringFlag{
				Name:  "kind",
				Usage: "narrow to a specific resource kind (e.g. instances)",
			},
			&cli.DurationFlag{
				Name:  "since",
				Usage: "relative cutoff (e.g. 6h, 2h30m) — overrides positional date when set",
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runReplayFrom(ctx, c, os.Stdout)
		},
	}
}

func runReplayFrom(ctx context.Context, c *cli.Command, out io.Writer) error {
	var since time.Time
	if d := c.Duration("since"); d > 0 {
		since = time.Now().UTC().Add(-d)
	} else {
		if c.NArg() < 1 {
			return fmt.Errorf("usage: linode-tui replay-from <YYYY-MM-DD>  (or --since <duration>)")
		}
		t, err := time.Parse("2006-01-02", c.Args().First())
		if err != nil {
			return fmt.Errorf("parse date: %w", err)
		}
		since = t
	}
	kindFilter := c.String("kind")

	// Pull a generous window — audit.Tail returns newest-first; we'll filter.
	all := audit.Tail(10000)
	var eligible []audit.Entry
	for _, e := range all {
		if e.Timestamp.Before(since) {
			break // earlier entries are even older
		}
		switch e.Action {
		case "delete", "bulk-delete":
		default:
			continue
		}
		if kindFilter != "" && e.Kind != kindFilter {
			continue
		}
		eligible = append(eligible, e)
	}

	if len(eligible) == 0 {
		fmt.Fprintln(out, "no idempotent (delete) entries since", since.Format(time.RFC3339))
		return nil
	}
	fmt.Fprintf(out, "found %d eligible entries since %s", len(eligible), since.Format(time.RFC3339))
	if kindFilter != "" {
		fmt.Fprintf(out, " (kind=%s)", kindFilter)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out)

	if !c.Bool("execute") {
		for _, e := range eligible {
			fmt.Fprintf(out, "  would replay-delete %s/%s (%s) from %s\n",
				e.Kind, e.ID, e.Label, e.Timestamp.Format("2006-01-02 15:04:05"))
		}
		fmt.Fprintln(out, "\n(dry-run — pass --execute to actually re-issue deletes)")
		return nil
	}

	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return err
	}
	cfg.ApplyOverrides(config.Overrides{Token: c.String("token"), Account: c.String("account")})
	tok, err := linode.ResolveToken(ctx, cfg)
	if err != nil {
		return err
	}
	client := linode.NewClient(tok)

	var failures int
	for _, e := range eligible {
		err := replayDelete(ctx, client, e)
		audit.Append(audit.Entry{
			Action: "replay-delete",
			Kind:   e.Kind,
			ID:     e.ID,
			Label:  e.Label,
			Err:    errString(err),
		})
		if err != nil {
			failures++
			fmt.Fprintf(out, "  ✗ %s/%s: %v\n", e.Kind, e.ID, err)
		} else {
			fmt.Fprintf(out, "  ✓ %s/%s\n", e.Kind, e.ID)
		}
	}
	fmt.Fprintf(out, "\ndone — %d ok, %d failed\n", len(eligible)-failures, failures)
	if failures > 0 {
		return fmt.Errorf("%d replays failed", failures)
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
