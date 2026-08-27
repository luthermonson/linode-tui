package views

import (
	"context"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("stackscripts", []string{"ss", "scripts", "stack"}, newStackScripts)
}

func newStackScripts(d Deps) View {
	return newListView(listOpts[linodego.Stackscript]{
		Deps:  d,
		Title: "StackScripts",
		Columns: []Col{
			{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
			{Title: "USERNAME", Width: 18, MinWidth: 8, Priority: PriMed},
			{Title: "LABEL", Width: 30, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "PUBLIC", Width: 8, MinWidth: 6, Priority: PriLow},
			{Title: "MINE", Width: 6, MinWidth: 4, Priority: PriHigh},
			{Title: "DEPLOY", Width: 8, MinWidth: 6, Priority: PriLow},
			{Title: "REV NOTE", Width: 32, MinWidth: 12, Priority: PriLowest},
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
