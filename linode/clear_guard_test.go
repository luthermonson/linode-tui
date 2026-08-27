package linode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

// guardMux is clearAccountMux plus a /profile endpoint, so ClearAccount can
// verify who the token actually belongs to.
func guardMux(t *testing.T, deletes *atomic.Int32, username string, profileStatus int) http.Handler {
	t.Helper()
	m := clearAccountMux(t, deletes)
	m.HandleFunc("/v4/profile", func(w http.ResponseWriter, r *http.Request) {
		if profileStatus != 0 && profileStatus != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(profileStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"reason": "Your OAuth token is not authorized"}},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(linodego.Profile{Username: username})
	})
	return m
}

// ClearAccount must run the name guard itself. Relying on callers to call
// ClearGuard first meant any new entry point (a CLI subcommand, a script)
// silently skipped it.
func TestClearAccountRunsGuardItself(t *testing.T) {
	tests := []struct {
		name    string
		opts    linode.ClearOptions
		wantErr string
	}{
		{"empty account", linode.ClearOptions{Account: "", Execute: true}, "account name is required"},
		{"prod name", linode.ClearOptions{Account: "prod-main", Execute: true}, "name contains 'prod'"},
		{"prod name dry-run", linode.ClearOptions{Account: "PROD", Execute: false}, "name contains 'prod'"},
		{"prod substring", linode.ClearOptions{Account: "us-production", Execute: true}, "name contains 'prod'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var deletes atomic.Int32
			client := newTestClient(t, guardMux(t, &deletes, "someone", http.StatusOK))

			var buf bytes.Buffer
			err := linode.ClearAccount(context.Background(), client, tc.opts, &buf)
			if err == nil {
				t.Fatalf("expected the guard to refuse %q", tc.opts.Account)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
			if n := deletes.Load(); n != 0 {
				t.Errorf("guard failed but %d mutation(s) still happened", n)
			}
			if buf.Len() != 0 {
				t.Errorf("guard should refuse before any output:\n%s", buf.String())
			}
		})
	}
}

func TestClearAccountForceBypassesNameGuard(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "someone", http.StatusOK))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "prod-main", Execute: false, Force: true,
	}, &buf)
	if err != nil {
		t.Fatalf("force should allow the run: %v", err)
	}
	if !strings.Contains(buf.String(), "DRY-RUN") {
		t.Errorf("expected the run to proceed:\n%s", buf.String())
	}
}

// The account name is a local label. Nothing tied it to the token until now,
// so a mislabelled account could destroy a different Linode account entirely.
func TestClearAccountIdentityMismatchAborts(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "prod-owner", http.StatusOK))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: true, ExpectedUsername: "dev-owner",
	}, &buf)
	if err == nil {
		t.Fatal("expected a refusal when the token belongs to another user")
	}
	for _, want := range []string{"prod-owner", "dev-owner", "refusing to clear"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
	if n := deletes.Load(); n != 0 {
		t.Fatalf("identity check failed but %d mutation(s) happened", n)
	}
	if buf.Len() != 0 {
		t.Errorf("identity check should refuse before any output:\n%s", buf.String())
	}
}

func TestClearAccountIdentityMatchProceeds(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "Dev-Owner", http.StatusOK))

	var buf bytes.Buffer
	// Usernames are compared case-insensitively.
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: false, ExpectedUsername: "dev-owner",
	}, &buf)
	if err != nil {
		t.Fatalf("matching identity should proceed: %v", err)
	}
	if !strings.Contains(buf.String(), "would delete: instance") {
		t.Errorf("expected a dry-run listing:\n%s", buf.String())
	}
}

// An unverifiable token is not a token to hand a destroy to.
func TestClearAccountUnverifiableIdentityAborts(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "", http.StatusUnauthorized))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: true, ExpectedUsername: "dev-owner",
	}, &buf)
	if err == nil {
		t.Fatal("expected a refusal when /profile can't be read")
	}
	if !strings.Contains(err.Error(), "cannot verify token identity") {
		t.Errorf("error = %v, want it to mention the failed verification", err)
	}
	if n := deletes.Load(); n != 0 {
		t.Fatalf("unverified token still performed %d mutation(s)", n)
	}
}

// LKE has to go first: node pools recreate any worker Linode deleted out from
// under them, and cluster-owned NodeBalancers/volumes are recreated by the
// CCM/CSI controllers until the cluster itself is gone.
func TestClearAccountDeletesLKEFirst(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "dev-owner", http.StatusOK))

	var buf bytes.Buffer
	_ = linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: false,
	}, &buf)

	out := buf.String()
	lke := strings.Index(out, "--- lke ---")
	if lke < 0 {
		t.Fatalf("no lke step in output:\n%s", out)
	}
	for _, after := range []string{"--- instances ---", "--- volumes ---", "--- nodebalancers ---"} {
		idx := strings.Index(out, after)
		if idx < 0 {
			t.Fatalf("no %q step in output:\n%s", after, out)
		}
		if idx < lke {
			t.Errorf("%s runs before lke; node pools/CCM will recreate what it deletes", after)
		}
	}
}

// A cancelled parent context must stop the run. Deletes used to be built from
// context.Background(), so Ctrl+C couldn't interrupt a destroy in progress.
func TestClearAccountHonoursCancelledContext(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "dev-owner", http.StatusOK))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err := linode.ClearAccount(ctx, client, linode.ClearOptions{
		Account: "dev", Execute: true,
	}, &buf)
	if err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	if !strings.Contains(err.Error(), "aborted") {
		t.Errorf("error = %v, want an abort", err)
	}
	if n := deletes.Load(); n != 0 {
		t.Errorf("cancelled run still performed %d mutation(s)", n)
	}
}

// Cancelling mid-run must stop before the next step instead of running the
// whole sweep to completion.
func TestClearAccountStopsMidRunOnCancel(t *testing.T) {
	var deletes atomic.Int32
	base := guardMux(t, &deletes, "dev-owner", http.StatusOK)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base.ServeHTTP(w, r)
		if r.URL.Path == "/v4/lke/clusters" {
			cancel() // stand-in for Ctrl+C after the first step
		}
	}))

	var buf bytes.Buffer
	err := linode.ClearAccount(ctx, client, linode.ClearOptions{
		Account: "dev", Execute: true,
	}, &buf)
	if err == nil {
		t.Fatal("expected the run to report the abort")
	}
	out := buf.String()
	if !strings.Contains(out, "--- lke ---") {
		t.Fatalf("first step should have run:\n%s", out)
	}
	if strings.Contains(out, "--- instances ---") {
		t.Errorf("run continued past cancellation:\n%s", out)
	}
}

// Non-empty buckets are skipped, so the account isn't actually empty — the run
// has to say so instead of reporting a clean "done".
func TestClearAccountReportsSkippedBuckets(t *testing.T) {
	var deletes atomic.Int32
	client := newTestClient(t, guardMux(t, &deletes, "dev-owner", http.StatusOK))

	var buf bytes.Buffer
	err := linode.ClearAccount(context.Background(), client, linode.ClearOptions{
		Account: "dev", Execute: true, ExpectedUsername: "dev-owner",
	}, &buf)
	if err == nil {
		t.Fatal("expected a partial-completion error")
	}
	if !strings.Contains(err.Error(), "left behind") || !strings.Contains(err.Error(), "bucket full") {
		t.Errorf("error = %v, want it to name the skipped bucket", err)
	}
	if strings.Contains(buf.String(), "\ndone · ") {
		t.Errorf("partial run must not print a clean done line:\n%s", buf.String())
	}
}
