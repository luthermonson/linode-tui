package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/luthermonson/linode-tui/buildinfo"
	"github.com/luthermonson/linode-tui/cli"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	buildinfo.Set(version, commit)

	// linodego's retry path writes to the standard logger on 429/503. That
	// goes to stderr, which is the same terminal the TUI has put into
	// alt-screen mode, and a stray "Received 429 ..." line paints over the
	// rendered frame and corrupts the display until the next full redraw.
	// Errors that matter surface through the TUI's own status line.
	// (--debug is not wired to a file sink yet, so there's nothing to keep.)
	log.SetOutput(io.Discard)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cli.NewApp(version, commit).Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
