package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

func TestTypedConfirmMatchFiresOnYes(t *testing.T) {
	ran := false
	onYes := tea.Cmd(func() tea.Msg { ran = true; return nil })

	c := newTypedConfirmModal("Delete N?", "5", onYes)
	c.typed = "5"
	c.form.State = huh.StateCompleted
	c.Update(nil)

	if !c.Done() {
		t.Fatal("expected Done")
	}
	if !c.Confirmed() {
		t.Fatal("expected Confirmed for matching input")
	}
	if c.onYes == nil {
		t.Fatal("onYes should still be set after match")
	}
	_ = c.onYes()
	if !ran {
		t.Fatal("onYes not invoked")
	}
}

func TestTypedConfirmMismatchDoesNotConfirm(t *testing.T) {
	c := newTypedConfirmModal("Delete N?", "5", nil)
	c.typed = "wrong"
	c.form.State = huh.StateCompleted
	c.Update(nil)

	if !c.Done() {
		t.Fatal("expected Done")
	}
	if c.Confirmed() {
		t.Fatal("must not confirm a mismatch")
	}
}

func TestTypedConfirmAbortDoesNotConfirm(t *testing.T) {
	c := newTypedConfirmModal("X?", "5", nil)
	c.form.State = huh.StateAborted
	c.Update(nil)

	if !c.Done() {
		t.Fatal("aborted form should be Done")
	}
	if c.Confirmed() {
		t.Fatal("aborted form must not confirm")
	}
}

func TestTypedConfirmStaysOpenWhileNormal(t *testing.T) {
	c := newTypedConfirmModal("X?", "5", nil)
	// State left at StateNormal — Update with any msg shouldn't flip done.
	c.Update(tea.KeyMsg{})
	if c.Done() {
		t.Fatal("should still be open while form is StateNormal")
	}
}
