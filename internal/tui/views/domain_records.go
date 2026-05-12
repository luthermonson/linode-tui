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
	Register("domain_records", []string{"records", "dnsrr"}, newDomainRecords)
}

func newDomainRecords(d Deps) View {
	domainID, _ := d.CtxInt("domain_id")
	domainName := d.CtxString("domain_name")
	title := "DNS Records"
	if domainName != "" {
		title = fmt.Sprintf("DNS Records · %s", domainName)
	}
	return newListView(listOpts[linodego.DomainRecord]{
		Deps:  d,
		Title: title,
		Columns: []table.Column{
			{Title: "ID", Width: 12},
			{Title: "TYPE", Width: 8},
			{Title: "NAME", Width: 24},
			{Title: "TARGET", Width: 36},
			{Title: "TTL", Width: 8},
			{Title: "PRI", Width: 6},
			{Title: "WEIGHT", Width: 8},
			{Title: "PORT", Width: 6},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.DomainRecord, error) {
			if domainID == 0 {
				return nil, fmt.Errorf("no domain context — use :domains then enter")
			}
			return c.Raw().ListDomainRecords(ctx, domainID, nil)
		},
		Rower: func(r linodego.DomainRecord) table.Row {
			return table.Row{
				strconv.Itoa(r.ID),
				string(r.Type),
				r.Name,
				r.Target,
				strconv.Itoa(r.TTLSec),
				strconv.Itoa(r.Priority),
				strconv.Itoa(r.Weight),
				strconv.Itoa(r.Port),
			}
		},
		Matcher: func(r linodego.DomainRecord, needle string) bool {
			return containsAny(needle, string(r.Type), r.Name, r.Target)
		},
		IDFn: func(r linodego.DomainRecord) string { return strconv.Itoa(r.ID) },
		Actions: []Action[linodego.DomainRecord]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(r linodego.DomainRecord) string {
					return fmt.Sprintf("DELETE record %s %s → %s?", r.Type, r.Name, r.Target)
				},
				Run: func(ctx context.Context, c *linode.Client, r linodego.DomainRecord) error {
					return c.Raw().DeleteDomainRecord(ctx, domainID, r.ID)
				},
			},
		},
	})
}
