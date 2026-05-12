package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// accountPicker is a tiny subform: huh.Select of account names. On completion,
// root's finishForm type-asserts and routes through the existing
// dispatchAccount switch flow.
type accountPicker struct {
	form    *huh.Form
	choice  string
	done    bool
	aborted bool
}

func newAccountPicker(names []string, active string) *accountPicker {
	opts := make([]huh.Option[string], 0, len(names))
	for _, n := range names {
		label := n
		if n == active {
			label = n + " (active)"
		}
		opts = append(opts, huh.NewOption(label, n))
	}
	p := &accountPicker{}
	if active != "" {
		p.choice = active
	}
	p.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Switch account").
				Description("Token re-resolves through env / config / 1Password.").
				Options(opts...).
				Value(&p.choice),
		),
	)
	return p
}

func (p *accountPicker) Init() tea.Cmd { return p.form.Init() }

func (p *accountPicker) Update(msg tea.Msg) tea.Cmd {
	next, cmd := p.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		p.form = f
	}
	switch p.form.State {
	case huh.StateCompleted:
		p.done = true
	case huh.StateAborted:
		p.done = true
		p.aborted = true
	}
	return cmd
}

func (p *accountPicker) View() string { return p.form.View() }

func (p *accountPicker) Done() bool { return p.done }

func (p *accountPicker) Result() string {
	if p.done && !p.aborted && p.choice != "" {
		return fmt.Sprintf("switching to %s…", p.choice)
	}
	return ""
}

func (p *accountPicker) Err() error { return nil }

// Selected returns the chosen account name, or "" if cancelled.
func (p *accountPicker) Selected() string {
	if p.aborted {
		return ""
	}
	return p.choice
}
