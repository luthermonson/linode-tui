package livetest

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/linode/linodego/v2"
)

const liveMutateEnv = "LINODE_TUI_LIVE_MUTATE"

func newMutatingClient(t *testing.T) *liveCtx {
	t.Helper()
	if os.Getenv(liveEnv) != "1" {
		t.Skipf("set %s=1 to run live tests", liveEnv)
	}
	if os.Getenv(liveMutateEnv) != "1" {
		t.Skipf("set %s=1 to run mutating live tests (creates + deletes real resources)", liveMutateEnv)
	}
	c := newLiveClient(t)
	return &liveCtx{t: t, c: c}
}

type liveCtx struct {
	t *testing.T
	c interface {
		Raw() *linodego.Client
	}
}

// TestCreateThenDeleteInstance creates the smallest possible Linode and
// deletes it. Runs only when both LINODE_TUI_LIVE=1 and
// LINODE_TUI_LIVE_MUTATE=1 are set.
func TestCreateThenDeleteInstance(t *testing.T) {
	lc := newMutatingClient(t)
	client := lc.c.Raw()

	cx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	regions, err := client.ListRegions(cx, nil)
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	region := ""
	for _, r := range regions {
		if r.Status == "ok" {
			region = r.ID
			break
		}
	}
	if region == "" {
		t.Skip("no usable region")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	label := fmt.Sprintf("linode-tui-livetest-%d", rng.Int31())
	booted := false
	created, err := client.CreateInstance(cx, linodego.InstanceCreateOptions{
		Region:   region,
		Type:     "g6-nanode-1",
		Label:    label,
		Booted:   &booted, // don't waste cycles booting; we'll delete immediately
		RootPass: "tempPasswordThatIsLongEnough123!",
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	t.Logf("created %s (id %d) — will delete", created.Label, created.ID)

	// Always attempt delete; log but don't double-fail.
	t.Cleanup(func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dcancel()
		if err := client.DeleteInstance(dctx, created.ID); err != nil {
			t.Errorf("cleanup DeleteInstance(%d): %v", created.ID, err)
		}
	})

	// Sanity: it's in the list.
	items, err := client.ListInstances(cx, nil)
	if err != nil {
		t.Fatalf("ListInstances post-create: %v", err)
	}
	found := false
	for _, it := range items {
		if it.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created instance %d not in list", created.ID)
	}
}
