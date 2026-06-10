package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DefaultAccount string             `yaml:"default_account"`
	ActiveTheme    string             `yaml:"active_theme"`
	Refresh        time.Duration      `yaml:"refresh"`
	Accounts       map[string]Account `yaml:"accounts"`
	Tools          Tools              `yaml:"tools"`
	// Bookmarks pin rows in a view. Keys are view names (e.g. "instances",
	// "volumes"); values are the row IDs returned by listOpts.IDFn.
	Bookmarks map[string][]string `yaml:"bookmarks,omitempty"`
	// StatsEnabled persists session counters to disk across launches when
	// true. Off by default — no implicit telemetry footprint.
	StatsEnabled bool `yaml:"stats_enabled,omitempty"`
	// StatsEndpoint, when set, is the URL `:stats post` sends the local
	// counters to. Manual gesture only — no automatic background uploads.
	StatsEndpoint string `yaml:"stats_endpoint,omitempty"`
	// LastSplit remembers the most recent :split view and its ratio so the
	// next launch starts already split.
	LastSplit SplitState `yaml:"last_split,omitempty"`
	// SplitRatios stores per-pair ratios so each (primary, secondary)
	// combination remembers its own size. Keys are "<primary>+<secondary>".
	SplitRatios map[string]float64 `yaml:"split_ratios,omitempty"`
	// AuditRetentionDays, when > 0, drops audit entries older than this on
	// startup. 0 = keep forever (bounded only by 2 MB rotation).
	AuditRetentionDays int `yaml:"audit_retention_days,omitempty"`
	// ReadOnly toggles a session-wide gate that blocks every mutating
	// command-bar action. Persisted so :read-only sticks across launches.
	ReadOnly bool `yaml:"read_only,omitempty"`
	// Fold breakpoints — override the built-in width/height thresholds that
	// hide the secondary, tertiary, and quaternary panes when the terminal
	// is small.
	FoldWidthSecondary    int `yaml:"fold_width_secondary,omitempty"`    // default 80
	FoldWidthTertiary     int `yaml:"fold_width_tertiary,omitempty"`     // default 120
	FoldHeightQuaternary  int `yaml:"fold_height_quaternary,omitempty"`  // default 30
	// FoldChar prefixes folded pane names in the divider (default "+").
	FoldChar string `yaml:"fold_char,omitempty"`
	// Layouts is a map of user-saved pane layouts (`:layout save <name>`).
	Layouts map[string]NamedLayout `yaml:"layouts,omitempty"`
	// RefreshOverrides maps a registered view name (e.g. "events", "instances")
	// to a per-view refresh interval. Falls back to Refresh when unset.
	RefreshOverrides map[string]time.Duration `yaml:"refresh_overrides,omitempty"`
	// LayoutDigests remembers the sha256 of each layout the last time it was
	// imported via `layout import-from`. A subsequent fetch from an unpinned
	// URL whose digest differs is surfaced as a warning, mitigating silent
	// upstream changes.
	LayoutDigests map[string]string `yaml:"layout_digests,omitempty"`

	path  string
	debug bool
}

// NamedLayout captures a complete pane configuration so :layout load can
// restore it later.
type NamedLayout struct {
	Primary    string  `yaml:"primary" json:"primary"`
	Secondary  string  `yaml:"secondary,omitempty" json:"secondary,omitempty"`
	Tertiary   string  `yaml:"tertiary,omitempty" json:"tertiary,omitempty"`
	Quaternary string  `yaml:"quaternary,omitempty" json:"quaternary,omitempty"`
	Ratio      float64 `yaml:"ratio,omitempty" json:"ratio,omitempty"`
	QuatRatio  float64 `yaml:"quat_ratio,omitempty" json:"quat_ratio,omitempty"`
}

// SplitState is the persisted multi-pane shape.
type SplitState struct {
	View      string  `yaml:"view,omitempty"`       // secondary pane
	Ratio     float64 `yaml:"ratio,omitempty"`      // primary/(secondary+tertiary)
	Right     string  `yaml:"right,omitempty"`      // tertiary pane (right of secondary)
	Down      string  `yaml:"down,omitempty"`       // quaternary pane (below middle row)
	Focused   string  `yaml:"focused,omitempty"`    // view name to focus on launch
	QuatRatio float64 `yaml:"quat_ratio,omitempty"` // height share of the quaternary pane
}

