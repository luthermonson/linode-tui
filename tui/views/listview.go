package views

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/luthermonson/linode-tui/audit"
	"github.com/luthermonson/linode-tui/config"
	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tools"
	"github.com/luthermonson/linode-tui/tui/theme"
)

type Lister[T any] func(ctx context.Context, c *linode.Client) ([]T, error)
type Rower[T any] func(T) table.Row
type Matcher[T any] func(T, string) bool
type OnEnter[T any] func(T, Deps) tea.Cmd

// Column priorities. Narrow panes drop the lowest-priority column first;
// PriPinned is never dropped, so a view always keeps its identity columns
// (ID + LABEL/NAME/DOMAIN) even on a phone-sized terminal.
const (
	PriPinned = 100 // never dropped
	PriHigh   = 70  // status, account — the second thing you look at
	PriMed    = 50  // region, type, engine…
	PriLow    = 30  // supporting detail
	PriLowest = 10  // tags, messages, long free text — first to go
)

// markerColWidth is the gutter prepended when IDFn is set: 3 cells so "★✓"
// fits with a trailing pad.
const markerColWidth = 3

// colPad is what the bubbles table spends per column *on top of* Column.Width.
// table.DefaultStyles gives Header and Cell a Padding(0, 1) — one cell either
// side — so a row costs sum(widths) + 2*len(cols). Forgetting this is how the
// old fixed layout overshot the pane and wrapped.
const colPad = 2

// Col is a table column plus the metadata resize() needs to fit it into a
// pane. It is field-compatible with table.Column (Title/Width) so existing
// column literals keep working; Width is the *preferred* width, MinWidth the
// floor when space is tight.
type Col struct {
	Title string
	Width int
	// MinWidth is the narrowest useful rendering. Zero derives one from the
	// header text. Clamped to Width.
	MinWidth int
	// Priority decides drop order — lower goes first. Zero infers from Title.
	Priority int
	// Flex marks a column that soaks up leftover width once every surviving
	// column has reached its preferred size.
	Flex bool
}

func (c Col) min() int {
	m := c.MinWidth
	if m <= 0 {
		m = lipgloss.Width(c.Title)
		if m < 3 {
			m = 3
		}
	}
	if m > c.Width {
		m = c.Width
	}
	if m < 1 {
		m = 1
	}
	return m
}

func (c Col) priority() int {
	if c.Priority > 0 {
		return c.Priority
	}
	return inferPriority(c.Title)
}

// inferPriority is the safety net for columns that didn't get an explicit
// Priority — it keeps a view usable even if someone adds a column and forgets
// the metadata.
func inferPriority(title string) int {
	switch strings.ToUpper(strings.TrimSpace(title)) {
	case "ID", "LABEL", "NAME", "DOMAIN":
		return PriPinned
	case "STATUS", "ACCOUNT", "KIND":
		return PriHigh
	case "REGION", "TYPE", "ENGINE":
		return PriMed
	case "TAGS", "MESSAGE", "DESCRIPTION", "REV NOTE", "SOA EMAIL", "HOSTNAME":
		return PriLowest
	}
	return PriLow
}

type listOpts[T any] struct {
	Deps    Deps
	Title   string
	Columns []Col
	Lister  Lister[T]
	Rower   Rower[T]
	Matcher Matcher[T]
	OnEnter OnEnter[T]
	Actions []Action[T]
	// KeyHandlers map a hot key to a handler that returns a tea.Cmd. Takes
	// precedence over Actions for the same key. Used for actions that need
	// custom forms (no simple confirm modal) — e.g. edit/resize/rebuild.
	KeyHandlers map[string]func(T, Deps) tea.Cmd
	// KeyHelp gives short descriptions for the keys above so the `?` overlay
	// can describe per-view bindings. Same keys as KeyHandlers; entries
	// without a description are silently skipped from help.
	KeyHelp map[string]string
	// IDFn enables multi-select. Returns a stable key per item. When set,
	// space toggles selection on the current row and D bulk-deletes selected
	// rows by calling the "d" Action.
	IDFn func(T) string
	// EditCmd, when set, powers `e` in the JSON detail viewport for an item.
	// Typically returns a tea.Cmd that emits ConfigureLinodeMsg or similar.
	EditCmd func(T, Deps) tea.Cmd
	// BookmarkKind, when set, enables * bookmark toggling. Values are
	// persisted to Cfg.Bookmarks[BookmarkKind] keyed by IDFn.
	BookmarkKind string
	// Refresh, when > 0, overrides cfg.Refresh for this view. Useful for
	// fast-moving data (events) or slow-moving data (account, billing).
	Refresh time.Duration
	// TagsFn, when set, powers the `#tag` filter prefix. Returns the tags
	// for a given row; filter with leading `#` matches only tag substrings.
	TagsFn func(T) []string
	// FieldFn, when set, powers `key:value` filter clauses. Multiple clauses
	// in the filter are AND'd. Unmapped keys fall back to the Matcher.
	FieldFn map[string]func(T) string
	// Sort, when set, orders items before the bookmark-first pass. Use
	// negative/zero/positive return like sort.Slice's less.
	Sort func(a, b T) int
}

