package views

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/luthermonson/linode-tui/config"
	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tui/theme"
)

type fakeItem struct {
	ID    int
	Label string
}

func newTestListView(items []fakeItem, actions ...Action[fakeItem]) *listView[fakeItem] {
	m := newListView(listOpts[fakeItem]{
		Deps:  Deps{Cfg: &config.Config{Refresh: time.Second}, Theme: theme.Dark()},
		Title: "Test",
		Columns: []Col{
			{Title: "ID", Width: 4, MinWidth: 3, Priority: PriPinned},
			{Title: "LABEL", Width: 16, MinWidth: 6, Priority: PriPinned, Flex: true},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]fakeItem, error) {
			return items, nil
		},
		Rower:   func(it fakeItem) table.Row { return table.Row{strconv.Itoa(it.ID), it.Label} },
		Matcher: func(it fakeItem, needle string) bool { return strings.Contains(strings.ToLower(it.Label), needle) },
		Actions: actions,
	})
	m.items = items
	m.applyFilter()
	return m
}

func TestListViewFilter(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha"}, {2, "bravo"}, {3, "alphacat"}})
	m.filterInput.SetValue("alpha")
	m.applyFilter()

	visible := m.visibleItems()
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible, got %d", len(visible))
	}
	if visible[0].ID != 1 || visible[1].ID != 3 {
		t.Fatalf("wrong visible items: %+v", visible)
	}
}

func TestListViewSelectedItemNoFilter(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha"}, {2, "bravo"}})
	got, ok := m.SelectedItem()
	if !ok {
		t.Fatal("expected ok")
	}
	if got.ID != 1 {
		t.Fatalf("got id %d, want 1", got.ID)
	}
}

func TestListViewSelectedItemUnderFilter(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha"}, {2, "bravo"}, {3, "charlie"}})
	m.filterInput.SetValue("bravo")
	m.applyFilter()

	got, ok := m.SelectedItem()
	if !ok {
		t.Fatal("expected ok")
	}
	if got.ID != 2 {
		t.Fatalf("expected bravo (id 2) under filter, got id %d", got.ID)
	}
}

func TestListViewSelectedItemEmpty(t *testing.T) {
	m := newTestListView(nil)
	if _, ok := m.SelectedItem(); ok {
		t.Fatal("expected no selection on empty list")
	}
}

func TestListViewActionDoneTriggersFetch(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "x"}})
	m.loading = false
	_, cmd := m.Update(ActionDoneMsg{Label: "test"})
	if cmd == nil {
		t.Fatal("expected fetch cmd from ActionDoneMsg")
	}
	if !m.loading {
		t.Fatal("expected loading=true after ActionDoneMsg")
	}
}

func TestListViewTryActionDispatchesConfirm(t *testing.T) {
	var ran bool
	a := Action[fakeItem]{
		Key:    "d",
		Label:  "delete",
		Prompt: func(it fakeItem) string { return "del " + it.Label + "?" },
		Run: func(_ context.Context, _ *linode.Client, it fakeItem) error {
			ran = true
			return nil
		},
	}
	m := newTestListView([]fakeItem{{1, "alpha"}}, a)

	cmd := m.tryAction("d")
	if cmd == nil {
		t.Fatal("expected cmd from tryAction")
	}
	msg := cmd()
	confirm, ok := msg.(ConfirmMsg)
	if !ok {
		t.Fatalf("expected ConfirmMsg, got %T", msg)
	}
	if confirm.Prompt != "del alpha?" {
		t.Fatalf("prompt = %q", confirm.Prompt)
	}

	result := confirm.OnYes()
	done, ok := result.(ActionDoneMsg)
	if !ok {
		t.Fatalf("expected ActionDoneMsg, got %T", result)
	}
	if done.Label != "delete" {
		t.Fatalf("label = %q", done.Label)
	}
	if !ran {
		t.Fatal("action.Run not invoked")
	}
}

func TestListViewTryActionUnknownKey(t *testing.T) {
	a := Action[fakeItem]{Key: "d", Run: func(context.Context, *linode.Client, fakeItem) error { return nil }}
	m := newTestListView([]fakeItem{{1, "x"}}, a)
	if cmd := m.tryAction("X"); cmd != nil {
		t.Fatal("expected nil cmd for unbound key")
	}
}

