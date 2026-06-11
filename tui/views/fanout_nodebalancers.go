package views

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("fanout_nodebalancers", []string{"fanout-nb", "fan-nb", "all-nb", "all-nodebalancers"}, newFanoutNodeBalancers)
}

type FanoutNodeBalancer struct {
	Account      string                `json:"account"`
	NodeBalancer linodego.NodeBalancer `json:"nodebalancer"`
}

func newFanoutNodeBalancers(d Deps) View {
	return newListView(listOpts[FanoutNodeBalancer]{
		Deps:  d,
		Title: "NodeBalancers (all accounts)",
		Columns: []table.Column{
			{Title: "ACCOUNT", Width: 12},
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 26},
			{Title: "REGION", Width: 14},
			{Title: "HOSTNAME", Width: 40},
			{Title: "IPv4", Width: 16},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]FanoutNodeBalancer, error) {
			return fanoutNodeBalancers(ctx, d)
		},
		Rower: func(fn FanoutNodeBalancer) table.Row {
			nb := fn.NodeBalancer
			return table.Row{
				fn.Account,
				strconv.Itoa(nb.ID),
				deref(nb.Label),
				nb.Region,
				deref(nb.Hostname),
				deref(nb.IPv4),
			}
		},
		Matcher: func(fn FanoutNodeBalancer, needle string) bool {
			nb := fn.NodeBalancer
			return containsAny(needle, fn.Account, deref(nb.Label), nb.Region, deref(nb.Hostname), deref(nb.IPv4)) ||
				tagMatch(nb.Tags, needle)
		},
		IDFn:         func(fn FanoutNodeBalancer) string { return fn.Account + ":" + strconv.Itoa(fn.NodeBalancer.ID) },
		BookmarkKind: "fanout_nodebalancers",
	})
}

func fanoutNodeBalancers(ctx context.Context, d Deps) ([]FanoutNodeBalancer, error) {
	names, err := fanoutAccountNames(d)
	if err != nil {
		return nil, err
	}
	var (
		mu   sync.Mutex
		out  []FanoutNodeBalancer
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
			items, err := c.Raw().ListNodeBalancers(ctx, nil)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				out = append(out, FanoutNodeBalancer{Account: name, NodeBalancer: it})
			}
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return joinFanout(out, errs)
}