type listLoadedMsg[T any] struct {
	id uint64
	// gen is the tick generation the fetch was issued under. Only a load from
	// the current generation is allowed to schedule the next tick — see
	// listView.tickGen.
	gen   uint64
	items []T
	err   error
}

type listTickMsg struct{ id, gen uint64 }

var listSeq atomic.Uint64

type listView[T any] struct {
	id   uint64
	opts listOpts[T]

	table       table.Model
	items       []T
	filterInput textinput.Model
	filtering   bool
	selected    map[string]bool
	bookmarks   map[string]bool
	drifts      map[string]bool

	// tickGen guards against tick-chain multiplication. Every refresh trigger
	// that isn't the tick itself (manual `r`, ActionDoneMsg, drill-in return)
	// bumps it, which retires the outstanding tick and the outstanding fetch:
	// stale ticks are dropped on arrival and stale loads don't schedule a
	// successor. Exactly one chain stays live no matter how many refreshes or
	// broadcast action results land.
	tickGen uint64

	// colIdx indexes opts.Columns for the columns currently on screen (the
	// marker gutter isn't in there); colWidths is the width assigned to each.
	// hiddenCols counts what didn't fit.
	colIdx     []int
	colWidths  []int
	hiddenCols int

	loading bool
	err     error
	// notice is a transient one-line hint shown in the status bar — used to
	// tell the user a key isn't supported here instead of swallowing it.
	// Cleared on the next keypress.
	notice string
	w, h   int
	stamp  time.Time
}

var (
	keyFilter    = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	keyEscape    = key.NewBinding(key.WithKeys("esc"))
	keyEnter     = key.NewBinding(key.WithKeys("enter"))
	keyRefresh   = key.NewBinding(key.WithKeys("ctrl+r", "r"), key.WithHelp("r", "refresh"))
	keyDetail    = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "view details"))
	keyToggleSel = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle select"))
	keyBulkDel   = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete selected"))
	keyBookmark  = key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "toggle bookmark"))
)

