package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/linode/tui/audit"
	"github.com/linode/tui/buildinfo"
	"github.com/linode/tui/cache"
	"github.com/linode/tui/config"
	"github.com/linode/tui/health"
	"github.com/linode/tui/linode"
	"github.com/linode/tui/tools"
	"github.com/linode/tui/tui/cmdbar"
	"github.com/linode/tui/tui/keys"
	"github.com/linode/tui/tui/theme"
	"github.com/linode/tui/tui/views"
)

func Run(ctx context.Context, cfg *config.Config, client *linode.Client) error {
	return RunWithView(ctx, cfg, client, "")
}

// RunWithView launches the TUI seeded with a specific view. Empty initialView
// falls back to "instances".
func RunWithView(ctx context.Context, cfg *config.Config, client *linode.Client, initialView string) error {
	return RunWithViewAndContext(ctx, cfg, client, initialView, nil)
}

// RunWithViewAndContext is RunWithView plus an initial Deps.Context map
// (e.g. focus_id to focus a specific row).
func RunWithViewAndContext(ctx context.Context, cfg *config.Config, client *linode.Client, initialView string, initialContext map[string]any) error {
	return RunFull(ctx, cfg, client, initialView, initialContext, false)
}

// RunFull is the most general entry point: lets the caller pass an initial
// view, a Deps.Context map, and a read-only switch (which blocks mutating
// command-bar dispatches and per-row actions). The runtime readOnly is the OR
// of the persisted cfg.ReadOnly and the explicit arg, so either side sticks.
func RunFull(ctx context.Context, cfg *config.Config, client *linode.Client, initialView string, initialContext map[string]any, readOnly bool) error {
	m := newModel(cfg, client)
	m.readOnly = readOnly || cfg.ReadOnly
	if initialView != "" {
		if f, ok := views.Resolve(initialView); ok {
			d := m.deps()
			if initialContext != nil {
				d.Context = initialContext
			}
			m.current = f(d)
			m.currentName = initialView
		} else {
			m.status = "unknown view: " + initialView + " (showing instances)"
		}
	}
	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithMouseCellMotion())
	_, err := prog.Run()
	return err
}

type model struct {
	startedAt   time.Time
	cfg         *config.Config
	client      *linode.Client
	theme       theme.Theme
	keys        keys.Map
	cmd         cmdbar.Model
	current       views.View
	currentName   string
	secondary     views.View
	secondaryName string
	tertiary      views.View
	tertiaryName  string
	quaternary    views.View
	quatName      string
	splitRatio    float64 // 0.5 = even; adjustable with +/- in split mode
	previewFollow bool    // true: re-fetch the focused row each tick
	previewKind   string  // primary view name at split-preview time
	quatRatio     float64 // share of total height the quaternary pane gets
	readOnly      bool    // session-scoped: blocks mutating dispatches
	stack         []viewFrame
	w, h          int
	status      string
	statusLog   []string
	stats       map[string]int

	installing   tools.Kind
	pendingDrill *views.DrillInMsg
	prompt       *installPrompt
	confirm      *confirmModal
	typedConfirm *typedConfirmModal
	helpOpen   bool
	helpFilter string
	form       subform
	detail     *detailModal
	installCh  chan tea.Msg
	spinner    spinner.Model
	helpBar    help.Model
	// username is the API-resolved /profile username, cached at startup so
	// the header reads "@<username>" instead of "@__cli__" when the token
	// came from LINODE_TOKEN (no named account).
	username string
}

type profileResolvedMsg struct{ username string }

type accountSwitchedMsg struct {
	Name  string
	Token string
	Err   error
}

// viewFrame is a stacked view used by drill-in navigation.
type viewFrame struct {
	name string
	view views.View
}

func newModel(cfg *config.Config, client *linode.Client) model {
	t, ok := theme.ByName(activeTheme(cfg))
	if !ok {
		t = theme.Dark()
	}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(t.Primary)
	hb := help.New()
	hb.Styles.ShortKey = lipgloss.NewStyle().Foreground(t.Secondary)
	hb.Styles.ShortDesc = lipgloss.NewStyle().Foreground(t.Muted)
	hb.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(t.Border)
	var stats map[string]int
	if cfg.StatsEnabled {
		stats = loadStats()
	}
	if stats == nil {
		stats = map[string]int{}
	}
	// Drop snapshot files that no longer correspond to any bookmark.
	views.PruneSnapshots(cfg.Bookmarks)

	// Apply audit retention (default 90 days when unset).
	retentionDays := cfg.AuditRetentionDays
	retentionDefault := false
	if retentionDays == 0 {
		retentionDays = 90
		retentionDefault = true
	}
	pruneNotice := ""
	prunedCount := 0
	if retentionDays > 0 {
		cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		if removed := audit.PruneOlderThan(cutoff); removed > 0 {
			prunedCount = removed
			pruneNotice = fmt.Sprintf("pruned %d audit entries older than %dd", removed, retentionDays)
			if retentionDefault {
				pruneNotice += " (set audit_retention_days in config to override the 90d default)"
			}
		}
	}

	if prunedCount > 0 {
		if stats == nil {
			stats = map[string]int{}
		}
		stats["audit:pruned_today"] += prunedCount
		if cfg.StatsEnabled {
			saveStats(stats)
		}
	}

	// Surface a short trail of recent mutations so a returning user
	// immediately sees what the last session did. Skipped when prune notice
	// is already taking the slot.
	if pruneNotice == "" {
		recent := audit.Tail(3)
		if len(recent) > 0 {
			today := time.Now().UTC().Truncate(24 * time.Hour)
			bold := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
			dim := lipgloss.NewStyle().Foreground(t.Muted)
			errStyle := lipgloss.NewStyle().Foreground(t.Error).Bold(true)
			parts := make([]string, 0, len(recent))
			for i, e := range recent {
				label := e.Label
				if label == "" {
					label = e.ID
				}
				if label == "" {
					label = e.Kind
				}
				txt := fmt.Sprintf("%s %s", e.Action, label)
				switch {
				case i == 0 && e.Err != "":
					// Most-recent failure pops red.
					txt = errStyle.Render(txt + " ✗")
				case e.Timestamp.UTC().After(today):
					txt = bold.Render(txt)
				default:
					txt = dim.Render(txt)
				}
				parts = append(parts, txt)
			}
			total := audit.Count()
			pruneNotice = dim.Render(fmt.Sprintf("recent: %d of %d actions: ", len(recent), total)) +
				strings.Join(parts, dim.Render(" · "))
		}
	}
	m := model{
		startedAt:  time.Now(),
		cfg:        cfg,
		client:     client,
		theme:      t,
		keys:       keys.Default(),
		cmd:        cmdbar.New(t),
		spinner:    sp,
		helpBar:    hb,
		stats:      stats,
		splitRatio: 0.5,
		quatRatio:  0.33,
		status:     pruneNotice,
	}
	m.cmd.SetCompletions(allCmdbarVerbs())
	if f, ok := views.Resolve("instances"); ok {
		m.current = f(m.deps())
		m.currentName = "instances"
	}
	// Restore last split if any.
	if cfg.LastSplit.View != "" {
		if f, ok := views.Resolve(cfg.LastSplit.View); ok {
			m.secondary = f(m.deps())
			m.secondaryName = cfg.LastSplit.View
			if cfg.LastSplit.Ratio > 0 {
				m.splitRatio = clampRatio(cfg.LastSplit.Ratio)
			}
		}
		if cfg.LastSplit.Right != "" {
			if f, ok := views.Resolve(cfg.LastSplit.Right); ok {
				m.tertiary = f(m.deps())
				m.tertiaryName = cfg.LastSplit.Right
			}
		}
		if cfg.LastSplit.Down != "" {
			if f, ok := views.Resolve(cfg.LastSplit.Down); ok {
				m.quaternary = f(m.deps())
				m.quatName = cfg.LastSplit.Down
			}
		}
		if cfg.LastSplit.QuatRatio > 0 {
			m.quatRatio = clampRatio(cfg.LastSplit.QuatRatio)
		}
		// Rotate live panes until the previously-focused one is primary.
		if cfg.LastSplit.Focused != "" {
			m = rotateFocus(m, cfg.LastSplit.Focused)
		}
	}
	return m
}

// rotateFocus rotates the live panes until target is in the primary slot, or
// returns the model unchanged when target isn't in any pane.
func rotateFocus(m model, target string) model {
	panes := []*views.View{&m.current, &m.secondary, &m.tertiary, &m.quaternary}
	names := []*string{&m.currentName, &m.secondaryName, &m.tertiaryName, &m.quatName}
	var ps []*views.View
	var ns []*string
	for i := range panes {
		if *panes[i] != nil {
			ps = append(ps, panes[i])
			ns = append(ns, names[i])
		}
	}
	for tries := 0; tries < len(ps); tries++ {
		if *ns[0] == target {
			return m
		}
		firstV, firstN := *ps[0], *ns[0]
		for i := 0; i < len(ps)-1; i++ {
			*ps[i] = *ps[i+1]
			*ns[i] = *ns[i+1]
		}
		*ps[len(ps)-1] = firstV
		*ns[len(ps)-1] = firstN
	}
	return m
}

func (m model) deps() views.Deps {
	return views.Deps{Cfg: m.cfg, Theme: m.theme, Linode: m.client}
}

// activeTheme resolves the theme name for the current model: the active
// account's override if set, otherwise the global cfg.ActiveTheme.
func activeTheme(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if acct, ok := cfg.Accounts[cfg.DefaultAccount]; ok && acct.Theme != "" {
		return acct.Theme
	}
	return cfg.ActiveTheme
}


func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.cmd.Init(), m.spinner.Tick, m.resolveProfileCmd()}
	if m.current != nil {
		cmds = append(cmds, m.current.Init())
	}
	return tea.Batch(cmds...)
}

// resolveProfileCmd fires /profile once at startup so the header can show
// the real Linode username instead of the synthetic "__cli__" placeholder.
// Best-effort: failures are swallowed.
func (m model) resolveProfileCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		prof, err := client.Raw().GetProfile(ctx)
		if err != nil || prof == nil {
			return profileResolvedMsg{}
		}
		return profileResolvedMsg{username: prof.Username}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prev := m.status
	nextAny, cmd := m.updateInner(msg)
	next, ok := nextAny.(model)
	if !ok {
		return nextAny, cmd
	}
	if next.status != prev && next.status != "" {
		entry := time.Now().Format("15:04:05") + " " + next.status
		next.statusLog = append(next.statusLog, entry)
		if len(next.statusLog) > 100 {
			next.statusLog = next.statusLog[len(next.statusLog)-100:]
		}
	}
	return next, cmd
}

