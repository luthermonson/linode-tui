package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/cache"
)

func cacheCommand() *cli.Command {
	return &cli.Command{
		Name:  "cache",
		Usage: "Inspect or prune the local ~/.cache/linode-tui directory",
		Commands: []*cli.Command{
			{
				Name:  "size",
				Usage: "Report bytes per subdirectory plus a total",
				Action: func(_ context.Context, _ *cli.Command) error {
					root, err := cache.Root()
					if err != nil {
						return err
					}
					sizes, total, err := cache.SubdirSizes(root)
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stdout, root)
					names := make([]string, 0, len(sizes))
					for n := range sizes {
						names = append(names, n)
					}
					sort.Strings(names)
					for _, n := range names {
						fmt.Fprintf(os.Stdout, "  %-20s  %s\n", n, cache.FormatBytes(sizes[n]))
					}
					fmt.Fprintf(os.Stdout, "\ntotal: %s\n", cache.FormatBytes(total))
					return nil
				},
			},
			{
				Name:      "prune",
				Usage:     "Delete one cache subdir (or `all` for everything)",
				ArgsUsage: "<subdir|all>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:     "i-know-what-im-doing",
						Usage:    "required confirmation flag",
						Required: true,
					},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.NArg() < 1 {
						return fmt.Errorf("usage: linode-tui cache prune <subdir|all>")
					}
					target := c.Args().First()
					root, err := cache.Root()
					if err != nil {
						return err
					}
					path := filepath.Join(root, target)
					if target == "all" {
						path = root
					}
					if err := os.RemoveAll(path); err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "removed %s\n", path)
					return nil
				},
			},
		},
	}
}