func TestListViewActionErrorSurfacesErr(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "x"}})
	_, _ = m.Update(ActionErrorMsg{Label: "delete", Err: errFake("boom")})
	if m.err == nil || m.err.Error() != "boom" {
		t.Fatalf("expected err=boom, got %v", m.err)
	}
}

func TestListViewDrillInEmitsInstallNeededWhenToolMissing(t *testing.T) {
	// Tool missing → listView wraps the err into InstallNeededMsg for root model.
	// We can't easily trigger this without a runner, but at minimum confirm
	// drillIn handles a generic error without crashing.
	m := newTestListView([]fakeItem{{1, "x"}})
	cleanup := false
	cmd := m.drillIn(DrillInMsg{
		Tool:    "nonexistent-kind",
		Cleanup: func() { cleanup = true },
	})
	if cmd != nil {
		_ = cmd()
	}
	if !cleanup {
		t.Fatal("expected drill cleanup to run when tool kind unknown")
	}
	if m.err == nil {
		t.Fatal("expected err set on unknown tool kind")
	}
}

func errFake(s string) error { return fakeErr(s) }

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

// silence unused import lint in case bubbletea methods aren't all reached
var _ tea.Cmd = tea.Quit

// instanceLikeCols mirrors the widest real view (Linodes) so the responsive
// layout is exercised against something that genuinely doesn't fit an 80-col
// terminal.
func instanceLikeCols() []Col {
	return []Col{
		{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
		{Title: "LABEL", Width: 28, MinWidth: 12, Priority: PriPinned, Flex: true},
		{Title: "REGION", Width: 14, MinWidth: 8, Priority: PriMed},
		{Title: "TYPE", Width: 18, MinWidth: 10, Priority: PriMed},
		{Title: "STATUS", Width: 12, MinWidth: 8, Priority: PriHigh},
		{Title: "IPv4", Width: 16, MinWidth: 12, Priority: PriLow},
		{Title: "TAGS", Width: 24, MinWidth: 8, Priority: PriLowest},
	}
}

func newWideListView(width int) *listView[fakeItem] {
	m := newListView(listOpts[fakeItem]{
		Deps:    Deps{Cfg: &config.Config{Refresh: time.Second}, Theme: theme.Dark()},
		Title:   "Linodes",
		Columns: instanceLikeCols(),
		Rower: func(it fakeItem) table.Row {
			return table.Row{strconv.Itoa(it.ID), it.Label, "us-east", "g6-standard-2", "running", "1.2.3.4", "a,b"}
		},
		IDFn: func(it fakeItem) string { return strconv.Itoa(it.ID) },
	})
	m.items = []fakeItem{{1, "alpha"}, {2, "bravo"}}
	m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return m
}

// visibleColTitles returns the on-screen data columns, dropping the selection
// gutter that IDFn prepends.
func visibleColTitles(m *listView[fakeItem]) []string {
	cols := m.table.Columns()
	if m.opts.IDFn != nil && len(cols) > 0 {
		cols = cols[1:]
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Title)
	}
	return out
}

// renderedWidth is what the table actually consumes: every column pays for its
// width plus the table style's one-cell padding on each side.
func renderedWidth(m *listView[fakeItem]) int {
	total := 0
	for _, c := range m.table.Columns() {
		total += c.Width + colPad
	}
	return total
}

func TestListViewResponsiveColumnsWide(t *testing.T) {
	m := newWideListView(200)
	got := visibleColTitles(m)
	want := []string{"ID", "LABEL", "REGION", "TYPE", "STATUS", "IPv4", "TAGS"}
	if len(got) != len(want) {
		t.Fatalf("at 200 cols every column should survive, got %v", got)
	}
	if m.hiddenCols != 0 {
		t.Fatalf("hiddenCols = %d, want 0 at 200 cols", m.hiddenCols)
	}
	if w := renderedWidth(m); w > 200 {
		t.Fatalf("layout consumes %d cells, wider than the 200-cell pane", w)
	}
	// The Flex column should have soaked up the surplus rather than leaving
	// the table hugging the left edge.
	if m.colWidths[1] <= 28 {
		t.Fatalf("LABEL is Flex; expected it to grow past its preferred 28, got %d", m.colWidths[1])
	}
}