func (m model) updateInner(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	if m.prompt != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.w, m.h = size.Width, size.Height
		}
		cmd := m.prompt.Update(msg)
		if m.prompt.Done() {
			return m.finishPrompt()
		}
		return m, cmd
	}
	if m.confirm != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.w, m.h = size.Width, size.Height
		}
		cmd := m.confirm.Update(msg)
		if m.confirm.Done() {
			return m.finishConfirm()
		}
		return m, cmd
	}
	if m.typedConfirm != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.w, m.h = size.Width, size.Height
		}
		cmd := m.typedConfirm.Update(msg)
		if m.typedConfirm.Done() {
			tc := m.typedConfirm
			m.typedConfirm = nil
			if tc.Confirmed() {
				return m, tc.onYes
			}
			m.status = "cancelled"
			return m, nil
		}
		return m, cmd
	}
	if m.form != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.w, m.h = size.Width, size.Height
		}
		cmd := m.form.Update(msg)
		if m.form.Done() {
			return m.finishForm()
		}
		return m, cmd
	}
	if m.detail != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.w, m.h = size.Width, size.Height
		}
		closed, editCmd, cmd := m.detail.Update(msg)
		if closed {
			m.detail = nil
			if editCmd != nil {
				return m, editCmd
			}
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m.broadcastSize()

	case cmdbar.SubmitMsg:
		return m.dispatch(msg.Input)

	case cmdbar.CancelMsg:
		return m, nil

	case views.InstallNeededMsg:
		if m.readOnly {
			m.status = "read-only: install blocked"
			if msg.Drill.Cleanup != nil {
				msg.Drill.Cleanup()
			}
			return m, nil
		}
		drill := msg.Drill
		m.pendingDrill = &drill
		if m.cfg.Tools.InstallDir != "" {
			m.installing = msg.Kind
			m.status = fmt.Sprintf("installing %s…", msg.Kind)
			ch, cmd := installCmdStream(m.cfg, msg.Kind)
			m.installCh = ch
			return m, cmd
		}
		m.prompt = newInstallPrompt(msg.Kind, tools.SuggestInstallDirs())
		return m, m.prompt.Init()

	case views.InstallProgressMsg:
		m.status = fmt.Sprintf("installing %s — %d%%", msg.Kind, msg.Percent)
		if m.installCh != nil {
			return m, waitOn(m.installCh)
		}
		return m, nil

	case views.InstallDoneMsg:
		m.installing = ""
		m.installCh = nil
		m.status = fmt.Sprintf("installed %s → %s", msg.Kind, msg.Path)
		drill := m.pendingDrill
		m.pendingDrill = nil
		if drill != nil {
			d := *drill
			return m, func() tea.Msg { return d }
		}
		return m, nil

	case accountSwitchedMsg:
		if msg.Err != nil {
			m.status = "account switch failed: " + msg.Err.Error()
			return m, nil
		}
		client, err := linode.NewClient(msg.Token)
		if err != nil {
			m.status = "account switch failed: " + err.Error()
			return m, nil
		}
		m.cfg.DefaultAccount = msg.Name
		m.client = client
		_ = m.cfg.Save()
		// Honor per-account theme override.
		if t, ok := theme.ByName(activeTheme(m.cfg)); ok && t.Name != m.theme.Name {
			m.theme = t
			m.cmd.SetTheme(t)
		}
		m.status = "switched to account: " + msg.Name
		if m.currentName != "" {
			if f, ok := views.Resolve(m.currentName); ok {
				m.current = f(m.deps())
				return m, m.current.Init()
			}
		}
		return m, nil

	case views.ConfirmMsg:
		if m.readOnly {
			m.status = "read-only: mutation blocked"
			return m, nil
		}
		m.confirm = newConfirmModalWithTheme(msg.Prompt, msg.OnYes, m.theme)
		return m, m.confirm.Init()

	case views.TypedConfirmMsg:
		if m.readOnly {
			m.status = "read-only: bulk mutation blocked"
			return m, nil
		}
		m.typedConfirm = newTypedConfirmModal(msg.Prompt, msg.Match, msg.OnYes)
		return m, m.typedConfirm.Init()

	case clearAccountDoneMsg:
		body := msg.output
		if msg.err != nil {
			body += "\nerror: " + msg.err.Error()
		}
		mode := "executed"
		if msg.dry {
			mode = "dry-run"
		}
		m.detail = newDetailModal(fmt.Sprintf("clear-account %s (%s)", msg.account, mode), body, m.theme, m.w, m.h, nil)
		if msg.err != nil {
			m.status = "clear-account failed: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("clear-account %s: %s", msg.account, mode)
		}
		return m, m.detail.Init()

	case auditClearedMsg:
		m.status = fmt.Sprintf("audit clear: removed %d entries", msg.removed)
		return m, nil

	case profileResolvedMsg:
		m.username = msg.username
		return m, nil

	case cachePrunedMsg:
		if msg.err != nil {
			m.status = "cache prune: " + msg.err.Error()
		} else {
			m.status = "cache pruned: " + msg.path
		}
		return m, nil

	case bookmarkClearedMsg:
		delete(m.cfg.Bookmarks, msg.kind)
		_ = m.cfg.Save()
		m.status = fmt.Sprintf("bookmark clear: removed %d under %s", msg.count, msg.kind)
		return m, nil

	case statsResetMsg:
		m.stats = map[string]int{}
		if msg.wipedDisk {
			m.status = "stats reset (memory + disk)"
		} else {
			m.status = "stats reset (memory only; no on-disk file found)"
		}
		return m, nil

	case statsPostResultMsg:
		if msg.err != nil {
			m.status = "stats post failed: " + msg.err.Error()
		} else {
			m.status = "stats posted → " + msg.endpoint
		}
		return m, nil

	case previewRefreshMsg:
		if m.secondary == nil || !m.previewFollow {
			return m, nil
		}
		ident, ok := m.current.(views.Identifiable)
		if !ok {
			return m, m.previewTick()
		}
		id := ident.SelectedID()
		if id == "" {
			return m, m.previewTick()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		body, err := resourceJSON(ctx, m.client, m.previewKind, id)
		cancel()
		if err == nil {
			if tv, ok := m.secondary.(*views.TextView); ok {
				tv.TitleText = previewTitle(m.previewKind, id, body)
				tv.Body = body
			}
		}
		return m, m.previewTick()

	case views.OpenDetailMsg:
		m.detail = newDetailModal(msg.Title, msg.Body, m.theme, m.w, m.h, msg.OnEdit)
		return m, m.detail.Init()

	case resourceDiffResultMsg:
		if msg.err != nil {
			m.status = "diff failed: " + msg.err.Error()
			return m, nil
		}
		m.status = ""
		m.detail = newDetailModal(msg.title, msg.body, m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case views.NavigateMsg:
		f, ok := views.Resolve(msg.Name)
		if !ok {
			m.status = "unknown view: " + msg.Name
			return m, nil
		}
		if m.current != nil {
			m.stack = append(m.stack, viewFrame{name: m.currentName, view: m.current})
		}
		deps := m.deps()
		deps.Context = msg.Context
		m.current = f(deps)
		m.currentName = msg.Name
		return m, m.current.Init()

	case views.ConfigureLinodeMsg:
		if m.readOnly {
			m.status = "read-only: configure blocked"
			return m, nil
		}
		m.form = newConfigLinode(m.client, msg.ID, msg.Label, msg.Action)
		m.status = ""
		return m, m.form.Init()

	case views.InstallErrorMsg:
		m.installing = ""
		m.installCh = nil
		m.status = fmt.Sprintf("install %s failed: %v", msg.Kind, msg.Err)
		if m.pendingDrill != nil && m.pendingDrill.Cleanup != nil {
			m.pendingDrill.Cleanup()
		}
		m.pendingDrill = nil
		return m, nil

	case tea.KeyMsg:
		if m.cmd.Active() {
			c, cmd := m.cmd.Update(msg)
			m.cmd = c
			return m, cmd
		}
		switch {
		case key(msg, m.keys.Quit):
			return m, tea.Quit
		case key(msg, m.keys.CmdBar):
			m.cmd.Open()
			return m, m.cmd.Init()
		case key(msg, m.keys.Help):
			m.helpOpen = !m.helpOpen
			m.helpFilter = ""
			return m, nil
		case key(msg, m.keys.Back):
			if len(m.stack) > 0 {
				top := m.stack[len(m.stack)-1]
				m.stack = m.stack[:len(m.stack)-1]
				m.current = top.view
				m.currentName = top.name
				return m, nil
			}
		case key(msg, m.keys.Cancel):
			// esc pops the drill-in stack unless the current view wants
			// to consume esc itself (e.g., clearing a filter).
			if len(m.stack) > 0 {
				if f, ok := m.current.(views.Filterable); !ok || !f.Filtering() {
					top := m.stack[len(m.stack)-1]
					m.stack = m.stack[:len(m.stack)-1]
					m.current = top.view
					m.currentName = top.name
					return m, nil
				}
			}
		case key(msg, m.keys.Replay):
			return m.dispatch(":undo")
		}
		if msg.String() == "tab" && m.secondary != nil {
			// Cycle focus across all live panes. Rotate by one step so the
			// focused (primary) slot becomes the next pane in order.
			panes := []*views.View{&m.current, &m.secondary, &m.tertiary, &m.quaternary}
			names := []*string{&m.currentName, &m.secondaryName, &m.tertiaryName, &m.quatName}
			// Compact out nil panes preserving order.
			var ps []*views.View
			var ns []*string
			for i := range panes {
				if *panes[i] != nil {
					ps = append(ps, panes[i])
					ns = append(ns, names[i])
				}
			}
			if len(ps) > 1 {
				firstV, firstN := *ps[0], *ns[0]
				for i := 0; i < len(ps)-1; i++ {
					*ps[i] = *ps[i+1]
					*ns[i] = *ns[i+1]
				}
				*ps[len(ps)-1] = firstV
				*ns[len(ps)-1] = firstN
			}
			m.cfg.LastSplit.Focused = m.currentName
			_ = m.cfg.Save()
			return m, nil
		}
		if m.secondary != nil {
			switch msg.String() {
			case "+", "=":
				m.splitRatio = clampRatio(m.splitRatio + 0.05)
				m.persistSplitRatio()
				return m.broadcastSize()
			case "-":
				m.splitRatio = clampRatio(m.splitRatio - 0.05)
				m.persistSplitRatio()
				return m.broadcastSize()
			case "]":
				if m.quaternary != nil {
					m.quatRatio = clampRatio(m.quatRatio + 0.05)
					return m.broadcastSize()
				}
			case "[":
				if m.quaternary != nil {
					m.quatRatio = clampRatio(m.quatRatio - 0.05)
					return m.broadcastSize()
				}
			}
		}
		if m.helpOpen {
			switch s := msg.String(); s {
			case "esc":
				if m.helpFilter != "" {
					m.helpFilter = ""
				} else {
					m.helpOpen = false
				}
				return m, nil
			case "backspace":
				if n := len(m.helpFilter); n > 0 {
					m.helpFilter = m.helpFilter[:n-1]
				}
				return m, nil
			default:
				if len(s) == 1 {
					m.helpFilter += s
				}
				return m, nil
			}
		}
	}

	var cmds []tea.Cmd
	if m.current != nil {
		next, cmd := m.current.Update(msg)
		m.current = asView(next, m.current)
		cmds = append(cmds, cmd)
	}
	// Forward non-key messages (ticks, loaded msgs) to the inactive panes so
	// their refresh loops keep ticking. Key input only goes to the primary.
	if _, isKey := msg.(tea.KeyMsg); !isKey {
		if m.secondary != nil {
			next, cmd := m.secondary.Update(msg)
			m.secondary = asView(next, m.secondary)
			cmds = append(cmds, cmd)
		}
		if m.tertiary != nil {
			next, cmd := m.tertiary.Update(msg)
			m.tertiary = asView(next, m.tertiary)
			cmds = append(cmds, cmd)
		}
		if m.quaternary != nil {
			next, cmd := m.quaternary.Update(msg)
			m.quaternary = asView(next, m.quaternary)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	header := m.headerView()
	body := ""
	switch {
	case m.prompt != nil:
		body = m.prompt.View()
	case m.confirm != nil:
		body = m.confirm.View()
	case m.typedConfirm != nil:
		body = m.typedConfirm.View()
	case m.form != nil:
		body = m.form.View()
	case m.detail != nil:
		body = m.detail.View()
	case m.helpOpen && m.current != nil:
		body = renderHelp(m.theme, m.current, m.helpFilter, m.layoutSummaries(), m.foldHints())
	case m.secondary != nil && m.current != nil:
		if m.secondaryFolded() {
			body = m.current.View()
			break
		}
		middle := m.secondary.View()
		if m.tertiary != nil && !m.tertiaryFolded() {
			vbar := lipgloss.NewStyle().Foreground(m.theme.Border).Render("│")
			middle = lipgloss.JoinHorizontal(lipgloss.Top,
				m.secondary.View(), vbar, m.tertiary.View())
		}
		parts := []string{m.current.View(), m.splitDivider(), middle}
		if m.quaternary != nil && !m.quaternaryFolded() {
			parts = append(parts, m.splitDivider(), m.quaternary.View())
		}
		body = lipgloss.JoinVertical(lipgloss.Left, parts...)
	case m.current != nil:
		body = m.current.View()
	}
	footer := m.footerView()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// broadcastSize sends synthetic WindowSizeMsg events to all visible panes
// using the current splitRatio. Returns the mutated model so the caller can
// rebind it.
func (m model) broadcastSize() (model, tea.Cmd) {
	var cmds []tea.Cmd
	primaryH, midH, quatH := m.h, 0, 0
	switch {
	case m.secondary != nil && m.secondaryFolded():
		// Bottom row folded away — primary owns the screen.
		primaryH = m.h
	case m.quaternary != nil && !m.quaternaryFolded():
		// Three horizontal strips. splitRatio sizes primary; quatRatio sizes
		// the bottom strip; middle gets the leftover.
		primaryH = int(float64(m.h) * m.splitRatio)
		quatH = int(float64(m.h) * m.quatRatio)
		midH = m.h - primaryH - quatH - 2 // -2 for two dividers
	case m.secondary != nil:
		primaryH = int(float64(m.h) * m.splitRatio)
		if primaryH < 5 {
			primaryH = 5
		}
		midH = m.h - primaryH - 1
	}
	if primaryH < 5 {
		primaryH = 5
	}
	if midH < 5 && m.secondary != nil {
		midH = 5
	}
	if quatH < 5 && m.quaternary != nil {
		quatH = 5
	}

	send := func(v views.View, w, h int) (views.View, tea.Cmd) {
		if v == nil {
			return v, nil
		}
		next, cmd := v.Update(tea.WindowSizeMsg{Width: w, Height: h})
		return asView(next, v), cmd
	}
	if m.current != nil {
		next, cmd := send(m.current, m.w, primaryH)
		m.current = next
		cmds = append(cmds, cmd)
	}
	folded := m.tertiaryFolded()
	if m.secondary != nil {
		w := m.w
		if m.tertiary != nil && !folded {
			w = m.w / 2
		}
		next, cmd := send(m.secondary, w, midH)
		m.secondary = next
		cmds = append(cmds, cmd)
	}
	if m.tertiary != nil && !folded {
		next, cmd := send(m.tertiary, m.w-m.w/2-1, midH)
		m.tertiary = next
		cmds = append(cmds, cmd)
	}
	if m.quaternary != nil {
		next, cmd := send(m.quaternary, m.w, quatH)
		m.quaternary = next
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

type previewRefreshMsg struct{}

type auditClearedMsg struct{ removed int }

type cachePrunedMsg struct {
	path string
	err  error
}

type statsResetMsg struct{ wipedDisk bool }

type bookmarkClearedMsg struct {
	kind  string
	count int
}

// previewTitle includes a Δ marker when the focused row's live JSON differs
// from its most recent snapshot. Snapshots only exist for bookmarked rows.
func previewTitle(kind, id, body string) string {
	t := kind + " · " + id
	if snap, err := views.LoadSnapshot(kind, id); err == nil && string(snap) != body {
		t += " · Δ drifted"
	}
	return t
}

// previewTick schedules the next follow refresh. Uses the cfg refresh
// interval (2s by default) so it matches the listView cadence.
func (m model) previewTick() tea.Cmd {
	d := m.cfg.Refresh
	if d <= 0 {
		d = 2 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return previewRefreshMsg{} })
}

func (m model) dispatchLayout(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.status = "usage: :layout save|load|list|delete <name>"
		return m, nil
	}
	switch args[0] {
	case "save":
		if len(args) < 2 {
			m.status = "usage: :layout save <name>"
			return m, nil
		}
		name := args[1]
		if m.cfg.Layouts == nil {
			m.cfg.Layouts = map[string]config.NamedLayout{}
		}
		m.cfg.Layouts[name] = config.NamedLayout{
			Primary:    m.currentName,
			Secondary:  m.secondaryName,
			Tertiary:   m.tertiaryName,
			Quaternary: m.quatName,
			Ratio:      m.splitRatio,
			QuatRatio:  m.quatRatio,
		}
		_ = m.cfg.Save()
		m.status = "saved layout " + name
		return m, nil

	case "load":
		if len(args) < 2 {
			m.status = "usage: :layout load <name>"
			return m, nil
		}
		l, ok := m.cfg.Layouts[args[1]]
		if !ok {
			m.status = "no such layout: " + args[1]
			return m, nil
		}
		return m.applyLayout(l)

	case "list":
		var b strings.Builder
		if len(m.cfg.Layouts) == 0 {
			b.WriteString("(no saved layouts — use `:layout save <name>` to capture the current panes)")
		} else {
			names := make([]string, 0, len(m.cfg.Layouts))
			for n := range m.cfg.Layouts {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				fmt.Fprintf(&b, "%s: %s\n", n, describeLayout(m.cfg.Layouts[n]))
			}
		}
		m.detail = newDetailModal("saved layouts", b.String(), m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "delete":
		if len(args) < 2 {
			m.status = "usage: :layout delete <name>"
			return m, nil
		}
		if _, ok := m.cfg.Layouts[args[1]]; !ok {
			m.status = "no such layout: " + args[1]
			return m, nil
		}
		delete(m.cfg.Layouts, args[1])
		_ = m.cfg.Save()
		m.status = "deleted layout " + args[1]
		return m, nil

	case "rename":
		if len(args) < 3 {
			m.status = "usage: :layout rename <old> <new>"
			return m, nil
		}
		oldName, newName := args[1], args[2]
		l, ok := m.cfg.Layouts[oldName]
		if !ok {
			m.status = "no such layout: " + oldName
			return m, nil
		}
		if _, exists := m.cfg.Layouts[newName]; exists {
			m.status = "name already in use: " + newName
			return m, nil
		}
		delete(m.cfg.Layouts, oldName)
		m.cfg.Layouts[newName] = l
		_ = m.cfg.Save()
		audit.Append(audit.Entry{
			Action: "layout-rename",
			Kind:   "layout",
			ID:     oldName,
			Label:  newName,
		})
		m.status = fmt.Sprintf("renamed layout %s → %s", oldName, newName)
		return m, nil

	case "export":
		if len(args) < 3 {
			m.status = "usage: :layout export <name> <path> [--json]"
			return m, nil
		}
		l, ok := m.cfg.Layouts[args[1]]
		if !ok {
			m.status = "no such layout: " + args[1]
			return m, nil
		}
		path := expandHomePath(args[2])
		asJSON := len(args) >= 4 && (args[3] == "--json" || args[3] == "json")
		doc := struct {
			LayoutVersion int                `yaml:"layout_version" json:"layout_version"`
			Name          string             `yaml:"name" json:"name"`
			Layout        config.NamedLayout `yaml:"layout" json:"layout"`
		}{LayoutVersion: 1, Name: args[1], Layout: l}
		var (
			data []byte
			err  error
		)
		if asJSON {
			data, err = json.MarshalIndent(doc, "", "  ")
		} else {
			data, err = yaml.Marshal(doc)
		}
		if err != nil {
			m.status = "export marshal: " + err.Error()
			return m, nil
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			m.status = "export write: " + err.Error()
			return m, nil
		}
		fmtName := "yaml"
		if asJSON {
			fmtName = "json"
		}
		m.status = fmt.Sprintf("exported layout %s (%s) → %s", args[1], fmtName, path)
		return m, nil

	case "import":
		if len(args) < 2 {
			m.status = "usage: :layout import <path> [name]"
			return m, nil
		}
		path := expandHomePath(args[1])
		data, err := os.ReadFile(path)
		if err != nil {
			m.status = "import read: " + err.Error()
			return m, nil
		}
		var doc struct {
			LayoutVersion int                `yaml:"layout_version"`
			Name          string             `yaml:"name"`
			Layout        config.NamedLayout `yaml:"layout"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			m.status = "import parse: " + err.Error()
			return m, nil
		}
		if doc.LayoutVersion > 1 {
			m.status = fmt.Sprintf("import: file uses layout_version %d, this build understands 1", doc.LayoutVersion)
			return m, nil
		}
		name := doc.Name
		if len(args) >= 3 {
			name = args[2]
		}
		if name == "" {
			m.status = "import: missing name (set in file or pass as arg)"
			return m, nil
		}
		if m.cfg.Layouts == nil {
			m.cfg.Layouts = map[string]config.NamedLayout{}
		}
		m.cfg.Layouts[name] = doc.Layout
		_ = m.cfg.Save()
		m.status = "imported layout " + name + " from " + path
		return m, nil

	case "pin":
		if len(args) < 3 {
			m.status = "usage: :layout pin <name> <base-url>"
			return m, nil
		}
		name := args[1]
		baseURL := args[2]
		l, ok := m.cfg.Layouts[name]
		if !ok {
			m.status = "no such layout: " + name
			return m, nil
		}
		digest := m.cfg.ActiveLayoutDigest(name)
		if digest == "" {
			data, err := yaml.Marshal(struct {
				LayoutVersion int                `yaml:"layout_version"`
				Name          string             `yaml:"name"`
				Layout        config.NamedLayout `yaml:"layout"`
			}{LayoutVersion: 1, Name: name, Layout: l})
			if err != nil {
				m.status = "pin marshal: " + err.Error()
				return m, nil
			}
			sum := sha256.Sum256(data)
			digest = hex.EncodeToString(sum[:])
		}
		parsed, err := neturl.Parse(baseURL)
		if err != nil {
			m.status = "pin: invalid url: " + err.Error()
			return m, nil
		}
		q := parsed.Query()
		q.Set("sha256", digest)
		parsed.RawQuery = q.Encode()
		m.detail = newDetailModal("pin "+name,
			fmt.Sprintf("Pinned URL (digest %s):\n\n%s\n", digest, parsed.String()),
			m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "share":
		if len(args) < 3 {
			m.status = "usage: :layout share <name> <base-url>"
			return m, nil
		}
		name := args[1]
		baseURL := args[2]
		l, ok := m.cfg.Layouts[name]
		if !ok {
			m.status = "no such layout: " + name
			return m, nil
		}
		digest := m.cfg.ActiveLayoutDigest(name)
		if digest == "" {
			data, err := yaml.Marshal(struct {
				LayoutVersion int                `yaml:"layout_version"`
				Name          string             `yaml:"name"`
				Layout        config.NamedLayout `yaml:"layout"`
			}{LayoutVersion: 1, Name: name, Layout: l})
			if err != nil {
				m.status = "share: " + err.Error()
				return m, nil
			}
			sum := sha256.Sum256(data)
			digest = hex.EncodeToString(sum[:])
		}
		parsed, err := neturl.Parse(baseURL)
		if err != nil {
			m.status = "share: invalid url: " + err.Error()
			return m, nil
		}
		q := parsed.Query()
		q.Set("sha256", digest)
		parsed.RawQuery = q.Encode()
		shareURL := parsed.String()
		// Trailing arg `open=true` launches the URL via the OS browser
		// opener. Keeps the gesture explicit so it can't accidentally fire
		// during normal sharing.
		if len(args) >= 4 && (args[3] == "open=true" || args[3] == "open") {
			if err := openBrowser(shareURL); err != nil {
				m.status = "share open: " + err.Error()
				return m, nil
			}
		}
		body := fmt.Sprintf("Paste this URL into `:layout import-from`:\n\n%s\n\nDigest is computed over the canonical YAML; serve identical bytes at the URL.", shareURL)
		m.detail = newDetailModal("share "+name, body, m.theme, m.w, m.h, nil)
		return m, nil

	case "import-from":
		if len(args) < 2 {
			m.status = "usage: :layout import-from <url> [name]"
			return m, nil
		}
		url := args[1]
		override := ""
		if len(args) >= 3 {
			override = args[2]
		}
		name, driftWarn, err := importLayoutFromURL(m.cfg, url, override)
		if err != nil {
			audit.Append(audit.Entry{
				Action:  "layout-import-from",
				Kind:    "layout",
				Account: m.cfg.DefaultAccount,
				Label:   url,
				Err:     err.Error(),
			})
			m.status = "import-from: " + err.Error()
			return m, nil
		}
		_ = m.cfg.Save()
		audit.Append(audit.Entry{
			Action:  "layout-import-from",
			Kind:    "layout",
			Account: m.cfg.DefaultAccount,
			ID:      name,
			Label:   url,
		})
		if driftWarn != "" {
			m.status = "imported " + name + " (warning: " + driftWarn + ")"
		} else {
			m.status = "imported layout " + name + " from " + url
		}
		return m, nil

	default:
		m.status = "usage: :layout save|load|list|delete|rename|export|import|import-from|share|pin <name>"
		return m, nil
	}
}

// importLayoutFromURL fetches a layout YAML, optionally verifies sha256 from
// the URL query string, caches the raw download, and writes it into cfg.
// Mirrors the `linode-tui layout import-from` CLI subcommand. The returned
// driftWarn is non-empty when the digest changed vs. the last-seen value
// for the same name (only when no sha256 pin was supplied).
func importLayoutFromURL(cfg *config.Config, rawURL, override string) (name, driftWarn string, err error) {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid url: %w", err)
	}
	expectedSum := parsed.Query().Get("sha256")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("fetch: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	gotSum := sha256.Sum256(data)
	gotDigest := hex.EncodeToString(gotSum[:])
	if expectedSum != "" && gotDigest != expectedSum {
		return "", "", fmt.Errorf("sha256 mismatch: got %s, want %s", gotDigest, expectedSum)
	}

	var doc struct {
		LayoutVersion int                `yaml:"layout_version"`
		Name          string             `yaml:"name"`
		Layout        config.NamedLayout `yaml:"layout"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", "", fmt.Errorf("parse: %w", err)
	}
	if doc.LayoutVersion > 1 {
		return "", "", fmt.Errorf("file uses layout_version %d; this build understands 1", doc.LayoutVersion)
	}
	name = doc.Name
	if override != "" {
		name = override
	}
	if name == "" {
		return "", "", fmt.Errorf("missing name (set in file or pass as 2nd arg)")
	}

	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "linode-tui", "layouts")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(dir, name+".yaml"), data, 0o644)
		}
	}

	if cfg.Layouts == nil {
		cfg.Layouts = map[string]config.NamedLayout{}
	}
	cfg.Layouts[name] = doc.Layout

	if expectedSum == "" {
		if prev := cfg.ActiveLayoutDigest(name); prev != "" && prev != gotDigest {
			driftWarn = fmt.Sprintf("digest changed (%s → %s); pin with ?sha256=%s", prev, gotDigest, gotDigest)
		}
	}
	cfg.RecordLayoutDigest(name, gotDigest)

	return name, driftWarn, nil
}

// applyLayout restores a NamedLayout: instantiates each pane fresh and rebroadcasts size.
func (m model) applyLayout(l config.NamedLayout) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if l.Primary != "" {
		if f, ok := views.Resolve(l.Primary); ok {
			m.current = f(m.deps())
			m.currentName = l.Primary
			cmds = append(cmds, m.current.Init())
		}
	}
	m.secondary, m.secondaryName = nil, ""
	m.tertiary, m.tertiaryName = nil, ""
	m.quaternary, m.quatName = nil, ""
	if l.Secondary != "" {
		if f, ok := views.Resolve(l.Secondary); ok {
			m.secondary = f(m.deps())
			m.secondaryName = l.Secondary
			cmds = append(cmds, m.secondary.Init())
		}
	}
	if l.Tertiary != "" {
		if f, ok := views.Resolve(l.Tertiary); ok {
			m.tertiary = f(m.deps())
			m.tertiaryName = l.Tertiary
			cmds = append(cmds, m.tertiary.Init())
		}
	}
	if l.Quaternary != "" {
		if f, ok := views.Resolve(l.Quaternary); ok {
			m.quaternary = f(m.deps())
			m.quatName = l.Quaternary
			cmds = append(cmds, m.quaternary.Init())
		}
	}
	if l.Ratio > 0 {
		m.splitRatio = clampRatio(l.Ratio)
	}
	if l.QuatRatio > 0 {
		m.quatRatio = clampRatio(l.QuatRatio)
	}
	mm, sizeCmd := m.broadcastSize()
	return mm, tea.Batch(append(cmds, sizeCmd)...)
}

// describeLayout returns a one-line composition string for a named layout.
// Shared by :layout list and the help overlay so they stay in sync.
func describeLayout(l config.NamedLayout) string {
	s := l.Primary
	if l.Secondary != "" {
		s += " │ " + l.Secondary
	}
	if l.Tertiary != "" {
		s += " │ " + l.Tertiary
	}
	if l.Quaternary != "" {
		s += " · " + l.Quaternary + " (below)"
	}
	return s
}

// layoutSummaries renders a short description per saved layout for the help
// overlay's "Saved layouts" section.
func (m model) layoutSummaries() map[string]string {
	if len(m.cfg.Layouts) == 0 {
		return nil
	}
	out := make(map[string]string, len(m.cfg.Layouts))
	for name, l := range m.cfg.Layouts {
		out[name] = describeLayout(l)
	}
	return out
}

// splitPairKey returns the key used to remember per-pair ratios.
func splitPairKey(primary, secondary string) string {
	return primary + "+" + secondary
}

// persistSplitRatio writes the current ratio under both LastSplit and the
// per-pair map.
func (m *model) persistSplitRatio() {
	if m.cfg == nil {
		return
	}
	m.cfg.LastSplit.Ratio = m.splitRatio
	if m.cfg.SplitRatios == nil {
		m.cfg.SplitRatios = map[string]float64{}
	}
	m.cfg.SplitRatios[splitPairKey(m.currentName, m.secondaryName)] = m.splitRatio
	_ = m.cfg.Save()
}

func clampRatio(r float64) float64 {
	if r < 0.2 {
		return 0.2
	}
	if r > 0.8 {
		return 0.8
	}
	return r
}

// refreshDefaults wraps config.RefreshDefaults so existing callers stay
// pointed at the same opinionated preset.
func refreshDefaults() map[string]time.Duration { return config.RefreshDefaults() }

// cmdbarVerbs returns the static command-bar verbs (everything except view
// names). Kept separate from views.Names() so the registry stays the single
// source of truth for views.
func cmdbarVerbs() []string {
	return []string{
		"account",
		"audit",
		"bookmark",
		"cache",
		"clear-account",
		"config",
		"diff",
		"doctor",
		"export",
		"fold-char",
		"layout",
		"new",
		"open",
		"pane",
		"read-only",
		"refresh",
		"replay-from",
		"replay-last",
		"split",
		"split-preview",
		"stats",
		"theme",
		"tools",
		"undo",
		"unsplit",
		"validate",
	}
}

// allCmdbarVerbs returns the union of cmdbarVerbs and view names — the
// complete set of first-token completions for the cmdbar.
func allCmdbarVerbs() []string {
	verbs := cmdbarVerbs()
	verbs = append(verbs, views.Names()...)
	sort.Strings(verbs)
	// Dedupe in case any verb name collides with a view name.
	out := verbs[:0]
	var prev string
	for i, v := range verbs {
		if i == 0 || v != prev {
			out = append(out, v)
			prev = v
		}
	}
	return out
}

// openBrowser hands a URL off to the OS browser launcher. Returns the
// underlying error if the platform's opener isn't available.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func renderBar(value, max int64, width int) string {
	if max <= 0 || width <= 0 {
		return ""
	}
	filled := int(float64(value) / float64(max) * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("·", width-filled)
}


func (m model) tertiaryFolded() bool {
	bp := m.cfg.FoldWidthTertiary
	if bp <= 0 {
		bp = 120
	}
	return m.tertiary != nil && m.w > 0 && m.w < bp
}

func (m model) secondaryFolded() bool {
	bp := m.cfg.FoldWidthSecondary
	if bp <= 0 {
		bp = 80
	}
	return m.secondary != nil && m.w > 0 && m.w < bp
}

func (m model) quaternaryFolded() bool {
	bp := m.cfg.FoldHeightQuaternary
	if bp <= 0 {
		bp = 30
	}
	return m.quaternary != nil && m.h > 0 && m.h < bp
}

// splitDivider renders the horizontal separator between split panes with
// labels for which pane is focused (top, gets keyboard) vs. background. The
// focused name is inverse-video so the eye locks on it.
func (m model) splitDivider() string {
	focused := lipgloss.NewStyle().
		Foreground(m.theme.Bg).
		Background(m.theme.Primary).
		Bold(true).
		Padding(0, 1)
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted).Padding(0, 1)
	border := lipgloss.NewStyle().Foreground(m.theme.Border)

	left := focused.Render(fmt.Sprintf("▶ %s", m.currentName))
	rightLabel := m.secondaryName + " · tab to focus"
	fc := m.foldGlyph()
	if m.tertiaryFolded() {
		rightLabel += " · " + fc + m.tertiaryName + " (folded)"
	}
	if m.quaternaryFolded() {
		rightLabel += " · " + fc + m.quatName + " (folded)"
	}
	right := muted.Render(rightLabel)
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if gap < 1 {
		gap = 1
	}
	return border.Render("── ") + left + border.Render(strings.Repeat("─", gap)) + right + border.Render(" ──")
}

