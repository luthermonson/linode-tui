package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
	"github.com/linode/tui/tui/views"
)

// runBatch executes a (possibly batched) tea.Cmd and returns the leaf
// messages, recursing through BatchMsg/sequenceMsg.
func runBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, runBatch(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func newTestClient(t *testing.T, h http.Handler) (*linode.Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(h)
	c, err := linode.NewClient("test-token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	c.SetBaseURL(ts.URL)
	t.Cleanup(ts.Close)
	return c, ts
}

func jsonResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func pagedResp(w http.ResponseWriter, data any) {
	jsonResp(w, map[string]any{
		"data":    data,
		"page":    1,
		"pages":   1,
		"results": 1,
	})
}

func TestCreateLinodeHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Region{{ID: "us-east", Label: "Newark, NJ", Status: "ok"}})
	})
	mux.HandleFunc("/v4/linode/types", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.LinodeType{{ID: "g6-nanode-1", Label: "Nanode 1GB", VCPUs: 1, Memory: 1024, Disk: 25}})
	})
	mux.HandleFunc("/v4/images", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Image{{ID: "linode/ubuntu22.04", Label: "Ubuntu 22.04 LTS", IsPublic: true, Status: "available"}})
	})
	mux.HandleFunc("/v4/linode/instances", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		jsonResp(w, linodego.Instance{ID: 42, Label: "demo", Region: "us-east", Type: "g6-nanode-1", Status: "provisioning"})
	})
	client, _ := newTestClient(t, mux)

	m := newCreateLinode(client)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v after all loads, want createPhaseForm; err=%v", m.phase, m.err)
	}

	m.label = "demo"
	m.region = "us-east"
	m.instanceType = "g6-nanode-1"
	m.image = "linode/ubuntu22.04"
	m.rootPass = "abcdefghijklm"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone {
		t.Fatalf("phase = %v after submit, want done; err=%v", m.phase, m.err)
	}
	if !strings.Contains(m.Result(), "demo") || !strings.Contains(m.Result(), "42") {
		t.Fatalf("Result = %q", m.Result())
	}
	if m.Err() != nil {
		t.Fatalf("unexpected err: %v", m.Err())
	}
}

