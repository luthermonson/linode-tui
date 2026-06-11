package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("domains", []string{"dom", "dns"}, newDomains)
}

func newDomains(d Deps) View {
	return newListView(listOpts[linodego.Domain]{
		Deps:  d,
		Title: "Domains",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "DOMAIN", Width: 36},
			{Title: "TYPE", Width: 10},
			{Title: "STATUS", Width: 12},
			{Title: "SOA EMAIL", Width: 28},
			{Title: "TAGS", Width: 24},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Domain, error) {
			return c.Raw().ListDomains(ctx, nil)
		},
		Rower: func(dn linodego.Domain) table.Row {
			return table.Row{
				strconv.Itoa(dn.ID),
				dn.Domain,
				string(dn.Type),
				string(dn.Status),
				dn.SOAEmail,
				strings.Join(dn.Tags, ","),
			}
		},
		Matcher: func(dn linodego.Domain, needle string) bool {
			return containsAny(needle, dn.Domain, string(dn.Type), string(dn.Status), dn.SOAEmail) ||
				tagMatch(dn.Tags, needle)
		},
		IDFn:         func(dn linodego.Domain) string { return strconv.Itoa(dn.ID) },
		BookmarkKind: "domains",
		TagsFn:       func(dn linodego.Domain) []string { return dn.Tags },
		OnEnter: func(dn linodego.Domain, _ Deps) tea.Cmd {
			return func() tea.Msg {
				return NavigateMsg{
					Name: "domain_records",
					Context: map[string]any{
						"domain_id":   dn.ID,
						"domain_name": dn.Domain,
					},
				}
			}
		},
		Actions: []Action[linodego.Domain]{
			{
				Key:    "d",
				Label:  "delete",
				Prompt: func(dn linodego.Domain) string { return fmt.Sprintf("DELETE domain %s (id %d)?", dn.Domain, dn.ID) },
				Run: func(ctx context.Context, c *linode.Client, dn linodego.Domain) error {
					return c.Raw().DeleteDomain(ctx, dn.ID)
				},
			},
		},
	})
}