func newListView[T any](o listOpts[T]) *listView[T] {
	t := table.New(
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(tableStyles(o.Deps.Theme))

	fi := textinput.New()
	fi.Prompt = "/"
	fi.CharLimit = 60

	bookmarks := map[string]bool{}
	if o.BookmarkKind != "" && o.Deps.Cfg != nil {
		for _, id := range o.Deps.Cfg.ActiveBookmarks()[o.BookmarkKind] {
			bookmarks[id] = true
		}
	}

	m := &listView[T]{
		id:          listSeq.Add(1),
		opts:        o,
		table:       t,
		filterInput: fi,
		selected:    map[string]bool{},
		bookmarks:   bookmarks,
		drifts:      map[string]bool{},
		loading:     true,
	}
	// Width is unknown until the first WindowSizeMsg; lay out at preferred
	// widths so the table is well-formed from frame zero.
	m.layout()
	return m
}

func (m *listView[T]) Title() string { return m.opts.Title }

func (m *listView[T]) Filtering() bool {
	return m.filtering || m.filterInput.Value() != ""
}

// SelectedID exposes the IDFn output of the currently highlighted row, or "".
func (m *listView[T]) SelectedID() string {
	if m.opts.IDFn == nil {
		return ""
	}
	it, ok := m.SelectedItem()
	if !ok {
		return ""
	}
	return m.opts.IDFn(it)
}

func (m *listView[T]) Counts() (total, bookmarked, visible int) {
	total = len(m.items)
	bookmarked = len(m.bookmarks)
	visible = len(m.visibleItems())
	return
}

func (m *listView[T]) ExportJSON(scope ExportScope) ([]byte, error) {
	items, err := m.exportItems(scope)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(items, "", "  ")
}

func (m *listView[T]) ExportCSV(scope ExportScope) (string, error) {
	items, err := m.exportItems(scope)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	header := make([]string, 0, len(m.opts.Columns))
	for _, c := range m.opts.Columns {
		header = append(header, c.Title)
	}
	if err := w.Write(header); err != nil {
		return "", err
	}
	for _, it := range items {
		if err := w.Write(m.opts.Rower(it)); err != nil {
			return "", err
		}
	}
	w.Flush()
	return b.String(), w.Error()
}

func (m *listView[T]) exportItems(scope ExportScope) ([]T, error) {
	switch scope {
	case "", ExportVisible:
		return m.visibleItems(), nil
	case ExportSelected:
		if m.opts.IDFn == nil {
			return nil, fmt.Errorf("selected scope: view doesn't support selection")
		}
		return m.selectedItemsList(), nil
	case ExportBookmarked:
		if m.opts.IDFn == nil {
			return nil, fmt.Errorf("bookmarked scope: view doesn't support bookmarks")
		}
		out := make([]T, 0, len(m.bookmarks))
		for _, it := range m.items {
			if m.bookmarks[m.opts.IDFn(it)] {
				out = append(out, it)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown export scope %q", scope)
	}
}

func (m *listView[T]) Help() []HelpEntry {
	out := []HelpEntry{
		{Key: "/", Desc: "filter"},
		{Key: "enter", Desc: "drill in / accept filter"},
		{Key: "r / ctrl+r", Desc: "refresh now"},
		{Key: "y", Desc: "view detail (json)"},
		{Key: "esc", Desc: "clear filter / go back"},
	}
	if m.opts.IDFn != nil {
		out = append(out,
			HelpEntry{Key: "space", Desc: "toggle row selection"},
			HelpEntry{Key: "D", Desc: "delete all selected (confirm)"},
		)
		if m.opts.BookmarkKind != "" {
			out = append(out, HelpEntry{Key: "*", Desc: "toggle bookmark (★ persists)"})
		}
	}
	if m.opts.TagsFn != nil {
		out = append(out, HelpEntry{Key: "#tag", Desc: "filter clause: rows whose tags contain tag"})
	}
	if len(m.opts.FieldFn) > 0 {
		keys := make([]string, 0, len(m.opts.FieldFn))
		for k := range m.opts.FieldFn {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, HelpEntry{
			Key:  "key:val",
			Desc: "filter clause; keys: " + strings.Join(keys, ", "),
		})
	}
	for _, a := range m.opts.Actions {
		out = append(out, HelpEntry{Key: a.Key, Desc: a.Label})
	}
	keys := make([]string, 0, len(m.opts.KeyHandlers))
	for k := range m.opts.KeyHandlers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		desc := m.opts.KeyHelp[k]
		if desc == "" {
			desc = "configure (form)"
		}
		out = append(out, HelpEntry{Key: k, Desc: desc})
	}
	return out
}

// Init kicks off the first load only. The poll chain is started by the load
// that comes back — scheduling a tick here as well would leave two chains
// running from the very first frame.
func (m *listView[T]) Init() tea.Cmd { return m.fetch() }

func (m *listView[T]) fetch() tea.Cmd {
	id, gen := m.id, m.tickGen
	if m.opts.Deps.Linode == nil {
		return func() tea.Msg {
			return listLoadedMsg[T]{id: id, gen: gen, err: fmt.Errorf("no linode client configured")}
		}
	}
	client := m.opts.Deps.Linode
	lister := m.opts.Lister
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := lister(ctx, client)
		return listLoadedMsg[T]{id: id, gen: gen, items: items, err: err}
	}
}

// refetch retires the current tick chain and any fetch still in flight, then
// issues a fresh load. The load's response restarts the chain — so a manual
// `r`, a drill-in return, or an ActionDoneMsg broadcast to every pane each
// leave exactly one chain behind instead of adding one.
func (m *listView[T]) refetch() tea.Cmd {
	m.tickGen++
	return m.fetch()
}

func (m *listView[T]) tick() tea.Cmd {
	d := refreshInterval(m.opts.Deps, m.opts.Deps.CtxString("view_name"), m.opts.Refresh)
	id, gen := m.id, m.tickGen
	return tea.Tick(d, func(time.Time) tea.Msg { return listTickMsg{id: id, gen: gen} })
}

func (m *listView[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resize()
		return m, nil

	case listLoadedMsg[T]:
		if msg.id != m.id {
			return m, nil
		}
		m.loading = false
		m.err = msg.err
		var focusCmd tea.Cmd
		if msg.err == nil {
			m.items = msg.items
			m.stamp = time.Now()
			// Prune selections that no longer exist (e.g. after a bulk delete).
			if m.opts.IDFn != nil && len(m.selected) > 0 {
				present := make(map[string]bool, len(m.items))
				for _, it := range m.items {
					present[m.opts.IDFn(it)] = true
				}
				for id := range m.selected {
					if !present[id] {
						delete(m.selected, id)
					}
				}
			}
			m.computeDrifts()
			m.applyFilter()
			focusCmd = m.consumeFocusID()
		}
		if msg.gen != m.tickGen {
			// A newer refresh already took over the chain; this load's data is
			// still good but it must not schedule a second tick.
			return m, focusCmd
		}
		if focusCmd != nil {
			return m, tea.Batch(m.tick(), focusCmd)
		}
		return m, m.tick()

	case listTickMsg:
		if msg.id != m.id || msg.gen != m.tickGen {
			return m, nil
		}
		return m, m.fetch()

	case ErrorMsg:
		m.err = msg.Err
		return m, nil

	case DrillInMsg:
		return m, m.drillIn(msg)

	case drillinDoneMsg:
		if msg.id != m.id {
			return m, nil
		}
		if msg.cleanup != nil {
			msg.cleanup()
		}
		m.loading = true
		return m, m.refetch()

	case tools.ExitMsg:
		// Surface drilled-in tool errors so a failed lish/k9s launch isn't
		// silent — without this the alt-screen flickers and the user has
		// no clue what happened.
		if msg.Err != nil {
			m.err = fmt.Errorf("%s: %w", msg.Kind, msg.Err)
		}
		return m, nil

	case ActionDoneMsg:
		// Broadcast to every pane, so this must not add a chain per pane.
		m.loading = true
		return m, m.refetch()

	case ActionErrorMsg:
		m.err = msg.Err
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
		// Any keypress dismisses the previous transient hint.
		m.notice = ""
		switch {
		case key.Matches(msg, keyFilter):
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink
		case key.Matches(msg, keyRefresh):
			m.loading = true
			return m, m.refetch()
		case key.Matches(msg, keyDetail):
			it, ok := m.SelectedItem()
			if !ok {
				return m, nil
			}
			body, err := json.MarshalIndent(it, "", "  ")
			if err != nil {
				m.err = err
				return m, nil
			}
			var onEdit tea.Cmd
			if m.opts.EditCmd != nil {
				onEdit = m.opts.EditCmd(it, m.opts.Deps)
			}
			return m, func() tea.Msg {
				return OpenDetailMsg{Title: m.opts.Title + " detail", Body: string(body), OnEdit: onEdit}
			}
		case key.Matches(msg, keyEnter):
			if m.opts.OnEnter == nil {
				m.notice = "this view doesn't support drill-in (enter)"
				return m, nil
			}
			it, ok := m.SelectedItem()
			if !ok {
				return m, nil
			}
			return m, m.opts.OnEnter(it, m.opts.Deps)
		case key.Matches(msg, keyToggleSel):
			if m.opts.IDFn == nil {
				m.notice = "this view doesn't support row selection"
				return m, nil
			}
			it, ok := m.SelectedItem()
			if !ok {
				return m, nil
			}
			id := m.opts.IDFn(it)
			if m.selected[id] {
				delete(m.selected, id)
			} else {
				m.selected[id] = true
			}
			m.applyFilter()
			return m, nil
		case key.Matches(msg, keyBulkDel):
			return m, m.bulkDelete()
		case key.Matches(msg, keyBookmark):
			// A view can define its own `*` semantics — the watchlist does,
			// because its rows are cross-kind and unstarring has to reach into
			// whichever kind the row came from.
			if handler, ok := m.opts.KeyHandlers["*"]; ok {
				if it, ok := m.SelectedItem(); ok {
					return m, handler(it, m.opts.Deps)
				}
				return m, nil
			}
			if m.opts.IDFn == nil || m.opts.BookmarkKind == "" {
				m.notice = "this view doesn't support bookmarks"
				return m, nil
			}
			it, ok := m.SelectedItem()
			if !ok {
				return m, nil
			}
			id := m.opts.IDFn(it)
			if m.bookmarks[id] {
				delete(m.bookmarks, id)
				removeSnapshot(m.opts.BookmarkKind, id)
			} else {
				m.bookmarks[id] = true
				if body, err := json.MarshalIndent(it, "", "  "); err == nil {
					saveSnapshot(m.opts.BookmarkKind, id, body)
				}
			}
			m.persistBookmarks()
			m.applyFilter()
			return m, nil
		}
		if handler, ok := m.opts.KeyHandlers[msg.String()]; ok {
			if item, ok := m.SelectedItem(); ok {
				if cmd := handler(item, m.opts.Deps); cmd != nil {
					return m, cmd
				}
			}
			return m, nil
		}
		if cmd := m.tryAction(msg.String()); cmd != nil {
			return m, cmd
		}
	}

	if mm, ok := msg.(tea.MouseMsg); ok {
		switch mm.Button {
		case tea.MouseButtonWheelUp:
			m.table.MoveUp(3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.table.MoveDown(3)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *listView[T]) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keyEnter), key.Matches(msg, keyEscape):
		m.filtering = false
		m.filterInput.Blur()
		if key.Matches(msg, keyEscape) {
			m.filterInput.SetValue("")
		}
		m.applyFilter()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.applyFilter()
	return m, cmd
}

func (m *listView[T]) applyFilter() {
	needle := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	rows := make([]table.Row, 0, len(m.items))
	hlStyle := lipgloss.NewStyle().
		Foreground(m.opts.Deps.Theme.Warn).
		Bold(true)
	for _, it := range m.orderedItems() {
		if needle != "" && !m.matches(it, needle) {
			continue
		}
		full := m.opts.Rower(it)
		row := make(table.Row, 0, len(m.colIdx)+1)
		if m.opts.IDFn != nil {
			id := m.opts.IDFn(it)
			row = append(row, makeRowMarker(m.bookmarks[id], m.selected[id], m.drifts[id]))
		}
		// Emit exactly one cell per *surviving* column, in screen order. A row
		// longer than the column set makes bubbles' renderRow index past the
		// end of its column slice.
		for i, ci := range m.colIdx {
			cell := ""
			if ci < len(full) {
				cell = full[ci]
			}
			if needle != "" {
				cell = highlightMatch(cell, needle, hlStyle, m.colWidths[i])
			}
			row = append(row, cell)
		}
		rows = append(rows, row)
	}
	m.table.SetRows(rows)
	// bubbles' SetRows only clamps the cursor downward, so after an empty
	// intermediate state (a column relayout clears the rows) it can be stuck
	// at -1 and SelectedRow would return nil forever.
	if len(rows) > 0 && m.table.Cursor() < 0 {
		m.table.SetCursor(0)
	}
}

// highlightMatch wraps every case-insensitive needle hit in s with hlStyle.
//
// Order matters: the cell is truncated to the column width *first*, while it's
// still plain text, and styled after. The other way round — style, then let
// bubbles/table run runewidth.Truncate over the result — cuts through an ANSI
// escape whenever a match sits near the column edge, and the colour bleeds
// across the rest of the row. maxWidth=0 means "don't truncate".
func highlightMatch(s, needle string, hlStyle lipgloss.Style, maxWidth int) string {
	if needle == "" || s == "" {
		return s
	}
	if maxWidth > 0 {
		s = truncateCells(s, maxWidth)
	}
	lower := strings.ToLower(s)
	n := strings.ToLower(needle)
	var b strings.Builder
	// Byte indices throughout: strings.Index returns them and lower/s share a
	// layout for the ASCII-folding done by ToLower, so multi-byte labels are
	// matched and sliced correctly rather than skipped.
	for i := 0; i < len(lower); {
		idx := strings.Index(lower[i:], n)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+idx])
		end := i + idx + len(n)
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(hlStyle.Render(s[i+idx : end]))
		i = end
	}
	return b.String()
}

