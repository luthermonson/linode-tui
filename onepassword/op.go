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
	"sync"
	"time"
)

var ErrOpNotInstalled = errors.New("op (1Password CLI) not found in PATH")

// ErrInvalidRef guards against passing something that isn't a "op://..."
// secret reference into exec.Command as an argument. Without this check a
// crafted value (e.g. one starting with "-") resolved from config could be
// interpreted by the op CLI as a flag rather than an argument.
var ErrInvalidRef = errors.New("invalid 1Password reference: must start with op://")

// opPathOnce caches the exec.LookPath("op") result for the life of the
// process. op resolution never changes at runtime (PATH is fixed once the
// process starts), and this is looked up on every secret read across every
// account, every refresh tick — no reason to hit the filesystem/PATH search
// each time.
var (
	opPathOnce sync.Once
	opPath     string
	opPathErr  error
)

func lookupOp() (string, error) {
	opPathOnce.Do(func() {
		opPath, opPathErr = exec.LookPath("op")
	})
	return opPath, opPathErr
}

func Available() bool {
	_, err := lookupOp()
	return err == nil
}

// readTimeout bounds how long we'll wait on the op CLI. Without it a blocked
// biometric/Touch ID prompt (e.g. the desktop app isn't running, or the user
// walked away) can hang startup or `:doctor` indefinitely.
const readTimeout = 30 * time.Second

// Read resolves a secret reference (e.g. "op://Work/linode-dev/credential") to
// its plaintext value. The op CLI handles unlocking via the desktop app.
func Read(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "op://") {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	bin, err := lookupOp()
	if err != nil {
		return "", ErrOpNotInstalled
	}

	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "read", "--no-newline", ref).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("op read %s: timed out after %s (biometric prompt blocked?)", ref, readTimeout)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("op read %s: %s", ref, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("op read %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}
