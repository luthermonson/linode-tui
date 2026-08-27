package cmdbar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/luthermonson/linode-tui/tui/theme"
)

func newTestModel(completions []string) Model {
	m := New(theme.Dark())
	m.SetCompletions(completions)
	m.Open()
	return m
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeStr(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(keyRunes(string(r)))
	}
	return m
}

func keyTab() tea.KeyMsg       { return tea.KeyMsg{Type: tea.KeyTab} }
func keyShiftTab() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyShiftTab} }
func keyUp() tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyUp} }
func keyDown() tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyDown} }
func keyEnter() tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyEnter} }
func keyEsc() tea.KeyMsg       { return tea.KeyMsg{Type: tea.KeyEsc} }
func keyBackspace() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyBackspace} }

// --- completeFirstToken / LCP behavior (existing logic) ---

func TestCompleteFirstToken(t *testing.T) {
	cases := []struct {
		name        string
		completions []string
		val         string
		want        string
	}{
		{"empty head", []string{"theme"}, "", ""},
		{"no match", []string{"theme"}, "zzz", ""},
		{"single match, no tail", []string{"theme"}, "th", "theme"},
		{"single match with tail preserved", []string{"theme"}, "th arg1", "theme arg1"},
		{"exact single match unaffected", []string{"theme"}, "theme", "theme"},
		{"multiple matches extend to lcp", []string{"theme-dark", "theme-light"}, "th", "theme-"},
		{"lcp already equals head: no extension", []string{"linodes", "list", "load", "lish"}, "l", ""},
		{"tail preserved through lcp extension", []string{"theme-dark", "theme-light"}, "the arg", "theme- arg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(theme.Dark())
			m.SetCompletions(tc.completions)
			got := m.completeFirstToken(tc.val)
			if got != tc.want {
				t.Errorf("completeFirstToken(%q) = %q, want %q", tc.val, got, tc.want)
			}
		})
	}
}

func TestCompleteFirstTokenNoCompletions(t *testing.T) {
	m := New(theme.Dark())
	if got := m.completeFirstToken("anything"); got != "" {
		t.Errorf("expected no completion with empty completions, got %q", got)
	}
}

// --- Tab cycling (menu-complete) ---

func TestTabCycling_SingleMatchCompletesFullyWithTrailingSpace(t *testing.T) {
	m := newTestModel([]string{"theme"})
	m = typeStr(m, "th")
	m, _ = m.Update(keyTab())
	if got, want := m.input.Value(), "theme "; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	if m.cycling {
		t.Fatalf("single-match completion should not enter cycling state")
	}
}

func TestTabCycling_NoMatchesIsNoop(t *testing.T) {
	m := newTestModel([]string{"theme"})
	m = typeStr(m, "zzz")
	m, _ = m.Update(keyTab())
	if got := m.input.Value(); got != "zzz" {
		t.Fatalf("input = %q, want unchanged %q", got, "zzz")
	}
}

func TestTabCycling_ForwardCyclesAndWraps(t *testing.T) {
	completions := []string{"linodes", "list", "load", "lish"}
	m := newTestModel(completions)
	m = typeStr(m, "l arg1")

	// First tab: lcp of the four candidates is "l", which equals head, so
	// this tab press should start cycling at candidate 0.
	m, _ = m.Update(keyTab())
	if !m.cycling {
		t.Fatalf("expected cycling to start when lcp == head")
	}
	if got, want := m.input.Value(), "linodes arg1"; got != want {
		t.Fatalf("input after first tab = %q, want %q", got, want)
	}

	wantSeq := []string{"list arg1", "load arg1", "lish arg1", "linodes arg1"}
	for i, want := range wantSeq {
		m, _ = m.Update(keyTab())
		if got := m.input.Value(); got != want {
			t.Fatalf("tab press %d: input = %q, want %q", i+2, got, want)
		}
	}
}

func TestTabCycling_ShiftTabCyclesBackward(t *testing.T) {
	completions := []string{"linodes", "list", "load", "lish"}
	m := newTestModel(completions)
	m = typeStr(m, "l")

	// Shift+tab with no prior extension should start cycling at the last
	// candidate.
	m, _ = m.Update(keyShiftTab())
	if got, want := m.input.Value(), "lish"; got != want {
		t.Fatalf("input after first shift+tab = %q, want %q", got, want)
	}

	wantSeq := []string{"load", "list", "linodes", "lish"}
	for i, want := range wantSeq {
		m, _ = m.Update(keyShiftTab())
		if got := m.input.Value(); got != want {
			t.Fatalf("shift+tab press %d: input = %q, want %q", i+2, got, want)
		}
	}
}

