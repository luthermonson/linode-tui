package cmdbar

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/luthermonson/linode-tui/tui/theme"
)

type SubmitMsg struct{ Input string }
type CancelMsg struct{}

// maxHistory bounds the in-memory command history ring.
const maxHistory = 50

type Model struct {
	input       textinput.Model
	active      bool
	theme       theme.Theme
	completions []string

	// Tab-cycling ("menu-complete") state. cycling is true once a Tab press
	// has exhausted LCP extension and started walking the candidate list.
	// Any key other than tab/shift+tab resets this state so the next Tab
	// re-derives matches from whatever is currently typed.
	cycling      bool
	cycleStem    string   // first token as originally typed, before cycling
	cycleTail    string   // text after the first token (including leading space), preserved across cycles
	cycleMatches []string // candidates being cycled through
	cycleIndex   int      // index into cycleMatches currently shown

	// History of submitted inputs, most recent last. historyPos is -1 when
	// not navigating history; otherwise it indexes into history and stash
	// holds the in-progress input the user had before navigation began.
	history    []string
	historyPos int
	stash      string
}

func New(t theme.Theme) Model {
	ti := textinput.New()
	ti.Prompt = ":"
	ti.CharLimit = 80
	return Model{input: ti, theme: t, historyPos: -1}
}

// SetCompletions registers the full universe of cmdbar verbs that tab and
// the inline match preview should consult. Pass the verb names (no leading
// colon) — first-token matching only.
func (m *Model) SetCompletions(words []string) {
	m.completions = words
}

func (m *Model) Open() {
	m.input.SetValue("")
	m.input.Focus()
	m.active = true
	m.resetCycle()
	m.historyPos = -1
	m.stash = ""
}

func (m *Model) Close() {
	m.input.Blur()
	m.active = false
	m.resetCycle()
	m.historyPos = -1
	m.stash = ""
}

func (m Model) Active() bool { return m.active }

func (m Model) Init() tea.Cmd { return textinput.Blink }

// History returns a copy of the submitted-input history, oldest first, so a
// caller can persist it across sessions.
func (m Model) History() []string {
	out := make([]string, len(m.history))
	copy(out, m.history)
	return out
}

// SetHistory replaces the in-memory history, e.g. when restoring from a
// caller-managed store. The most recent maxHistory entries are kept, oldest
// first, preserving the input order.
func (m *Model) SetHistory(h []string) {
	if len(h) > maxHistory {
		h = h[len(h)-maxHistory:]
	}
	m.history = append([]string(nil), h...)
	m.historyPos = -1
	m.stash = ""
}

func (m *Model) pushHistory(val string) {
	if val == "" {
		return
	}
	if n := len(m.history); n > 0 && m.history[n-1] == val {
		return
	}
	m.history = append(m.history, val)
	if len(m.history) > maxHistory {
		m.history = m.history[len(m.history)-maxHistory:]
	}
}

func (m *Model) resetCycle() {
	m.cycling = false
	m.cycleStem = ""
	m.cycleTail = ""
	m.cycleMatches = nil
	m.cycleIndex = 0
}