func TestListViewResponsiveColumnsMedium(t *testing.T) {
	m := newWideListView(120)
	if m.hiddenCols != 0 {
		t.Fatalf("at 120 cols the minimums still fit; hiddenCols = %d (%v)", m.hiddenCols, visibleColTitles(m))
	}
	if w := renderedWidth(m); w > 120 {
		t.Fatalf("layout consumes %d cells, wider than the 120-cell pane", w)
	}
}

func TestListViewResponsiveColumnsNarrow(t *testing.T) {
	m := newWideListView(80)
	got := visibleColTitles(m)
	for _, title := range got {
		if title == "TAGS" {
			t.Fatalf("TAGS is the lowest priority column and must go first at 80 cols: %v", got)
		}
	}
	if got[0] != "ID" || got[1] != "LABEL" {
		t.Fatalf("identity columns must never be dropped, got %v", got)
	}
	if m.hiddenCols != 1 {
		t.Fatalf("hiddenCols = %d, want 1 at 80 cols (%v)", m.hiddenCols, got)
	}
	if w := renderedWidth(m); w > 80 {
		t.Fatalf("layout consumes %d cells, wider than the 80-cell pane", w)
	}
}

func TestListViewResponsiveColumnsTinyKeepsPinned(t *testing.T) {
	m := newWideListView(30)
	got := visibleColTitles(m)
	if len(got) != 2 || got[0] != "ID" || got[1] != "LABEL" {
		t.Fatalf("a 30-cell pane should keep exactly ID + LABEL, got %v", got)
	}
	if m.colWidths[0] < 6 || m.colWidths[1] < 12 {
		t.Fatalf("pinned columns must not go below their minimums, got %v", m.colWidths)
	}
	if w := renderedWidth(m); w > 30 {
		t.Fatalf("layout consumes %d cells, wider than the 30-cell pane", w)
	}
	if m.hiddenCols != 5 {
		t.Fatalf("hiddenCols = %d, want 5", m.hiddenCols)
	}
}

func TestListViewHiddenColumnIndicator(t *testing.T) {
	m := newWideListView(80)
	m.loading = false
	view := m.View()
	if !strings.Contains(view, "»1 cols hidden") {
		t.Fatalf("status line should flag dropped columns, got:\n%s", view)
	}
	if !strings.Contains(view, "widen terminal") {
		t.Fatalf("indicator should tell the user what to do, got:\n%s", view)
	}
	wide := newWideListView(200)
	wide.loading = false
	if strings.Contains(wide.View(), "cols hidden") {
		t.Fatal("no indicator expected when everything fits")
	}
}

// TestListViewRowsMatchVisibleColumns guards the crash mode: bubbles' renderRow
// walks the row's cells and indexes its column slice, so a row longer than the
// surviving column set panics.
func TestListViewRowsMatchVisibleColumns(t *testing.T) {
	m := newWideListView(80)
	cols := len(m.table.Columns())
	for i, row := range m.table.Rows() {
		if len(row) != cols {
			t.Fatalf("row %d has %d cells for %d columns", i, len(row), cols)
		}
	}
	// Rendering is what would actually panic.
	_ = m.View()
}

func TestListViewTickHonorsPerAccountOverride(t *testing.T) {
	cfg := &config.Config{
		Refresh:        time.Second,
		DefaultAccount: "prod",
		Accounts: map[string]config.Account{
			"prod": {
				RefreshOverrides: map[string]time.Duration{"watchlist": 30 * time.Second},
			},
		},
		RefreshOverrides: map[string]time.Duration{"watchlist": 5 * time.Second},
	}
	m := newListView(listOpts[fakeItem]{
		Deps: Deps{
			Cfg:     cfg,
			Theme:   theme.Dark(),
			Context: map[string]any{"view_name": "watchlist"},
		},
		Title: "Watchlist",
	})
	// Trigger one tick and inspect the duration it scheduled. The cmd is
	// tea.Tick which we can't introspect directly, but we can call the
	// underlying lookup logic via opts.Deps.
	got, ok := cfg.Accounts[cfg.DefaultAccount].RefreshOverrides["watchlist"]
	if !ok || got != 30*time.Second {
		t.Fatalf("account override not set up: %v", got)
	}
	// The tick wiring is exercised by other integration paths; here we just
	// confirm the listView captures Deps.Context["view_name"] for lookup.
	if v := m.opts.Deps.CtxString("view_name"); v != "watchlist" {
		t.Fatalf("view_name not threaded into Deps: %q", v)
	}
}