func (m model) headerView() string {
	titleStyle := lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true)
	sep := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(" · ")
	accentStyle := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true)

	left := titleStyle.Render("linode-tui")
	// Prefer the API-resolved username when the active account is the
	// synthetic "__cli__" placeholder (i.e., token came from
	// LINODE_TOKEN with no named account). Falls back to the account
	// name otherwise.
	label := ""
	switch acct := m.cfg.DefaultAccount; {
	case acct == "__cli__" || acct == "":
		if m.username != "" {
			label = m.username
		}
	default:
		label = acct
	}
	if label != "" {
		left += sep + accentStyle.Render("@"+label)
	}
	if m.current != nil {
		left += sep + titleStyle.Render(m.current.Title())
		if c, ok := m.current.(views.Counter); ok {
			total, bookmarked, visible := c.Counts()
			parts := []string{fmt.Sprintf("%d", total)}
			if bookmarked > 0 {
				parts = append(parts, fmt.Sprintf("★%d", bookmarked))
			}
			if visible != total {
				parts = append(parts, fmt.Sprintf("/%d", visible))
			}
			left += lipgloss.NewStyle().Foreground(m.theme.Muted).Render(" (" + strings.Join(parts, " · ") + ")")
		}
	}
	if m.readOnly {
		pill := lipgloss.NewStyle().
			Foreground(m.theme.Bg).
			Background(m.theme.Warn).
			Bold(true).
			Padding(0, 1).
			Render("READ-ONLY")
		left += " " + pill
	}
	return lipgloss.NewStyle().Width(m.w).Padding(0, 1).Render(left)
}

