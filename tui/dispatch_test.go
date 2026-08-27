package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"

	"github.com/luthermonson/linode-tui/config"
	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tui/cmdbar"
	"github.com/luthermonson/linode-tui/tui/keys"
	"github.com/luthermonson/linode-tui/tui/theme"
	"github.com/luthermonson/linode-tui/tui/views"
)

// testModel builds a model that is safe to drive from a test: its config lives
// in a temp dir (so Save() never touches the real one) and the cache/audit
// directories are redirected there too, since several verbs read
// os.UserCacheDir(). The model is assembled directly rather than via newModel
// so the constructor's startup audit prune doesn't run.
//
// DefaultAccount is deliberately left empty: `:clear-account` refuses to do
// anything without an account name, which keeps the verb-coverage test below
// from wandering anywhere destructive.
func testModel(t *testing.T) model {
	t.Helper()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", cacheDir) // linux
	t.Setenv("LOCALAPPDATA", cacheDir)   // windows
	t.Setenv("HOME", dir)                // darwin/linux fallback
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := config.Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.ActiveTheme = "light"
	cfg.DefaultAccount = ""

	client, err := linode.NewClient("test-token-not-used")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	th, ok := theme.ByName(cfg.ActiveTheme)
	if !ok {
		t.Fatalf("theme %q not found", cfg.ActiveTheme)
	}
	m := model{
		startedAt:  time.Now(),
		cfg:        cfg,
		client:     client,
		theme:      th,
		keys:       keys.Default(),
		cmd:        cmdbar.New(th),
		stats:      map[string]int{},
		splitRatio: 0.5,
		quatRatio:  0.33,
	}
	if f, ok := views.Resolve("instances"); ok {
		m.current = f(m.deps())
		m.currentName = "instances"
	}
	return m
}

// dispatchTo runs one command-bar input and returns the resulting model.
func dispatchTo(t *testing.T, m model, input string) model {
	t.Helper()
	next, _ := m.dispatch(input)
	got, ok := next.(model)
	if !ok {
		t.Fatalf("dispatch(%q) returned %T, want tui.model", input, next)
	}
	return got
}

// TestAllCmdbarVerbsDispatch is the regression lock for phantom verbs: the
// command bar used to offer tab-completions (`replay-last`, `replay-from`)
// that dispatch had no case for, so accepting the completion produced
// "unknown command". Every completion the bar offers must route somewhere.
func TestAllCmdbarVerbsDispatch(t *testing.T) {
	for _, verb := range allCmdbarVerbs() {
		verb := verb
		t.Run(verb, func(t *testing.T) {
			m := testModel(t)
			got := dispatchTo(t, m, verb)
			if strings.HasPrefix(got.status, "unknown command") {
				t.Fatalf("verb %q is completable but not dispatchable: status = %q", verb, got.status)
			}
		})
	}
}

// TestDispatchAcceptsLeadingColon covers the ctrl+y path, which calls
// dispatch with the ":undo" spelling while the command bar submits "undo".
func TestDispatchAcceptsLeadingColon(t *testing.T) {
	m := testModel(t)
	got := dispatchTo(t, m, ":undo")
	if strings.HasPrefix(got.status, "unknown command") {
		t.Fatalf(`dispatch(":undo") = %q, want it routed to the undo flow`, got.status)
	}
}

func TestThemeDispatchPersists(t *testing.T) {
	m := testModel(t)
	got := dispatchTo(t, m, "theme dark")

	if got.cfg.ActiveTheme != "dark" {
		t.Errorf("cfg.ActiveTheme = %q, want %q", got.cfg.ActiveTheme, "dark")
	}
	if got.theme.Name != "dark" {
		t.Errorf("live theme = %q, want %q", got.theme.Name, "dark")
	}
	if got.status == "" {
		t.Error("status is empty; expected confirmation that the theme changed")
	}

	data, err := os.ReadFile(got.cfg.Path())
	if err != nil {
		t.Fatalf("config was not written: %v", err)
	}
	var onDisk struct {
		ActiveTheme string `yaml:"active_theme"`
	}
	if err := yaml.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("parse saved config: %v", err)
	}
	if onDisk.ActiveTheme != "dark" {
		t.Errorf("saved active_theme = %q, want %q", onDisk.ActiveTheme, "dark")
	}
}