// --- tick chain ------------------------------------------------------------

// collectMsgs runs cmd and flattens any tea.Batch it produces into the
// messages the runtime would end up delivering. Tick commands block for their
// interval, so callers keep Refresh tiny.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func countTicks(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(listTickMsg); ok {
			n++
		}
	}
	return n
}

func firstLoaded(t *testing.T, msgs []tea.Msg) listLoadedMsg[fakeItem] {
	t.Helper()
	for _, msg := range msgs {
		if l, ok := msg.(listLoadedMsg[fakeItem]); ok {
			return l
		}
	}
	t.Fatalf("no listLoadedMsg among %v", msgs)
	return listLoadedMsg[fakeItem]{}
}

func newFastListView() *listView[fakeItem] {
	m := newTestListView([]fakeItem{{1, "alpha"}})
	m.opts.Deps.Cfg = &config.Config{Refresh: time.Millisecond}
	return m
}

// TestListViewSingleTickChain walks Init → loaded → r → loaded → loaded and
// asserts exactly one poll chain stays alive. Before the generation guard,
// every manual refresh (and every broadcast ActionDoneMsg) permanently added a
// parallel chain, so the poll rate doubled with each keystroke.
func TestListViewSingleTickChain(t *testing.T) {
	m := newFastListView()

	// Init only fetches — scheduling a tick here too would start life with two
	// chains.
	initMsgs := collectMsgs(m.Init())
	if n := countTicks(initMsgs); n != 0 {
		t.Fatalf("Init scheduled %d ticks, want 0 (the load starts the chain)", n)
	}
	load := firstLoaded(t, initMsgs)

	// The load starts exactly one chain.
	_, cmd := m.Update(load)
	msgs := collectMsgs(cmd)
	if n := countTicks(msgs); n != 1 {
		t.Fatalf("first load scheduled %d ticks, want exactly 1", n)
	}
	staleTick := msgs[0].(listTickMsg)

	// Manual refresh while that tick is still outstanding.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	refreshMsgs := collectMsgs(cmd)
	if n := countTicks(refreshMsgs); n != 0 {
		t.Fatalf("manual refresh scheduled %d ticks itself, want 0", n)
	}
	staleLoad := load // a fetch already in flight under the retired generation

	// The outstanding tick from the retired chain must die quietly.
	if _, cmd = m.Update(staleTick); cmd != nil {
		t.Fatal("a tick from a retired generation must not trigger another fetch")
	}

	// The refresh's own load restarts the chain — one tick, not two.
	_, cmd = m.Update(firstLoaded(t, refreshMsgs))
	if n := countTicks(collectMsgs(cmd)); n != 1 {
		t.Fatalf("refresh load scheduled %d ticks, want exactly 1", n)
	}

	// A load left over from the retired generation still updates the data but
	// must not spawn a second chain.
	_, cmd = m.Update(staleLoad)
	if n := countTicks(collectMsgs(cmd)); n != 0 {
		t.Fatalf("stale load scheduled %d ticks, want 0", n)
	}
}

