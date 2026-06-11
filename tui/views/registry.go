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
}

var registry []entry

func Register(name string, aliases []string, f Factory) {
	registry = append(registry, entry{name: name, aliases: aliases, factory: f})
}

func Resolve(query string) (Factory, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, false
	}
	for _, e := range registry {
		if e.name == q || slices.Contains(e.aliases, q) {
			return tagFactory(e.name, e.factory), true
		}
	}
	for _, e := range registry {
		if strings.HasPrefix(e.name, q) {
			return tagFactory(e.name, e.factory), true
		}
	}
	return nil, false
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
