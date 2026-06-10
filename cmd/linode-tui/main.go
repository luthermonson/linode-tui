package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/linode/tui/buildinfo"
	"github.com/linode/tui/cli"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	buildinfo.Set(version, commit)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cli.NewApp(version, commit).Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