func (m *Model) resetHistoryNav() {
	m.historyPos = -1
	m.stash = ""
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.pushHistory(val)
			m.Close()
			return m, func() tea.Msg { return SubmitMsg{Input: val} }
		case "esc":
			m.Close()
			return m, func() tea.Msg { return CancelMsg{} }
		case "tab":
			m.tabCycle(true)
			return m, nil
		case "shift+tab":
			m.tabCycle(false)
			return m, nil
		case "up":
			m.historyUp()
			return m, nil
		case "down":
			m.historyDown()
			return m, nil
		default:
			// Any other key (typing, backspace, delete, etc.) invalidates
			// tab-cycling and history-navigation state.
			m.resetCycle()
			m.resetHistoryNav()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// historyUp moves toward older entries. It only activates when the input is
// empty or history navigation is already in progress, per shell-ish
// semantics: Up from a fresh prompt shows the most recent command.
func (m *Model) historyUp() {
	if len(m.history) == 0 {
		return
	}
	if m.historyPos == -1 {
		if m.input.Value() != "" {
			return
		}
		m.stash = m.input.Value()
		m.historyPos = len(m.history) - 1
	} else if m.historyPos > 0 {
		m.historyPos--
	}
	m.setInput(m.history[m.historyPos])
}

// historyDown moves toward newer entries, and past the newest restores the
// in-progress input that was present before navigation began.
func (m *Model) historyDown() {
	if m.historyPos == -1 {
		return
	}
	m.historyPos++
	if m.historyPos >= len(m.history) {
		m.historyPos = -1
		m.setInput(m.stash)
		m.stash = ""
		return
	}
	m.setInput(m.history[m.historyPos])
}

func (m *Model) setInput(val string) {
	m.input.SetValue(val)
	m.input.SetCursor(len(val))
}

// tabCycle implements menu-complete: the first Tab extends the first token
// to the longest common prefix among matches (as before); once no further
// extension is possible and multiple candidates remain, subsequent Tab
// presses cycle forward through them and Shift+Tab cycles backward.
func (m *Model) tabCycle(forward bool) {
	if m.cycling {
		if len(m.cycleMatches) == 0 {
			m.resetCycle()
			return
		}
		if forward {
			m.cycleIndex = (m.cycleIndex + 1) % len(m.cycleMatches)
		} else {
			m.cycleIndex = (m.cycleIndex - 1 + len(m.cycleMatches)) % len(m.cycleMatches)
		}
		m.setInput(m.cycleMatches[m.cycleIndex] + m.cycleTail)
		return
	}

	head, tail := splitFirstToken(m.input.Value())
	matches := m.matchingCompletions(head)
	if len(matches) == 0 {
		return
	}
	if len(matches) == 1 {
		val := matches[0]
		if tail == "" {
			val += " "
		} else {
			val += tail
		}
		m.setInput(val)
		return
	}

	if ext := m.completeFirstToken(m.input.Value()); ext != "" {
		// LCP extends beyond what's typed; take it, same as before. Don't
		// start cycling yet — that happens once LCP == head.
		m.setInput(ext)
		return
	}

	// No further extension possible; start (or restart) cycling.
	m.cycling = true
	m.cycleStem = head
	m.cycleTail = tail
	m.cycleMatches = matches
	if forward {
		m.cycleIndex = 0
	} else {
		m.cycleIndex = len(matches) - 1
	}
	m.setInput(m.cycleMatches[m.cycleIndex] + m.cycleTail)
}

// splitFirstToken splits val into its first whitespace-delimited token and
// the remainder (tail includes the leading separator, if any).
func splitFirstToken(val string) (head, tail string) {
	if i := strings.IndexByte(val, ' '); i >= 0 {
		return val[:i], val[i:]
	}
	return val, ""
}

// matchingCompletions returns every registered completion whose name starts
// with head, in registration order.
func (m Model) matchingCompletions(head string) []string {
	if len(m.completions) == 0 || head == "" {
		return nil
	}
	var matches []string
	for _, c := range m.completions {
		if strings.HasPrefix(c, head) {
			matches = append(matches, c)
		}
	}
	return matches
}

// completeFirstToken returns a replacement string when there's a sensible
// completion of the current input, or "" otherwise. It preserves the
// pre-cycling LCP-completion behavior and is exercised directly by tests.
func (m Model) completeFirstToken(val string) string {
	head, tail := splitFirstToken(val)
	if head == "" {
		return ""
	}
	matches := m.matchingCompletions(head)
	if len(matches) == 0 {
		return ""
	}
	if len(matches) == 1 {
		return matches[0] + tail
	}
	lcp := matches[0]
	for _, c := range matches[1:] {
		lcp = longestCommonPrefix(lcp, c)
		if lcp == "" {
			break
		}
	}
	if lcp == head {
		// No extension possible; leave the input alone (callers can still
		// see the candidate list rendered below).
		return ""
	}
	return lcp + tail
}

func longestCommonPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// matchesFor returns up to max completions that start with the first token.
func (m Model) matchesFor(val string, max int) []string {
	head, _ := splitFirstToken(val)
	if head == "" {
		return nil
	}
	var out []string
	for _, c := range m.completions {
		if strings.HasPrefix(c, head) && c != head {
			out = append(out, c)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

// View renders the input line and, while active, a second hint line so the
// layout doesn't jump between zero and one candidate lines. The hint line is
// empty (but still present) when there are no matches.
func (m Model) View() string {
	if !m.active {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(m.theme.Primary)
	line := style.Render(m.input.View())

	matches := m.matchesFor(m.input.Value(), 6)
	var hintBody string
	if m.cycling && len(m.cycleMatches) > 0 {
		hintBody = "tab: " + m.renderCycleCandidates()
	} else if len(matches) > 0 {
		hintBody = "tab: " + strings.Join(matches, "  ")
	}

	hint := ""
	if hintBody != "" {
		hint = lipgloss.NewStyle().Foreground(m.theme.Muted).Render("  " + hintBody)
	}
	return line + "\n" + hint
}

// renderCycleCandidates renders the cycling candidate list (capped at 6, the
// same as the plain hint) with the currently selected candidate bracketed
// and highlighted in the theme's Primary color.
func (m Model) renderCycleCandidates() string {
	const max = 6
	selected := lipgloss.NewStyle().Foreground(m.theme.Primary).Render("[" + m.cycleMatches[m.cycleIndex] + "]")

	shown := m.cycleMatches
	idx := m.cycleIndex
	if len(shown) > max {
		// Keep the window centered on the selection so it's always visible.
		start := idx - max/2
		if start < 0 {
			start = 0
		}
		if start+max > len(shown) {
			start = len(shown) - max
		}
		shown = shown[start : start+max]
		idx = m.cycleIndex - start
	}

	parts := make([]string, len(shown))
	for i, c := range shown {
		if i == idx {
			parts[i] = selected
		} else {
			parts[i] = c
		}
	}
	return strings.Join(parts, "  ")
}

func (m *Model) SetTheme(t theme.Theme) { m.theme = t }
