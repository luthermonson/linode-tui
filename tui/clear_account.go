package tui

import (
	"bytes"
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/luthermonson/linode-tui/linode"
)

type clearAccountDoneMsg struct {
	account string
	dry     bool
	output  string
	err     error
}

// dispatchClearAccount handles `:clear-account [dry-run]`. A dry run shows
// what would be deleted with no confirmation; a real run requires typing the
// profile username (or the account name when the username is unknown).
func (m model) dispatchClearAccount(args []string) (tea.Model, tea.Cmd) {
	if m.readOnly {
		m.status = "read-only: mutation blocked"
		return m, nil
	}
	account := m.cfg.DefaultAccount
	if err := linode.ClearGuard(account, false); err != nil {
		m.status = "clear-account: " + err.Error()
		return m, nil
	}
	if len(args) > 0 {
		if args[0] != "dry-run" {
			m.status = "usage: :clear-account [dry-run]"
			return m, nil
		}
		m.status = fmt.Sprintf("clear-account: dry-run on %q…", account)
		return m, clearAccountCmd(m.client, account, m.username, false)
	}
	// The API-resolved /profile username is the real identity of the token;
	// the config label is only what this machine happens to call it. Confirm
	// against the username whenever it's known, and pass it down so
	// ClearAccount re-checks it against /profile before deleting anything.
	match := m.username
	if match == "" {
		match = account
	}
	m.typedConfirm = newTypedConfirmModal(
		fmt.Sprintf("Delete EVERY resource on account %q (user %q)? This cannot be undone.", account, match),
		match,
		clearAccountCmd(m.client, account, m.username, true),
	)
	return m, m.typedConfirm.Init()
}

func clearAccountCmd(client *linode.Client, account, username string, execute bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		var buf bytes.Buffer
		err := linode.ClearAccount(ctx, client, linode.ClearOptions{
			Account:          account,
			Execute:          execute,
			ExpectedUsername: username,
		}, &buf)
		return clearAccountDoneMsg{account: account, dry: !execute, output: buf.String(), err: err}
	}
}