type Account struct {
	Token string `yaml:"token,omitempty"`
	OPRef string `yaml:"op_ref,omitempty"`
	// LishUsername overrides the username used for lish SSH access. When
	// empty, lish flows call GetProfile to resolve it. Useful for restricted
	// tokens that can't read /profile.
	LishUsername string `yaml:"lish_username,omitempty"`
	// DefaultSSHKeys holds the labels of SSH keys to pre-select in create
	// forms. Updated automatically after each successful create.
	DefaultSSHKeys []string `yaml:"default_ssh_keys,omitempty"`
	// LastCreate caches the region/type/image last used in a create form
	// for this account so the next create pre-populates.
	LastCreate CreateDefaults `yaml:"last_create,omitempty"`
	// Theme overrides the global active_theme when this account is selected.
	// Useful for making prod feel different from dev.
	Theme string `yaml:"theme,omitempty"`
	// RefreshOverrides overlays Config.RefreshOverrides when this account is
	// active. Same key shape (view name → duration). Useful for prod accounts
	// that warrant slower polling.
	RefreshOverrides map[string]time.Duration `yaml:"refresh_overrides,omitempty"`
	// LayoutDigests pins layout sha256 digests for this account. Falls back
	// to Config.LayoutDigests when unset.
	LayoutDigests map[string]string `yaml:"layout_digests,omitempty"`
	// Bookmarks scopes the favorites set to this account so dev/prod can
	// star different things. When the account is active, ActiveBookmarks
	// returns this map (else falls back to Config.Bookmarks).
	Bookmarks map[string][]string `yaml:"bookmarks,omitempty"`
}

// ActiveBookmarks returns the bookmark map currently in force: the active
// account's map if set, else the global Config.Bookmarks.
func (c *Config) ActiveBookmarks() map[string][]string {
	if c.DefaultAccount != "" {
		if acct, ok := c.Accounts[c.DefaultAccount]; ok && acct.Bookmarks != nil {
			return acct.Bookmarks
		}
	}
	return c.Bookmarks
}

// SetBookmarks replaces the bookmark list for one kind in the active scope
// (per-account when DefaultAccount is set, else global). Pass nil to drop
// the key entirely.
func (c *Config) SetBookmarks(kind string, ids []string) {
	if c.DefaultAccount != "" {
		if acct, ok := c.Accounts[c.DefaultAccount]; ok {
			if acct.Bookmarks == nil {
				acct.Bookmarks = map[string][]string{}
			}
			if ids == nil {
				delete(acct.Bookmarks, kind)
			} else {
				acct.Bookmarks[kind] = ids
			}
			c.Accounts[c.DefaultAccount] = acct
			return
		}
	}
	if c.Bookmarks == nil {
		c.Bookmarks = map[string][]string{}
	}
	if ids == nil {
		delete(c.Bookmarks, kind)
	} else {
		c.Bookmarks[kind] = ids
	}
}

// ActiveLayoutDigest returns the per-account digest for name if set, else
// the global one. Empty string if neither has a pin.
func (c *Config) ActiveLayoutDigest(name string) string {
	if acct, ok := c.Accounts[c.DefaultAccount]; ok {
		if d, ok := acct.LayoutDigests[name]; ok && d != "" {
			return d
		}
	}
	return c.LayoutDigests[name]
}

// RecordLayoutDigest stores the digest under the active account (if any)
// and falls back to global storage. Keeps both layers in sync so a switch
// of DefaultAccount doesn't lose history.
func (c *Config) RecordLayoutDigest(name, digest string) {
	if c.LayoutDigests == nil {
		c.LayoutDigests = map[string]string{}
	}
	c.LayoutDigests[name] = digest
	if c.DefaultAccount != "" {
		acct, ok := c.Accounts[c.DefaultAccount]
		if !ok {
			return
		}
		if acct.LayoutDigests == nil {
			acct.LayoutDigests = map[string]string{}
		}
		acct.LayoutDigests[name] = digest
		c.Accounts[c.DefaultAccount] = acct
	}
}

// CreateDefaults is per-account scratchpad for the create form.
type CreateDefaults struct {
	Region string `yaml:"region,omitempty"`
	Type   string `yaml:"type,omitempty"`
	Image  string `yaml:"image,omitempty"`
}