// truncateCells cuts s to at most max terminal cells, appending "…" when it
// had to cut. Rune- and width-aware (CJK/emoji count as two cells), which
// len(s) is not.
func truncateCells(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	limit := max - 1 // room for the ellipsis
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > limit {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

func (m *listView[T]) resize() {
	// A relayout round-trips the rows through empty, which drops the cursor.
	cursor := m.table.Cursor()
	m.layout()
	if m.w > 0 {
		// Without a width the table's viewport never clips and long rows wrap,
		// scrambling every line below them.
		m.table.SetWidth(m.w)
	}
	h := m.h - 4
	if h < 5 {
		h = 5
	}
	m.table.SetHeight(h)
	w := m.w - 4
	if w < 10 {
		w = 10
	}
	m.filterInput.Width = w
	m.applyFilter()
	if cursor > 0 {
		m.table.SetCursor(cursor)
	}
}

// layout picks which columns fit in the current pane width and how wide each
// one gets. Columns are dropped lowest-priority-first (rightmost breaks ties)
// until the minimum widths fit; the leftover is spent first bringing survivors
// up to their preferred width, then padding out the Flex columns.
func (m *listView[T]) layout() {
	all := m.opts.Columns
	keep := make([]int, 0, len(all))
	widths := make([]int, 0, len(all))

	avail := m.w
	if m.opts.IDFn != nil {
		avail -= markerColWidth + colPad
	}
	if m.w <= 0 || avail <= 0 {
		// No size yet (first frame, tests): everything at preferred width.
		for i, c := range all {
			keep = append(keep, i)
			widths = append(widths, c.Width)
		}
		m.setCols(keep, widths)
		return
	}

	total := 0
	for i, c := range all {
		keep = append(keep, i)
		widths = append(widths, c.min())
		total += c.min() + colPad
	}

	for total > avail {
		victim := -1
		for pos, ci := range keep {
			if all[ci].priority() >= PriPinned {
				continue
			}
			// <= so ties resolve to the rightmost column, which is where the
			// least interesting data tends to live.
			if victim < 0 || all[ci].priority() <= all[keep[victim]].priority() {
				victim = pos
			}
		}
		if victim < 0 {
			// Only pinned columns left and they still don't fit: keep them at
			// their minimum and let SetWidth clip. This is the phone-sized
			// terminal case — ID + LABEL, nothing else.
			break
		}
		total -= widths[victim] + colPad
		keep = append(keep[:victim], keep[victim+1:]...)
		widths = append(widths[:victim], widths[victim+1:]...)
	}

	slack := avail - total
	if slack > 0 {
		// Grow toward preferred widths, proportional to how short each is.
		deficit := 0
		for i, ci := range keep {
			if d := all[ci].Width - widths[i]; d > 0 {
				deficit += d
			}
		}
		if deficit > 0 {
			give := min(slack, deficit)
			handed := 0
			for i, ci := range keep {
				d := all[ci].Width - widths[i]
				if d <= 0 {
					continue
				}
				share := give * d / deficit
				widths[i] += share
				handed += share
			}
			for i, ci := range keep {
				for handed < give && widths[i] < all[ci].Width {
					widths[i]++
					handed++
				}
			}
			slack -= handed
		}
	}
	if slack > 0 {
		// Whatever is still free goes to the Flex columns, proportionally.
		flexTotal := 0
		for _, ci := range keep {
			if all[ci].Flex {
				flexTotal += max(all[ci].Width, 1)
			}
		}
		if flexTotal > 0 {
			handed := 0
			for i, ci := range keep {
				if !all[ci].Flex {
					continue
				}
				share := slack * max(all[ci].Width, 1) / flexTotal
				widths[i] += share
				handed += share
			}
			for i, ci := range keep {
				if handed >= slack {
					break
				}
				if !all[ci].Flex {
					continue
				}
				widths[i] += slack - handed
				handed = slack
			}
		}
	}
	m.setCols(keep, widths)
}

// setCols installs a column layout on the table. Rows are cleared first:
// SetColumns re-renders immediately, and the rows still in the model carry a
// cell per *old* column, which would index past the new (shorter) column
// slice. applyFilter repopulates them.
func (m *listView[T]) setCols(idx, widths []int) {
	m.colIdx, m.colWidths = idx, widths
	m.hiddenCols = len(m.opts.Columns) - len(idx)
	cols := make([]table.Column, 0, len(idx)+1)
	if m.opts.IDFn != nil {
		// 3-wide so we can show "★", "✓", or "★✓" right-padded.
		cols = append(cols, table.Column{Title: " ", Width: markerColWidth})
	}
	for i, ci := range idx {
		cols = append(cols, table.Column{Title: m.opts.Columns[ci].Title, Width: widths[i]})
	}
	m.table.SetRows(nil)
	m.table.SetColumns(cols)
}

// consumeFocusID looks for a "focus_id" hint in Deps.Context. If present and
// the corresponding row exists in the loaded items, cursor jumps to it and a
// one-shot OpenDetailMsg is emitted. The context entry is cleared so subsequent
// fetches don't keep re-focusing.
func (m *listView[T]) consumeFocusID() tea.Cmd {
	if m.opts.IDFn == nil || m.opts.Deps.Context == nil {
		return nil
	}
	v, ok := m.opts.Deps.Context["focus_id"]
	if !ok {
		return nil
	}
	target, _ := v.(string)
	if target == "" {
		delete(m.opts.Deps.Context, "focus_id")
		return nil
	}
	visible := m.visibleItems()
	for i, it := range visible {
		if m.opts.IDFn(it) == target {
			m.table.SetCursor(i)
			delete(m.opts.Deps.Context, "focus_id")
			body, err := json.MarshalIndent(it, "", "  ")
			if err != nil {
				return nil
			}
			var onEdit tea.Cmd
			if m.opts.EditCmd != nil {
				onEdit = m.opts.EditCmd(it, m.opts.Deps)
			}
			return func() tea.Msg {
				return OpenDetailMsg{Title: m.opts.Title + " detail", Body: string(body), OnEdit: onEdit}
			}
		}
	}
	// Item not present yet — leave focus_id so a later refresh can pick it up
	// (e.g. paginated accounts with very many rows).
	return nil
}

// makeRowMarker returns a 3-wide glyph string for the bookmark/selection
// combination. Δ replaces ★ when the bookmarked row has drifted from its
// last-captured snapshot.
func makeRowMarker(bookmarked, selected, drifted bool) string {
	star := ""
	switch {
	case bookmarked && drifted:
		star = "Δ"
	case bookmarked:
		star = "★"
	}
	switch {
	case star != "" && selected:
		return star + "✓"
	case star != "":
		return star
	case selected:
		return "✓"
	default:
		return " "
	}
}

// computeDrifts refreshes the drift cache after a successful fetch. Only
// considers bookmarked rows; non-bookmarked never have a snapshot to compare
// against.
func (m *listView[T]) computeDrifts() {
	m.drifts = map[string]bool{}
	if m.opts.IDFn == nil || m.opts.BookmarkKind == "" || len(m.bookmarks) == 0 {
		return
	}
	for _, it := range m.items {
		id := m.opts.IDFn(it)
		if !m.bookmarks[id] {
			continue
		}
		if hasDrift(m.opts.BookmarkKind, id, it) {
			m.drifts[id] = true
		}
	}
}

func (m *listView[T]) persistBookmarks() {
	if m.opts.BookmarkKind == "" || m.opts.Deps.Cfg == nil {
		return
	}
	ids := make([]string, 0, len(m.bookmarks))
	for id := range m.bookmarks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		m.opts.Deps.Cfg.SetBookmarks(m.opts.BookmarkKind, nil)
	} else {
		m.opts.Deps.Cfg.SetBookmarks(m.opts.BookmarkKind, ids)
	}
	_ = m.opts.Deps.Cfg.Save()
}

func (m *listView[T]) bulkDelete() tea.Cmd {
	var action *Action[T]
	for i := range m.opts.Actions {
		if m.opts.Actions[i].Key == "d" {
			action = &m.opts.Actions[i]
			break
		}
	}
	if action == nil {
		m.notice = "this view doesn't support delete"
		return nil
	}
	if m.opts.IDFn == nil {
		m.notice = "this view doesn't support row selection"
		return nil
	}
	if len(m.selected) == 0 {
		m.notice = "nothing selected — space toggles the highlighted row"
		return nil
	}
	items := m.selectedItemsList()
	if len(items) == 0 {
		return nil
	}
	client := m.opts.Deps.Linode
	run := action.Run
	cfg := m.opts.Deps.Cfg
	kind := m.opts.BookmarkKind
	idFn := m.opts.IDFn
	onYes := func() tea.Msg {
		var firstErr error
		var done int
		for _, it := range items {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := run(ctx, client, it)
			id := ""
			if idFn != nil {
				id = idFn(it)
			}
			audit.Append(audit.Entry{
				Account: accountName(cfg),
				Action:  "bulk-delete",
				Kind:    kind,
				ID:      id,
				Err:     errString(err),
			})
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				done++
			}
			cancel()
		}
		if firstErr != nil {
			return ActionErrorMsg{Label: "bulk delete", Err: fmt.Errorf("%d/%d failed: %w", len(items)-done, len(items), firstErr)}
		}
		return ActionDoneMsg{Label: fmt.Sprintf("deleted %d", done)}
	}
	if len(items) > 10 {
		return func() tea.Msg {
			return TypedConfirmMsg{
				Prompt: fmt.Sprintf("DELETE %d items? Type the count to confirm:", len(items)),
				Match:  strconv.Itoa(len(items)),
				OnYes:  func() tea.Msg { return onYes() },
			}
		}
	}
	return func() tea.Msg {
		return ConfirmMsg{
			Prompt: fmt.Sprintf("DELETE %d selected items? This cannot be undone.", len(items)),
			OnYes:  func() tea.Msg { return onYes() },
		}
	}
}