// A per-account override shadows the global theme, so a bare `:theme <name>`
// has to retarget the override — writing the global would leave the change
// half-applied and lose it on the next launch.
func TestThemeDispatchRetargetsAccountOverride(t *testing.T) {
	m := testModel(t)
	m.cfg.DefaultAccount = "dev"
	m.cfg.Accounts["dev"] = config.Account{Token: "x", Theme: "dracula"}

	got := dispatchTo(t, m, "theme gruvbox-dark")

	if acct := got.cfg.Accounts["dev"]; acct.Theme != "gruvbox-dark" {
		t.Errorf("accounts[dev].theme = %q, want %q", acct.Theme, "gruvbox-dark")
	}
	if got.cfg.ActiveTheme != "light" {
		t.Errorf("global active_theme = %q, want it left alone at %q", got.cfg.ActiveTheme, "light")
	}
	if activeTheme(got.cfg) != "gruvbox-dark" {
		t.Errorf("activeTheme() = %q, want %q", activeTheme(got.cfg), "gruvbox-dark")
	}
}

func TestActionDoneMsgSetsStatus(t *testing.T) {
	m := testModel(t)
	next, cmd := m.Update(views.ActionDoneMsg{Label: "delete instance"})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.model", next)
	}
	if !strings.Contains(got.status, "delete instance") {
		t.Errorf("status = %q, want it to mention the action label", got.status)
	}
	// The message must also keep flowing to the panes — listView answers an
	// ActionDoneMsg with a re-fetch, and swallowing it at the root would mean
	// the list never reflects the mutation.
	if cmd == nil {
		t.Error("no command returned; ActionDoneMsg did not reach the pane fan-out")
	}
}

func TestActionErrorMsgSetsStatus(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(views.ActionErrorMsg{Label: "resize", Err: os.ErrPermission})
	got := next.(model)
	if !strings.Contains(got.status, "resize") || !strings.Contains(got.status, "failed") {
		t.Errorf("status = %q, want it to report the failed action", got.status)
	}
}

// A modal must not swallow background traffic: a listTickMsg consumed while a
// confirm/detail modal is up permanently breaks that pane's
// tick→fetch→loaded→tick chain.
func TestModalsForwardBackgroundMessages(t *testing.T) {
	m := testModel(t)
	m.detail = newDetailModal("t", "body", m.theme, 80, 24, nil)

	_, cmd := m.Update(views.ActionDoneMsg{Label: "x"})
	if cmd == nil {
		t.Error("modal swallowed a background message instead of forwarding it to the panes")
	}
}

// Keys, on the other hand, belong to the modal alone.
func TestModalsKeepKeys(t *testing.T) {
	m := testModel(t)
	m.detail = newDetailModal("t", "body", m.theme, 80, 24, nil)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got := next.(model)
	if got.detail == nil {
		t.Error("detail modal closed on a plain key")
	}
}

// stubView is a minimal views.View that records the keys it receives and can
// claim to be filtering on demand.
type stubView struct {
	filtering bool
	gotKeys   []string
}

func (s *stubView) Init() tea.Cmd { return nil }
func (s *stubView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		s.gotKeys = append(s.gotKeys, k.String())
	}
	return s, nil
}
func (s *stubView) View() string    { return "" }
func (s *stubView) Title() string   { return "stub" }
func (s *stubView) Filtering() bool { return s.filtering }

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// While a view's filter input is focused, `:` and `?` are filter text — the
// root bindings have to stand down or field filters like `region:us-east`
// can't be typed at all.
func TestFilterKeepsPunctuationKeys(t *testing.T) {
	for _, r := range []rune{':', '?', '-'} {
		t.Run(string(r), func(t *testing.T) {
			m := testModel(t)
			stub := &stubView{filtering: true}
			m.current = stub
			m.secondary = &stubView{} // enables the +/-/[/] pane gestures

			next, _ := m.Update(runeKey(r))
			got := next.(model)
			if got.cmd.Active() {
				t.Errorf("%q opened the command bar while filtering", r)
			}
			if got.helpOpen {
				t.Errorf("%q opened help while filtering", r)
			}
			if len(stub.gotKeys) != 1 || stub.gotKeys[0] != string(r) {
				t.Errorf("view received %v, want [%q]", stub.gotKeys, string(r))
			}
		})
	}
}

