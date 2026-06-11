package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("fanout_firewalls", []string{"fanout-fw", "fan-firewalls", "fan-fw", "all-firewalls"}, newFanoutFirewalls)
}

type FanoutFirewall struct {
	Account  string            `json:"account"`
	Firewall linodego.Firewall `json:"firewall"`
}

func newFanoutFirewalls(d Deps) View {
	return newListView(listOpts[FanoutFirewall]{
		Deps:  d,
		Title: "Firewalls (all accounts)",
		Columns: []table.Column{
			{Title: "ACCOUNT", Width: 12},
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 30},
			{Title: "STATUS", Width: 12},
			{Title: "ENTITIES", Width: 10},
			{Title: "TAGS", Width: 24},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]FanoutFirewall, error) {
			return fanoutFirewalls(ctx, d)
		},
		Rower: func(ff FanoutFirewall) table.Row {
			f := ff.Firewall
			return table.Row{
				ff.Account,
				strconv.Itoa(f.ID), f.Label, string(f.Status),
				strconv.Itoa(len(f.Entities)),
				strings.Join(f.Tags, ","),
			}
		},
		Matcher: func(ff FanoutFirewall, needle string) bool {
			f := ff.Firewall
			return containsAny(needle, ff.Account, f.Label, string(f.Status)) || tagMatch(f.Tags, needle)
		},
		IDFn:         func(ff FanoutFirewall) string { return ff.Account + ":" + strconv.Itoa(ff.Firewall.ID) },
		BookmarkKind: "fanout_firewalls",
	})
}

func fanoutFirewalls(ctx context.Context, d Deps) ([]FanoutFirewall, error) {
	names, err := fanoutAccountNames(d)
	if err != nil {
		return nil, err
	}
	var (
		mu   sync.Mutex
		out  []FanoutFirewall
		errs []string
		wg   sync.WaitGroup
	)
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			c, err := fanoutClient(ctx, d, name)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			items, err := c.Raw().ListFirewalls(ctx, nil)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				out = append(out, FanoutFirewall{Account: name, Firewall: it})
			}
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return joinFanout(out, errs)
}
