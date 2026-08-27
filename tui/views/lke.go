package views

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tools"
)

func init() {
	Register("lke", []string{"kubernetes", "k8s", "clusters", "cluster"}, newLKE)
}

func newLKE(d Deps) View {
	return newListView(listOpts[linodego.LKECluster]{
		Deps:  d,
		Title: "LKE Clusters",
		Columns: []Col{
			{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
			{Title: "LABEL", Width: 30, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "REGION", Width: 14, MinWidth: 8, Priority: PriMed},
			{Title: "STATUS", Width: 12, MinWidth: 8, Priority: PriHigh},
			{Title: "K8S", Width: 10, MinWidth: 6, Priority: PriMed},
			{Title: "TIER", Width: 10, MinWidth: 6, Priority: PriLow},
			{Title: "HA", Width: 6, MinWidth: 3, Priority: PriLow},
			{Title: "TAGS", Width: 20, MinWidth: 8, Priority: PriLowest},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.LKECluster, error) {
			return c.Raw().ListLKEClusters(ctx, nil)
		},
		Rower: func(l linodego.LKECluster) table.Row {
			return table.Row{
				strconv.Itoa(l.ID),
				l.Label,
				l.Region,
				string(l.Status),
				l.K8sVersion,
				l.Tier,
				yesNo(l.ControlPlane.HighAvailability),
				strings.Join(l.Tags, ","),
			}
		},
		Matcher: func(l linodego.LKECluster, needle string) bool {
			return containsAny(needle, l.Label, l.Region, string(l.Status), l.K8sVersion, l.Tier) ||
				tagMatch(l.Tags, needle)
		},
		IDFn:         func(l linodego.LKECluster) string { return strconv.Itoa(l.ID) },
		BookmarkKind: "lke",
		TagsFn:       func(l linodego.LKECluster) []string { return l.Tags },
		FieldFn: map[string]func(linodego.LKECluster) string{
			"region": func(l linodego.LKECluster) string { return l.Region },
			"status": func(l linodego.LKECluster) string { return string(l.Status) },
			"label":  func(l linodego.LKECluster) string { return l.Label },
			"k8s":    func(l linodego.LKECluster) string { return l.K8sVersion },
			"tier":   func(l linodego.LKECluster) string { return l.Tier },
		},
		OnEnter: func(c linodego.LKECluster, _ Deps) tea.Cmd {
			id := c.ID
			return func() tea.Msg {
				return NavigateMsg{
					Name:    "lke_detail",
					Context: map[string]any{"focus_id": id, "cluster_id": id},
				}
			}
		},
		KeyHandlers: map[string]func(linodego.LKECluster, Deps) tea.Cmd{
			"c": openLKECluster,
		},
		KeyHelp: map[string]string{
			"c": "connect: launch k9s directly (fetches kubeconfig; skips detail view)",
		},
		Actions: []Action[linodego.LKECluster]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(l linodego.LKECluster) string {
					return fmt.Sprintf("DELETE cluster %s (id %d)? All node pools will be destroyed.", l.Label, l.ID)
				},
				Run: func(ctx context.Context, c *linode.Client, l linodego.LKECluster) error {
					return c.Raw().DeleteLKECluster(ctx, l.ID)
				},
			},
		},
	})
}

func openLKECluster(c linodego.LKECluster, d Deps) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		kc, err := d.Linode.Raw().GetLKEClusterKubeconfig(ctx, c.ID)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("fetch kubeconfig for %q: %w", c.Label, err)}
		}
		decoded, err := base64.StdEncoding.DecodeString(kc.KubeConfig)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("decode kubeconfig: %w", err)}
		}
		// Kubeconfigs hold bearer tokens — keep them short-lived. Write to
		// os.TempDir with a random suffix, 0600 perms, and tear the file
		// down the moment k9s exits (Cleanup callback below). k9s expects
		// a filesystem path; there's no portable in-memory mechanism that
		// works across Linux/macOS/Windows.
		tmp, err := os.CreateTemp("", fmt.Sprintf("linode-tui-kc-%d-*.yaml", c.ID))
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("write kubeconfig: %w", err)}
		}
		_ = tmp.Chmod(0o600)
		if _, err := tmp.Write(decoded); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return ErrorMsg{Err: fmt.Errorf("write kubeconfig: %w", err)}
		}
		_ = tmp.Close()
		path := tmp.Name()
		return DrillInMsg{
			Tool:    tools.KindKubernetes,
			Vars:    kubeconfigVars{Kubeconfig: path},
			Env:     []string{"KUBECONFIG=" + path},
			Cleanup: func() { _ = os.Remove(path) },
		}
	}
}

// kubeconfigVars is the template input for tools.kubernetes.args.
type kubeconfigVars struct {
	Kubeconfig string
}
