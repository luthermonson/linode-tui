package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// newAt returns a Config rooted at a throwaway path, the way Load does for a
// config file that doesn't exist yet.
func newAt(t *testing.T) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load (missing file): %v", err)
	}
	if cfg.path != path {
		t.Fatalf("path not set: %q", cfg.path)
	}
	return cfg
}

func TestSaveLoadRoundTrip(t *testing.T) {
	cfg := newAt(t)
	cfg.DefaultAccount = "dev"
	cfg.ActiveTheme = "gruvbox-dark"
	cfg.Refresh = 30 * time.Second
	cfg.Accounts = map[string]Account{
		"dev": {
			Token:            "tok-dev",
			LishUsername:     "luther",
			DefaultSSHKeys:   []string{"laptop", "yubikey"},
			LastCreate:       CreateDefaults{Region: "us-ord", Type: "g6-nanode-1", Image: "linode/debian12"},
			Theme:            "dracula",
			RefreshOverrides: map[string]time.Duration{"events": 3 * time.Second},
			LayoutDigests:    map[string]string{"default": "abc123"},
			Bookmarks:        map[string][]string{"instances": {"1", "2"}},
		},
		"staging": {OPRef: "op://vault/item/token"},
	}
	cfg.Bookmarks = map[string][]string{"volumes": {"7"}}
	cfg.StatsEnabled = true
	cfg.StatsEndpoint = "https://example.test/stats"
	cfg.LastSplit = SplitState{View: "events", Ratio: 0.6, Right: "logs", Down: "audit", Focused: "instances", QuatRatio: 0.25}
	cfg.SplitRatios = map[string]float64{"instances+events": 0.55}
	cfg.AuditRetentionDays = 14
	cfg.ReadOnly = true
	cfg.Mouse = true
	cfg.FoldWidthSecondary = 90
	cfg.FoldWidthTertiary = 130
	cfg.FoldHeightQuaternary = 40
	cfg.FoldChar = "»"
	cfg.Layouts = map[string]NamedLayout{
		"default": {Primary: "instances", Secondary: "events", Tertiary: "volumes", Quaternary: "audit", Ratio: 0.6, QuatRatio: 0.3},
	}
	cfg.RefreshOverrides = map[string]time.Duration{"events": 2 * time.Second, "images": time.Minute}
	cfg.LayoutDigests = map[string]string{"default": "deadbeef"}
	cfg.Tools.InstallDir = filepath.Join(t.TempDir(), "bin")
	cfg.Tools.Kubernetes.Version = "v0.50.18"
	cfg.Tools.Kubernetes.Retries = 3

	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Unexported bookkeeping isn't serialized; compare the persisted surface.
	want := *cfg
	want.path, want.debug, want.persistedAccount = "", false, ""
	normalized := *got
	normalized.path, normalized.debug, normalized.persistedAccount = "", false, ""

	if !reflect.DeepEqual(want, normalized) {
		t.Errorf("round-trip mismatch\nwant: %#v\ngot:  %#v", want, normalized)
	}
	if got.persistedAccount != "dev" {
		t.Errorf("persistedAccount = %q, want %q", got.persistedAccount, "dev")
	}
}

// TestSaveNeverPersistsCLIToken is the regression test for the token
// exfiltration bug: a LINODE_TOKEN-only session that happened to save the
// config (a bookmark, :mouse, a tool install) wrote the environment token into
// config.yaml and pinned default_account to the synthetic "__cli__".
func TestSaveNeverPersistsCLIToken(t *testing.T) {
	cfg := newAt(t)
	cfg.DefaultAccount = "dev"
	cfg.Accounts["dev"] = Account{Token: "tok-dev"}
	cfg.persistedAccount = "dev"

	cfg.ApplyOverrides(Overrides{Token: "sekrit-env-token"})
	if cfg.DefaultAccount != CLIAccount {
		t.Fatalf("in-memory default account = %q, want %q", cfg.DefaultAccount, CLIAccount)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "sekrit-env-token") {
		t.Errorf("env token leaked to disk:\n%s", body)
	}
	if strings.Contains(body, CLIAccount) {
		t.Errorf("%s leaked to disk:\n%s", CLIAccount, body)
	}

	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.DefaultAccount != "dev" {
		t.Errorf("default_account = %q, want the pre-override %q", reloaded.DefaultAccount, "dev")
	}
	if _, ok := reloaded.Accounts[CLIAccount]; ok {
		t.Error("cli account survived a save/load cycle")
	}
	// The in-memory session must be untouched by the scrubbing.
	if cfg.Accounts[CLIAccount].Token != "sekrit-env-token" {
		t.Error("Save mutated the live config instead of a copy")
	}
	if cfg.DefaultAccount != CLIAccount {
		t.Error("Save mutated the live DefaultAccount")
	}
}

