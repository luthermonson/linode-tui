package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
)

func init() {
	Register("fanout_domains", []string{"fanout-dns", "fan-domains", "fan-dns", "all-domains"}, newFanoutDomains)
}

type FanoutDomain struct {
	Account string          `json:"account"`
	Domain  linodego.Domain `json:"domain"`
}

func newFanoutDomains(d Deps) View {
	return newListView(listOpts[FanoutDomain]{
		Deps:  d,
		Title: "Domains (all accounts)",
		Columns: []table.Column{
			{Title: "ACCOUNT", Width: 12},
			{Title: "ID", Width: 10},
			{Title: "DOMAIN", Width: 32},
			{Title: "TYPE", Width: 10},
			{Title: "STATUS", Width: 12},
			{Title: "SOA EMAIL", Width: 28},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]FanoutDomain, error) {
			return fanoutDomains(ctx, d)
		},
		Rower: func(fd FanoutDomain) table.Row {
			d := fd.Domain
			return table.Row{
				fd.Account, strconv.Itoa(d.ID), d.Domain, string(d.Type), string(d.Status), d.SOAEmail,
			}
		},
		Matcher: func(fd FanoutDomain, needle string) bool {
			d := fd.Domain
			return containsAny(needle, fd.Account, d.Domain, string(d.Type), string(d.Status), d.SOAEmail) ||
				tagMatch(d.Tags, needle)
		},
		IDFn:         func(fd FanoutDomain) string { return fd.Account + ":" + strconv.Itoa(fd.Domain.ID) },
		BookmarkKind: "fanout_domains",
	})
}

func fanoutDomains(ctx context.Context, d Deps) ([]FanoutDomain, error) {
	names, err := fanoutAccountNames(d)
	if err != nil {
		return nil, err
	}
	var (
		mu   sync.Mutex
		out  []FanoutDomain
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
			items, err := c.Raw().ListDomains(ctx, nil)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				out = append(out, FanoutDomain{Account: name, Domain: it})
			}
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return joinFanout(out, errs)
}

var _ = strings.Join
