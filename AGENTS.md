# linode-tui — Agent Guide

This file orients automated coding agents (Claude Code, Cursor, Aider, etc.) working in this repo. Humans should also skim it.

## What this is

A k9s-style TUI for the [Linode API](https://www.linode.com/docs/api/). Resource views (Linodes, NodeBalancers, LKE clusters, Volumes, VPCs, Firewalls, Images, Domains, Object Storage, StackScripts, Placement Groups, DBaaS, Events, Account) are reachable through a `:` command bar, filtered with `/`, and acted on with contextual hotkeys. Drilling into an LKE cluster execs into `k9s`; drilling into a DBaaS instance execs into `lazysql` (or any tool the user has configured).

## Stack

| Concern         | Library                                                                  |
| --------------- | ------------------------------------------------------------------------ |
| CLI             | `github.com/urfave/cli/v3`                                                |
| TUI runtime     | `github.com/charmbracelet/bubbletea`                                      |
| TUI widgets     | `github.com/charmbracelet/bubbles`                                        |
| Styling         | `github.com/charmbracelet/lipgloss`                                       |
| Forms           | `github.com/charmbracelet/huh`                                            |
| Linode API      | `github.com/linode/linodego`                                              |
| Config          | YAML at `~/.config/linode-tui/config.yaml` (see `config`)         |
| Secrets         | 1Password `op` CLI (preferred) or `LINODE_TOKEN` env var                  |

Use `urfave/cli/v3` (not cobra). Use `bubbletea`'s message-driven model — no goroutines reaching directly into the UI tree; emit `tea.Cmd`s.

## Layout

```
cmd/linode-tui/        # urfave entrypoint; just calls into cli
cli/          # urfave app, flags, subcommands
config/       # YAML config, defaults, theme/account/tool persistence
linode/       # linodego client wrapper, pagination, rate-limit handling
onepassword/  # `op` CLI shell-out for token resolution
tools/        # external exec (k9s, lazysql, …): resolve, install, run
tui/
  app.go               # root Bubble Tea model: header + body + footer + cmdbar
  keys/                # global keymap, per-view keymap composition
  theme/               # lipgloss styles for light/dark/dracula/solarized-light
  cmdbar/              # `:` command palette
  views/
    registry.go        # name → view factory; how `:` dispatches
    instances.go
    nodebalancers.go
    lke.go
    ...
```

Keep each resource view in one file under `tui/views/`. Each view implements a small interface (see `registry.go`).

## Conventions

- **Go**: target the toolchain in `go.mod`. Run `gofmt`/`goimports` before committing. `go vet ./...` should be clean.
- **Errors**: bubble them up; never `log.Fatal` outside of `cmd/`. Render errors in a status-bar toast — see `tui/app.go`.
- **Comments**: only when WHY isn't obvious from the code. Don't narrate.
- **Tests**: prefer integration tests against linodego's `httptest` fixtures over heavy mocking. Skip live API tests by default; opt in with `LINODE_TUI_LIVE=1`.
- **Theming**: never hard-code colors in views. Pull every style from `tui/theme`. Themes hot-swap via `:theme <name>` — views must rebuild styles from the current theme on each render rather than caching.
- **Refresh**: views accept a refresh interval (default 2s) and emit a `tea.Tick` themselves. Don't spawn raw goroutines; use `tea.Cmd`.
- **Pagination**: `linodego` auto-walks all pages when `ListXxx(ctx, nil)` is called (see `handlePaginatedResults` in `request_helpers.go`). Don't add page loops to view listers — they already see every page. If you need a specific page, pass a `*linodego.ListOptions` with `PageOptions.Page` set.
- **Retries**: `linodego.NewClient` calls `SetRetries()` by default — it retries 429s, 503s, Linode-busy responses, request timeouts, GOAWAY frames, and NGINX transient errors with exponential backoff. Don't add an outer retry loop. Add additional conditions via `c.Raw().AddRetryCondition(...)` if needed.
- **Mouse**: `tea.WithMouseCellMotion` is enabled in `tui.Run`. `listView` translates wheel-up/down into `table.MoveUp(3)`/`MoveDown(3)`. Click-to-select isn't wired — `bubbles/table` doesn't expose row hit-testing, so the row-Y math is ours to do if we want it. Most users will keep using arrow / `j` / `k`.
- **Watchlist freshness**: `:watchlist` refetches every `cfg.Refresh` (2s by default), so a bookmark toggled in another view shows up on the next tick. Forcing an immediate refresh requires a global event bus — punted; it'd add coupling for a ~2s win.
- **Mutations**: destructive actions (delete, reboot, power-off, resize, rebuild) MUST go through a confirm modal. No exceptions — even `:delete-everything` style helpers.
- **Secrets**: never write resolved tokens to disk. The config stores 1Password references (`op://Vault/Item/credential`) or, for testing, a literal token. Resolved tokens live only in memory.
- **External tools**: any subprocess goes through `tools`. Don't `exec.Command` from view code.

## Config schema

Source of truth is `config/config.go`. High-level shape:

```yaml
default_account: dev
active_theme: light          # dark | light | dracula | solarized-light
refresh: 2s

accounts:
  dev:  { op_ref: "op://Work/linode-dev/credential" }
  e2e:  { op_ref: "op://Work/linode-e2e/credential" }
  prod: { op_ref: "op://Work/linode-prod/credential" }

tools:
  install_dir: ~/.local/bin
  kubernetes:
    exec: k9s
    args: ["--kubeconfig", "{{.Kubeconfig}}"]
    mode: tui                # tui = terminal handoff; gui = fire-and-forget
    auto_install: true
  mysql:
    exec: lazysql
    args: ["-driver", "mysql", "-url", "{{.DSN}}"]
    mode: tui
    auto_install: true
  postgresql:
    exec: lazysql
    args: ["-driver", "postgres", "-url", "{{.DSN}}"]
    mode: tui
    auto_install: true
```

Templating uses Go's `text/template`. Variables passed in depend on the tool — see `tools/runner.go` for the per-tool context structs.

## CLI surface

- Bare `linode-tui` → TUI. Flags: `--token`, `--account`, `--refresh`, `--theme`, `--config`, `--debug`.
- `linode-tui version` → version + commit + linodego version.
- Future: `linode-tui clear-account` (deferred; do not implement until explicitly asked).

No headless `list`/`get` subcommands — the [official linode-cli](https://github.com/linode/linode-cli) already does that. This binary is a TUI plus the occasional utility command that does something the official CLI doesn't.

## How to add a new resource view

1. Create `tui/views/<resource>.go`.
2. Implement the `View` interface (`Init`, `Update`, `View`, `KeyMap`, `Title`).
3. Register in `tui/views/registry.go` with name + aliases (e.g. `instances`, `linodes`, `inst`, `li`).
4. Add hotkeys to the per-view keymap. Re-use shared keys (`enter`, `/`, `?`, `esc`) — don't redefine them.
5. If the view supports mutations, wire each through `huh` confirm forms in `tui/views/<resource>_actions.go`.
6. Test list pagination with at least 200 fake records to exercise the `bubbles/table` virtualization.

## How to add a new external tool

1. Add the default entry to `config/default.go`.
2. If `auto_install: true` is supported, add a release fetcher in `tools/install/`.
3. Define the per-tool context struct (what `{{.Kubeconfig}}` / `{{.DSN}}` resolve to) and surface it from the view that invokes the tool.
4. Document the new tool in this file's tools list.

## Agent behavior expectations

- **Match the existing layout.** Don't reorganize directories without asking. New code goes in the most specific existing subpackage.
- **Don't introduce new top-level dependencies casually.** If you reach for cobra, viper, kingpin, tview, tcell, gocui, color, or a new logger, stop and check — we deliberately picked the stack above.
- **Prefer composition in `tea.Model`.** Views own their state; the root model is a thin router.
- **No `panic` outside of `cmd/`.** No `os.Exit` from library packages.
- **No fmt.Println in TUI code.** Everything user-facing goes through the Bubble Tea render path.
- **Respect `mode: gui`.** GUI tools must not call `tea.ExecProcess` (that captures the terminal) — use `exec.Command(...).Start()` and return.
- **Destructive actions need confirms.** Always. Even in tests, use the confirm path.
- **Don't commit secrets.** Never check in a `LINODE_TOKEN`, a real `op://` reference to a personal vault, or a populated `config.local.yaml`.
- **Don't add "Generated with Claude Code" or `Co-Authored-By` footers to commits or PRs.**

## Out of scope (for now)

- Headless list/get subcommands (use `linode-cli`).
- Longview view (metrics shape doesn't fit a table-driven UI).
- Create/resize forms for resources beyond Linodes, NodeBalancers, LKE, VPCs, Volumes (v2).
- `clear-account` utility (planned, but do not implement until requested).
- Multi-tenant token storage encryption (we rely on 1Password for at-rest).