func TestCreateLinodeRegionsErrorAborts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"reason":"boom"}]}`, 500)
	})
	mux.HandleFunc("/v4/linode/types", func(w http.ResponseWriter, r *http.Request) { pagedResp(w, []linodego.LinodeType{}) })
	mux.HandleFunc("/v4/images", func(w http.ResponseWriter, r *http.Request) { pagedResp(w, []linodego.Image{}) })
	client, _ := newTestClient(t, mux)

	m := newCreateLinode(client)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseAborted {
		t.Fatalf("phase = %v, want aborted", m.phase)
	}
	if m.Err() == nil {
		t.Fatal("expected error")
	}
}

func TestCreateNodeBalancerHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Region{{ID: "us-east", Label: "NJ", Status: "ok"}})
	})
	mux.HandleFunc("/v4/nodebalancers", func(w http.ResponseWriter, r *http.Request) {
		label := "demo"
		jsonResp(w, linodego.NodeBalancer{ID: 1, Label: &label, Region: "us-east"})
	})
	client, _ := newTestClient(t, mux)

	m := newCreateNodeBalancer(client)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form; err=%v", m.phase, m.err)
	}
	m.label = "demo"
	m.region = "us-east"
	m.throttle = "0"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone {
		t.Fatalf("phase = %v, want done; err=%v", m.phase, m.err)
	}
	if !strings.Contains(m.Result(), "1") {
		t.Fatalf("Result = %q", m.Result())
	}
}

func TestCreateVolumeHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Region{{ID: "us-east", Label: "NJ", Status: "ok"}})
	})
	mux.HandleFunc("/v4/volumes", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, linodego.Volume{ID: 7, Label: "vol", Region: "us-east", Size: 50})
	})
	client, _ := newTestClient(t, mux)

	m := newCreateVolume(client)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form", m.phase)
	}
	m.label = "vol"
	m.region = "us-east"
	m.size = "50"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone || m.result.Size != 50 {
		t.Fatalf("phase=%v size=%d err=%v", m.phase, m.result.Size, m.err)
	}
}

func TestCreateVPCHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Region{{ID: "us-east", Label: "NJ", Status: "ok"}})
	})
	mux.HandleFunc("/v4/vpcs", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, linodego.VPC{ID: 3, Label: "main", Region: "us-east"})
	})
	client, _ := newTestClient(t, mux)

	m := newCreateVPC(client)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form", m.phase)
	}
	m.label = "main"
	m.region = "us-east"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone {
		t.Fatalf("phase = %v; err=%v", m.phase, m.err)
	}
}

func TestCreateLKEHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Region{{ID: "us-east", Label: "NJ", Status: "ok"}})
	})
	mux.HandleFunc("/v4/linode/types", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.LinodeType{{ID: "g6-standard-1", VCPUs: 1, Memory: 2048}})
	})
	mux.HandleFunc("/v4/lke/versions", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.LKEVersion{{ID: "1.30"}})
	})
	mux.HandleFunc("/v4/lke/clusters", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, linodego.LKECluster{ID: 9, Label: "k", Region: "us-east", K8sVersion: "1.30"})
	})
	client, _ := newTestClient(t, mux)

	m := newCreateLKE(client)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form; err=%v", m.phase, m.err)
	}
	m.label = "k"
	m.region = "us-east"
	m.k8sVersion = "1.30"
	m.poolType = "g6-standard-1"
	m.poolCount = "3"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone || m.result.ID != 9 {
		t.Fatalf("phase=%v err=%v", m.phase, m.err)
	}
}

func TestConfigLinodeEditHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/linode/instances/", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, linodego.Instance{ID: 42, Label: "new-label"})
	})
	client, _ := newTestClient(t, mux)

	m := newConfigLinode(client, 42, "old-label", views.ConfigureEdit)
	// Edit doesn't preload — Init returns form-build cmd directly.
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form", m.phase)
	}
	m.newLabel = "new-label"
	m.newTags = "a, b"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone {
		t.Fatalf("phase = %v; err=%v", m.phase, m.err)
	}
}

func TestConfigLinodeResizeLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/linode/types", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.LinodeType{{ID: "g6-standard-2", VCPUs: 2, Memory: 4096}})
	})
	mux.HandleFunc("/v4/linode/instances/", func(w http.ResponseWriter, r *http.Request) {
		// Resize is POST /v4/linode/instances/{id}/resize — linodego still
		// tries to decode JSON, so respond with an empty object.
		jsonResp(w, map[string]any{})
	})
	client, _ := newTestClient(t, mux)

	m := newConfigLinode(client, 42, "demo", views.ConfigureResize)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form", m.phase)
	}
	m.newType = "g6-standard-2"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone {
		t.Fatalf("phase = %v; err=%v", m.phase, m.err)
	}
}

func TestConfigLinodeRebuildLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/images", func(w http.ResponseWriter, r *http.Request) {
		pagedResp(w, []linodego.Image{{ID: "linode/ubuntu22.04", Label: "U", IsPublic: true, Status: "available"}})
	})
	mux.HandleFunc("/v4/linode/instances/", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, linodego.Instance{ID: 42, Label: "demo"})
	})
	client, _ := newTestClient(t, mux)

	m := newConfigLinode(client, 42, "demo", views.ConfigureRebuild)
	for _, msg := range runBatch(m.Init()) {
		m.Update(msg)
	}
	if m.phase != createPhaseForm {
		t.Fatalf("phase = %v, want form", m.phase)
	}
	m.newImage = "linode/ubuntu22.04"
	m.newRootPass = "abcdefghijklm"
	m.phase = createPhaseSubmitting
	for _, msg := range runBatch(m.submit()) {
		m.Update(msg)
	}
	if m.phase != createPhaseDone {
		t.Fatalf("phase = %v; err=%v", m.phase, m.err)
	}
}