func TestTabCycling_ExtendsLcpBeforeCycling(t *testing.T) {
	completions := []string{"theme-dark", "theme-light"}
	m := newTestModel(completions)
	m = typeStr(m, "th")

	// First tab extends to the shared "theme-" prefix without cycling.
	m, _ = m.Update(keyTab())
	if m.cycling {
		t.Fatalf("expected lcp extension, not cycling, on first tab")
	}
	if got, want := m.input.Value(), "theme-"; got != want {
		t.Fatalf("input after first tab = %q, want %q", got, want)
	}

	// Second tab: lcp == head now, so cycling begins.
	m, _ = m.Update(keyTab())
	if !m.cycling {
		t.Fatalf("expected cycling to start on second tab once lcp == head")
	}
	if got, want := m.input.Value(), "theme-dark"; got != want {
		t.Fatalf("input after second tab = %q, want %q", got, want)
	}
}

func TestTabCycling_ResetsOnTyping(t *testing.T) {
	completions := []string{"linodes", "list", "load", "lish"}
	m := newTestModel(completions)
	m = typeStr(m, "l")
	m, _ = m.Update(keyTab())
	if !m.cycling {
		t.Fatalf("expected cycling to have started")
	}

	// Typing a character should reset cycling state.
	m, _ = m.Update(keyRunes("x"))
	if m.cycling {
		t.Fatalf("expected cycling state to reset after typing")
	}
}

func TestTabCycling_ResetsOnBackspace(t *testing.T) {
	completions := []string{"linodes", "list", "load", "lish"}
	m := newTestModel(completions)
	m = typeStr(m, "l")
	m, _ = m.Update(keyTab())
	if !m.cycling {
		t.Fatalf("expected cycling to have started")
	}

	m, _ = m.Update(keyBackspace())
	if m.cycling {
		t.Fatalf("expected cycling state to reset after backspace")
	}
}

func TestTabCycling_ResetsOnEsc(t *testing.T) {
	completions := []string{"linodes", "list", "load", "lish"}
	m := newTestModel(completions)
	m = typeStr(m, "l")
	m, _ = m.Update(keyTab())
	if !m.cycling {
		t.Fatalf("expected cycling to have started")
	}
	m, _ = m.Update(keyEsc())
	if m.cycling {
		t.Fatalf("expected cycling state to reset after esc")
	}
	if m.Active() {
		t.Fatalf("esc should close the cmdbar")
	}
}

// --- History ---

func submit(m Model, s string) Model {
	m = typeStr(m, s)
	m, _ = m.Update(keyEnter())
	return m
}

func TestHistory_DedupeConsecutiveAndSkipEmpty(t *testing.T) {
	m := New(theme.Dark())
	m.Open()
	m = submit(m, "list")
	m.Open()
	m = submit(m, "list") // consecutive duplicate, should be deduped
	m.Open()
	m = submit(m, "") // empty, should be skipped
	m.Open()
	m = submit(m, "load")

	got := m.History()
	want := []string{"list", "load"}
	if len(got) != len(want) {
		t.Fatalf("History() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("History() = %v, want %v", got, want)
		}
	}
}

func TestHistory_UpDownNavigation(t *testing.T) {
	m := New(theme.Dark())
	m.SetHistory([]string{"theme dark", "list", "load"})
	m.Open()

	// Up from a fresh (empty) prompt shows the most recent command.
	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "load"; got != want {
		t.Fatalf("after Up: input = %q, want %q", got, want)
	}
	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "list"; got != want {
		t.Fatalf("after second Up: input = %q, want %q", got, want)
	}
	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "theme dark"; got != want {
		t.Fatalf("after third Up: input = %q, want %q", got, want)
	}
	// Further Up at the oldest entry stays put.
	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "theme dark"; got != want {
		t.Fatalf("Up at oldest entry: input = %q, want %q", got, want)
	}

	// Down moves toward newest.
	m, _ = m.Update(keyDown())
	if got, want := m.input.Value(), "list"; got != want {
		t.Fatalf("after Down: input = %q, want %q", got, want)
	}
	m, _ = m.Update(keyDown())
	if got, want := m.input.Value(), "load"; got != want {
		t.Fatalf("after second Down: input = %q, want %q", got, want)
	}
	// Down past the newest restores the in-progress (empty) input.
	m, _ = m.Update(keyDown())
	if got, want := m.input.Value(), ""; got != want {
		t.Fatalf("after Down past newest: input = %q, want %q", got, want)
	}
}