func (m *listView[T]) selectedItemsList() []T {
	out := make([]T, 0, len(m.selected))
	for _, it := range m.items {
		if m.opts.IDFn == nil {
			continue
		}
		if m.selected[m.opts.IDFn(it)] {
			out = append(out, it)
		}
	}
	return out
}

func (m *listView[T]) tryAction(pressed string) tea.Cmd {
	for _, a := range m.opts.Actions {
		if a.Key != pressed {
			continue
		}
		item, ok := m.SelectedItem()
		if !ok {
			return nil
		}
		action := a
		client := m.opts.Deps.Linode
		cfg := m.opts.Deps.Cfg
		kind := m.opts.BookmarkKind
		var id string
		if m.opts.IDFn != nil {
			id = m.opts.IDFn(item)
		}
		return func() tea.Msg {
			return ConfirmMsg{
				Prompt: action.Prompt(item),
				OnYes: func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					err := action.Run(ctx, client, item)
					audit.Append(audit.Entry{
						Account: accountName(cfg),
						Action:  action.Label,
						Kind:    kind,
						ID:      id,
						Err:     errString(err),
					})
					if err != nil {
						return ActionErrorMsg{Label: action.Label, Err: err}
					}
					return ActionDoneMsg{Label: action.Label}
				},
			}
		}
	}
	return nil
}

