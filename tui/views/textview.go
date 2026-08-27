package views

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// TextView is a read-only view that renders a static string. Used by
// :split-preview to drop a snapshot of the focused row into a pane.
//
// The body is backed by a viewport (mirroring detailModal) because the
// snapshots it shows are pretty-printed JSON: rendering that raw overflowed
// the pane and pushed the footer off-screen, with no way to scroll.
type TextView struct {
	TitleText string
	Body      string
	viewport  viewport.Model
	// rendered is the body currently loaded into the viewport. Callers are
	// allowed to poke Body directly (the split-preview refresh does), so View
	// reconciles rather than trusting SetBody to have been used.
	rendered string
	w, h     int
}

// NewTextView returns a constructor-ready TextView.
func NewTextView(title, body string) *TextView {
	vp := viewport.New(80, 20)
	vp.SetContent(body)
	return &TextView{TitleText: title, Body: body, rendered: body, viewport: vp}
}

func (t *TextView) Init() tea.Cmd { return nil }

func (t *TextView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		t.w, t.h = size.Width, size.Height
		t.viewport.Width = size.Width
		h := size.Height
		if h < 1 {
			h = 1
		}
		t.viewport.Height = h
		t.viewport.SetContent(t.Body)
		t.rendered = t.Body
		return t, nil
	}
	var cmd tea.Cmd
	t.viewport, cmd = t.viewport.Update(msg)
	return t, cmd
}

func (t *TextView) Title() string { return t.TitleText }

func (t *TextView) View() string {
	if t.Body != t.rendered {
		t.viewport.SetContent(t.Body)
		t.rendered = t.Body
	}
	return t.viewport.View()
}

// SetBody replaces the content and rewinds to the top.
func (t *TextView) SetBody(body string) {
	t.Body = body
	t.rendered = body
	t.viewport.SetContent(body)
	t.viewport.GotoTop()
}
