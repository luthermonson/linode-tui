package cmdbar

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/linode/tui/internal/tui/theme"
)

type SubmitMsg struct{ Input string }
type CancelMsg struct{}

type Model struct {
	input  textinput.Model
	active bool
	theme  theme.Theme
}

func New(t theme.Theme) Model {
	ti := textinput.New()
	ti.Prompt = ":"
	ti.CharLimit = 80
	return Model{input: ti, theme: t}
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
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(m.theme.Primary)
	return style.Render(m.input.View())
}

func (m *Model) SetTheme(t theme.Theme) { m.theme = t }