func accountName(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.DefaultAccount
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *listView[T]) drillIn(msg DrillInMsg) tea.Cmd {
	id := m.id
	cmd, err := RunDrillIn(m.opts.Deps, msg, func(cleanup func()) tea.Msg {
		return drillinDoneMsg{id: id, cleanup: cleanup}
	})
	if err != nil {
		m.err = err
		return nil
	}
	return cmd
}

func (m *listView[T]) View() string {
	th := m.opts.Deps.Theme
	mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
	errorStyle := lipgloss.NewStyle().Foreground(th.Error)
	warnStyle := lipgloss.NewStyle().Foreground(th.Warn)

	title := strings.ToLower(m.opts.Title)
	filtered := m.Filtering() && strings.TrimSpace(m.filterInput.Value()) != ""
	visible := len(m.visibleItems())

	var status string
	switch {
	case m.err != nil:
		status = errorStyle.Render(fmt.Sprintf("error: %v", m.err))
	case m.loading && len(m.items) == 0:
		status = mutedStyle.Render("loading…")
	default:
		count := fmt.Sprintf("%d %s", len(m.items), title)
		if filtered {
			count = fmt.Sprintf("%d/%d %s", visible, len(m.items), title)
		}
		s := fmt.Sprintf("%s · refreshed %s ago", count, time.Since(m.stamp).Truncate(time.Second))
		if n := len(m.selected); n > 0 {
			s = fmt.Sprintf("%d selected · ", n) + s
		}
		status = mutedStyle.Render(s)
	}
	if m.hiddenCols > 0 {
		status += warnStyle.Render(fmt.Sprintf("  »%d cols hidden — widen terminal", m.hiddenCols))
	}
	if m.notice != "" {
		status += warnStyle.Render("  " + m.notice)
	}

	parts := []string{m.table.View()}
	// An empty table is just a header row and dead space — say why.
	switch {
	case m.err == nil && !m.loading && len(m.items) == 0:
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("no %s found — press r to refresh", title)))
	case m.err == nil && filtered && visible == 0:
		parts = append(parts, mutedStyle.Render(
			fmt.Sprintf("no %s match %q — esc clears the filter", title, strings.TrimSpace(m.filterInput.Value()))))
	}
	// The filter input belongs directly under the table it filters; below the
	// status line it lands on the last row of the pane and gets clipped.
	if m.filtering || m.filterInput.Value() != "" {
		parts = append(parts, m.filterInput.View())
	}
	parts = append(parts, status)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// SelectedItem returns the currently highlighted item or false if the table is
