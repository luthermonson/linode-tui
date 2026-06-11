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

type Model struct {
	input       textinput.Model
	active      bool
	theme       theme.Theme
	completions []string
}

func New(t theme.Theme) Model {
	ti := textinput.New()
	ti.Prompt = ":"
	ti.CharLimit = 80
	return Model{input: ti, theme: t}
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
}

func (m *Model) Close() {
	m.input.Blur()
	m.active = false
}

func (m Model) Active() bool { return m.active }

func (m Model) Init() tea.Cmd { return textinput.Blink }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.Close()
			return m, func() tea.Msg { return SubmitMsg{Input: val} }
		case "esc":
			m.Close()
			return m, func() tea.Msg { return CancelMsg{} }
		case "tab":
			// Complete the first token to the longest common prefix among
			// matching completions; if exactly one matches, take it.
			if c := m.completeFirstToken(m.input.Value()); c != "" {
				m.input.SetValue(c)
				m.input.SetCursor(len(c))
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// completeFirstToken returns a replacement string when there's a sensible
// completion of the current input, or "" otherwise.
func (m Model) completeFirstToken(val string) string {
	if len(m.completions) == 0 {
		return ""
	}
	firstSpace := strings.IndexByte(val, ' ')
	head := val
	tail := ""
	if firstSpace >= 0 {
		head = val[:firstSpace]
		tail = val[firstSpace:]
	}
	if head == "" {
		return ""
	}
	var matches []string
	for _, c := range m.completions {
		if strings.HasPrefix(c, head) {
			matches = append(matches, c)
		}
	}
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
	if len(m.completions) == 0 {
		return nil
	}
	head := val
	if i := strings.IndexByte(val, ' '); i >= 0 {
		head = val[:i]
	}
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

func (m Model) View() string {
	if !m.active {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(m.theme.Primary)
	line := style.Render(m.input.View())
	matches := m.matchesFor(m.input.Value(), 6)
	if len(matches) == 0 {
		return line
	}
	hint := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(
		"  tab: " + strings.Join(matches, "  "),
	)
	return line + "\n" + hint
}

func (m *Model) SetTheme(t theme.Theme) { m.theme = t }
