package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"gopkg.in/ini.v1"

	"github.com/linode/tui/config"
)

// maybeImportLinodeCLI is the first-run fallback: when no token resolves but
// an existing linode-cli config is on disk, offer to seed accounts from it.
// Returns true when accounts were imported and saved.
func maybeImportLinodeCLI(cfg *config.Config) bool {
	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	src := filepath.Join(home, ".config", "linode-cli")
	if _, err := os.Stat(src); err != nil {
		return false
	}

	fmt.Fprintf(os.Stderr, "No usable token found (set LINODE_TOKEN or configure an account).\n")
	fmt.Fprintf(os.Stderr, "Found a linode-cli config at %s — import its accounts? [y/N] ", src)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
	default:
		return false
	}

	n, err := importLinodeCLIConfig(cfg, src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
		return false
	}
	if n == 0 {
		fmt.Fprintln(os.Stderr, "no accounts with tokens found in linode-cli config")
		return false
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
		return false
	}
	fmt.Fprintf(os.Stderr, "imported %d account(s) to %s\n", n, cfg.Path())
	return true
}

// importLinodeCLIConfig copies token + last-create defaults for every profile
// in a linode-cli ini file into cfg.Accounts. Returns the import count.
func importLinodeCLIConfig(cfg *config.Config, src string) (int, error) {
	f, err := ini.Load(src)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", src, err)
	}
	if cfg.Accounts == nil {
		cfg.Accounts = map[string]config.Account{}
	}

	defaultUser := ""
	if d, err := f.GetSection("DEFAULT"); err == nil {
		defaultUser = d.Key("default-user").String()
	}

	imported := 0
	for _, section := range f.Sections() {
		name := section.Name()
		if name == "DEFAULT" || name == ini.DefaultSection {
			continue
		}
		token := section.Key("token").String()
		if token == "" {
			continue
		}
		acct := cfg.Accounts[name]
		acct.Token = token
		// Pre-fill the LastCreate so new linodes default to what the cli
		// was last configured with.
		if r := section.Key("region").String(); r != "" {
			acct.LastCreate.Region = r
		}
		if t := section.Key("type").String(); t != "" {
			acct.LastCreate.Type = t
		}
		if img := section.Key("image").String(); img != "" {
			acct.LastCreate.Image = img
		}
		cfg.Accounts[name] = acct
		imported++
	}

	if defaultUser != "" && cfg.DefaultAccount == "" {
		cfg.DefaultAccount = defaultUser
	}
	return imported, nil
}
