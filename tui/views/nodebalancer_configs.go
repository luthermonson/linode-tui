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
	Register("nodebalancer_configs", []string{"nbconfigs"}, newNodeBalancerConfigs)
}

func newNodeBalancerConfigs(d Deps) View {
	nbID, _ := d.CtxInt("nodebalancer_id")
	nbLabel := d.CtxString("nodebalancer_label")
	title := "NB Configs"
	if nbLabel != "" {
		title = fmt.Sprintf("NB Configs · %s", nbLabel)
	}
	return newListView(listOpts[linodego.NodeBalancerConfig]{
		Deps:  d,
		Title: title,
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "PORT", Width: 6},
			{Title: "PROTO", Width: 8},
			{Title: "ALGORITHM", Width: 12},
			{Title: "STICKINESS", Width: 12},
			{Title: "CHECK", Width: 10},
			{Title: "PATH", Width: 24},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.NodeBalancerConfig, error) {
			if nbID == 0 {
				return nil, fmt.Errorf("no nodebalancer context — use :nodebalancers then enter")
			}
			return c.Raw().ListNodeBalancerConfigs(ctx, nbID, nil)
		},
		Rower: func(cfg linodego.NodeBalancerConfig) table.Row {
			return table.Row{
				strconv.Itoa(cfg.ID),
				strconv.Itoa(cfg.Port),
				string(cfg.Protocol),
				string(cfg.Algorithm),
				string(cfg.Stickiness),
				string(cfg.Check),
				cfg.CheckPath,
			}
		},
		Matcher: func(cfg linodego.NodeBalancerConfig, needle string) bool {
			return containsAny(needle,
				strconv.Itoa(cfg.Port),
				string(cfg.Protocol),
				string(cfg.Algorithm),
				cfg.CheckPath,
			)
		},
		IDFn: func(cfg linodego.NodeBalancerConfig) string { return strconv.Itoa(cfg.ID) },
		Actions: []Action[linodego.NodeBalancerConfig]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(cfg linodego.NodeBalancerConfig) string {
					return fmt.Sprintf("DELETE config %d (port %d/%s)?", cfg.ID, cfg.Port, cfg.Protocol)
				},
				Run: func(ctx context.Context, c *linode.Client, cfg linodego.NodeBalancerConfig) error {
					return c.Raw().DeleteNodeBalancerConfig(ctx, nbID, cfg.ID)
				},
			},
		},
	})
}