// …and when the filter is closed those same keys are the global bindings.
func TestPunctuationKeysBindWhenNotFiltering(t *testing.T) {
	m := testModel(t)
	m.current = &stubView{filtering: false}

	next, _ := m.Update(runeKey(':'))
	if !next.(model).cmd.Active() {
		t.Error(`":" did not open the command bar`)
	}

	next, _ = m.Update(runeKey('?'))
	if !next.(model).helpOpen {
		t.Error(`"?" did not open help`)
	}
}

// Help promises "esc to close". With a drill-in on the stack, esc used to pop
// the stack behind the still-open overlay instead.
func TestEscClosesHelpBeforePoppingStack(t *testing.T) {
	m := testModel(t)
	m.helpOpen = true
	m.stack = []viewFrame{{name: "instances", view: &stubView{}}}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if got.helpOpen {
		t.Error("esc did not close the help overlay")
	}
	if len(got.stack) != 1 {
		t.Errorf("esc popped the drill-in stack (len %d) while help was open", len(got.stack))
	}
}

// `:split` replaces the pane a `:split-preview follow` chain writes into, so
// the chain has to be retired — otherwise a second follow leaves two tick
// loops racing on the same slot.
func TestSplitRetiresPreviewFollow(t *testing.T) {
	m := testModel(t)
	m.previewFollow = true
	m.previewKind = "instances"
	gen := m.previewGen

	got := dispatchTo(t, m, "split events")
	if got.previewFollow {
		t.Error("previewFollow still set after :split")
	}
	if got.previewKind != "" {
		t.Errorf("previewKind = %q, want it cleared", got.previewKind)
	}
	if got.previewGen == gen {
		t.Error("previewGen not bumped; the old follow chain is still live")
	}

	// A tick from the retired generation must be a no-op.
	next, cmd := got.Update(previewRefreshMsg{gen: gen})
	if _, ok := next.(model); !ok {
		t.Fatal("unexpected model type")
	}
	if cmd != nil {
		t.Error("a superseded preview tick scheduled more work")
	}
}

// Bookmark writes go through the active scope; clear/export/import used to
// read the global map, which is empty under `:bookmark scope account`.
func TestBookmarkClearUsesActiveScope(t *testing.T) {
	m := testModel(t)
	m.cfg.DefaultAccount = "dev"
	m.cfg.Accounts["dev"] = config.Account{
		Token:     "x",
		Bookmarks: map[string][]string{"instances": {"1", "2"}},
	}

	got := dispatchTo(t, m, "bookmark clear instances")
	if got.typedConfirm == nil {
		t.Fatalf("no confirmation raised; status = %q", got.status)
	}

	got.cfg.SetBookmarks("instances", nil)
	if ids := got.cfg.ActiveBookmarks()["instances"]; len(ids) != 0 {
		t.Errorf("bookmarks not cleared from the active scope: %v", ids)
	}
}

func TestBookmarkExportUsesActiveScope(t *testing.T) {
	m := testModel(t)
	m.cfg.DefaultAccount = "dev"
	m.cfg.Accounts["dev"] = config.Account{
		Token:     "x",
		Bookmarks: map[string][]string{"volumes": {"7"}},
	}
	out := filepath.Join(t.TempDir(), "bookmarks.yaml")

	got := dispatchTo(t, m, "bookmark export "+out)
	if strings.Contains(got.status, "0 bookmark") {
		t.Fatalf("exported from the global scope: %q", got.status)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("export not written: %v", err)
	}
	if !strings.Contains(string(data), "volumes") {
		t.Errorf("export missing the account-scoped bookmarks:\n%s", data)
	}
}

// audit_retention_days: 0 means "keep forever". The default has to come from
// config.Default(), not from treating 0 as unset at startup.
func TestAuditRetentionDefault(t *testing.T) {
	if got := config.Default().AuditRetentionDays; got != 90 {
		t.Errorf("config.Default().AuditRetentionDays = %d, want 90", got)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("audit_retention_days: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AuditRetentionDays != 0 {
		t.Errorf("explicit 0 was overwritten with %d; 0 must mean keep-forever", cfg.AuditRetentionDays)
	}
}