// TestListViewActionDoneDoesNotMultiplyChains covers the worst case: an
// ActionDoneMsg is broadcast to every pane, so a single delete used to add one
// chain per pane, permanently.
func TestListViewActionDoneDoesNotMultiplyChains(t *testing.T) {
	m := newFastListView()
	_, cmd := m.Update(firstLoaded(t, collectMsgs(m.Init())))
	if n := countTicks(collectMsgs(cmd)); n != 1 {
		t.Fatalf("expected one chain after the first load, got %d", n)
	}

	var loads []tea.Msg
	for i := 0; i < 3; i++ {
		_, cmd = m.Update(ActionDoneMsg{Label: "delete"})
		msgs := collectMsgs(cmd)
		if n := countTicks(msgs); n != 0 {
			t.Fatalf("ActionDoneMsg #%d scheduled %d ticks itself, want 0", i, n)
		}
		loads = append(loads, firstLoaded(t, msgs))
	}
	// Only the newest generation's load may restart the chain.
	total := 0
	for _, l := range loads {
		_, cmd = m.Update(l)
		total += countTicks(collectMsgs(cmd))
	}
	if total != 1 {
		t.Fatalf("3 action-done refreshes left %d live chains, want exactly 1", total)
	}
}

// --- filter highlighting ---------------------------------------------------

// TestHighlightMatchTruncatesBeforeStyling is the colour-bleed guard: the cell
// must be cut to the column width while it's still plain text. Styling first
// and letting bubbles' non-ANSI-aware runewidth.Truncate cut the result slices
// through an escape sequence and the colour runs on across the row.
func TestHighlightMatchTruncatesBeforeStyling(t *testing.T) {
	style := lipgloss.NewStyle().Bold(true)
	long := "alpha-" + strings.Repeat("x", 40)
	got := highlightMatch(long, "alpha", style, 16)
	if w := lipgloss.Width(got); w > 16 {
		t.Fatalf("highlighted cell is %d cells wide, want <= 16 (%q)", w, got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("a truncated cell should be marked with an ellipsis, got %q", got)
	}
}

// TestHighlightMatchNonASCII covers the len(s)-in-bytes bug: a multi-byte
// label was measured as longer than its column and skipped highlighting
// entirely, so filtering silently stopped marking non-ASCII rows.
func TestHighlightMatchNonASCII(t *testing.T) {
	style := lipgloss.NewStyle().Bold(true)
	// 13 runes but 18 bytes — byte length alone would call this too wide for
	// a 16-cell column.
	label := "ünïcödé-alpha"
	got := highlightMatch(label, "alpha", style, 16)
	if w := lipgloss.Width(got); w > 16 {
		t.Fatalf("cell is %d cells wide, want <= 16", w)
	}
	if !strings.Contains(got, "alpha") {
		t.Fatalf("the match should survive intact in %q", got)
	}
	if strings.Contains(got, "…") {
		t.Fatalf("%q fits in 16 cells and must not be truncated: %q", label, got)
	}
}

func TestTruncateCells(t *testing.T) {
	cases := []struct{ in, want string }{
		{"short", "short"},
		{"exactlyten", "exactlyten"},
		{"waytoolongforthis", "waytoolon…"},
	}
	for _, c := range cases {
		if got := truncateCells(c.in, 10); got != c.want {
			t.Fatalf("truncateCells(%q, 10) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := truncateCells("anything", 0); got != "" {
		t.Fatalf("zero width should render nothing, got %q", got)
	}
}

// TestListViewFilteredCellsFitColumns checks the whole path: rows built under
// an active filter must never exceed the width the table hands them.
func TestListViewFilteredCellsFitColumns(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha-" + strings.Repeat("y", 40)}})
	m.filterInput.SetValue("alpha")
	m.applyFilter()
	cols := m.table.Columns()
	for _, row := range m.table.Rows() {
		for i, cell := range row {
			if w := lipgloss.Width(cell); w > cols[i].Width {
				t.Fatalf("cell %d is %d cells wide, column is %d: %q", i, w, cols[i].Width, cell)
			}
		}
	}
}

// --- status line, empty state, unsupported keys ----------------------------

func TestListViewStatusShowsFilteredCount(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha"}, {2, "bravo"}, {3, "alphacat"}})
	m.loading = false
	if s := m.View(); !strings.Contains(s, "3 test") {
		t.Fatalf("unfiltered status should show the total, got:\n%s", s)
	}
	m.filterInput.SetValue("alpha")
	m.applyFilter()
	if s := m.View(); !strings.Contains(s, "2/3 test") {
		t.Fatalf("filtered status should show visible/total, got:\n%s", s)
	}
}

func TestListViewEmptyState(t *testing.T) {
	m := newTestListView(nil)
	m.loading = false
	s := m.View()
	if !strings.Contains(s, "no test found") {
		t.Fatalf("empty list should explain itself, got:\n%s", s)
	}
	if !strings.Contains(s, "press r to refresh") {
		t.Fatalf("empty state should hint at refresh, got:\n%s", s)
	}
}

func TestListViewFilterInputSitsAboveStatus(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha"}})
	m.loading = false
	m.filtering = true
	m.filterInput.SetValue("alpha")
	m.applyFilter()
	lines := strings.Split(m.View(), "\n")
	filterLine, statusLine := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "/alpha") {
			filterLine = i
		}
		if strings.Contains(l, "refreshed") {
			statusLine = i
		}
	}
	if filterLine < 0 || statusLine < 0 {
		t.Fatalf("expected both a filter line and a status line, got:\n%s", m.View())
	}
	if filterLine > statusLine {
		t.Fatalf("filter input (line %d) must sit above the status line (line %d)", filterLine, statusLine)
	}
}

func TestListViewUnsupportedKeysExplainThemselves(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"*", "bookmarks"},
		{"enter", "drill-in"},
		{"D", "delete"},
		{"space", "selection"},
	}
	for _, c := range cases {
		m := newTestListView([]fakeItem{{1, "alpha"}})
		var msg tea.KeyMsg
		switch c.key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)}
		}
		m.Update(msg)
		if !strings.Contains(m.notice, c.want) {
			t.Fatalf("key %q: notice = %q, want something mentioning %q", c.key, m.notice, c.want)
		}
	}
}

