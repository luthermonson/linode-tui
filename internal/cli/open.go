package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/linode/linodego"
	"github.com/urfave/cli/v3"

	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/linode"
	"github.com/linode/tui/internal/tui"
)

func openCommand() *cli.Command {
	return &cli.Command{
		Name:      "open",
		Usage:     "Launch the TUI directly into a specific resource view",
		ArgsUsage: "<resource> [id]   (e.g. linodes 12345, volumes 42, fanout-instances dev:12345)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to config file (default ~/.config/linode-tui/config.yaml)",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print resource(s) as JSON to stdout and exit; skips the TUI",
			},
			&cli.BoolFlag{
				Name:  "csv",
				Usage: "print resource(s) as CSV to stdout and exit; skips the TUI",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.NArg() < 1 {
				return fmt.Errorf("usage: linode-tui open <resource> [id]")
			}
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			cfg.ApplyOverrides(config.Overrides{
				Token:   c.String("token"),
				Account: c.String("account"),
			})
			token, err := linode.ResolveToken(ctx, cfg)
			if err != nil {
				return err
			}
			client := linode.NewClient(token)
			resource := c.Args().First()
			id := c.Args().Get(1)
			if c.Bool("json") {
				return jsonDump(ctx, client, resource, id, os.Stdout)
			}
			if c.Bool("csv") {
				return csvDump(ctx, client, resource, id, os.Stdout)
			}
			var initialCtx map[string]any
			if id != "" {
				initialCtx = map[string]any{"focus_id": id}
			}
			return tui.RunWithViewAndContext(ctx, cfg, client, resource, initialCtx)
		},
	}
}

// jsonDump fetches the requested resource type from the API and pretty-prints
// it. When id is non-empty, narrows to the single matching row. Resource names
// match the TUI's command-bar aliases.
func jsonDump(ctx context.Context, c *linode.Client, resource, id string, out io.Writer) error {
	switch resource {
	case "linodes", "instances", "inst", "li":
		items, err := c.Raw().ListInstances(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(it linodego.Instance) string { return strconv.Itoa(it.ID) })
	case "volumes", "vol", "vols":
		items, err := c.Raw().ListVolumes(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(v linodego.Volume) string { return strconv.Itoa(v.ID) })
	case "nodebalancers", "nb":
		items, err := c.Raw().ListNodeBalancers(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(nb linodego.NodeBalancer) string { return strconv.Itoa(nb.ID) })
	case "lke", "kubernetes", "k8s", "clusters":
		items, err := c.Raw().ListLKEClusters(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(l linodego.LKECluster) string { return strconv.Itoa(l.ID) })
	case "firewalls", "fw":
		items, err := c.Raw().ListFirewalls(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(f linodego.Firewall) string { return strconv.Itoa(f.ID) })
	case "domains", "dom", "dns":
		items, err := c.Raw().ListDomains(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(d linodego.Domain) string { return strconv.Itoa(d.ID) })
	case "vpcs", "vpc":
		items, err := c.Raw().ListVPCs(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(v linodego.VPC) string { return strconv.Itoa(v.ID) })
	case "images", "img":
		items, err := c.Raw().ListImages(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(i linodego.Image) string { return i.ID })
	case "databases", "db", "dbs", "dbaas":
		items, err := c.Raw().ListDatabases(ctx, nil)
		if err != nil {
			return err
		}
		return printJSONFiltered(out, items, id, func(d linodego.Database) string { return strconv.Itoa(d.ID) })
	default:
		return fmt.Errorf("--json not supported for resource %q (supported: instances, volumes, nodebalancers, lke, firewalls, domains, vpcs, images, databases)", resource)
	}
}