func TestHistory_UpDoesNothingWithNonEmptyInputNotNavigating(t *testing.T) {
	m := New(theme.Dark())
	m.SetHistory([]string{"list", "load"})
	m.Open()
	m = typeStr(m, "abc")

	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "abc"; got != want {
		t.Fatalf("Up with non-empty non-navigating input: input = %q, want %q", got, want)
	}
}

func TestHistory_TypingExitsNavigation(t *testing.T) {
	m := New(theme.Dark())
	m.SetHistory([]string{"list", "load"})
	m.Open()

	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "load"; got != want {
		t.Fatalf("after Up: input = %q, want %q", got, want)
	}
	m, _ = m.Update(keyRunes("x"))
	if m.historyPos != -1 {
		t.Fatalf("expected historyPos to reset after typing, got %d", m.historyPos)
	}

	// A subsequent Up should now be a no-op since input is non-empty and
	// navigation was reset.
	before := m.input.Value()
	m, _ = m.Update(keyUp())
	if got := m.input.Value(); got != before {
		t.Fatalf("Up after typing exited nav: input = %q, want unchanged %q", got, before)
	}
}

func TestHistory_InProgressInputRestoredWhenStartedEmpty(t *testing.T) {
	m := New(theme.Dark())
	m.SetHistory([]string{"list"})
	m.Open()

	m, _ = m.Update(keyUp())
	if got, want := m.input.Value(), "list"; got != want {
		t.Fatalf("after Up: input = %q, want %q", got, want)
	}
	m, _ = m.Update(keyDown())
	if got, want := m.input.Value(), ""; got != want {
		t.Fatalf("after Down past newest: input = %q, want %q", got, want)
	}
}

func TestHistory_SetHistoryCapsAtMax(t *testing.T) {
	long := make([]string, 0, maxHistory+10)
	for i := 0; i < maxHistory+10; i++ {
		long = append(long, string(rune('a'+i%26)))
	}
	m := New(theme.Dark())
	m.SetHistory(long)
	got := m.History()
	if len(got) != maxHistory {
		t.Fatalf("History() length = %d, want %d", len(got), maxHistory)
	}
	if got[len(got)-1] != long[len(long)-1] {
		t.Fatalf("History() should keep the most recent entries; got last %q, want %q", got[len(got)-1], long[len(long)-1])
	}
}

// --- View() line-count stability ---

func TestView_LineCountStableWithAndWithoutMatches(t *testing.T) {
	m := newTestModel([]string{"linodes", "list", "load"})

	// No matches (empty input): still two lines.
	linesEmpty := strings.Count(m.View(), "\n") + 1
	if linesEmpty != 2 {
		t.Fatalf("View() with no matches has %d lines, want 2", linesEmpty)
	}

	m = typeStr(m, "l")
	linesWithMatches := strings.Count(m.View(), "\n") + 1
	if linesWithMatches != 2 {
		t.Fatalf("View() with matches has %d lines, want 2", linesWithMatches)
	}

	if linesEmpty != linesWithMatches {
		t.Fatalf("line count changed between no-match (%d) and match (%d) states", linesEmpty, linesWithMatches)
	}
}

func TestView_InactiveReturnsEmpty(t *testing.T) {
	m := New(theme.Dark())
	if got := m.View(); got != "" {
		t.Fatalf("inactive View() = %q, want empty", got)
	}
}

func TestView_HighlightsCyclingCandidate(t *testing.T) {
	completions := []string{"linodes", "list", "load", "lish"}
	m := newTestModel(completions)
	m = typeStr(m, "l")
	m, _ = m.Update(keyTab())

	view := m.View()
	if !strings.Contains(view, "[linodes]") {
		t.Fatalf("expected cycling hint to bracket the selected candidate, got: %q", view)
	}
}
