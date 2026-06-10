// Package onepassword shells out to the 1Password CLI (`op`). We use the CLI
// rather than the Go SDK because the CLI piggybacks on the desktop app's
// biometric/touchID unlock — the right UX for an interactive TUI. The SDK
// requires a service-account token, which would defeat the point.
package onepassword

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrOpNotInstalled = errors.New("op (1Password CLI) not found in PATH")

func Available() bool {
	_, err := exec.LookPath("op")
	return err == nil
}

// Read resolves a secret reference (e.g. "op://Work/linode-dev/credential") to
// its plaintext value. The op CLI handles unlocking via the desktop app.
func Read(ctx context.Context, ref string) (string, error) {
	if !Available() {
		return "", ErrOpNotInstalled
	}
	out, err := exec.CommandContext(ctx, "op", "read", "--no-newline", ref).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("op read %s: %s", ref, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("op read %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}