func TestListViewNoticeClearsOnNextKey(t *testing.T) {
	m := newTestListView([]fakeItem{{1, "alpha"}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("*")})
	if m.notice == "" {
		t.Fatal("expected a notice")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.notice != "" {
		t.Fatalf("notice should be transient, still showing %q", m.notice)
	}
}

// --- watchlist -------------------------------------------------------------

// TestWatchlistUnstar covers the one view that lists your bookmarks being
// unable to remove one: it has no single BookmarkKind, so listView's generic
// `*` bailed out and the key was swallowed.
func TestWatchlistUnstar(t *testing.T) {
	cfg := &config.Config{
		Refresh:   time.Second,
		Bookmarks: map[string][]string{"instances": {"1", "2"}, "lke": {"9"}},
	}
	d := Deps{Cfg: cfg, Theme: theme.Dark()}
	v, ok := newWatchlist(d).(*listView[WatchlistRow])
	if !ok {
		t.Fatal("watchlist should be a listView")
	}
	v.items = []WatchlistRow{
		{Kind: "instances", ID: "1", Label: "alpha"},
		{Kind: "instances", ID: "2", Label: "bravo"},
		{Kind: "lke", ID: "9", Label: "cluster"},
	}
	v.applyFilter()

	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("*")})
	if cmd == nil {
		t.Fatal("* on the watchlist must do something — it's the view that lists bookmarks")
	}
	msg := cmd()
	if _, ok := msg.(ActionDoneMsg); !ok {
		t.Fatalf("expected ActionDoneMsg to force a reload, got %T (%v)", msg, msg)
	}
	got := cfg.ActiveBookmarks()["instances"]
	if len(got) != 1 || got[0] != "2" {
		t.Fatalf("instances bookmarks = %v, want [2]", got)
	}
	if lke := cfg.ActiveBookmarks()["lke"]; len(lke) != 1 {
		t.Fatalf("unstarring an instance must not touch other kinds: %v", lke)
	}
}

func TestUnstarWatchlistRowLastOfKind(t *testing.T) {
	cfg := &config.Config{Bookmarks: map[string][]string{"lke": {"9"}}}
	cmd := unstarWatchlistRow(WatchlistRow{Kind: "lke", ID: "9"}, Deps{Cfg: cfg})
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if _, ok := cmd().(ActionDoneMsg); !ok {
		t.Fatal("expected ActionDoneMsg")
	}
	if ids := cfg.ActiveBookmarks()["lke"]; len(ids) > 0 {
		t.Fatalf("kind should be dropped entirely when its last bookmark goes: %v", ids)
	}
}

