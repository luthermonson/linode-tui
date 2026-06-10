package linode

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/linode/linodego/v2"
)

// ClearOptions controls a ClearAccount run.
type ClearOptions struct {
	Account string
	Execute bool // false = dry-run
	Exclude []string
	Grep    string
}

// ClearGuard validates an account name before a clear run. Names containing
// "prod" are refused unless force is set.
func ClearGuard(name string, force bool) error {
	if name == "" {
		return fmt.Errorf("account name is required")
	}
	if strings.Contains(strings.ToLower(name), "prod") && !force {
		return fmt.Errorf("refusing to clear %q (name contains 'prod')", name)
	}
	return nil
}

// ClearAccount iterates known resource types (instances, volumes,
// nodebalancers, LKE clusters, firewalls, domains, custom images, my
// stackscripts, VPCs+subnets, placement groups, DBaaS, object storage
// buckets) and deletes them, writing a line per action to out. Dry-run unless
// opts.Execute is set.
func ClearAccount(ctx context.Context, client *Client, opts ClearOptions, out io.Writer) error {
	exclude := map[string]bool{}
	for _, s := range opts.Exclude {
		exclude[strings.TrimSpace(s)] = true
	}

	w := &clearWriter{out: out, dry: !opts.Execute, grep: opts.Grep}
	w.printf("clearing account %q (mode: %s)", opts.Account, modeStr(w.dry))
	if w.grep != "" {
		w.printf(" · grep=%q", w.grep)
	}
	w.printf("\n")

	steps := []clearStep{
		{kind: "instances", run: clearInstances},
		{kind: "volumes", run: clearVolumes},
		{kind: "nodebalancers", run: clearNodeBalancers},
		{kind: "lke", run: clearLKEClusters},
		{kind: "firewalls", run: clearFirewalls},
		{kind: "domains", run: clearDomains},
		{kind: "images", run: clearImages},
		{kind: "stackscripts", run: clearStackScripts},
		{kind: "vpcs", run: clearVPCs},
		{kind: "placementgroups", run: clearPlacementGroups},
		{kind: "databases", run: clearDatabases},
		{kind: "objectstorage", run: clearObjectStorage},
	}
	for _, s := range steps {
		if exclude[s.kind] {
			w.printf("[skip] %s\n", s.kind)
			continue
		}
		w.printf("--- %s ---\n", s.kind)
		s.run(ctx, client, w)
	}

	if w.failures > 0 {
		return fmt.Errorf("%d failures during clear-account", w.failures)
	}
	w.printf("done · mode: %s\n", modeStr(w.dry))
	return nil
}

func modeStr(dry bool) string {
	if dry {
		return "DRY-RUN (no resources deleted)"
	}
	return "EXECUTING"
}

type clearStep struct {
	kind string
	run  func(context.Context, *Client, *clearWriter)
}

type clearWriter struct {
	out      io.Writer
	dry      bool
	grep     string
	failures int
}

// matchGrep reports whether label passes the grep filter (case-insensitive
// substring). Empty grep always matches.
func (w *clearWriter) matchGrep(label string) bool {
	if w.grep == "" {
		return true
	}
	return strings.Contains(strings.ToLower(label), strings.ToLower(w.grep))
}

func (w *clearWriter) printf(format string, args ...any) {
	fmt.Fprintf(w.out, format, args...)
}

func (w *clearWriter) ok(action, name string) {
	w.printf("  %s: %s\n", action, name)
}

func (w *clearWriter) fail(action, name string, err error) {
	w.failures++
	w.printf("  ! %s %s: %v\n", action, name, err)
}

func clearTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func clearInstances(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListInstances(ctx, nil)
	if err != nil {
		w.fail("list", "instances", err)
		return
	}
	for _, it := range items {
		if !w.matchGrep(it.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("instance %s (id %d)", it.Label, it.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteInstance(ictx, it.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("instance %d", it.ID), err)
		}
	}
}

func clearVolumes(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListVolumes(ctx, nil)
	if err != nil {
		w.fail("list", "volumes", err)
		return
	}
	for _, v := range items {
		if !w.matchGrep(v.Label) {
			continue
		}
		if v.LinodeID != nil {
			action := dryOr("detach", w.dry)
			w.ok(action, fmt.Sprintf("volume %s (id %d, attached to %d)", v.Label, v.ID, *v.LinodeID))
			if !w.dry {
				dctx, cancel := clearTimeout(30 * time.Second)
				if err := c.Raw().DetachVolume(dctx, v.ID); err != nil {
					w.fail("detach", fmt.Sprintf("volume %d", v.ID), err)
				}
				cancel()
			}
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("volume %s (id %d)", v.Label, v.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteVolume(ictx, v.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("volume %d", v.ID), err)
		}
	}
}

func clearNodeBalancers(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListNodeBalancers(ctx, nil)
	if err != nil {
		w.fail("list", "nodebalancers", err)
		return
	}
	for _, nb := range items {
		label := ""
		if nb.Label != nil {
			label = *nb.Label
		}
		if !w.matchGrep(label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("nodebalancer %s (id %d)", label, nb.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteNodeBalancer(ictx, nb.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("nodebalancer %d", nb.ID), err)
		}
	}
}

func clearLKEClusters(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListLKEClusters(ctx, nil)
	if err != nil {
		w.fail("list", "lke clusters", err)
		return
	}
	for _, l := range items {
		if !w.matchGrep(l.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("lke %s (id %d)", l.Label, l.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(60 * time.Second)
		err := c.Raw().DeleteLKECluster(ictx, l.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("lke %d", l.ID), err)
		}
	}
}

func clearFirewalls(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListFirewalls(ctx, nil)
	if err != nil {
		w.fail("list", "firewalls", err)
		return
	}
	for _, f := range items {
		if !w.matchGrep(f.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("firewall %s (id %d)", f.Label, f.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteFirewall(ictx, f.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("firewall %d", f.ID), err)
		}
	}
}

func clearDomains(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListDomains(ctx, nil)
	if err != nil {
		w.fail("list", "domains", err)
		return
	}
	for _, d := range items {
		if !w.matchGrep(d.Domain) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("domain %s (id %d)", d.Domain, d.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteDomain(ictx, d.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("domain %d", d.ID), err)
		}
	}
}

func clearImages(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListImages(ctx, nil)
	if err != nil {
		w.fail("list", "images", err)
		return
	}
	for _, img := range items {
		if img.IsPublic {
			continue // can't delete public images
		}
		if !w.matchGrep(img.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("image %s (%s)", img.ID, img.Label))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteImage(ictx, img.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("image %s", img.ID), err)
		}
	}
}

func clearStackScripts(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListStackscripts(ctx, nil)
	if err != nil {
		w.fail("list", "stackscripts", err)
		return
	}
	for _, s := range items {
		if !s.Mine {
			continue
		}
		if !w.matchGrep(s.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("stackscript %s (id %d)", s.Label, s.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteStackscript(ictx, s.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("stackscript %d", s.ID), err)
		}
	}
}

func clearVPCs(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListVPCs(ctx, nil)
	if err != nil {
		w.fail("list", "vpcs", err)
		return
	}
	for _, v := range items {
		if !w.matchGrep(v.Label) {
			continue
		}
		for _, sub := range v.Subnets {
			action := dryOr("delete", w.dry)
			w.ok(action, fmt.Sprintf("vpc-subnet %d (vpc %d)", sub.ID, v.ID))
			if w.dry {
				continue
			}
			ictx, cancel := clearTimeout(30 * time.Second)
			err := c.Raw().DeleteVPCSubnet(ictx, v.ID, sub.ID)
			cancel()
			if err != nil {
				w.fail("delete", fmt.Sprintf("vpc-subnet %d", sub.ID), err)
			}
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("vpc %s (id %d)", v.Label, v.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteVPC(ictx, v.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("vpc %d", v.ID), err)
		}
	}
}

func clearPlacementGroups(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListPlacementGroups(ctx, nil)
	if err != nil {
		w.fail("list", "placement groups", err)
		return
	}
	for _, pg := range items {
		if !w.matchGrep(pg.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("placement-group %s (id %d)", pg.Label, pg.ID))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeletePlacementGroup(ictx, pg.ID)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("placement-group %d", pg.ID), err)
		}
	}
}

func clearDatabases(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListDatabases(ctx, nil)
	if err != nil {
		w.fail("list", "databases", err)
		return
	}
	for _, db := range items {
		if !w.matchGrep(db.Label) {
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("database %s (id %d, %s)", db.Label, db.ID, db.Engine))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(60 * time.Second)
		err := deleteDatabaseByEngine(ictx, c, db)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("database %d", db.ID), err)
		}
	}
}

func deleteDatabaseByEngine(ctx context.Context, c *Client, db linodego.Database) error {
	switch {
	case strings.HasPrefix(db.Engine, "mysql"):
		return c.Raw().DeleteMySQLDatabase(ctx, db.ID)
	case strings.HasPrefix(db.Engine, "postgres"):
		return c.Raw().DeletePostgresDatabase(ctx, db.ID)
	default:
		return fmt.Errorf("unknown engine: %s", db.Engine)
	}
}

func clearObjectStorage(ctx context.Context, c *Client, w *clearWriter) {
	items, err := c.Raw().ListObjectStorageBuckets(ctx, nil)
	if err != nil {
		w.fail("list", "object storage", err)
		return
	}
	for _, b := range items {
		if !w.matchGrep(b.Label) {
			continue
		}
		if b.Objects > 0 {
			w.printf("  [skip] bucket %s (%d objects — empty manually first)\n", b.Label, b.Objects)
			continue
		}
		action := dryOr("delete", w.dry)
		w.ok(action, fmt.Sprintf("bucket %s (%s)", b.Label, b.Region))
		if w.dry {
			continue
		}
		ictx, cancel := clearTimeout(30 * time.Second)
		err := c.Raw().DeleteObjectStorageBucket(ictx, b.Region, b.Label)
		cancel()
		if err != nil {
			w.fail("delete", fmt.Sprintf("bucket %s", b.Label), err)
		}
	}
}

func dryOr(action string, dry bool) string {
	if dry {
		return "would " + action
	}
	return action
}
