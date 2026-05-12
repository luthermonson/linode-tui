package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/linode/tui/internal/tools"
)

const installPromptCustomOpt = "__custom__"

type installPrompt struct {
	form   *huh.Form
	kind   tools.Kind
	choice string
	custom string
}

func newInstallPrompt(kind tools.Kind, suggestions []string) *installPrompt {
	options := make([]huh.Option[string], 0, len(suggestions)+1)
	for _, s := range suggestions {
		options = append(options, huh.NewOption(s, s))
	}
	options = append(options, huh.NewOption("Custom path…", installPromptCustomOpt))

	m := &installPrompt{kind: kind}
	if len(suggestions) > 0 {
		m.choice = suggestions[0]
	}
	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Install %s where?", kind)).
				Description("Subsequent installs will reuse this directory.").
				Options(options...).
				Value(&m.choice),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Custom path:").
				Value(&m.custom),
		).WithHideFunc(func() bool { return m.choice != installPromptCustomOpt }),
	)
	return m
}

func (p *installPrompt) Init() tea.Cmd { return p.form.Init() }

func (p *installPrompt) Update(msg tea.Msg) tea.Cmd {
	next, cmd := p.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		p.form = f
	}
	return cmd
}

func (p *installPrompt) View() string { return p.form.View() }

func (p *installPrompt) Done() bool {
	return p.form.State == huh.StateCompleted || p.form.State == huh.StateAborted
}

func (p *installPrompt) Aborted() bool { return p.form.State == huh.StateAborted }

// Result returns the chosen install directory. Empty string means cancel.
func (p *installPrompt) Result() string {
	if p.Aborted() {
		return ""
	}
	if p.choice == installPromptCustomOpt {
		return p.custom
	}
	return p.choice
}
