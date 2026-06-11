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
	Register("fanout_instances", []string{"fanout-linodes", "fan-instances", "fan-li", "all-linodes"}, newFanoutInstances)
}

// FanoutInstance ties an instance back to the account it was fetched from so
// the merged view can sort and label correctly.
type FanoutInstance struct {
	Account  string            `json:"account"`
	Instance linodego.Instance `json:"instance"`
}

func newFanoutInstances(d Deps) View {
	return newListView(listOpts[FanoutInstance]{
		Deps:  d,
		Title: "Linodes (all accounts)",
		Columns: []table.Column{
			{Title: "ACCOUNT", Width: 12},
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 28},
			{Title: "REGION", Width: 14},
			{Title: "TYPE", Width: 18},
			{Title: "STATUS", Width: 12},
			{Title: "IPv4", Width: 16},
			{Title: "TAGS", Width: 20},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]FanoutInstance, error) {
			return fanoutInstances(ctx, d)
		},
		Rower: func(fi FanoutInstance) table.Row {
			it := fi.Instance
			ip := ""
			if len(it.IPv4) > 0 && it.IPv4[0] != nil {
				ip = it.IPv4[0].String()
			}
			return table.Row{
				fi.Account,
				strconv.Itoa(it.ID),
				it.Label,
				it.Region,
				it.Type,
				string(it.Status),
				ip,
				strings.Join(it.Tags, ","),
			}
		},
		Matcher: func(fi FanoutInstance, needle string) bool {
			it := fi.Instance
			return containsAny(needle, fi.Account, it.Label, it.Region, it.Type, string(it.Status)) ||
				tagMatch(it.Tags, needle)
		},
		IDFn:         func(fi FanoutInstance) string { return fi.Account + ":" + strconv.Itoa(fi.Instance.ID) },
		BookmarkKind: "fanout_instances",
	})
}

// fanoutInstances queries every configured account in parallel and merges
// results. Individual account failures are non-fatal; their errors are joined
// into the returned error so the caller's listView can surface them.
func fanoutInstances(ctx context.Context, d Deps) ([]FanoutInstance, error) {
	names, err := fanoutAccountNames(d)
	if err != nil {
		return nil, err
	}
	var (
		mu   sync.Mutex
		out  []FanoutInstance
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
			items, err := c.ListInstances(ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				out = append(out, FanoutInstance{Account: name, Instance: it})
			}
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return joinFanout(out, errs)
}
