// Package livetest holds read-only integration tests that hit the real Linode
// API. Skipped by default; enable with:
//
//	LINODE_TUI_LIVE=1 LINODE_TOKEN=... go test ./internal/livetest/...
//
// These tests never mutate resources. Add new tests sparingly — they consume
// real API quota.
package livetest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/linode/tui/linode"
)

const liveEnv = "LINODE_TUI_LIVE"

func newLiveClient(t *testing.T) *linode.Client {
	t.Helper()
	if os.Getenv(liveEnv) != "1" {
		t.Skipf("set %s=1 to run live tests", liveEnv)
	}
	tok := os.Getenv("LINODE_TOKEN")
	if tok == "" {
		t.Skip("LINODE_TOKEN not set")
	}
	c, err := linode.NewClient(tok)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return c
}

func ctx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func TestListInstances(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	items, err := c.ListInstances(cx)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	t.Logf("got %d instances", len(items))
}

func TestListRegions(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	items, err := c.Raw().ListRegions(cx, nil)
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one region")
	}
	t.Logf("got %d regions", len(items))
}

func TestListTypes(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	items, err := c.Raw().ListTypes(cx, nil)
	if err != nil {
		t.Fatalf("ListTypes: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one type")
	}
	t.Logf("got %d types", len(items))
}

func TestListVolumes(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	if _, err := c.Raw().ListVolumes(cx, nil); err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
}

func TestListNodeBalancers(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	if _, err := c.Raw().ListNodeBalancers(cx, nil); err != nil {
		t.Fatalf("ListNodeBalancers: %v", err)
	}
}

func TestListLKEClusters(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	if _, err := c.Raw().ListLKEClusters(cx, nil); err != nil {
		t.Fatalf("ListLKEClusters: %v", err)
	}
}

func TestListLKEVersions(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	items, err := c.Raw().ListLKEVersions(cx, nil)
	if err != nil {
		t.Fatalf("ListLKEVersions: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one k8s version")
	}
	t.Logf("got %d k8s versions", len(items))
}

func TestListEvents(t *testing.T) {
	c := newLiveClient(t)
	cx, cancel := ctx(t)
	defer cancel()
	if _, err := c.Raw().ListEvents(cx, nil); err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
}