func (m model) footerView() string {
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted).Padding(0, 1)
	if m.cmd.Active() {
		return m.cmd.View()
	}
	bindings := m.keys.ShortHelp()
	if len(audit.Tail(1)) > 0 {
		bindings = append(bindings, m.keys.Replay)
	}
	hint := m.helpBar.ShortHelpView(bindings)
	if m.secondary != nil {
		focused := lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true).
			Render(fmt.Sprintf("▶ %s", m.currentName))
		hint = focused + " " + muted.Render("· ") + hint
	}
	if folds := m.foldHints(); folds != "" {
		hint = muted.Render(folds+" · ") + hint
	}
	if m.status != "" {
		hint = muted.Render(m.status+" · ") + hint
	} else {
		hint = muted.Render(hint)
	}
	if m.busy() {
		return m.spinner.View() + " " + hint
	}
	return hint
}

func (m model) busy() bool {
	return m.installing != "" || m.installCh != nil || m.form != nil
}

// foldGlyph returns the prefix used for folded pane labels — configurable
// via config.FoldChar; defaults to "+".
func (m model) foldGlyph() string {
	if m.cfg != nil && m.cfg.FoldChar != "" {
		return m.cfg.FoldChar
	}
	return "+"
}

// foldHints summarizes which panes are currently folded due to terminal size,
// for display in the footer. Empty when nothing is folded.
func (m model) foldHints() string {
	var folds []string
	fc := m.foldGlyph()
	if m.secondaryFolded() {
		folds = append(folds, "secondary folded (widen terminal)")
	}
	if m.tertiaryFolded() {
		folds = append(folds, fc+m.tertiaryName+" folded")
	}
	if m.quaternaryFolded() {
		folds = append(folds, fc+m.quatName+" folded (taller)")
	}
	return strings.Join(folds, ", ")
}