func printJSONFiltered[T any](w io.Writer, items []T, id string, idFn func(T) string) error {
	if id != "" {
		for _, it := range items {
			if idFn(it) == id {
				return printJSON(w, it)
			}
		}
		return fmt.Errorf("id %q not found", id)
	}
	return printJSON(w, items)
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// csvDump emits a CSV with a stable column set per resource. Same alias set as
// jsonDump.
func csvDump(ctx context.Context, c *linode.Client, resource, id string, out io.Writer) error {
	switch resource {
	case "linodes", "instances", "inst", "li":
		items, err := c.Raw().ListInstances(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "region", "type", "status", "ipv4", "tags"}, filterItems(items, id, func(it linodego.Instance) string { return strconv.Itoa(it.ID) }), func(it linodego.Instance) []string {
			ip := ""
			if len(it.IPv4) > 0 && it.IPv4[0] != nil {
				ip = it.IPv4[0].String()
			}
			return []string{
				strconv.Itoa(it.ID), it.Label, it.Region, it.Type,
				string(it.Status), ip, strings.Join(it.Tags, ","),
			}
		})
	case "volumes", "vol", "vols":
		items, err := c.Raw().ListVolumes(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "region", "status", "size_gb", "attached_to", "tags"}, filterItems(items, id, func(v linodego.Volume) string { return strconv.Itoa(v.ID) }), func(v linodego.Volume) []string {
			attached := ""
			if v.LinodeID != nil {
				attached = strconv.Itoa(*v.LinodeID)
			}
			return []string{
				strconv.Itoa(v.ID), v.Label, v.Region, string(v.Status),
				strconv.Itoa(v.Size), attached, strings.Join(v.Tags, ","),
			}
		})
	case "nodebalancers", "nb":
		items, err := c.Raw().ListNodeBalancers(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "region", "hostname", "ipv4", "tags"}, filterItems(items, id, func(nb linodego.NodeBalancer) string { return strconv.Itoa(nb.ID) }), func(nb linodego.NodeBalancer) []string {
			label, host, ipv4 := "", "", ""
			if nb.Label != nil {
				label = *nb.Label
			}
			if nb.Hostname != nil {
				host = *nb.Hostname
			}
			if nb.IPv4 != nil {
				ipv4 = *nb.IPv4
			}
			return []string{strconv.Itoa(nb.ID), label, nb.Region, host, ipv4, strings.Join(nb.Tags, ",")}
		})
	case "lke", "kubernetes", "k8s", "clusters":
		items, err := c.Raw().ListLKEClusters(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "region", "k8s_version", "status", "tier", "tags"}, filterItems(items, id, func(l linodego.LKECluster) string { return strconv.Itoa(l.ID) }), func(l linodego.LKECluster) []string {
			return []string{
				strconv.Itoa(l.ID), l.Label, l.Region, l.K8sVersion,
				string(l.Status), l.Tier, strings.Join(l.Tags, ","),
			}
		})
	case "firewalls", "fw":
		items, err := c.Raw().ListFirewalls(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "status", "tags"}, filterItems(items, id, func(f linodego.Firewall) string { return strconv.Itoa(f.ID) }), func(f linodego.Firewall) []string {
			return []string{strconv.Itoa(f.ID), f.Label, string(f.Status), strings.Join(f.Tags, ",")}
		})
	case "domains", "dom", "dns":
		items, err := c.Raw().ListDomains(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "domain", "type", "status", "soa_email", "tags"}, filterItems(items, id, func(d linodego.Domain) string { return strconv.Itoa(d.ID) }), func(d linodego.Domain) []string {
			return []string{strconv.Itoa(d.ID), d.Domain, string(d.Type), string(d.Status), d.SOAEmail, strings.Join(d.Tags, ",")}
		})
	case "vpcs", "vpc":
		items, err := c.Raw().ListVPCs(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "region", "subnets", "description"}, filterItems(items, id, func(v linodego.VPC) string { return strconv.Itoa(v.ID) }), func(v linodego.VPC) []string {
			return []string{strconv.Itoa(v.ID), v.Label, v.Region, strconv.Itoa(len(v.Subnets)), v.Description}
		})
	case "images", "img":
		items, err := c.Raw().ListImages(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "type", "status", "vendor", "size_mb", "is_public"}, filterItems(items, id, func(i linodego.Image) string { return i.ID }), func(i linodego.Image) []string {
			pub := "false"
			if i.IsPublic {
				pub = "true"
			}
			return []string{i.ID, i.Label, i.Type, string(i.Status), i.Vendor, strconv.Itoa(i.Size), pub}
		})
	case "databases", "db", "dbs", "dbaas":
		items, err := c.Raw().ListDatabases(ctx, nil)
		if err != nil {
			return err
		}
		return writeCSV(out, []string{"id", "label", "engine", "version", "region", "status", "port", "host"}, filterItems(items, id, func(d linodego.Database) string { return strconv.Itoa(d.ID) }), func(d linodego.Database) []string {
			return []string{strconv.Itoa(d.ID), d.Label, d.Engine, d.Version, d.Region, string(d.Status), strconv.Itoa(d.Port), d.Hosts.Primary}
		})
	default:
		return fmt.Errorf("--csv not supported for resource %q", resource)
	}
}

func filterItems[T any](items []T, id string, idFn func(T) string) []T {
	if id == "" {
		return items
	}
	for _, it := range items {
		if idFn(it) == id {
			return []T{it}
		}
	}
	return nil
}

func writeCSV[T any](w io.Writer, header []string, items []T, row func(T) []string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, it := range items {
		if err := cw.Write(row(it)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
