package keys

import "github.com/charmbracelet/bubbles/key"

type Map struct {
	Quit    key.Binding
	Help    key.Binding
	CmdBar  key.Binding
	Filter  key.Binding
	Cancel  key.Binding
	Back    key.Binding
	Refresh key.Binding
	Enter   key.Binding
	Up      key.Binding
	Down    key.Binding
	// Replay opens the audit-replay flow for the most recent mutating entry
	// (matches `:undo` with a step of 0).
	Replay key.Binding
}

func Default() Map {
	return Map{
		Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		CmdBar:  key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel/clear")),
		Back:    key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "back")),
		Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Replay:  key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "replay last audit entry")),
	}
}

// ShortHelp returns the small footer-bar bindings.
func (m Map) ShortHelp() []key.Binding {
	return []key.Binding{m.CmdBar, m.Filter, m.Help, m.Back, m.Quit}
}

// FullHelp returns rows of bindings for the long-form help overlay.
func (m Map) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.CmdBar, m.Filter, m.Refresh, m.Enter},
		{m.Help, m.Cancel, m.Back, m.Quit},
		{m.Up, m.Down},
	}
}
