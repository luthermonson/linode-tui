package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

func installCompletionCommand() *cli.Command {
	return &cli.Command{
		Name:      "install-completion",
		Usage:     "Write a completion script for your shell to the standard user location",
		ArgsUsage: "[bash | zsh | fish]   (auto-detects $SHELL when omitted)",
		Action: func(ctx context.Context, c *cli.Command) error {
			return runInstallCompletion(ctx, c, os.Stdout)
		},
	}
}

func runInstallCompletion(_ context.Context, c *cli.Command, out io.Writer) error {
	shell := c.Args().First()
	if shell == "" {
		shell = detectShell()
	}
	if shell == "" {
		return fmt.Errorf("can't detect shell — pass one: bash | zsh | fish")
	}

	target, hint, err := completionTarget(shell)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	script, err := exec.Command(self, "completion", shell).Output()
	if err != nil {
		return fmt.Errorf("generate completion script: %w", err)
	}
	if err := os.WriteFile(target, script, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}

	fmt.Fprintf(out, "installed completion → %s\n", target)
	if hint != "" {
		fmt.Fprintln(out, hint)
	}
	return nil
}

func detectShell() string {
	s := os.Getenv("SHELL")
	if s == "" {
		return ""
	}
	base := filepath.Base(s)
	switch base {
	case "bash", "zsh", "fish":
		return base
	}
	// Some macOS users run `bash`-but-it's-Apple's-version; treat as bash.
	if strings.HasPrefix(base, "bash") {
		return "bash"
	}
	return ""
}

func completionTarget(shell string) (path, hint string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	switch shell {
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "linode-tui"),
			"Source it now: `source ~/.local/share/bash-completion/completions/linode-tui` (or restart your shell).",
			nil
	case "zsh":
		return filepath.Join(home, ".zfunc", "_linode-tui"),
			"Make sure `~/.zfunc` is in your fpath. Add to ~/.zshrc if needed:\n  fpath=(~/.zfunc $fpath)\n  autoload -Uz compinit && compinit",
			nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "linode-tui.fish"),
			"Fish picks it up on the next shell start.",
			nil
	default:
		return "", "", fmt.Errorf("unsupported shell %q (want: bash, zsh, fish)", shell)
	}
}