func (m model) dispatch(input string) (tea.Model, tea.Cmd) {
	if input == "" {
		return m, nil
	}
	parts := strings.Fields(input)
	head := parts[0]

	switch head {
	case "config":
		if len(parts) >= 2 && parts[1] == "path" {
			m.status = "config: " + m.cfg.Path()
			return m, nil
		}
		if len(parts) < 2 || parts[1] == "show" {
			redacted := *m.cfg
			redactedAccounts := map[string]config.Account{}
			for name, a := range redacted.Accounts {
				if a.Token != "" {
					a.Token = "***redacted***"
				}
				redactedAccounts[name] = a
			}
			redacted.Accounts = redactedAccounts
			data, err := yaml.Marshal(&redacted)
			if err != nil {
				m.status = "config show: " + err.Error()
				return m, nil
			}
			m.detail = newDetailModal("config "+m.cfg.Path(), string(data), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		}
		m.status = "usage: :config show | :config path"
		return m, nil

	case "theme":
		if len(parts) < 2 {
			m.status = "usage: :theme " + strings.Join(theme.Names(), "|") + "  ·  :theme account <name> <theme>"
			return m, nil
		}
		// :theme list — show available themes and per-account overrides
		if parts[1] == "list" {
			var b strings.Builder
			b.WriteString("available themes (sample colors: P/S/A/Ok/Warn/Err):\n")
			for _, n := range theme.Names() {
				if t, ok := theme.ByName(n); ok {
					swatch := func(c lipgloss.Color, label string) string {
						return lipgloss.NewStyle().Foreground(c).Render(label)
					}
					line := fmt.Sprintf("  %-18s  %s %s %s %s %s %s",
						n,
						swatch(t.Primary, "P"),
						swatch(t.Secondary, "S"),
						swatch(t.Accent, "A"),
						swatch(t.Ok, "Ok"),
						swatch(t.Warn, "Warn"),
						swatch(t.Error, "Err"),
					)
					if n == m.cfg.ActiveTheme {
						line += "  ← active"
					}
					b.WriteString(line + "\n")
				}
			}
			b.WriteString(fmt.Sprintf("\nactive (global): %s\n", m.cfg.ActiveTheme))
			if len(m.cfg.Accounts) > 0 {
				b.WriteString("\nper-account overrides:\n")
				names := make([]string, 0, len(m.cfg.Accounts))
				for n := range m.cfg.Accounts {
					if n == "__cli__" {
						continue
					}
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					override := m.cfg.Accounts[n].Theme
					marker := ""
					if n == m.cfg.DefaultAccount {
						marker = " (active)"
					}
					if override == "" {
						b.WriteString(fmt.Sprintf("  %s%s: (inherits global)\n", n, marker))
					} else {
						b.WriteString(fmt.Sprintf("  %s%s: %s\n", n, marker, override))
					}
				}
			}
			m.detail = newDetailModal("themes", b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		}
		// :theme account <name> <theme>
		if parts[1] == "account" {
			if len(parts) < 4 {
				m.status = "usage: :theme account <name> <theme>"
				return m, nil
			}
			name, themeName := parts[2], parts[3]
			if _, ok := m.cfg.Accounts[name]; !ok {
				m.status = "account not found: " + name
				return m, nil
			}
			if _, ok := theme.ByName(themeName); !ok {
				m.status = "unknown theme: " + themeName
				return m, nil
			}
			acct := m.cfg.Accounts[name]
			acct.Theme = themeName
			m.cfg.Accounts[name] = acct
			_ = m.cfg.Save()
			m.status = fmt.Sprintf("account %s theme = %s", name, themeName)
			// If it's the active account, switch live.
			if name == m.cfg.DefaultAccount {
				if t, ok := theme.ByName(themeName); ok {
					m.theme = t
					m.cmd.SetTheme(t)
				}
			}
			return m, nil
		}
		if t, ok := theme.ByName(parts[1]); ok {
			m.theme = t
			m.cfg.ActiveTheme = t.Name
			m.cmd.SetTheme(t)
		} else {
			m.status = "unknown theme: " + parts[1]
		}
		return m, nil

	case "refresh":
		// `:refresh` — show current settings.
		// `:refresh <dur>` — set the global refresh.
		// `:refresh <view> <dur>` — set a per-view override.
		// `:refresh <view> off` — drop a per-view override.
		switch len(parts) {
		case 1:
			var b strings.Builder
			fmt.Fprintf(&b, "global refresh: %s\n\n", m.cfg.Refresh)
			b.WriteString("config-wide overrides:\n")
			if len(m.cfg.RefreshOverrides) == 0 {
				b.WriteString("  (none)\n")
			} else {
				names := make([]string, 0, len(m.cfg.RefreshOverrides))
				for n := range m.cfg.RefreshOverrides {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					fmt.Fprintf(&b, "  %-18s %s\n", n, m.cfg.RefreshOverrides[n])
				}
			}
			if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok && len(acct.RefreshOverrides) > 0 {
				fmt.Fprintf(&b, "\naccount %q overrides (take precedence):\n", m.cfg.DefaultAccount)
				names := make([]string, 0, len(acct.RefreshOverrides))
				for n := range acct.RefreshOverrides {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					fmt.Fprintf(&b, "  %-18s %s\n", n, acct.RefreshOverrides[n])
				}
			}
			m.detail = newDetailModal("refresh", b.String(), m.theme, m.w, m.h, nil)
			return m, nil
		case 2:
			if parts[1] == "defaults" || parts[1] == "preset" {
				m.cfg.RefreshOverrides = refreshDefaults()
				_ = m.cfg.Save()
				m.status = fmt.Sprintf("applied %d refresh defaults", len(m.cfg.RefreshOverrides))
				return m, nil
			}
			d, err := time.ParseDuration(parts[1])
			if err != nil {
				m.status = "refresh: " + err.Error()
				return m, nil
			}
			m.cfg.Refresh = d
			_ = m.cfg.Save()
			m.status = "global refresh = " + d.String()
			return m, nil
		case 3:
			view := strings.ToLower(parts[1])
			if parts[2] == "off" || parts[2] == "clear" {
				delete(m.cfg.RefreshOverrides, view)
				_ = m.cfg.Save()
				m.status = "refresh override cleared for " + view
				return m, nil
			}
			d, err := time.ParseDuration(parts[2])
			if err != nil {
				m.status = "refresh: " + err.Error()
				return m, nil
			}
			if m.cfg.RefreshOverrides == nil {
				m.cfg.RefreshOverrides = map[string]time.Duration{}
			}
			m.cfg.RefreshOverrides[view] = d
			_ = m.cfg.Save()
			m.status = fmt.Sprintf("refresh[%s] = %s", view, d)
			return m, nil
		default:
			m.status = "usage: :refresh [<view>] <dur|off>"
			return m, nil
		}

	case "account":
		return m.dispatchAccount(parts[1:])

	case "clear-account":
		return m.dispatchClearAccount(parts[1:])

	case "tools":
		return m.dispatchTools(parts[1:])

	case "new", "create":
		return m.dispatchNew(parts[1:])

	case "layout":
		return m.dispatchLayout(parts[1:])

	case "read-only":
		if len(parts) >= 2 && parts[1] == "why" {
			var b strings.Builder
			if m.readOnly {
				b.WriteString("Read-only is ON.\n\nBlocked actions:\n")
				for _, line := range []string{
					"  • `:new <kind>`           create flows",
					"  • single-row delete (`d`)  any per-row Action",
					"  • bulk delete (`D`)        typed-confirm bulk",
					"  • `:configure tags …`     and `T`/`e`/`z`/`B` keys",
					"  • `:undo execute`         (rebuilds, deletes, …)",
					"  • `:tools install|upgrade`",
					"  • install pipeline (k9s/lazysql lazy fetch)",
				} {
					b.WriteString(line + "\n")
				}
				b.WriteString("\nStill allowed: reads, filter, watchlist, export, theme, account switch, snapshots, drill-ins.")
				b.WriteString("\nToggle off with `:read-only`.")
			} else {
				b.WriteString("Read-only is OFF — all actions enabled.\n\nToggle on with `:read-only`.")
			}
			m.detail = newDetailModal("read-only mode", b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		}
		m.readOnly = !m.readOnly
		m.cfg.ReadOnly = m.readOnly
		_ = m.cfg.Save()
		if m.readOnly {
			m.status = "read-only: ON (mutations blocked)"
		} else {
			m.status = "read-only: OFF"
		}
		return m, nil

	case "reload":
		newCfg, err := config.Load(m.cfg.Path())
		if err != nil {
			m.status = "reload failed: " + err.Error()
			return m, nil
		}
		m.cfg = newCfg
		if t, ok := theme.ByName(activeTheme(m.cfg)); ok {
			m.theme = t
			m.cmd.SetTheme(t)
		}
		var cmds []tea.Cmd
		if m.currentName != "" {
			if f, ok := views.Resolve(m.currentName); ok {
				m.current = f(m.deps())
				cmds = append(cmds, m.current.Init())
			}
		}
		if m.secondaryName != "" {
			if f, ok := views.Resolve(m.secondaryName); ok {
				m.secondary = f(m.deps())
				cmds = append(cmds, m.secondary.Init())
			}
		}
		if m.tertiaryName != "" {
			if f, ok := views.Resolve(m.tertiaryName); ok {
				m.tertiary = f(m.deps())
				cmds = append(cmds, m.tertiary.Init())
			}
		}
		m.status = "reloaded " + m.cfg.Path()
		return m, tea.Batch(cmds...)

	case "fanout", "fan":
		sub := "instances"
		if len(parts) >= 2 {
			sub = parts[1]
		}
		target := ""
		switch sub {
		case "instances", "linodes", "li", "inst":
			target = "fanout_instances"
		case "volumes", "vol":
			target = "fanout_volumes"
		case "nodebalancers", "nb":
			target = "fanout_nodebalancers"
		case "lke", "k8s", "kubernetes", "clusters":
			target = "fanout_lke"
		case "firewalls", "fw":
			target = "fanout_firewalls"
		case "domains", "dns", "dom":
			target = "fanout_domains"
		default:
			m.status = "fanout supports: instances | volumes | nodebalancers | lke | firewalls | domains"
			return m, nil
		}
		f, ok := views.Resolve(target)
		if !ok {
			m.status = "unknown fanout view: " + target
			return m, nil
		}
		deps := m.deps()
		if len(parts) >= 3 {
			deps.Context = map[string]any{"accounts": parts[2]}
		}
		m.current = f(deps)
		m.currentName = target
		return m, m.current.Init()

	case "log":
		body := strings.Join(m.statusLog, "\n")
		if body == "" {
			body = "(no status messages yet)"
		}
		m.detail = newDetailModal("status log", body, m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "snapshots":
		if len(parts) < 3 {
			m.status = "usage: :snapshots <resource> <id>"
			return m, nil
		}
		kind, id := parts[1], parts[2]
		versions := views.ListSnapshots(kind, id)
		var b strings.Builder
		if len(versions) == 0 {
			b.WriteString("no snapshots for " + kind + "/" + id + " — bookmark to capture")
		} else {
			fmt.Fprintf(&b, "snapshots for %s/%s (newest first):\n\n", kind, id)
			for i, v := range versions {
				p, _ := views.SnapshotPath(kind, id)
				size := int64(-1)
				if info, err := os.Stat(filepath.Join(p, v+".json")); err == nil {
					size = info.Size()
				}
				fmt.Fprintf(&b, "  @%d  %s  %d bytes\n", i, v, size)
			}
			b.WriteString("\nuse :diff snapshot " + kind + " " + id + " @N to compare\n")
		}
		m.detail = newDetailModal("snapshots · "+kind+"/"+id, b.String(), m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "undo":
		step := 0
		if len(parts) >= 3 && parts[1] == "step" {
			n, err := strconv.Atoi(parts[2])
			if err != nil || n < 0 {
				m.status = "usage: :undo [step N] [execute]"
				return m, nil
			}
			step = n
		}
		entries := audit.Tail(step + 1)
		if len(entries) == 0 {
			m.status = "no recorded actions to undo"
			return m, nil
		}
		if step >= len(entries) {
			m.status = fmt.Sprintf("only %d entries in audit log (asked for step %d)", len(entries), step)
			return m, nil
		}
		entry := entries[step]
		// `:undo execute` (no step) or `:undo step N execute` runs the inverse.
		last := parts[len(parts)-1]
		if last == "execute" {
			// Only "create" actions are safe to auto-reverse; executeUndo
			// enforces this for any depth.
			return m.executeUndo(entry)
		}
		var b strings.Builder
		stamp := entry.Timestamp.Format("2006-01-02 15:04:05")
		fmt.Fprintf(&b, "entry @%d: %s on %s/%s (%s)\nresult: %s\nat: %s\n\n",
			step, entry.Action, entry.Kind, entry.ID, entry.Label, undoResult(entry), stamp)
		b.WriteString(undoHint(entry))
		if step == 0 && entry.Action == "create" && entry.Err == "" {
			b.WriteString("\n\nrun `:undo execute` to delete this resource (with confirmation).")
		}
		title := "undo · last recorded action"
		if step > 0 {
			title = fmt.Sprintf("undo · @%d (%d entries back)", step, step)
		}
		m.detail = newDetailModal(title, b.String(), m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "cache":
		root, err := cache.Root()
		if err != nil {
			m.status = "cache: " + err.Error()
			return m, nil
		}
		sub := "size"
		if len(parts) >= 2 {
			sub = parts[1]
		}
		switch sub {
		case "size":
			sizes, total, err := cache.SubdirSizes(root)
			if err != nil {
				m.status = "cache: " + err.Error()
				return m, nil
			}
			// Surface audit.log as its own labeled row if it's the only
			// root-level file (typical case). Keep "_" name for anything
			// else.
			if auditPath, err := audit.Path(); err == nil {
				if info, err := os.Stat(auditPath); err == nil {
					sizes["audit.log"] = info.Size()
					if rootBytes, ok := sizes["_"]; ok {
						if rest := rootBytes - info.Size(); rest > 0 {
							sizes["_"] = rest
						} else {
							delete(sizes, "_")
						}
					}
				}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s\n\n", root)
			if len(sizes) == 0 {
				b.WriteString("(empty)\n")
			}
			names := make([]string, 0, len(sizes))
			var maxSz int64
			for n, sz := range sizes {
				names = append(names, n)
				if sz > maxSz {
					maxSz = sz
				}
			}
			sort.Strings(names)
			barStyle := lipgloss.NewStyle().Foreground(m.theme.Accent)
			for _, n := range names {
				bar := renderBar(sizes[n], maxSz, 20)
				fmt.Fprintf(&b, "  %-12s  %10s  %s\n", n, cache.FormatBytes(sizes[n]), barStyle.Render(bar))
			}
			fmt.Fprintf(&b, "\ntotal: %s\n", cache.FormatBytes(total))
			m.detail = newDetailModal("cache size", b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		case "prune":
			if len(parts) < 3 {
				m.status = "usage: :cache prune <subdir|all>"
				return m, nil
			}
			target := parts[2]
			if target == "all" {
				m.typedConfirm = newTypedConfirmModal(
					fmt.Sprintf("type 'prune all' to delete EVERYTHING under %s", root),
					"prune all",
					func() tea.Msg {
						err := os.RemoveAll(root)
						return cachePrunedMsg{path: root, err: err}
					},
				)
				return m, m.typedConfirm.Init()
			}
			path := filepath.Join(root, target)
			if _, err := os.Stat(path); err != nil {
				m.status = "cache prune: " + err.Error()
				return m, nil
			}
			m.typedConfirm = newTypedConfirmModal(
				fmt.Sprintf("type 'prune' to delete %s", path),
				"prune",
				func() tea.Msg {
					err := os.RemoveAll(path)
					return cachePrunedMsg{path: path, err: err}
				},
			)
			return m, m.typedConfirm.Init()
		default:
			m.status = "usage: :cache size | :cache prune <subdir>"
			return m, nil
		}

	case "audit":
		// `:audit` — show last 200 (alias for `:audit tail`)
		// `:audit tail [n]` — show last n entries
		// `:audit purge <dur> [kind]` — prune entries older than dur, optionally filtered by kind
		sub := "tail"
		if len(parts) >= 2 {
			sub = parts[1]
		}
		switch sub {
		case "grep":
			if len(parts) < 3 {
				m.status = "usage: :audit grep <pattern> [--err] [account=<name>]"
				return m, nil
			}
			needle := strings.ToLower(parts[2])
			errOnly := false
			account := ""
			for _, a := range parts[3:] {
				switch {
				case a == "--err" || a == "err":
					errOnly = true
				case strings.HasPrefix(a, "account="):
					account = strings.TrimPrefix(a, "account=")
				}
			}
			pool := audit.Tail(1000)
			var matches []audit.Entry
			for _, e := range pool {
				if errOnly && e.Err == "" {
					continue
				}
				if account != "" && e.Account != account {
					continue
				}
				blob := strings.ToLower(e.Action + " " + e.Kind + " " + e.ID + " " + e.Label + " " + e.Err)
				if strings.Contains(blob, needle) {
					matches = append(matches, e)
				}
			}
			if len(matches) == 0 {
				m.status = "audit grep: no matches for " + parts[2]
				return m, nil
			}
			var b strings.Builder
			for _, e := range matches {
				marker := "✓"
				if e.Err != "" {
					marker = "✗"
				}
				fmt.Fprintf(&b, "%s  %s  %-10s  %s/%s  %s",
					marker, e.Timestamp.Format("2006-01-02 15:04:05"),
					e.Action, e.Kind, e.ID, e.Label)
				if e.Err != "" {
					fmt.Fprintf(&b, "  err=%s", e.Err)
				}
				b.WriteString("\n")
			}
			m.detail = newDetailModal(fmt.Sprintf("audit grep %q (%d hits)", parts[2], len(matches)),
				b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		case "recent":
			n := 10
			errOnly := false
			noMarker := false
			for _, a := range parts[2:] {
				switch a {
				case "--err", "err":
					errOnly = true
				case "--no-marker", "no-marker":
					noMarker = true
				default:
					if v, err := strconv.Atoi(a); err == nil && v > 0 {
						n = v
					}
				}
			}
			// Pull a larger pool when filtering for errors so we still end
			// up with up to n after the filter prunes successes.
			pool := n
			if errOnly {
				pool = n * 10
				if pool > 1000 {
					pool = 1000
				}
			}
			pulled := audit.Tail(pool)
			entries := pulled
			if errOnly {
				entries = entries[:0]
				for _, e := range pulled {
					if e.Err != "" {
						entries = append(entries, e)
					}
				}
				if len(entries) > n {
					entries = entries[:n]
				}
			}
			if len(entries) == 0 {
				if errOnly {
					m.status = "audit: no failed entries"
				} else {
					m.status = "audit: no entries"
				}
				return m, nil
			}
			today := time.Now().UTC().Truncate(24 * time.Hour)
			bold := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true)
			dim := lipgloss.NewStyle().Foreground(m.theme.Muted)
			dotRed := lipgloss.NewStyle().Foreground(m.theme.Error).Render("●")
			dotGreen := lipgloss.NewStyle().Foreground(m.theme.Ok).Render("●")
			dotYellow := lipgloss.NewStyle().Foreground(m.theme.Warn).Render("●")
			var b strings.Builder
			for _, e := range entries {
				label := e.Label
				if label == "" {
					label = e.ID
				}
				stamp := e.Timestamp.Format("01-02 15:04:05")
				line := fmt.Sprintf("%s  %-18s  %s/%s  %s", stamp, e.Action, e.Kind, e.ID, label)
				if e.Err != "" {
					line += "  err=" + e.Err
				}
				var dot string
				switch {
				case e.Err != "":
					dot = dotRed
				case e.Timestamp.UTC().After(today):
					dot = dotGreen
					line = bold.Render(line)
				default:
					dot = dotYellow
					line = dim.Render(line)
				}
				if noMarker {
					b.WriteString(line + "\n")
				} else {
					b.WriteString(dot + " " + line + "\n")
				}
			}
			title := fmt.Sprintf("audit recent (last %d of %d)", len(entries), audit.Count())
			m.detail = newDetailModal(title, b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		case "clear":
			if m.readOnly {
				m.status = "read-only: audit clear blocked"
				return m, nil
			}
			m.typedConfirm = newTypedConfirmModal(
				"type 'clear' to wipe the entire audit log (irreversible)",
				"clear",
				func() tea.Msg {
					removed := audit.PruneOlderThan(time.Now())
					return auditClearedMsg{removed: removed}
				},
			)
			return m, m.typedConfirm.Init()
		case "purge":
			if len(parts) < 3 {
				m.status = "usage: :audit purge <dur> [kind]"
				return m, nil
			}
			d, err := time.ParseDuration(parts[2])
			if err != nil {
				m.status = "audit purge: " + err.Error()
				return m, nil
			}
			cutoff := time.Now().Add(-d)
			var removed int
			if len(parts) >= 4 {
				removed = audit.PruneOlderThanKind(cutoff, parts[3])
			} else {
				removed = audit.PruneOlderThan(cutoff)
			}
			suffix := "ies"
			if removed == 1 {
				suffix = "y"
			}
			m.status = fmt.Sprintf("audit purge: removed %d entr%s older than %s", removed, suffix, d)
			return m, nil
		case "tail":
			n := 200
			filter := audit.Filter{}
			// Bare `:audit` lands here with len(parts)==1, so parts[2:]
			// would be out of range.
			var tailArgs []string
			if len(parts) > 2 {
				tailArgs = parts[2:]
			}
			for _, a := range tailArgs {
				switch {
				case a == "--err" || a == "err":
					filter.ErrOnly = true
				case strings.HasPrefix(a, "account="):
					filter.Account = strings.TrimPrefix(a, "account=")
				default:
					if v, err := strconv.Atoi(a); err == nil && v > 0 {
						n = v
					}
				}
			}
			pool := n
			if filter.Account != "" || filter.ErrOnly {
				pool = n * 10
				if pool > 1000 {
					pool = 1000
				}
			}
			all := audit.Tail(pool)
			entries := all[:0]
			for _, e := range all {
				if filter.Matches(e) {
					entries = append(entries, e)
				}
			}
			if len(entries) > n {
				entries = entries[:n]
			}
			var b strings.Builder
			if len(entries) == 0 {
				b.WriteString("(no mutating actions recorded yet)")
			} else {
				for _, e := range entries {
					marker := "✓"
					if e.Err != "" {
						marker = "✗"
					}
					fmt.Fprintf(&b, "%s  %s  %-8s  %-12s  %-10s  %s",
						marker, e.Timestamp.Format("2006-01-02 15:04:05"),
						e.Action, e.Kind, e.ID, e.Label)
					if e.Err != "" {
						fmt.Fprintf(&b, "  err=%s", e.Err)
					}
					b.WriteString("\n")
				}
			}
			m.detail = newDetailModal(fmt.Sprintf("audit log (last %d)", n), b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		default:
			m.status = "usage: :audit tail [n] | :audit purge <dur> [kind]"
			return m, nil
		}

	case "doctor":
		// `:doctor` runs every check. `:doctor <section> [<section>...]`
		// filters by name. A trailing `--json` token renders JSON instead
		// of the friendly check list. `:doctor fix` runs the small set of
		// fixable cleanups (orphan .tmp files) and reports the result.
		if len(parts) >= 2 && parts[1] == "fix" {
			root, err := cache.Root()
			if err != nil {
				m.status = "doctor fix: " + err.Error()
				return m, nil
			}
			removed := 0
			_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if strings.HasSuffix(d.Name(), ".tmp") {
					if err := os.Remove(p); err == nil {
						removed++
					}
				}
				return nil
			})
			m.status = fmt.Sprintf("doctor fix: removed %d orphan .tmp file(s)", removed)
			return m, nil
		}
		asJSON := false
		sections := map[string]bool{}
		groups := map[string]bool{}
		for _, a := range parts[1:] {
			switch {
			case a == "--json" || a == "json":
				asJSON = true
			case strings.HasPrefix(a, "group="):
				groups[strings.ToLower(strings.TrimPrefix(a, "group="))] = true
			default:
				sections[strings.ToLower(a)] = true
			}
		}
		results := health.Run(context.Background(), m.cfg, health.Options{
			KnownViews: views.Names(),
		})
		if len(sections) > 0 {
			filtered := results[:0]
			for _, r := range results {
				if sections[r.Name] {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}
		if len(groups) > 0 {
			filtered := results[:0]
			for _, r := range results {
				g := r.Group
				if g == "" {
					g = "other"
				}
				if groups[g] {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}
		if len(results) == 0 {
			m.status = "doctor: no checks matched"
			return m, nil
		}
		var body string
		if asJSON {
			data, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				m.status = "doctor json: " + err.Error()
				return m, nil
			}
			body = string(data)
		} else {
			groupOrder := []string{"config", "tools", "runtime", "layout", "other"}
			byGroup := map[string][]health.Result{}
			for _, r := range results {
				g := r.Group
				if g == "" {
					g = "other"
				}
				byGroup[g] = append(byGroup[g], r)
			}
			var b strings.Builder
			firstGroup := true
			for _, g := range groupOrder {
				rows := byGroup[g]
				if len(rows) == 0 {
					continue
				}
				if !firstGroup {
					b.WriteString("\n")
				}
				firstGroup = false
				for _, r := range rows {
					mark := "✓"
					switch {
					case r.OK:
					case r.Optional:
						mark = "~"
					default:
						mark = "✗"
					}
					fmt.Fprintf(&b, "%s  %-10s  %s\n", mark, r.Name, r.Detail)
					if r.Suggestion != "" {
						fmt.Fprintf(&b, "              → %s\n", r.Suggestion)
					}
				}
			}
			body = b.String()
		}
		m.detail = newDetailModal("doctor", body, m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "stats":
		if len(parts) >= 2 && parts[1] == "reset" {
			// `:stats reset` clears only the in-memory counters. `:stats
			// reset all` also removes the persisted file, gated by a typed
			// confirm because it's irreversible.
			if len(parts) >= 3 && parts[2] == "all" {
				m.typedConfirm = newTypedConfirmModal(
					"type 'reset' to wipe in-memory counters AND the on-disk stats file",
					"reset",
					func() tea.Msg {
						removed := false
						if p, err := statsPath(); err == nil {
							if err := os.Remove(p); err == nil {
								removed = true
							}
						}
						return statsResetMsg{wipedDisk: removed}
					},
				)
				return m, m.typedConfirm.Init()
			}
			m.stats = map[string]int{}
			m.status = "stats reset (in-memory only; use ':stats reset all' to wipe disk too)"
			return m, nil
		}
		if len(parts) >= 2 && parts[1] == "post" {
			if m.cfg.StatsEndpoint == "" {
				m.status = "stats_endpoint not set in config"
				return m, nil
			}
			endpoint := m.cfg.StatsEndpoint
			snapshot := map[string]int{}
			for k, v := range m.stats {
				snapshot[k] = v
			}
			uptime := int(time.Since(m.startedAt).Seconds())
			signals := map[string]int{
				"refresh_overrides": len(m.cfg.RefreshOverrides),
				"saved_layouts":     len(m.cfg.Layouts),
				"accounts":          len(m.cfg.Accounts),
				"bookmark_kinds":    len(m.cfg.Bookmarks),
			}
			m.status = "posting stats → " + endpoint + "…"
			return m, func() tea.Msg {
				err := postStats(endpoint, snapshot, uptime, signals)
				return statsPostResultMsg{endpoint: endpoint, err: err}
			}
		}
		body := renderStats(m.stats)
		m.detail = newDetailModal("usage stats (session-local)", body, m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "export":
		return m.dispatchExport(parts[1:])

	case "pane":
		if len(parts) < 3 {
			m.status = "usage: :pane <primary|secondary|tertiary|quaternary> <view>"
			return m, nil
		}
		slot := strings.ToLower(parts[1])
		viewName := parts[2]
		f, ok := views.Resolve(viewName)
		if !ok {
			m.status = "unknown view: " + viewName
			return m, nil
		}
		switch slot {
		case "primary", "p":
			m.current = f(m.deps())
			m.currentName = viewName
			cmd := m.current.Init()
			return m, cmd
		case "secondary", "s":
			m.secondary = f(m.deps())
			m.secondaryName = viewName
			m.cfg.LastSplit.View = viewName
			_ = m.cfg.Save()
			return m, m.secondary.Init()
		case "tertiary", "t":
			m.tertiary = f(m.deps())
			m.tertiaryName = viewName
			m.cfg.LastSplit.Right = viewName
			_ = m.cfg.Save()
			return m, m.tertiary.Init()
		case "quaternary", "q":
			m.quaternary = f(m.deps())
			m.quatName = viewName
			m.cfg.LastSplit.Down = viewName
			_ = m.cfg.Save()
			return m, m.quaternary.Init()
		default:
			m.status = "unknown slot: " + slot
			return m, nil
		}

	case "split":
		if len(parts) < 2 {
			m.status = "usage: :split <view>"
			return m, nil
		}
		name := parts[1]
		f, ok := views.Resolve(name)
		if !ok {
			m.status = "unknown view: " + name
			return m, nil
		}
		m.secondary = f(m.deps())
		m.secondaryName = name
		// Load per-pair ratio if remembered; otherwise keep current.
		if r, ok := m.cfg.SplitRatios[splitPairKey(m.currentName, name)]; ok {
			m.splitRatio = clampRatio(r)
		}
		m.cfg.LastSplit = config.SplitState{View: name, Ratio: m.splitRatio}
		_ = m.cfg.Save()
		return m, m.secondary.Init()

	case "unsplit":
		m.secondary = nil
		m.secondaryName = ""
		m.tertiary = nil
		m.tertiaryName = ""
		m.quaternary = nil
		m.quatName = ""
		m.previewFollow = false
		m.previewKind = ""
		m.cfg.LastSplit = config.SplitState{}
		_ = m.cfg.Save()
		return m, nil

	case "split-preview":
		ident, ok := m.current.(views.Identifiable)
		if !ok {
			m.status = "current view doesn't expose a focused row"
			return m, nil
		}
		id := ident.SelectedID()
		if id == "" {
			m.status = "no row focused"
			return m, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		body, err := resourceJSON(ctx, m.client, m.currentName, id)
		cancel()
		if err != nil {
			m.status = "preview: " + err.Error()
			return m, nil
		}
		m.secondary = views.NewTextView(previewTitle(m.currentName, id, body), body)
		m.secondaryName = "preview"
		m.previewKind = m.currentName
		m.previewFollow = len(parts) >= 2 && parts[1] == "follow"
		var cmds []tea.Cmd
		cmds = append(cmds, m.secondary.Init())
		if m.previewFollow {
			cmds = append(cmds, m.previewTick())
		}
		return m, tea.Batch(cmds...)

	case "split-right":
		if m.secondary == nil {
			m.status = ":split-right needs an existing :split first"
			return m, nil
		}
		if len(parts) < 2 {
			m.status = "usage: :split-right <view>"
			return m, nil
		}
		name := parts[1]
		f, ok := views.Resolve(name)
		if !ok {
			m.status = "unknown view: " + name
			return m, nil
		}
		m.tertiary = f(m.deps())
		m.tertiaryName = name
		m.cfg.LastSplit.Right = name
		_ = m.cfg.Save()
		return m, m.tertiary.Init()

	case "split-down":
		if m.secondary == nil {
			m.status = ":split-down needs an existing :split first"
			return m, nil
		}
		if len(parts) < 2 {
			m.status = "usage: :split-down <view>"
			return m, nil
		}
		name := parts[1]
		f, ok := views.Resolve(name)
		if !ok {
			m.status = "unknown view: " + name
			return m, nil
		}
		m.quaternary = f(m.deps())
		m.quatName = name
		m.cfg.LastSplit.Down = name
		_ = m.cfg.Save()
		return m, m.quaternary.Init()

	case "configure":
		if len(parts) < 3 {
			m.status = "usage: :configure tags <csv>  (acts on the focused row)"
			return m, nil
		}
		if parts[1] != "tags" {
			m.status = "usage: :configure tags <csv>"
			return m, nil
		}
		csv := strings.Join(parts[2:], " ")
		ident, ok := m.current.(views.Identifiable)
		if !ok {
			m.status = "current view doesn't expose a selection"
			return m, nil
		}
		idStr := ident.SelectedID()
		if idStr == "" {
			m.status = "no row focused"
			return m, nil
		}
		idInt, err := strconv.Atoi(idStr)
		if err != nil {
			m.status = ":configure tags only supports numeric-id views (try the form: T)"
			return m, nil
		}
		m.form = newConfigLinodeWithPrefill(m.client, idInt, "selected", views.ConfigureTags, csv)
		m.status = ""
		return m, m.form.Init()

	case "validate":
		// Re-run the same warning set validate-config uses, scoped to the
		// in-memory cfg. We do not re-decode with KnownFields=true here
		// (only file-level parse can do that).
		var warnings []string
		if m.cfg.ActiveTheme != "" {
			if _, ok := theme.ByName(m.cfg.ActiveTheme); !ok {
				warnings = append(warnings, fmt.Sprintf("active_theme %q is not one of: %v", m.cfg.ActiveTheme, theme.Names()))
			}
		}
		for name, acct := range m.cfg.Accounts {
			if name == "__cli__" {
				continue
			}
			if acct.Token == "" && acct.OPRef == "" {
				warnings = append(warnings, fmt.Sprintf("account %q has no token and no op_ref", name))
			}
			if acct.Theme != "" {
				if _, ok := theme.ByName(acct.Theme); !ok {
					warnings = append(warnings, fmt.Sprintf("account %q theme %q is not one of: %v", name, acct.Theme, theme.Names()))
				}
			}
		}
		if m.cfg.DefaultAccount != "" {
			if _, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; !ok {
				warnings = append(warnings, fmt.Sprintf("default_account %q is not in accounts", m.cfg.DefaultAccount))
			}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s\n\n", m.cfg.Path())
		if len(warnings) == 0 {
			b.WriteString("✓ no warnings\n")
		} else {
			for _, w := range warnings {
				b.WriteString("~ " + w + "\n")
			}
			fmt.Fprintf(&b, "\n%d warning(s)\n", len(warnings))
		}
		m.detail = newDetailModal("validate", b.String(), m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "fold-char":
		if len(parts) < 2 {
			cur := m.cfg.FoldChar
			if cur == "" {
				cur = "+ (default)"
			}
			m.status = "fold_char = " + cur + " · usage: :fold-char <char|reset>"
			return m, nil
		}
		if parts[1] == "reset" || parts[1] == "default" {
			m.cfg.FoldChar = ""
			_ = m.cfg.Save()
			m.status = "fold_char reset to default (+)"
			return m, nil
		}
		m.cfg.FoldChar = parts[1]
		_ = m.cfg.Save()
		m.status = "fold_char = " + parts[1]
		return m, nil

	case "bookmark":
		// `:bookmark export <path>` — dump current bookmarks to YAML.
		// `:bookmark list` — show counts per kind.
		if len(parts) < 2 {
			scope := "global"
			if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok && acct.Bookmarks != nil {
				scope = "account=" + m.cfg.DefaultAccount
			}
			m.status = "scope=" + scope + " · usage: :bookmark list|export|import|migrate|mv|clear|scope"
			return m, nil
		}
		switch parts[1] {
		case "list":
			active := m.cfg.ActiveBookmarks()
			var b strings.Builder
			scope := "global"
			if _, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok && m.cfg.Accounts[m.cfg.DefaultAccount].Bookmarks != nil {
				scope = "account=" + m.cfg.DefaultAccount
			}
			fmt.Fprintf(&b, "scope: %s\n\n", scope)
			if len(active) == 0 {
				b.WriteString("(no bookmarks)")
			} else {
				kinds := make([]string, 0, len(active))
				for k := range active {
					kinds = append(kinds, k)
				}
				sort.Strings(kinds)
				for _, k := range kinds {
					fmt.Fprintf(&b, "%-16s %d\n", k, len(active[k]))
				}
			}
			m.detail = newDetailModal("bookmarks", b.String(), m.theme, m.w, m.h, nil)
			return m, m.detail.Init()
		case "scope":
			if len(parts) < 3 {
				cur := "global"
				if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok && acct.Bookmarks != nil {
					cur = "account=" + m.cfg.DefaultAccount
				}
				m.status = "bookmark scope: " + cur + " · usage: :bookmark scope <global|account>"
				return m, nil
			}
			switch parts[2] {
			case "account":
				if m.cfg.DefaultAccount == "" {
					m.status = "bookmark scope: no active account"
					return m, nil
				}
				acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]
				if !ok {
					m.status = "bookmark scope: active account not in config"
					return m, nil
				}
				if acct.Bookmarks == nil {
					acct.Bookmarks = map[string][]string{}
					m.cfg.Accounts[m.cfg.DefaultAccount] = acct
					_ = m.cfg.Save()
				}
				m.status = "bookmark scope = account " + m.cfg.DefaultAccount
				return m, nil
			case "global":
				if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok && acct.Bookmarks != nil {
					acct.Bookmarks = nil
					m.cfg.Accounts[m.cfg.DefaultAccount] = acct
					_ = m.cfg.Save()
				}
				m.status = "bookmark scope = global"
				return m, nil
			default:
				m.status = "bookmark scope: expected global|account"
				return m, nil
			}

		case "migrate":
			dryRun := len(parts) >= 3 && (parts[2] == "--dry-run" || parts[2] == "dry-run")
			if m.cfg.DefaultAccount == "" {
				m.status = "bookmark migrate: no active account"
				return m, nil
			}
			acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]
			if !ok {
				m.status = "bookmark migrate: active account not in config"
				return m, nil
			}
			if len(m.cfg.Bookmarks) == 0 {
				m.status = "bookmark migrate: no global bookmarks to move"
				return m, nil
			}
			moved := 0
			if acct.Bookmarks == nil {
				acct.Bookmarks = map[string][]string{}
			}
			for kind, ids := range m.cfg.Bookmarks {
				existing := map[string]bool{}
				for _, id := range acct.Bookmarks[kind] {
					existing[id] = true
				}
				for _, id := range ids {
					if !existing[id] {
						if !dryRun {
							acct.Bookmarks[kind] = append(acct.Bookmarks[kind], id)
						}
						existing[id] = true
						moved++
					}
				}
			}
			if dryRun {
				m.status = fmt.Sprintf("bookmark migrate (dry-run): would move %d bookmark(s) to account %s", moved, m.cfg.DefaultAccount)
				return m, nil
			}
			m.cfg.Accounts[m.cfg.DefaultAccount] = acct
			m.cfg.Bookmarks = nil
			_ = m.cfg.Save()
			m.status = fmt.Sprintf("bookmark migrate: moved %d bookmark(s) to account %s", moved, m.cfg.DefaultAccount)
			return m, nil

		case "mv":
			if len(parts) < 5 {
				m.status = "usage: :bookmark mv <kind> <from-id> <to-id>"
				return m, nil
			}
			kind, fromID, toID := parts[2], parts[3], parts[4]
			ids := m.cfg.ActiveBookmarks()[kind]
			if len(ids) == 0 {
				m.status = "bookmark mv: no bookmarks under kind " + kind
				return m, nil
			}
			found := false
			for i, id := range ids {
				if id == fromID {
					ids[i] = toID
					found = true
					break
				}
			}
			if !found {
				m.status = fmt.Sprintf("bookmark mv: %s not bookmarked under %s", fromID, kind)
				return m, nil
			}
			// Dedupe in case toID was already bookmarked.
			seen := map[string]bool{}
			deduped := ids[:0]
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					deduped = append(deduped, id)
				}
			}
			m.cfg.SetBookmarks(kind, deduped)
			_ = m.cfg.Save()
			m.status = fmt.Sprintf("bookmark mv: %s/%s → %s", kind, fromID, toID)
			return m, nil

		case "clear":
			if len(parts) < 3 {
				m.status = "usage: :bookmark clear <kind>"
				return m, nil
			}
			kind := parts[2]
			if _, ok := m.cfg.Bookmarks[kind]; !ok {
				m.status = "bookmark clear: no bookmarks under kind " + kind
				return m, nil
			}
			count := len(m.cfg.Bookmarks[kind])
			m.typedConfirm = newTypedConfirmModal(
				fmt.Sprintf("type 'clear %s' to remove %d bookmark(s)", kind, count),
				"clear "+kind,
				func() tea.Msg {
					return bookmarkClearedMsg{kind: kind, count: count}
				},
			)
			return m, m.typedConfirm.Init()
		case "import":
			if len(parts) < 3 {
				m.status = "usage: :bookmark import <path> [merge]"
				return m, nil
			}
			path := expandHomePath(parts[2])
			merge := len(parts) >= 4 && (parts[3] == "merge" || parts[3] == "--merge")
			data, err := os.ReadFile(path)
			if err != nil {
				m.status = "bookmark import read: " + err.Error()
				return m, nil
			}
			var doc struct {
				Version   int                 `yaml:"version"`
				Bookmarks map[string][]string `yaml:"bookmarks"`
			}
			if err := yaml.Unmarshal(data, &doc); err != nil {
				m.status = "bookmark import parse: " + err.Error()
				return m, nil
			}
			if doc.Version > 1 {
				m.status = fmt.Sprintf("bookmark import: file version %d > 1", doc.Version)
				return m, nil
			}
			if !merge || m.cfg.Bookmarks == nil {
				m.cfg.Bookmarks = map[string][]string{}
			}
			added := 0
			for kind, ids := range doc.Bookmarks {
				existing := map[string]bool{}
				for _, id := range m.cfg.Bookmarks[kind] {
					existing[id] = true
				}
				for _, id := range ids {
					if !existing[id] {
						m.cfg.Bookmarks[kind] = append(m.cfg.Bookmarks[kind], id)
						existing[id] = true
						added++
					}
				}
			}
			_ = m.cfg.Save()
			mode := "replace"
			if merge {
				mode = "merge"
			}
			m.status = fmt.Sprintf("bookmark import (%s): +%d new bookmark(s)", mode, added)
			return m, nil
		case "export":
			if len(parts) < 3 {
				m.status = "usage: :bookmark export <path>"
				return m, nil
			}
			path := expandHomePath(parts[2])
			data, err := yaml.Marshal(struct {
				Version   int                 `yaml:"version"`
				Bookmarks map[string][]string `yaml:"bookmarks"`
			}{Version: 1, Bookmarks: m.cfg.Bookmarks})
			if err != nil {
				m.status = "bookmark export: " + err.Error()
				return m, nil
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				m.status = "bookmark export write: " + err.Error()
				return m, nil
			}
			n := 0
			for _, ids := range m.cfg.Bookmarks {
				n += len(ids)
			}
			m.status = fmt.Sprintf("exported %d bookmark(s) → %s", n, path)
			return m, nil
		default:
			m.status = "usage: :bookmark export <path> | :bookmark import <path> [merge] | :bookmark list | :bookmark clear <kind>"
			return m, nil
		}

	case "open":
		if len(parts) < 2 {
			m.status = "usage: :open <resource> [id]"
			return m, nil
		}
		resource := parts[1]
		id := ""
		if len(parts) >= 3 {
			id = parts[2]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		body, err := resourceJSON(ctx, m.client, resource, id)
		if err != nil {
			m.status = "open: " + err.Error()
			return m, nil
		}
		title := resource
		if id != "" {
			title = resource + "/" + id
		}
		m.detail = newDetailModal(title, body, m.theme, m.w, m.h, nil)
		return m, m.detail.Init()

	case "diff":
		if len(parts) >= 4 && parts[1] == "snapshot" {
			resource, id := parts[2], parts[3]
			// Optional 5th arg: @N selects an older snapshot version.
			version := 0
			if len(parts) >= 5 && strings.HasPrefix(parts[4], "@") {
				n, err := strconv.Atoi(strings.TrimPrefix(parts[4], "@"))
				if err != nil || n < 0 {
					m.status = "usage: :diff snapshot <resource> <id> [@N]"
					return m, nil
				}
				version = n
			}
			snap, err := views.LoadSnapshotAt(resource, id, version)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			client := m.client
			th := m.theme
			m.status = fmt.Sprintf("diffing snapshot@%d ↔ current for %s/%s…", version, resource, id)
			return m, func() tea.Msg { return snapshotDiffCmd(client, th, resource, id, snap)() }
		}
		if len(parts) < 4 {
			m.status = "usage: :diff <resource> <id1> <id2>  ·  :diff snapshot <resource> <id>"
			return m, nil
		}
		resource, id1, id2 := parts[1], parts[2], parts[3]
		client := m.client
		th := m.theme
		m.status = fmt.Sprintf("diffing %s/%s ↔ %s/%s…", resource, id1, resource, id2)
		return m, func() tea.Msg { return resourceDiffCmd(client, th, resource, id1, id2)() }
	}

	if f, ok := views.Resolve(head); ok {
		m.current = f(m.deps())
		m.currentName = head
		m.bumpStat("view:" + head)
		return m, m.current.Init()
	}
	m.status = "unknown command: " + head
	return m, nil
}

func key(msg tea.KeyMsg, b interface{ Keys() []string }) bool {
	return slices.Contains(b.Keys(), msg.String())
}

func asView(next tea.Model, prev views.View) views.View {
	if v, ok := next.(views.View); ok {
		return v
	}
	return prev
}

func (m model) dispatchNew(args []string) (tea.Model, tea.Cmd) {
	if m.readOnly {
		m.status = "read-only: :new blocked"
		return m, nil
	}
	if len(args) == 0 {
		m.status = "usage: :new (linode|nodebalancer|volume|vpc|lke)"
		return m, nil
	}
	var f subform
	switch args[0] {
	case "linode", "instance":
		f = newCreateLinodeForCfg(m.client, m.cfg)
	case "nodebalancer", "nb":
		f = newCreateNodeBalancer(m.client)
	case "volume", "vol":
		f = newCreateVolume(m.client)
	case "vpc":
		f = newCreateVPC(m.client)
	case "lke", "cluster", "kubernetes":
		f = newCreateLKE(m.client)
	default:
		m.status = "unknown resource for :new: " + args[0]
		return m, nil
	}
	m.form = f
	m.status = ""
	return m, f.Init()
}

func (m model) finishForm() (tea.Model, tea.Cmd) {
	f := m.form
	m.form = nil
	if p, ok := f.(*accountPicker); ok {
		name := p.Selected()
		if name == "" {
			m.status = "account switch cancelled"
			return m, nil
		}
		return m.dispatchAccount([]string{name})
	}
	if result := f.Result(); result != "" {
		m.status = result
		if m.current != nil {
			next, cmd := m.current.Update(views.ActionDoneMsg{Label: "form"})
			m.current = asView(next, m.current)
			return m, cmd
		}
		return m, nil
	}
	if err := f.Err(); err != nil {
		m.status = "failed: " + err.Error()
	} else {
		m.status = "cancelled"
	}
	return m, nil
}

func (m model) finishConfirm() (tea.Model, tea.Cmd) {
	c := m.confirm
	m.confirm = nil
	if c.Confirmed() && c.onYes != nil {
		return m, c.onYes
	}
	m.status = "cancelled"
	return m, nil
}

func (m model) finishPrompt() (tea.Model, tea.Cmd) {
	p := m.prompt
	kind := p.kind
	dir := p.Result()
	m.prompt = nil
	if dir == "" {
		m.status = fmt.Sprintf("install %s cancelled", kind)
		if m.pendingDrill != nil && m.pendingDrill.Cleanup != nil {
			m.pendingDrill.Cleanup()
		}
		m.pendingDrill = nil
		return m, nil
	}
	m.cfg.Tools.InstallDir = dir
	m.installing = kind
	m.status = fmt.Sprintf("installing %s → %s…", kind, dir)
	ch, cmd := installCmdStream(m.cfg, kind)
	m.installCh = ch
	return m, cmd
}

func (m model) dispatchAccount(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		names := make([]string, 0, len(m.cfg.Accounts))
		for n := range m.cfg.Accounts {
			if n == "__cli__" {
				continue
			}
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			m.status = "no accounts configured (active = " + m.cfg.DefaultAccount + ")"
			return m, nil
		}
		m.form = newAccountPicker(names, m.cfg.DefaultAccount)
		m.status = ""
		return m, m.form.Init()
	}
	name := args[0]
	if name == m.cfg.DefaultAccount {
		m.status = "already on account: " + name
		return m, nil
	}
	if _, ok := m.cfg.Accounts[name]; !ok {
		m.status = "account not found: " + name
		return m, nil
	}
	m.status = "switching to " + name + "…"
	cfg := m.cfg
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := linode.ResolveTokenForAccount(ctx, cfg, name)
		if err != nil {
			return accountSwitchedMsg{Name: name, Err: err}
		}
		return accountSwitchedMsg{Name: name, Token: tok}
	}
}

// bumpStat increments a named counter on the receiver. Counters are session-
// local; reset on every launch. We never write them to disk to avoid an
// implicit telemetry footprint — :stats simply shows what's been clicked
// during the current session.
func (m *model) bumpStat(key string) {
	if m.stats == nil {
		m.stats = map[string]int{}
	}
	m.stats[key]++
	if m.cfg != nil && m.cfg.StatsEnabled {
		saveStats(m.stats)
	}
}

// executeUndo dispatches the inverse of the entry. Only "create" actions
// have a safe automatic reversal (delete the just-created resource); everything
// else requires manual review.
func (m model) executeUndo(e audit.Entry) (tea.Model, tea.Cmd) {
	if e.Action != "create" {
		m.status = "no safe auto-undo for action " + e.Action + " — see `:undo` for hints"
		return m, nil
	}
	if e.Err != "" {
		m.status = "last create failed — nothing to undo"
		return m, nil
	}
	idStr := e.ID
	idInt, err := strconv.Atoi(idStr)
	if err != nil {
		m.status = ":undo execute: non-numeric id " + idStr
		return m, nil
	}
	client := m.client
	kind := e.Kind
	prompt := fmt.Sprintf("DELETE just-created %s id=%s (%s)? This cannot be undone.", kind, idStr, e.Label)
	cmd := func() tea.Msg {
		return views.ConfirmMsg{
			Prompt: prompt,
			OnYes: func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := deleteByKind(ctx, client, kind, idInt)
				audit.Append(audit.Entry{
					Action: "undo-create",
					Kind:   kind,
					ID:     idStr,
					Label:  e.Label,
					Err:    errMsg(err),
				})
				if err != nil {
					return views.ActionErrorMsg{Label: "undo create", Err: err}
				}
				return views.ActionDoneMsg{Label: "undo create"}
			},
		}
	}
	return m, cmd
}

// deleteByKind dispatches a delete to the appropriate linodego endpoint.
func deleteByKind(ctx context.Context, c *linode.Client, kind string, id int) error {
	switch kind {
	case "instances":
		return c.Raw().DeleteInstance(ctx, id)
	case "volumes":
		return c.Raw().DeleteVolume(ctx, id)
	case "nodebalancers":
		return c.Raw().DeleteNodeBalancer(ctx, id)
	case "lke":
		return c.Raw().DeleteLKECluster(ctx, id)
	case "vpcs":
		return c.Raw().DeleteVPC(ctx, id)
	default:
		return fmt.Errorf("don't know how to delete kind %q", kind)
	}
}

// undoResult returns "ok" or "failed" depending on the entry's err field.
func undoResult(e audit.Entry) string {
	if e.Err != "" {
		return "failed (" + e.Err + ")"
	}
	return "ok"
}

// undoHint produces a human-readable suggestion for reversing the action.
// Most operations aren't safely auto-reversible — we describe the manual path
// rather than executing it.
func undoHint(e audit.Entry) string {
	if e.Err != "" {
		return "Last action failed — nothing to undo."
	}
	switch e.Action {
	case "delete", "bulk-delete":
		hint := "Resource is gone. "
		if _, err := views.LoadSnapshot(e.Kind, e.ID); err == nil {
			hint += "A snapshot exists: `:diff snapshot " + e.Kind + " " + e.ID + "` shows its previous state.\n"
			hint += "Use `:new " + singularKind(e.Kind) + "` and fill the form from the snapshot to recreate."
		} else {
			hint += "No snapshot was captured; recreate manually via `:new " + singularKind(e.Kind) + "`."
		}
		return hint
	case "create":
		return "To reverse: run `d` (or `D` if bulk) on " + e.Kind + " row " + e.ID + "."
	case "edit", "tags", "resize", "rebuild":
		if _, err := views.LoadSnapshot(e.Kind, e.ID); err == nil {
			return "To compare: `:diff snapshot " + e.Kind + " " + e.ID + " @1` shows the state before this change.\n" +
				"To restore: use `:configure " + e.Kind + " " + e.ID + "` with values from the older snapshot."
		}
		return "No snapshot captured before this change — manual recovery only."
	default:
		return "(no automated reversal known for this action)"
	}
}

// singularKind maps a plural view kind to the singular `:new` argument.
func singularKind(plural string) string {
	switch plural {
	case "instances":
		return "linode"
	case "volumes":
		return "volume"
	case "nodebalancers":
		return "nodebalancer"
	case "vpcs":
		return "vpc"
	case "lke":
		return "lke"
	default:
		return plural
	}
}

type statsPostResultMsg struct {
	endpoint string
	err      error
}

// postStats POSTs a snapshot of the counter map as JSON to endpoint. Body
// includes only counter keys+values plus buildinfo (version/commit/OS/arch),
// uptime seconds, and a small set of config signals (counts only, no values)
// — no token, no account names, no host data.
func postStats(endpoint string, counters map[string]int, uptimeSec int, signals map[string]int) error {
	body, err := json.Marshal(map[string]any{
		"counters":       counters,
		"build":          buildinfo.Identity(),
		"uptime_seconds": uptimeSec,
		"config_signals": signals,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Linode-TUI-Version", buildinfo.Version)
	req.Header.Set("User-Agent", "linode-tui/"+buildinfo.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("POST %s: %s", endpoint, resp.Status)
	}
	return nil
}

func renderStats(stats map[string]int) string {
	if len(stats) == 0 {
		return "(no events recorded yet — switch views or run commands to populate)"
	}
	type entry struct {
		k string
		v int
	}
	rows := make([]entry, 0, len(stats))
	for k, v := range stats {
		rows = append(rows, entry{k, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].v != rows[j].v {
			return rows[i].v > rows[j].v
		}
		return rows[i].k < rows[j].k
	})
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%5d  %s\n", r.v, r.k)
	}
	return b.String()
}

func (m model) dispatchExport(args []string) (tea.Model, tea.Cmd) {
	format := "csv"
	if len(args) > 0 {
		format = strings.ToLower(args[0])
	}
	if format != "csv" && format != "json" {
		m.status = "usage: :export csv|json [visible|selected|bookmarked] [path]"
		return m, nil
	}
	exp, ok := m.current.(views.Exportable)
	if !ok || m.current == nil {
		m.status = "current view doesn't support export"
		return m, nil
	}

	scope := views.ExportVisible
	rest := args[1:]
	if len(rest) > 0 {
		switch views.ExportScope(rest[0]) {
		case views.ExportVisible, views.ExportSelected, views.ExportBookmarked:
			scope = views.ExportScope(rest[0])
			rest = rest[1:]
		}
	}

	var (
		data []byte
		err  error
	)
	if format == "json" {
		data, err = exp.ExportJSON(scope)
	} else {
		var s string
		s, err = exp.ExportCSV(scope)
		data = []byte(s)
	}
	if err != nil {
		m.status = "export failed: " + err.Error()
		return m, nil
	}

	path := ""
	if len(rest) >= 1 {
		path = expandHomePath(rest[0])
	} else {
		cache, e := os.UserCacheDir()
		if e != nil {
			m.status = "export: locate cache dir: " + e.Error()
			return m, nil
		}
		dir := filepath.Join(cache, "linode-tui", "exports")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			m.status = "export: mkdir: " + err.Error()
			return m, nil
		}
		safe := strings.NewReplacer(" ", "_", "·", "_").Replace(strings.ToLower(m.current.Title()))
		path = filepath.Join(dir, fmt.Sprintf("%s_%s_%s.%s", safe, string(scope), time.Now().Format("20060102-150405"), format))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		m.status = "export: write: " + err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("exported %s (%d bytes) → %s", scope, len(data), path)
	return m, nil
}

func expandHomePath(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

func (m model) dispatchTools(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.status = "usage: :tools upgrade [kind] | :tools relocate <dir>"
		return m, nil
	}
	switch args[0] {
	case "upgrade", "install":
		if m.readOnly {
			m.status = "read-only: :tools " + args[0] + " blocked"
			return m, nil
		}
		kinds := tools.KnownKinds()
		if len(args) >= 2 {
			kinds = []tools.Kind{tools.Kind(args[1])}
		}
		cmds := make([]tea.Cmd, 0, len(kinds))
		for _, k := range kinds {
			cmds = append(cmds, installCmd(m.cfg, k))
		}
		verb := "upgrading"
		if args[0] == "install" {
			verb = "installing"
		}
		m.status = fmt.Sprintf("%s %d tool(s)…", verb, len(kinds))
		return m, tea.Sequence(cmds...)

	case "relocate":
		if len(args) < 2 {
			m.status = "usage: :tools relocate <dir>"
			return m, nil
		}
		if err := tools.Relocate(m.cfg, args[1]); err != nil {
			m.status = "relocate failed: " + err.Error()
			return m, nil
		}
		m.status = "relocated to " + m.cfg.Tools.InstallDir
		return m, nil

	case "dir":
		dir := m.cfg.Tools.InstallDir
		if dir == "" {
			dir = "(unset — auto-picked on first install)"
		}
		m.status = "install dir: " + dir
		return m, nil

	default:
		m.status = "usage: :tools install|upgrade [kind] | :tools relocate <dir> | :tools dir"
		return m, nil
	}
}

// installCmdStream spawns the install in a goroutine that feeds Progress/Done/
// Error msgs to a channel. Returns the channel (so the model can re-subscribe
// after Progress msgs) and the initial wait cmd.
func installCmdStream(cfg *config.Config, kind tools.Kind) (chan tea.Msg, tea.Cmd) {
	ch := make(chan tea.Msg, 16)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		progress := func(done, total int64) {
			if total <= 0 {
				return
			}
			p := int(done * 100 / total)
			select {
			case ch <- views.InstallProgressMsg{Kind: kind, Percent: p}:
			default:
			}
		}
		path, err := tools.InstallWithProgress(ctx, kind, cfg, progress)
		if err != nil {
			ch <- views.InstallErrorMsg{Kind: kind, Err: err}
		} else {
			ch <- views.InstallDoneMsg{Kind: kind, Path: path}
		}
		close(ch)
	}()
	return ch, waitOn(ch)
}

func waitOn(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-ch
		if !ok {
			return nil
		}
		return m
	}
}

// installCmd kept for callers (e.g. :tools upgrade) that don't care about
// progress; emits a single Done/Error.
func installCmd(cfg *config.Config, kind tools.Kind) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		path, err := tools.Install(ctx, kind, cfg)
		if err != nil {
			return views.InstallErrorMsg{Kind: kind, Err: err}
		}
		return views.InstallDoneMsg{Kind: kind, Path: path}
	}
}
