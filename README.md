# linode-tui

A [k9s](https://k9scli.io/)-inspired terminal UI for the [Linode API](https://www.linode.com/docs/api/). Resource views are reached through a `:` command palette, filtered with `/`, and acted on with contextual hot-keys.

Built on Bubble Tea + linodego + urfave/cli/v3.

```
linode-tui · Linodes
┌────────┬────────────────┬─────────┬──────────────────┬──────────┬──────────────┐
│ ID     │ LABEL          │ REGION  │ TYPE             │ STATUS   │ IPv4         │
├────────┼────────────────┼─────────┼──────────────────┼──────────┼──────────────┤
│ 12345  │ prod-web-1     │ us-east │ g6-standard-2    │ running  │ 198.51.100.1 │
│ 12346  │ prod-web-2     │ us-east │ g6-standard-2    │ running  │ 198.51.100.2 │
│ ✓ 12347│ dev-scratch    │ us-mia  │ g6-nanode-1      │ stopped  │ 198.51.100.3 │
└────────┴────────────────┴─────────┴──────────────────┴──────────┴──────────────┘
1 selected · 3 linodes · refreshed 1s ago
: command  / filter  ? help  ctrl+c quit
```

## Features

- **12 resource views**: Linodes, Volumes, NodeBalancers, Firewalls, Images, Domains, LKE clusters, VPCs, Placement Groups, StackScripts, Object Storage, DBaaS, Events
- **CRUD on the important ones**: create / reboot / boot / shutdown / delete / resize / rebuild for Linodes; create + delete for NodeBalancers / Volumes / VPCs / LKE; delete on the rest (subject to API constraints — public images, non-empty buckets, etc.)
- **Drill into specialized TUIs**:
  - `enter` on an LKE cluster → fetches its kubeconfig and execs into [k9s](https://k9scli.io/)
  - `enter` on a DBaaS instance → builds a connection URL and execs into [lazysql](https://github.com/jorgerojas26/lazysql)
- **Lazy auto-install** of external tools with checksum verification and a first-run prompt for an install dir
- **1Password integration** for tokens — config stores `op://` references, never plaintext
- **Account switcher** — multi-account config with on-the-fly switching
- **Themes**: dark, light, dracula, solarized-light — switchable live via `:theme <name>`
- **Configurable exec**: point `tools.kubernetes.exec` / `tools.mysql.exec` / `tools.postgresql.exec` at anything (Lens, MySQL Workbench, etc.)
- **Bookmarks + watchlist** — star (`*`) any row; bookmarked resources float to the top of their view and join a synthetic `:watchlist` with drift detection
- **Saved layouts** — capture pane shapes with `:layout save <name>`, share them as `?sha256=…` pinned URLs, and auto-load a `default` layout on launch
- **Audit log** — every mutating action gets a JSON line in `~/.cache/linode-tui/audit.log`; powers `ctrl+y` replay-last, `:undo`, and the "recent: …" startup banner
- **`doctor`** — health checks (config / tools / runtime / layout) with `--watch`, `--json`, `--no-color`, and inline remediation hints
- **`:clear-account` utility** to wipe dev/e2e accounts (typed-username confirm, dry-run preview, refuses `prod*` accounts)

See **[docs/USAGE.md](docs/USAGE.md)** for the full reference — every command-bar verb, every CLI subcommand, the audit/bookmark/layout workflows, and recipes.

## Install

```bash
go install github.com/luthermonson/linode-tui/cmd/linode-tui@latest
```

Or build from source:

```bash
git clone https://github.com/luthermonson/linode-tui && cd tui
go build -o linode-tui ./cmd/linode-tui
```

Requires Go 1.26+.

## Quick start

```bash
export LINODE_TOKEN=...
linode-tui
```

Or set up `~/.config/linode-tui/config.yaml`:

```yaml
default_account: dev
active_theme: light
refresh: 2s

accounts:
  dev:
    op_ref: "op://Work/linode-dev/credential"
  e2e:
    op_ref: "op://Work/linode-e2e/credential"
  perso:
    token: "..."   # not recommended; prefer op_ref

tools:
  install_dir: ~/.local/bin   # auto-picked on first install
  kubernetes:
    exec: k9s
    args: ["--kubeconfig", "{{.Kubeconfig}}"]
    mode: tui
    auto_install: true
    version: ""               # blank = pinned default (currently v0.50.18)
  mysql:
    exec: lazysql
    args: ["{{.DSN}}"]
    mode: tui
    auto_install: true
  postgresql:
    exec: lazysql
    args: ["{{.DSN}}"]
    mode: tui
    auto_install: true
```

`mode: gui` works for non-TUI desktop apps (MySQL Workbench, Lens, etc.) — fire-and-forget via `exec.Start`.

### Lish in a new terminal tab (iTerm2)

Drop into a fresh iTerm tab instead of suspending the TUI:

```yaml
tools:
  lish:
    exec: /usr/bin/osascript
    args:
      - -e
      - |
        tell application "iTerm2"
          tell current window
            create tab with default profile command "ssh -t {{.Username}}@lish-{{.Region}}.linode.com {{.Label}}"
          end tell
        end tell
    mode: gui
    auto_install: false
```

For tmux:

```yaml
tools:
  lish:
    exec: tmux
    args: ["new-window", "ssh -t {{.Username}}@lish-{{.Region}}.linode.com {{.Label}}"]
    mode: gui
    auto_install: false
```

## Key bindings

### Global

| Key | Action |
|---|---|
| `:` | Open command bar |
| `?` | Toggle help overlay (then any key filters) |
| `/` | Filter current view |
| `r` / `ctrl+r` | Refresh now |
| `enter` | Drill into selected row |
| `esc` | Cancel modal / clear filter |
| `ctrl+c` | Quit |
| `↑/↓` `j/k` | Move cursor |

### On a Linode row

| Key | Action |
|---|---|
| `R` | Reboot |
| `b` | Boot |
| `s` | Shutdown |
| `d` | Delete (with confirm) |
| `e` | Edit label + tags |
| `z` | Resize plan |
| `B` | Rebuild from image |
| `y` | View JSON detail |

### On any row that supports it

| Key | Action |
|---|---|
| `d` | Delete (with confirm) |
| `y` | View JSON detail |
| `space` | Toggle row selection |
| `D` | Delete all selected rows (single confirm) |
| `enter` | Drill in (NodeBalancer → configs, Domain → records, LKE → k9s, DBaaS → lazysql) |
| `esc` | Pop drill-in / clear filter |

## Command bar

A small cross-section — see [docs/USAGE.md](docs/USAGE.md) for the full list.

| Command | Effect |
|---|---|
| `:linodes` / `:instances` / `:inst` | Switch to Linodes view (aliases vary per view) |
| `:databases` / `:lke` / `:volumes` / etc. | Switch view |
| `:watchlist` | Synthetic view across all bookmarked rows with drift detection |
| `:theme dark\|light\|dracula\|solarized-light` | Change theme |
| `:account` / `:account <name>` | List / switch active account |
| `:layout save\|load\|list\|share` `<name>` | Manage saved pane layouts |
| `:bookmark list\|migrate\|export\|scope` | Manage bookmarks |
| `:audit recent\|grep\|tail\|purge\|clear` | Inspect or trim the audit log |
| `:refresh [view] <dur\|off>` / `:refresh defaults` | Per-view refresh overrides |
| `:doctor [section] [group=name] [--json]` | Health checks |
| `:tools upgrade\|relocate\|dir` | External tool management |
| `:new linode\|nodebalancer\|volume\|vpc\|lke` | Open create form |

## CLI subcommands

The CLI surface is intentionally tiny — utility functionality lives behind the `:` command palette in the TUI. See [docs/USAGE.md](docs/USAGE.md#cli-subcommands).

| Command | What it does |
|---|---|
| `linode-tui` | Launch the TUI (`--view`, `--layout`, `--pane`, `--read-only`, …) |
| `linode-tui doctor` | Health checks: config, tools, runtime, layout |
| `linode-tui version` | Print version info |
| `linode-tui completion` / `install-completion` | Shell completion |

## `:clear-account` utility

Bulk wipes the active Linode account from inside the TUI. Intended for dev/e2e accounts that need periodic resets.

```
:clear-account dry-run    # preview what would be deleted (no confirmation)
:clear-account            # type your username in the confirm popup to execute
```

Refuses any account whose name contains `prod`. Deletes in dependency-aware order; public images and non-empty buckets are skipped automatically.

## Use as a linode-cli plugin

If you already type `linode-cli`, alias it through to the TUI:

```bash
# ~/.bashrc / ~/.zshrc
linode() {
  if [ "$1" = "tui" ]; then
    shift
    command linode-tui "$@"
  else
    command linode-cli "$@"
  fi
}
```

Now `linode tui`, `linode tui --view lke --focus 12345`, and `linode tui doctor`
all route through this binary while the rest of `linode-cli` keeps working as
normal. For fish:

```fish
function linode
  if test "$argv[1]" = "tui"
    command linode-tui $argv[2..-1]
  else
    command linode-cli $argv
  end
end
```

## Shell completion

```bash
linode-tui completion bash   # source or install to /etc/bash_completion.d
linode-tui completion zsh
linode-tui completion fish
linode-tui completion pwsh
```

## Releases

Cross-platform binaries (Linux / macOS / Windows × amd64/arm64) are produced by [GoReleaser](https://goreleaser.com/) on every tag — see `.goreleaser.yml`. Artifacts and `checksums.txt` are attached to each GitHub release; a `homebrew-tap` formula is updated for `brew install luthermonson/tap/linode-tui`.

Cut a release locally with:

```bash
goreleaser release --snapshot --clean   # dry run (writes to ./dist)
```

## Project layout

```
cmd/linode-tui/        urfave entrypoint
cli/          CLI commands (default = TUI, version, completion)
config/       YAML config: accounts, themes, refresh, tools
linode/       linodego wrapper + token resolution (env > flag > account.Token > op_ref)
onepassword/  `op` CLI shell-out
tools/        external exec (k9s, lazysql): release registry, download, sha256, extract
tui/          Bubble Tea root model, modals, subforms
tui/views/    one file per resource view, generic listView[T] framework
```

See `AGENTS.md` for the architecture overview written for automated coding agents (and humans curious about conventions).

## Development

```bash
go test ./...        # unit tests
go vet ./...
go build ./...
```

CI runs `go test -race`, `go vet`, `go build`, and `golangci-lint` on Linux / macOS / Windows for every push and PR (see `.github/workflows/ci.yml`).

## Tradeoffs worth knowing

- **No headless `list`/`get` subcommands.** The official [linode-cli](https://github.com/linode/linode-cli) covers that ground; this binary is the TUI plus the occasional utility command.
- **Pinned tool versions are baked in.** Override per-tool via `tools.*.version` in config, or live-bump with `:tools upgrade`.
- **`mode: tui` external tools fully replace our terminal** via `tea.ExecProcess`. On exit, the list view resumes its refresh loop. `mode: gui` apps are detached.
- **DBaaS is paywalled.** If an account doesn't have it enabled, the `:databases` view shows a 403.

## License

MIT.
