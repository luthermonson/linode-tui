package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego"

	"github.com/linode/tui/internal/linode"
)

func init() {
	Register("fanout_lke", []string{"fanout-lke", "fan-lke", "fan-k8s", "all-lke"}, newFanoutLKE)
}

type FanoutLKE struct {
	Account string              `json:"account"`
	Cluster linodego.LKECluster `json:"cluster"`
}

func newFanoutLKE(d Deps) View {
	return newListView(listOpts[FanoutLKE]{
		Deps:  d,
		Title: "LKE Clusters (all accounts)",
		Columns: []table.Column{
			{Title: "ACCOUNT", Width: 12},
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 28},
			{Title: "REGION", Width: 14},
			{Title: "STATUS", Width: 12},
			{Title: "K8S", Width: 10},
			{Title: "TIER", Width: 10},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]FanoutLKE, error) {
			return fanoutLKEClusters(ctx, d)
		},
		Rower: func(fl FanoutLKE) table.Row {
			l := fl.Cluster
			return table.Row{
				fl.Account,
				strconv.Itoa(l.ID),
				l.Label, l.Region, string(l.Status), l.K8sVersion, l.Tier,
			}
		},
		Matcher: func(fl FanoutLKE, needle string) bool {
			l := fl.Cluster
			return containsAny(needle, fl.Account, l.Label, l.Region, string(l.Status), l.K8sVersion, l.Tier) ||
				tagMatch(l.Tags, needle)
		},
		IDFn:         func(fl FanoutLKE) string { return fl.Account + ":" + strconv.Itoa(fl.Cluster.ID) },
		BookmarkKind: "fanout_lke",
	})
}

func fanoutLKEClusters(ctx context.Context, d Deps) ([]FanoutLKE, error) {
	names, err := fanoutAccountNames(d)
	if err != nil {
		return nil, err
	}
	var (
		mu   sync.Mutex
		out  []FanoutLKE
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
			items, err := c.Raw().ListLKEClusters(ctx, nil)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				out = append(out, FanoutLKE{Account: name, Cluster: it})
			}
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return joinFanout(out, errs)
}

var _ = strings.Join
