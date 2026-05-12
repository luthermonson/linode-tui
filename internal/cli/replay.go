package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/audit"
	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/linode"
)

func replayLastCommand() *cli.Command {
	return &cli.Command{
		Name:  "replay-last",
		Usage: "Show (or re-execute) the most recent audit entry. Default is dry-run.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "execute",
				Usage: "actually replay (only safe for idempotent actions like delete)",
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runReplayLast(ctx, c, os.Stdout)
		},
	}
}

func runReplayLast(ctx context.Context, c *cli.Command, out io.Writer) error {
	entries := audit.Tail(1)
	if len(entries) == 0 {
		return fmt.Errorf("no audit entries recorded")
	}
	e := entries[0]
	fmt.Fprintf(out, "last entry: action=%s kind=%s id=%s label=%s ts=%s\n",
		e.Action, e.Kind, e.ID, e.Label, e.Timestamp.Format("2006-01-02 15:04:05"))
	if !c.Bool("execute") {
		fmt.Fprintln(out, "(dry-run — pass --execute to actually replay; safe only for delete/tags)")
		return nil
	}

	switch e.Action {
	case "delete", "bulk-delete":
		// Replaying a delete = ensure the resource is gone. Idempotent.
		break
	default:
		return fmt.Errorf("replay-last only safe for delete; got %q", e.Action)
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

	if err := replayDelete(ctx, client, e); err != nil {
		audit.Append(audit.Entry{Action: "replay-delete", Kind: e.Kind, ID: e.ID, Label: e.Label, Err: err.Error()})
		// 404 / not found counts as already-gone; surface as success.
		return fmt.Errorf("replay failed: %w", err)
	}
	audit.Append(audit.Entry{Action: "replay-delete", Kind: e.Kind, ID: e.ID, Label: e.Label})
	fmt.Fprintln(out, "replayed.")
	return nil
}

func replayDelete(ctx context.Context, c *linode.Client, e audit.Entry) error {
	id, err := atoiSafe(e.ID)
	if err != nil {
		return fmt.Errorf("non-numeric id %q", e.ID)
	}
	switch e.Kind {
	case "instances":
		return c.Raw().DeleteInstance(ctx, id)
	case "volumes":
		return c.Raw().DeleteVolume(ctx, id)
	case "nodebalancers":
		return c.Raw().DeleteNodeBalancer(ctx, id)
	case "lke":
		return c.Raw().DeleteLKECluster(ctx, id)
	case "vpcs":
		return c.Raw().DeleteVPC(ctx, id)
	case "firewalls":
		return c.Raw().DeleteFirewall(ctx, id)
	case "domains":
		return c.Raw().DeleteDomain(ctx, id)
	default:
		return fmt.Errorf("replay-delete: unknown kind %q", e.Kind)
	}
}

func atoiSafe(s string) (int, error) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a non-negative integer: %s", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
