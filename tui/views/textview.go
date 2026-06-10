package views

import (
	tea "github.com/charmbracelet/bubbletea"
)

// TextView is a minimal read-only view that renders a static string. Used by
// :split-preview to drop a snapshot of the focused row into a pane.
type TextView struct {
	TitleText string
	Body      string
	w, h      int
}

// NewTextView returns a constructor-ready TextView.
func NewTextView(title, body string) *TextView {
	return &TextView{TitleText: title, Body: body}
}

func (t *TextView) Init() tea.Cmd { return nil }
func (t *TextView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		t.w, t.h = size.Width, size.Height
	}
	return t, nil
}
func (t *TextView) Title() string { return t.TitleText }
func (t *TextView) View() string  { return t.Body }
