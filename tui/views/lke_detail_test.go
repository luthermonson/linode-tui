package views

import (
	"os"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/luthermonson/linode-tui/config"
	"github.com/luthermonson/linode-tui/tools"
)

// kubeconfigToolCfg returns a config whose kubernetes tool resolves to the
// currently-running test binary. resolveExec only stats the path — the exec is
// never actually run in these tests — so any existing executable works and the
// runner reaches its success branch cross-platform.
func kubeconfigToolCfg() *config.Config {
	return &config.Config{
		Tools: config.Tools{
			Kubernetes: config.Tool{Exec: os.Args[0], Mode: config.ModeTUI},
		},
	}
}

// TestLKEDetailDrillInDefersCleanup is the regression guard for issue #2:
// k9s reported "Watcher failed for contexts -- stat <path>" because the
// kubeconfig temp file was deleted out from under it. That happened because
// runDrillIn called Cleanup() synchronously inside the tea.Sequence closure —
// and tea.Sequence invokes each command's function immediately without waiting
// for the blocking ExecProcess (k9s) to finish. Cleanup must instead ride on
// the done message, run only when that message is later handled.
func TestLKEDetailDrillInDefersCleanup(t *testing.T) {
	m := &lkeDetail{deps: Deps{Cfg: kubeconfigToolCfg()}}
	cleaned := false
	cmd := m.runDrillIn(DrillInMsg{
		Tool:    tools.KindKubernetes,
		Vars:    kubeconfigVars{Kubeconfig: "/tmp/kc.yaml"},
		Cleanup: func() { cleaned = true },
	})
	if cmd == nil {
		t.Fatal("expected a drill-in command, got nil")
	}

	// Unpack the tea.Sequence the way bubbletea's runtime does: the returned
	// command yields a slice of commands ([exec, doneClosure]). Invoke the
	// done closure directly — exactly what execSequenceMsg does after Sending
	// the exec — and confirm it does NOT clean up as a side effect.
	seq := reflect.ValueOf(cmd())
	if seq.Kind() != reflect.Slice || seq.Len() < 2 {
		t.Fatalf("expected a sequence of >=2 commands, got %v", seq.Kind())
	}
	doneClosure, ok := seq.Index(seq.Len() - 1).Interface().(tea.Cmd)
	if !ok {
		t.Fatal("last sequence element is not a tea.Cmd")
	}
	done := doneClosure()
	if cleaned {
		t.Fatal("cleanup ran while building the done message — the kubeconfig " +
			"would be deleted before k9s reads it (issue #2 regression)")
	}
	if _, ok := done.(lkeDetailDrillDoneMsg); !ok {
		t.Fatalf("expected lkeDetailDrillDoneMsg, got %T", done)
	}

	// Cleanup fires only once the done message is handled — after k9s exits.
	if _, _ = m.Update(done); !cleaned {
		t.Fatal("expected cleanup to run when the done message is handled")
	}
}

// TestLKEDetailDrillInToolMissing verifies a missing tool bubbles up an
// InstallNeededMsg (mirroring listView.drillIn) rather than dead-ending in the
// detail view, and preserves Cleanup on the carried drill.
func TestLKEDetailDrillInToolMissing(t *testing.T) {
	cfg := &config.Config{
		Tools: config.Tools{
			Kubernetes: config.Tool{Exec: "linode-tui-no-such-binary", Mode: config.ModeTUI, AutoInstall: true},
		},
	}
	m := &lkeDetail{deps: Deps{Cfg: cfg}}
	cleaned := false
	cmd := m.runDrillIn(DrillInMsg{Tool: tools.KindKubernetes, Cleanup: func() { cleaned = true }})
	if cmd == nil {
		t.Fatal("expected an InstallNeededMsg command, got nil")
	}
	msg := cmd()
	need, ok := msg.(InstallNeededMsg)
	if !ok {
		t.Fatalf("expected InstallNeededMsg, got %T", msg)
	}
	if need.Kind != tools.KindKubernetes {
		t.Fatalf("expected kind kubernetes, got %q", need.Kind)
	}
	if cleaned {
		t.Fatal("cleanup must be preserved for the re-dispatched drill, not run on install-needed")
	}
	if need.Drill.Cleanup == nil {
		t.Fatal("expected the carried drill to retain its Cleanup")
	}
}

// TestLKEDetailDrillInGenericError verifies a non-install error still runs
// Cleanup (so the temp kubeconfig isn't leaked) and surfaces on the view.
func TestLKEDetailDrillInGenericError(t *testing.T) {
	cfg := &config.Config{
		Tools: config.Tools{
			Kubernetes: config.Tool{Exec: "linode-tui-no-such-binary", Mode: config.ModeTUI, AutoInstall: false},
		},
	}
	m := &lkeDetail{deps: Deps{Cfg: cfg}}
	cleaned := false
	cmd := m.runDrillIn(DrillInMsg{Tool: tools.KindKubernetes, Cleanup: func() { cleaned = true }})
	if cmd != nil {
		t.Fatal("expected nil command on generic error")
	}
	if !cleaned {
		t.Fatal("expected cleanup to run on a non-install error")
	}
	if m.data.err == nil {
		t.Fatal("expected the error to surface on the detail view")
	}
}
