package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/linode/tui/tui/theme"
)

// confirmModal is a thin wrapper around a single-question huh.Confirm form.
// The host model (app.go) shows it in the body, forwards messages while
// active, and on Done() runs onYes if Confirmed().
type confirmModal struct {
	form        *huh.Form
	value       bool
	onYes       tea.Cmd
	theme       theme.Theme
	destructive bool
}

func newConfirmModal(prompt string, onYes tea.Cmd) *confirmModal {
	return newConfirmModalWithTheme(prompt, onYes, theme.Theme{})
}

func newConfirmModalWithTheme(prompt string, onYes tea.Cmd, th theme.Theme) *confirmModal {
	m := &confirmModal{
		onYes:       onYes,
		theme:       th,
		destructive: isDestructive(prompt),
	}
	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(prompt).
				Affirmative("Yes").
				Negative("No").
				Value(&m.value),
		),
	)
	return m
}

// isDestructive heuristics on the prompt text: any all-caps verb suggests we
// should highlight the modal.
func isDestructive(prompt string) bool {
	upper := strings.ToUpper(prompt)
	for _, kw := range []string{"DELETE", "REBUILD", "WIPE", "DESTROY", "RESET"} {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

func (c *confirmModal) Init() tea.Cmd { return c.form.Init() }

func (c *confirmModal) Update(msg tea.Msg) tea.Cmd {
	next, cmd := c.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		c.form = f
	}
	return cmd
}

func (c *confirmModal) View() string {
	body := c.form.View()
	if c.destructive && c.theme.Error != "" {
		border := lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(c.theme.Error).
			Padding(0, 1)
		return border.Render(body)
	}
	return body
}

func (c *confirmModal) Done() bool {
	return c.form.State == huh.StateCompleted || c.form.State == huh.StateAborted
}

func (c *confirmModal) Confirmed() bool {
	return c.form.State == huh.StateCompleted && c.value
}