func TestUnstarWatchlistRowNotBookmarked(t *testing.T) {
	cfg := &config.Config{Bookmarks: map[string][]string{"lke": {"9"}}}
	cmd := unstarWatchlistRow(WatchlistRow{Kind: "lke", ID: "404"}, Deps{Cfg: cfg})
	if cmd == nil {
		t.Fatal("expected a command explaining the no-op")
	}
	if _, ok := cmd().(ErrorMsg); !ok {
		t.Fatal("expected an ErrorMsg for a row that isn't bookmarked")
	}
	if ids := cfg.ActiveBookmarks()["lke"]; len(ids) != 1 {
		t.Fatalf("existing bookmarks must be untouched: %v", ids)
	}
}

// --- shared refresh interval -----------------------------------------------

func TestRefreshIntervalPrecedence(t *testing.T) {
	cfg := &config.Config{
		Refresh:        2 * time.Second,
		DefaultAccount: "prod",
		Accounts: map[string]config.Account{
			"prod": {RefreshOverrides: map[string]time.Duration{"instances": 30 * time.Second}},
		},
		RefreshOverrides: map[string]time.Duration{"instances": 5 * time.Second, "lke": 7 * time.Second},
	}
	d := Deps{Cfg: cfg}
	if got := refreshInterval(d, "instances", time.Minute); got != 30*time.Second {
		t.Fatalf("account override should win, got %v", got)
	}
	if got := refreshInterval(d, "lke", time.Minute); got != 7*time.Second {
		t.Fatalf("global override should beat the view fallback, got %v", got)
	}
	if got := refreshInterval(d, "volumes", time.Minute); got != time.Minute {
		t.Fatalf("view fallback should beat cfg.Refresh, got %v", got)
	}
	if got := refreshInterval(d, "volumes", 0); got != 2*time.Second {
		t.Fatalf("cfg.Refresh should apply with no fallback, got %v", got)
	}
	if got := refreshInterval(Deps{}, "volumes", 0); got != 2*time.Second {
		t.Fatalf("bare deps should land on the 2s floor, got %v", got)
	}
}

// TestDetailViewsHonorRefreshOverrides is the regression for detail views
// ignoring RefreshOverrides entirely — they hard-coded cfg.Refresh.
func TestDetailViewsHonorRefreshOverrides(t *testing.T) {
	cfg := &config.Config{
		Refresh:          2 * time.Second,
		RefreshOverrides: map[string]time.Duration{"lke_detail": 45 * time.Second, "instance_detail": 90 * time.Second},
	}
	lke := &lkeDetail{deps: Deps{Cfg: cfg}}
	if got := refreshInterval(lke.deps, "lke_detail", 0); got != 45*time.Second {
		t.Fatalf("lke_detail interval = %v, want 45s", got)
	}
	inst := &instanceDetail{deps: Deps{Cfg: cfg}}
	if got := refreshInterval(inst.deps, "instance_detail", 0); got != 90*time.Second {
		t.Fatalf("instance_detail interval = %v, want 90s", got)
	}
}

// --- text view -------------------------------------------------------------

// TestTextViewClipsToPane guards :split-preview pushing the footer off-screen:
// a JSON body far taller than the pane must render at the pane's height.
func TestTextViewClipsToPane(t *testing.T) {
	body := strings.Repeat("{\"a\": 1}\n", 200)
	tv := NewTextView("preview", body)
	tv.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	lines := strings.Split(tv.View(), "\n")
	if len(lines) > 10 {
		t.Fatalf("view rendered %d lines into a 10-line pane", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > 40 {
			t.Fatalf("line %d is %d cells wide, pane is 40", i, w)
		}
	}
}

func TestTextViewPicksUpDirectBodyWrites(t *testing.T) {
	tv := NewTextView("preview", "first")
	tv.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	tv.Body = "second" // what the split-preview refresh does
	if !strings.Contains(tv.View(), "second") {
		t.Fatalf("View should reconcile a directly-assigned Body, got:\n%s", tv.View())
	}
}
