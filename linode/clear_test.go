package linode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
)

func TestClearGuard(t *testing.T) {
	if err := linode.ClearGuard("", false); err == nil {
		t.Fatal("expected error for empty name")
	}
	if err := linode.ClearGuard("prod-main", false); err == nil {
		t.Fatal("expected error for prod-* without force")
	}
	if err := linode.ClearGuard("PROD", false); err == nil {
		t.Fatal("expected error for case-insensitive prod")
	}
	if err := linode.ClearGuard("prod-main", true); err != nil {
		t.Fatalf("force should allow prod: %v", err)
	}
	if err := linode.ClearGuard("dev", false); err != nil {
		t.Fatalf("dev should be allowed: %v", err)
	}
}

func newTestClient(t *testing.T, h http.Handler) *linode.Client {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	c, err := linode.NewClient("test-token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	c.SetBaseURL(ts.URL)
	return c
}

func paged(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data, "page": 1, "pages": 1, "results": 1,
	})
}

func empty(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func clearAccountMux(t *testing.T, deletes *atomic.Int32) *http.ServeMux {
	t.Helper()
	m := http.NewServeMux()
	// All list endpoints return a single fake item.
	m.HandleFunc("/v4/linode/instances", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Instance{{ID: 1, Label: "demo"}})
	})
	m.HandleFunc("/v4/volumes", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Volume{{ID: 2, Label: "v"}})
	})
	m.HandleFunc("/v4/nodebalancers", func(w http.ResponseWriter, r *http.Request) {
		l := "nb"
		paged(w, []linodego.NodeBalancer{{ID: 3, Label: &l}})
	})
	m.HandleFunc("/v4/lke/clusters", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.LKECluster{{ID: 4, Label: "k"}})
	})
	m.HandleFunc("/v4/networking/firewalls", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Firewall{{ID: 5, Label: "f"}})
	})
	m.HandleFunc("/v4/domains", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Domain{{ID: 6, Domain: "ex.test"}})
	})
	m.HandleFunc("/v4/images", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Image{
			{ID: "private/123", Label: "mine", IsPublic: false},
			{ID: "linode/ubuntu22.04", Label: "pub", IsPublic: true},
		})
	})
	m.HandleFunc("/v4/linode/stackscripts", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Stackscript{
			{ID: 7, Label: "mine", Mine: true},
			{ID: 8, Label: "theirs", Mine: false},
		})
	})
	m.HandleFunc("/v4/vpcs", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.VPC{{ID: 9, Label: "v", Subnets: []linodego.VPCSubnet{{ID: 99}}}})
	})
	m.HandleFunc("/v4/placement/groups", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.PlacementGroup{{ID: 10, Label: "pg"}})
	})
	m.HandleFunc("/v4/databases/instances", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.Database{{ID: 11, Label: "db", Engine: "mysql/8"}})
	})
	m.HandleFunc("/v4/object-storage/buckets", func(w http.ResponseWriter, r *http.Request) {
		paged(w, []linodego.ObjectStorageBucket{
			{Label: "empty", Region: "us-east", Objects: 0},
			{Label: "full", Region: "us-east", Objects: 5},
		})
	})

	// Catch-all DELETE counter (and POST detach).
	m.HandleFunc("/v4/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete, http.MethodPost:
			deletes.Add(1)
			empty(w)
		default:
			http.NotFound(w, r)
		}
	})
	return m
}

func TestClearAccountDryRunNoMutations(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, clearAccountMux(t, &deletes))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: false,
	}, &buf)
	if err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	if n := deletes.Load(); n != 0 {
		t.Fatalf("expected 0 mutations in dry-run, got %d", n)
	}
	out := buf.String()
	for _, want := range []string{"DRY-RUN", "would delete: instance", "would delete: volume", "would delete: nodebalancer", "[skip] bucket full"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q\n---\n%s", want, out)
		}
	}
	// Public images and not-mine stackscripts must be skipped.
	if strings.Contains(out, "would delete: image linode/ubuntu22.04") {
		t.Error("public image must not be queued for delete")
	}
	if strings.Contains(out, "would delete: stackscript theirs") {
		t.Error("not-mine stackscript must not be queued for delete")
	}
}

func TestClearAccountExecuteMutates(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, clearAccountMux(t, &deletes))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: true,
	}, &buf)
	if err != nil {
		t.Fatalf("execute should not error: %v", err)
	}
	if deletes.Load() == 0 {
		t.Fatal("expected at least one mutation")
	}
	out := buf.String()
	if !strings.Contains(out, "EXECUTING") {
		t.Errorf("expected EXECUTING in output, got:\n%s", out)
	}
}

func TestClearAccountExcludeSkipsStep(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, clearAccountMux(t, &deletes))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: false, Exclude: []string{"instances", "volumes"},
	}, &buf)
	if err != nil {
		t.Fatalf("dry-run with excludes should not error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[skip] instances") {
		t.Errorf("expected [skip] instances:\n%s", out)
	}
	if !strings.Contains(out, "[skip] volumes") {
		t.Errorf("expected [skip] volumes:\n%s", out)
	}
	if strings.Contains(out, "--- instances ---") {
		t.Error("excluded step should not run")
	}
}