// A --token session with no prior config must not invent a default_account.
func TestSaveCLIOnlySessionLeavesDefaultAccountEmpty(t *testing.T) {
	cfg := newAt(t)
	cfg.ApplyOverrides(Overrides{Token: "tok"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.DefaultAccount != "" {
		t.Errorf("default_account = %q, want empty", reloaded.DefaultAccount)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	cfg := newAt(t)
	cfg.DefaultAccount = "dev"
	cfg.Accounts["dev"] = Account{Token: "tok"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(cfg.path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind (err=%v)", err)
	}
	entries, err := os.ReadDir(filepath.Dir(cfg.path))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("unexpected files in config dir: %v", names)
	}
	// A second save must replace, not append or corrupt.
	cfg.ActiveTheme = "light"
	if err := cfg.Save(); err != nil {
		t.Fatalf("second save: %v", err)
	}
	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.ActiveTheme != "light" {
		t.Errorf("theme = %q, want light", reloaded.ActiveTheme)
	}
}

func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not meaningful on windows")
	}
	cfg := newAt(t)
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	fi, err := os.Stat(cfg.path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %v, want 0600", perm)
	}
	di, err := os.Stat(filepath.Dir(cfg.path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir mode = %v, want 0700 (it holds a token file)", perm)
	}
}

// Concurrent saves must never produce a truncated or interleaved file. Run
// with -race to also exercise the mutex.
func TestSaveConcurrent(t *testing.T) {
	cfg := newAt(t)
	cfg.DefaultAccount = "dev"
	cfg.Accounts["dev"] = Account{Token: "tok"}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cfg.Save(); err != nil {
				t.Errorf("concurrent save: %v", err)
			}
		}()
	}
	wg.Wait()

	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload after concurrent saves: %v", err)
	}
	if reloaded.Accounts["dev"].Token != "tok" {
		t.Errorf("config damaged by concurrent saves: %#v", reloaded.Accounts)
	}
}

func TestSaveWithoutPath(t *testing.T) {
	cfg := Default()
	if err := cfg.Save(); err == nil {
		t.Fatal("expected an error saving a config with no path")
	}
}

