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
