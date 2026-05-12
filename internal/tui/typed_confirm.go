package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// typedConfirmModal asks the user to type a specific string before confirming.
// Used by bulk-delete with many selections so a stray space + enter can't wipe
// a list of resources.
type typedConfirmModal struct {
	form    *huh.Form
	match   string
	typed   string
	onYes   tea.Cmd
	done    bool
	confirm bool
}

func newTypedConfirmModal(prompt, match string, onYes tea.Cmd) *typedConfirmModal {
	m := &typedConfirmModal{match: match, onYes: onYes}
	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(prompt).
				Description(fmt.Sprintf("Type %q to confirm.", match)).
				Validate(func(s string) error {
					if s != match {
						return fmt.Errorf("doesn't match %q", match)
					}
					return nil
				}).
				Value(&m.typed),
		),
	)
	return m
}

func (c *typedConfirmModal) Init() tea.Cmd { return c.form.Init() }

func (c *typedConfirmModal) Update(msg tea.Msg) tea.Cmd {
	next, cmd := c.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		c.form = f
	}
	switch c.form.State {
	case huh.StateCompleted:
		c.done = true
		c.confirm = c.typed == c.match
	case huh.StateAborted:
		c.done = true
	}
	return cmd
}

func (c *typedConfirmModal) View() string { return c.form.View() }

func (c *typedConfirmModal) Done() bool      { return c.done }
func (c *typedConfirmModal) Confirmed() bool { return c.confirm }
