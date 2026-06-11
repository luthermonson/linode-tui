package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/luthermonson/linode-tui/tui/theme"
)

type detailModal struct {
	vp     viewport.Model
	title  string
	theme  theme.Theme
	onEdit tea.Cmd
}

func newDetailModal(title, body string, th theme.Theme, width, height int, onEdit tea.Cmd) *detailModal {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}
	// leave room for title + footer
	vp := viewport.New(width-2, height-4)
	vp.SetContent(body)
	return &detailModal{vp: vp, title: title, theme: th, onEdit: onEdit}
}

func (d *detailModal) Init() tea.Cmd { return nil }

// Update returns (closed, editCmd, cmd). When the user pressed 'e' and onEdit
// is non-nil, editCmd carries it so the caller can fire it after dismissing.
func (d *detailModal) Update(msg tea.Msg) (closed bool, editCmd tea.Cmd, cmd tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			return true, nil, nil
		case "e":
			if d.onEdit != nil {
				return true, d.onEdit, nil
			}
		case "ctrl+d":
			d.vp.HalfPageDown()
			return false, nil, nil
		case "ctrl+u":
			d.vp.HalfPageUp()
			return false, nil, nil
		case "ctrl+f", "pgdown", " ":
			d.vp.PageDown()
			return false, nil, nil
		case "ctrl+b", "pgup":
			d.vp.PageUp()
			return false, nil, nil
		case "g", "home":
			d.vp.GotoTop()
			return false, nil, nil
		case "G", "end":
			d.vp.GotoBottom()
			return false, nil, nil
		}
	case tea.WindowSizeMsg:
		d.vp.Width = m.Width - 2
		d.vp.Height = m.Height - 4
	}
	vp, c := d.vp.Update(msg)
	d.vp = vp
	return false, nil, c
}

func (d *detailModal) View() string {
	title := lipgloss.NewStyle().Foreground(d.theme.Primary).Bold(true).Render(d.title)
	hintStr := "↑/↓ line · ctrl+u/d half-page · g/G top/bot · esc/q close"
	if d.onEdit != nil {
		hintStr = "↑/↓ line · ctrl+u/d half-page · e edit · esc/q close"
	}
	hint := lipgloss.NewStyle().Foreground(d.theme.Muted).Render(hintStr)
	body := lipgloss.NewStyle().Foreground(d.theme.Text).Render(d.vp.View())
	return fmt.Sprintf("%s\n%s\n%s", title, body, hint)
}