type Tools struct {
	InstallDir string `yaml:"install_dir"`
	Kubernetes Tool   `yaml:"kubernetes"`
	MySQL      Tool   `yaml:"mysql"`
	PostgreSQL Tool   `yaml:"postgresql"`
	Lish       Tool   `yaml:"lish"`
	// SSH is the direct SSH-to-public-IP shortcut bound to lowercase `c`
	// on a Linode row. Default: `ssh root@{{.IP}}`. Override `Exec`/`Args`
	// to pass a key, custom user, jump host, etc.
	SSH Tool `yaml:"ssh"`
}

type ToolMode string

const (
	ModeTUI ToolMode = "tui"
	ModeGUI ToolMode = "gui"
)

type Tool struct {
	Exec        string   `yaml:"exec"`
	Args        []string `yaml:"args"`
	Mode        ToolMode `yaml:"mode"`
	AutoInstall bool     `yaml:"auto_install"`
	// Version, when set, overrides the hardcoded pinned release version used
	// by `:tools upgrade` and lazy install. Format: same tag as upstream
	// releases (e.g. "v0.50.18"). Leave empty to use the built-in default.
	Version string `yaml:"version,omitempty"`
	// Retries is the number of retry attempts for the install pipeline on
	// transient errors (network, non-checksum failures). 0 = no retry.
	Retries int `yaml:"retries,omitempty"`
}

// RefreshDefaults returns an opinionated baseline of per-view refresh
// intervals. Fast-moving views (events) get a snappy interval; slow-moving
// or expensive ones (images, stackscripts) get longer intervals to avoid
// wasted API calls.
func RefreshDefaults() map[string]time.Duration {
	return map[string]time.Duration{
		"events":          2 * time.Second,
		"instances":       5 * time.Second,
		"nodebalancers":   10 * time.Second,
		"volumes":         10 * time.Second,
		"firewalls":       15 * time.Second,
		"lke":             10 * time.Second,
		"databases":       15 * time.Second,
		"domains":         30 * time.Second,
		"images":          60 * time.Second,
		"stackscripts":    60 * time.Second,
		"objectstorage":   60 * time.Second,
		"placementgroups": 30 * time.Second,
		"vpcs":            30 * time.Second,
		"watchlist":       2 * time.Second,
	}
}

type Overrides struct {
	Token   string
	Account string
	Refresh time.Duration
	Theme   string
	Debug   bool
}

func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = defaultPath()
		if err != nil {
			return nil, err
		}
	}

	cfg := Default()
	cfg.path = path

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cfg, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.fillToolDefaults()
	return cfg, nil
}

func (c *Config) ApplyOverrides(o Overrides) {
	if o.Token != "" {
		c.Accounts["__cli__"] = Account{Token: o.Token}
		c.DefaultAccount = "__cli__"
	}
	if o.Account != "" {
		c.DefaultAccount = o.Account
	}
	if o.Refresh > 0 {
		c.Refresh = o.Refresh
	}
	if o.Theme != "" {
		c.ActiveTheme = o.Theme
	}
	c.debug = o.Debug
}

func (c *Config) Debug() bool { return c.debug }
func (c *Config) Path() string { return c.path }

func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config: no path set")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0o600)
}

func defaultPath() (string, error) {
	home, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "linode-tui", "config.yaml"), nil
}

func (c *Config) fillToolDefaults() {
	d := Default()
	if c.Tools.InstallDir == "" {
		c.Tools.InstallDir = d.Tools.InstallDir
	}
	c.Tools.Kubernetes = mergeTool(c.Tools.Kubernetes, d.Tools.Kubernetes)
	c.Tools.MySQL = mergeTool(c.Tools.MySQL, d.Tools.MySQL)
	c.Tools.PostgreSQL = mergeTool(c.Tools.PostgreSQL, d.Tools.PostgreSQL)
	c.Tools.Lish = mergeTool(c.Tools.Lish, d.Tools.Lish)
}

func mergeTool(have, def Tool) Tool {
	if have.Exec == "" {
		have.Exec = def.Exec
	}
	if len(have.Args) == 0 {
		have.Args = def.Args
	}
	if have.Mode == "" {
		have.Mode = def.Mode
	}
	return have
}
