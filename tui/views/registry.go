package views

import (
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/luthermonson/linode-tui/config"
	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tui/theme"
)

type View interface {
	tea.Model
	Title() string
}

type Deps struct {
	Cfg     *config.Config
	Theme   theme.Theme
	Linode  *linode.Client
	Context map[string]any
}

// CtxInt returns an int value from Deps.Context or (0, false).
func (d Deps) CtxInt(key string) (int, bool) {
	if d.Context == nil {
		return 0, false
	}
	v, ok := d.Context[key]
	if !ok {
		return 0, false
	}
	i, ok := v.(int)
	return i, ok
}

// CtxString returns a string value from Deps.Context or "".
func (d Deps) CtxString(key string) string {
	if d.Context == nil {
		return ""
	}
	if v, ok := d.Context[key].(string); ok {
		return v
	}
	return ""
}

type Factory func(Deps) View

type entry struct {
	name    string
	aliases []string
	factory Factory
	// child marks a drill-in view that only works with parent context in
	// Deps.Context (e.g. nodebalancer_configs needs a nodebalancer_id). Such
	// views are reachable via NavigateMsg from their parent but are excluded
	// from the top-level command bar so they don't appear as standalone verbs.
	child bool
}

var registry []entry

func Register(name string, aliases []string, f Factory) {
	registry = append(registry, entry{name: name, aliases: aliases, factory: f})
}

// RegisterChild is Register for a context-only drill-in view. It resolves and
// navigates exactly like a normal view, but IsChild reports true and
// NavCompletions omits it, so it never surfaces as a bare top-level command.
func RegisterChild(name string, aliases []string, f Factory) {
	registry = append(registry, entry{name: name, aliases: aliases, factory: f, child: true})
}

func Resolve(query string) (Factory, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, false
	}
	// Exact match on a canonical name or alias.
	for _, e := range registry {
		if e.name == q || slices.Contains(e.aliases, q) {
			return tagFactory(e.name, e.factory), true
		}
	}
	// Prefix match — on names first, then aliases — so e.g. `:lin` resolves to
	// instances via its "linodes" alias, not just `:instances`. Child views are
	// skipped here: they resolve only by exact name/alias (which is how a
	// parent's NavigateMsg reaches them), so a prefix like `:node` lands on the
	// top-level `nodebalancers`, never the `nodebalancer_configs` drill-in.
	for _, e := range registry {
		if e.child {
			continue
		}
		if strings.HasPrefix(e.name, q) {
			return tagFactory(e.name, e.factory), true
		}
	}
	for _, e := range registry {
		if e.child {
			continue
		}
		for _, a := range e.aliases {
			if strings.HasPrefix(a, q) {
				return tagFactory(e.name, e.factory), true
			}
		}
	}
	return nil, false
}

// IsChild reports whether query names a context-only drill-in view (by
// canonical name or alias). Used by the command bar to reject bare invocations
// with a helpful hint instead of navigating to an empty/erroring view.
func IsChild(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	for _, e := range registry {
		if e.child && (e.name == q || slices.Contains(e.aliases, q)) {
			return true
		}
	}
	return false
}

// tagFactory wraps a factory so the resolved view's registered name is
// available to it via Deps.Context["view_name"]. Used for per-view
// configuration (refresh overrides, etc.).
func tagFactory(name string, f Factory) Factory {
	return func(d Deps) View {
		if d.Context == nil {
			d.Context = map[string]any{}
		}
		if _, set := d.Context["view_name"]; !set {
			d.Context["view_name"] = name
		}
		return f(d)
	}
}

func Names() []string {
	out := make([]string, 0, len(registry))
	for _, e := range registry {
		out = append(out, e.name)
	}
	sort.Strings(out)
	return out
}

// NavCompletions returns every first-token completion the command bar should
// offer: the canonical names AND aliases of all top-level views. Context-only
// child views are omitted (they can't be opened without a parent). This is what
// makes `:lin` → linodes, `:pg` → placementgroups, etc. discoverable, and keeps
// drill-ins like nodebalancer_configs out of the menu.
func NavCompletions() []string {
	var out []string
	for _, e := range registry {
		if e.child {
			continue
		}
		out = append(out, e.name)
		out = append(out, e.aliases...)
	}
	sort.Strings(out)
	return out
}
