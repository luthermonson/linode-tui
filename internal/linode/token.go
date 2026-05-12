package linode

import (
	"context"
	"errors"
	"fmt"

	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/onepassword"
)

var ErrNoToken = errors.New("no Linode API token resolved")

// ResolveToken resolves the active account's token. Convenience wrapper for
// ResolveTokenForAccount(ctx, cfg, cfg.DefaultAccount).
func ResolveToken(ctx context.Context, cfg *config.Config) (string, error) {
	return ResolveTokenForAccount(ctx, cfg, cfg.DefaultAccount)
}

// ResolveTokenForAccount walks: literal Token on the named account → op_ref via
// the 1Password CLI. Doesn't mutate cfg.
func ResolveTokenForAccount(ctx context.Context, cfg *config.Config, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: set LINODE_TOKEN, pass --token, or configure an account", ErrNoToken)
	}
	acct, ok := cfg.Accounts[name]
	if !ok {
		return "", fmt.Errorf("account %q not found in config", name)
	}
	if acct.Token != "" {
		return acct.Token, nil
	}
	if acct.OPRef != "" {
		tok, err := onepassword.Read(ctx, acct.OPRef)
		if err != nil {
			return "", fmt.Errorf("resolve %s via 1Password: %w", name, err)
		}
		if tok == "" {
			return "", fmt.Errorf("account %q resolved to empty token", name)
		}
		return tok, nil
	}
	return "", fmt.Errorf("%w: account %q has neither token nor op_ref", ErrNoToken, name)
}