// empty / out of bounds. Used by drill-in actions (e.g. LKE → k9s).
func (m *listView[T]) SelectedItem() (T, bool) {
	var zero T
	row := m.table.SelectedRow()
	if row == nil {
		return zero, false
	}
	idx := m.table.Cursor()
	// filter shifts indices; resolve via the rendered row's first cell (ID).
	if idx < 0 || idx >= len(m.items) {
		return zero, false
	}
	// Build a parallel slice of visible items in row order to look up by index.
	visible := m.visibleItems()
	if idx >= len(visible) {
		return zero, false
	}
	return visible[idx], true
}

func (m *listView[T]) visibleItems() []T {
	needle := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	source := m.orderedItems()
	if needle == "" {
		return source
	}
	out := make([]T, 0, len(source))
	for _, it := range source {
		if m.matches(it, needle) {
			out = append(out, it)
		}
	}
	return out
}

// matches splits the filter into whitespace-separated tokens and requires all
// of them to pass (AND). Tokens with a "#" prefix route to TagsFn; tokens of
// the form "key:value" route to FieldFn[key]; everything else falls back to
// the view's Matcher.
func (m *listView[T]) matches(it T, needle string) bool {
	tokens := strings.Fields(needle)
	if len(tokens) == 0 {
		return true
	}
	for _, tok := range tokens {
		if !m.matchToken(it, tok) {
			return false
		}
	}
	return true
}

