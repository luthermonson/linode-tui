package tui

import tea "github.com/charmbracelet/bubbletea"

// subform is implemented by the modal models that take over the body of the
// TUI for a multi-step interaction: create / configure flows. The root model
// holds at most one subform at a time.
type subform interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	View() string
	Done() bool
	// Result returns the human-readable success message after Done.
	// Empty when the subform failed or was cancelled.
	Result() string
	Err() error
}
