package views

import (
	"slices"
	"testing"
)

// The registry is populated by each view file's init(), so these tests run
// against the real, fully-registered set.

func TestResolvePrefixMatchesAlias(t *testing.T) {
	// `:lin` should resolve to instances via its "linodes" alias — previously
	// only the canonical name was prefix-matched, so `:lin` was unknown.
	for _, q := range []string{"lin", "linode", "linodes", "li", "inst", "instances"} {
		if _, ok := Resolve(q); !ok {
			t.Errorf("Resolve(%q) = not found; want the instances view", q)
		}
	}
}

func TestResolvePrefixSkipsChildViews(t *testing.T) {
	// `:node` is a prefix of both nodebalancers and the nodebalancer_configs
	// drill-in; it must land on the top-level nodebalancers, never the child.
	// tagFactory stamps the resolved view's registered name into its context,
	// so build it with an empty Deps and read view_name back to confirm which
	// view we got.
	f, ok := Resolve("node")
	if !ok {
		t.Fatal("Resolve(\"node\") = not found; want nodebalancers")
	}
	d := Deps{Context: map[string]any{}} // non-nil so tagFactory writes into it
	_ = f(d)
	if got := d.CtxString("view_name"); got != "nodebalancers" {
		t.Errorf("Resolve(\"node\") resolved to %q; want nodebalancers", got)
	}
	// A child still resolves by exact name — that's how a parent's NavigateMsg
	// reaches it.
	if _, ok := Resolve("nodebalancer_configs"); !ok {
		t.Error("Resolve(\"nodebalancer_configs\") should still resolve by exact name")
	}
}

func TestNavCompletionsIncludesAliasesExcludesChildren(t *testing.T) {
	comp := NavCompletions()
	// Aliases of top-level views must be offered.
	for _, want := range []string{"instances", "linodes", "li", "nodebalancers", "nb", "placementgroups", "pg", "objectstorage", "buckets"} {
		if !slices.Contains(comp, want) {
			t.Errorf("NavCompletions() missing %q", want)
		}
	}
	// Context-only child views (and their aliases) must NOT appear.
	for _, bad := range []string{"nodebalancer_configs", "nbconfigs", "domain_records", "records", "instance_detail", "lke_detail"} {
		if slices.Contains(comp, bad) {
			t.Errorf("NavCompletions() unexpectedly contains child view %q", bad)
		}
	}
}

func TestResolveName(t *testing.T) {
	cases := map[string]string{
		"lke":      "lke",
		"k8s":      "lke",       // alias
		"clusters": "lke",       // alias
		"linodes":  "instances", // alias
		"lin":      "instances", // alias prefix
		"nb":       "nodebalancers",
		"node":     "nodebalancers", // prefix, child skipped
		"domains":  "domains",
	}
	for q, want := range cases {
		if got, ok := ResolveName(q); !ok || got != want {
			t.Errorf("ResolveName(%q) = (%q, %v); want (%q, true)", q, got, ok, want)
		}
	}
	if _, ok := ResolveName("definitely-not-a-view"); ok {
		t.Error("ResolveName(unknown) should be not-ok")
	}
}

func TestIDDrill(t *testing.T) {
	want := map[string]IDDrillTarget{
		"instances":            {"instance_detail", "instance_id"},
		"lke":                  {"lke_detail", "cluster_id"},
		"domains":              {"domain_records", "domain_id"},
		"nodebalancers":        {"nodebalancer_configs", "nodebalancer_id"},
		"lke_detail":           {"lke_detail", "cluster_id"},
		"nodebalancer_configs": {"nodebalancer_configs", "nodebalancer_id"},
	}
	for name, exp := range want {
		got, ok := IDDrill(name)
		if !ok || got != exp {
			t.Errorf("IDDrill(%q) = (%+v, %v); want (%+v, true)", name, got, ok, exp)
		}
	}
	// Flat lists with no detail page must not id-drill.
	for _, name := range []string{"volumes", "images", "firewalls", "vpcs"} {
		if _, ok := IDDrill(name); ok {
			t.Errorf("IDDrill(%q) = ok; want not-ok (no detail view)", name)
		}
	}
	// The user-facing path: alias/prefix verb → canonical name → drill target.
	name, ok := ResolveName("k8s")
	if !ok {
		t.Fatal("ResolveName(\"k8s\") failed")
	}
	if d, ok := IDDrill(name); !ok || d.View != "lke_detail" {
		t.Errorf("IDDrill(ResolveName(\"k8s\")) = (%+v, %v); want lke_detail", d, ok)
	}
}

func TestIsChild(t *testing.T) {
	for _, name := range []string{"nodebalancer_configs", "nbconfigs", "domain_records", "records", "dnsrr", "instance_detail", "linode_detail", "lke_detail", "cluster_detail"} {
		if !IsChild(name) {
			t.Errorf("IsChild(%q) = false; want true", name)
		}
	}
	for _, name := range []string{"instances", "linodes", "nodebalancers", "domains", "lke", ""} {
		if IsChild(name) {
			t.Errorf("IsChild(%q) = true; want false", name)
		}
	}
}