func TestFillToolDefaults(t *testing.T) {
	def := Default().Tools
	// mergeTool deliberately doesn't merge AutoInstall: it's a plain bool with
	// no "unset" sentinel, so an explicit `auto_install: false` in a config
	// would be un-disableable. Expectations below reflect that.
	noAuto := func(tool Tool) Tool { tool.AutoInstall = false; return tool }
	tests := []struct {
		name string
		in   Tools
		want Tools
	}{
		{
			name: "all empty gets every default",
			in:   Tools{},
			want: Tools{
				InstallDir: def.InstallDir,
				Kubernetes: noAuto(def.Kubernetes), MySQL: noAuto(def.MySQL), PostgreSQL: noAuto(def.PostgreSQL),
				Lish: noAuto(def.Lish), SSH: noAuto(def.SSH),
			},
		},
		{
			// The reported bug: a config written before tools.ssh existed left
			// SSH zero-valued, so `c` on a Linode row failed with
			// "ssh: exec not configured".
			name: "legacy config with no ssh section",
			in: Tools{
				Kubernetes: Tool{Exec: "k9s", Args: []string{"--kubeconfig", "{{.Kubeconfig}}"}, Mode: ModeTUI},
				MySQL:      Tool{Exec: "lazysql", Args: []string{"{{.DSN}}"}, Mode: ModeTUI},
				PostgreSQL: Tool{Exec: "lazysql", Args: []string{"{{.DSN}}"}, Mode: ModeTUI},
				Lish:       Tool{Exec: "ssh", Args: []string{"-t"}, Mode: ModeTUI},
			},
			want: Tools{
				InstallDir: def.InstallDir,
				Kubernetes: Tool{Exec: "k9s", Args: []string{"--kubeconfig", "{{.Kubeconfig}}"}, Mode: ModeTUI},
				MySQL:      Tool{Exec: "lazysql", Args: []string{"{{.DSN}}"}, Mode: ModeTUI},
				PostgreSQL: Tool{Exec: "lazysql", Args: []string{"{{.DSN}}"}, Mode: ModeTUI},
				Lish:       Tool{Exec: "ssh", Args: []string{"-t"}, Mode: ModeTUI},
				SSH:        def.SSH,
			},
		},
		{
			name: "user overrides survive the merge",
			in: Tools{
				InstallDir: "/opt/bin",
				Kubernetes: Tool{Exec: "kubectl", Args: []string{"get", "pods"}, Mode: ModeGUI},
				MySQL:      Tool{Exec: "mycli", Args: []string{"{{.DSN}}"}, Mode: ModeGUI},
				PostgreSQL: Tool{Exec: "pgcli", Args: []string{"{{.DSN}}"}, Mode: ModeGUI},
				Lish:       Tool{Exec: "mosh", Args: []string{"{{.Label}}"}, Mode: ModeGUI},
				SSH:        Tool{Exec: "ssh", Args: []string{"-i", "~/.ssh/id_ed25519", "admin@{{.IP}}"}, Mode: ModeGUI},
			},
			want: Tools{
				InstallDir: "/opt/bin",
				Kubernetes: Tool{Exec: "kubectl", Args: []string{"get", "pods"}, Mode: ModeGUI},
				MySQL:      Tool{Exec: "mycli", Args: []string{"{{.DSN}}"}, Mode: ModeGUI},
				PostgreSQL: Tool{Exec: "pgcli", Args: []string{"{{.DSN}}"}, Mode: ModeGUI},
				Lish:       Tool{Exec: "mosh", Args: []string{"{{.Label}}"}, Mode: ModeGUI},
				SSH:        Tool{Exec: "ssh", Args: []string{"-i", "~/.ssh/id_ed25519", "admin@{{.IP}}"}, Mode: ModeGUI},
			},
		},
		{
			name: "partial ssh section fills only the gaps",
			in:   Tools{SSH: Tool{Exec: "autossh"}},
			want: Tools{
				InstallDir: def.InstallDir,
				Kubernetes: noAuto(def.Kubernetes), MySQL: noAuto(def.MySQL), PostgreSQL: noAuto(def.PostgreSQL),
				Lish: noAuto(def.Lish),
				SSH:  Tool{Exec: "autossh", Args: def.SSH.Args, Mode: def.SSH.Mode},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Tools: tc.in}
			cfg.fillToolDefaults()
			if !reflect.DeepEqual(cfg.Tools, tc.want) {
				t.Errorf("fillToolDefaults()\n got: %#v\nwant: %#v", cfg.Tools, tc.want)
			}
			for name, tool := range map[string]Tool{
				"kubernetes": cfg.Tools.Kubernetes,
				"mysql":      cfg.Tools.MySQL,
				"postgresql": cfg.Tools.PostgreSQL,
				"lish":       cfg.Tools.Lish,
				"ssh":        cfg.Tools.SSH,
			} {
				if tool.Exec == "" {
					t.Errorf("tool %q has no exec after merge", name)
				}
			}
		})
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadClampsRefresh(t *testing.T) {
	path := writeConfig(t, `
default_account: dev
refresh: 1ms
accounts:
  dev:
    token: tok
    refresh_overrides:
      events: 5ms
refresh_overrides:
  instances: 250ms
  images: 60s
  volumes: 0s
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Refresh != MinRefresh {
		t.Errorf("global refresh = %v, want clamped to %v", cfg.Refresh, MinRefresh)
	}
	if got := cfg.RefreshOverrides["instances"]; got != MinRefresh {
		t.Errorf("instances override = %v, want %v", got, MinRefresh)
	}
	if got := cfg.RefreshOverrides["images"]; got != time.Minute {
		t.Errorf("images override = %v, want untouched 1m", got)
	}
	if _, ok := cfg.RefreshOverrides["volumes"]; ok {
		t.Error("zero override should be dropped, not kept as a busy loop")
	}
	if got := cfg.Accounts["dev"].RefreshOverrides["events"]; got != MinRefresh {
		t.Errorf("per-account events override = %v, want %v", got, MinRefresh)
	}
}

func TestLoadDropsUnknownDefaultAccount(t *testing.T) {
	path := writeConfig(t, `
default_account: ghost
accounts:
  dev:
    token: tok
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DefaultAccount != "" {
		t.Errorf("default_account = %q, want cleared (no matching accounts entry)", cfg.DefaultAccount)
	}
}

func TestLoadKeepsKnownDefaultAccount(t *testing.T) {
	path := writeConfig(t, `
default_account: dev
accounts:
  dev:
    token: tok
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DefaultAccount != "dev" {
		t.Errorf("default_account = %q, want dev", cfg.DefaultAccount)
	}
}

// The --refresh flag used to carry a 2s default Value, which made every launch
// look like an explicit override and silently discarded `refresh:` from the
// config file.
func TestApplyOverridesRefreshOnlyWhenSet(t *testing.T) {
	cfg := Default()
	cfg.Refresh = 30 * time.Second

	cfg.ApplyOverrides(Overrides{Refresh: 2 * time.Second, RefreshSet: false})
	if cfg.Refresh != 30*time.Second {
		t.Errorf("unset flag clobbered config refresh: got %v, want 30s", cfg.Refresh)
	}

	cfg.ApplyOverrides(Overrides{Refresh: 5 * time.Second, RefreshSet: true})
	if cfg.Refresh != 5*time.Second {
		t.Errorf("explicit --refresh ignored: got %v, want 5s", cfg.Refresh)
	}

	cfg.ApplyOverrides(Overrides{Refresh: time.Millisecond, RefreshSet: true})
	if cfg.Refresh != MinRefresh {
		t.Errorf("sub-second --refresh not clamped: got %v, want %v", cfg.Refresh, MinRefresh)
	}
}

func TestApplyOverridesAccountIsPersistable(t *testing.T) {
	cfg := newAt(t)
	cfg.Accounts["staging"] = Account{Token: "tok"}
	cfg.ApplyOverrides(Overrides{Account: "staging", Token: "env-token"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// --account names a real account, so it should stick; --token must not.
	if reloaded.DefaultAccount != "staging" {
		t.Errorf("default_account = %q, want staging", reloaded.DefaultAccount)
	}
	if reloaded.Accounts["staging"].Token != "tok" {
		t.Errorf("named account token was overwritten: %q", reloaded.Accounts["staging"].Token)
	}
}

// An explicit audit_retention_days: 0 (= keep forever) must survive a
// Save→Load round-trip. With yaml omitempty the key was dropped on Save and
// the next Load re-seeded the 90-day default, silently re-enabling pruning
// for users who deliberately opted out.
func TestAuditRetentionZeroRoundTrips(t *testing.T) {
	cfg := newAt(t)
	if cfg.AuditRetentionDays != 90 {
		t.Fatalf("fresh config retention = %d, want the 90-day default", cfg.AuditRetentionDays)
	}
	cfg.AuditRetentionDays = 0 // user opts out of pruning
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.AuditRetentionDays != 0 {
		t.Errorf("retention after round-trip = %d, want 0 (keep forever)", reloaded.AuditRetentionDays)
	}
}