func (m *listView[T]) matchToken(it T, tok string) bool {
	if strings.HasPrefix(tok, "#") {
		if m.opts.TagsFn == nil {
			return false
		}
		tagNeedle := strings.TrimPrefix(tok, "#")
		if tagNeedle == "" {
			return len(m.opts.TagsFn(it)) > 0
		}
		return tagMatch(m.opts.TagsFn(it), tagNeedle)
	}
	if idx := strings.IndexByte(tok, ':'); idx > 0 && m.opts.FieldFn != nil {
		key, val := tok[:idx], tok[idx+1:]
		if fn, ok := m.opts.FieldFn[key]; ok {
			return strings.Contains(strings.ToLower(fn(it)), val)
		}
	}
	if m.opts.Matcher == nil {
		return true
	}
	return m.opts.Matcher(it, tok)
}

// orderedItems returns m.items sorted by Sort (if set), then with bookmarked
// rows pulled to the front. Always returns a fresh slice when sorted to avoid
// mutating m.items.
func (m *listView[T]) orderedItems() []T {
	src := m.items
	if m.opts.Sort != nil {
		src = append([]T(nil), m.items...)
		slices.SortStableFunc(src, m.opts.Sort)
	}
	if m.opts.IDFn == nil || len(m.bookmarks) == 0 {
		return src
	}
	out := make([]T, 0, len(src))
	rest := make([]T, 0, len(src))
	for _, it := range src {
		if m.bookmarks[m.opts.IDFn(it)] {
			out = append(out, it)
		} else {
			rest = append(rest, it)
		}
	}
	return append(out, rest...)
}

func tableStyles(th theme.Theme) table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(th.Primary).
		BorderForeground(th.Border).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(th.Bg).
		Background(th.Primary).
		Bold(false)
	s.Cell = s.Cell.Foreground(th.Text)
	return s
}
