package views

import (
	"context"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego"

	"github.com/linode/tui/internal/linode"
)

func init() {
	Register("stackscripts", []string{"ss", "scripts", "stack"}, newStackScripts)
}

func newStackScripts(d Deps) View {
	return newListView(listOpts[linodego.Stackscript]{
		Deps:  d,
		Title: "StackScripts",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "USERNAME", Width: 18},
			{Title: "LABEL", Width: 30},
			{Title: "PUBLIC", Width: 8},
			{Title: "MINE", Width: 6},
			{Title: "DEPLOY", Width: 8},
			{Title: "REV NOTE", Width: 32},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Stackscript, error) {
			return c.Raw().ListStackscripts(ctx, nil)
		},
		Rower: func(s linodego.Stackscript) table.Row {
			return table.Row{
				strconv.Itoa(s.ID),
				s.Username,
				s.Label,
				yesNo(s.IsPublic),
				yesNo(s.Mine),
				strconv.Itoa(s.DeploymentsActive),
				truncate(s.RevNote, 32),
			}
		},
		Matcher: func(s linodego.Stackscript, needle string) bool {
			return containsAny(needle, s.Label, s.Username, s.Description, s.RevNote)
		},
		IDFn: func(s linodego.Stackscript) string { return strconv.Itoa(s.ID) },
		Actions: []Action[linodego.Stackscript]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(s linodego.Stackscript) string {
					if !s.Mine {
						return fmt.Sprintf("Cannot delete stackscript %s — not yours.", s.Label)
					}
					return fmt.Sprintf("DELETE stackscript %s (id %d)?", s.Label, s.ID)
				},
				Run: func(ctx context.Context, c *linode.Client, s linodego.Stackscript) error {
					if !s.Mine {
						return fmt.Errorf("not yours")
					}
					return c.Raw().DeleteStackscript(ctx, s.ID)
				},
			},
		},
	})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	return s[:max-1] + "…"
}
