# Usage

A comprehensive reference for `linode-tui`. Quick links:

- [Two surfaces, one binary](#two-surfaces-one-binary)
- [Configuration](#configuration)
- [TUI workflow](#tui-workflow)
- [Command bar verbs](#command-bar-verbs)
- [CLI subcommands](#cli-subcommands)
- [Bookmarks, layouts, and the audit log](#bookmarks-layouts-and-the-audit-log)
- [Health checks](#health-checks)
- [Caching and storage](#caching-and-storage)
- [Telemetry](#telemetry)

---

## Two surfaces, one binary

`linode-tui` is an **interactive TUI** — built on Bubble Tea, k9s-style command palette via `:`. Utility gestures (config inspection, audit log, layouts, bookmarks, cache) live behind palette commands inside the TUI; the headless CLI surface is deliberately limited to `doctor`, `version`, and shell completion.

---

## Configuration

Default path: `~/.config/linode-tui/config.yaml` (`%AppData%\linode-tui\config.yaml` on Windows). Override with `--config <path>` on any subcommand.

Inspect what's currently in effect (in the TUI):

```
:config path        # just the resolved path
:config show        # full YAML (Token fields redacted)
```

Validate it with `:validate`, or headlessly via `linode-tui doctor` (config
validity is one of its check groups).

### Key config fields

```yaml
default_account: dev          # picks accounts.<name> as the active one
active_theme: dark            # dark | light | dracula | solarized-light
refresh: 2s                   # global default for view refresh

refresh_overrides:            # per-view refresh interval (overrides `refresh`)
  events: 2s
  instances: 5s
  images: 60s

accounts:
  dev:
    op_ref: "op://Work/linode-dev/credential"
    theme: light              # per-account theme override
    refresh_overrides:        # per-account refresh overrides (highest priority)
      events: 1s
    bookmarks:                # per-account bookmark scope (see below)
      instances: ["12345"]
    layout_digests:           # per-account layout pin (sha256)
      default: "abc123…"
  prod:
    op_ref: "op://Work/linode-prod/credential"
    theme: dracula

bookmarks:                    # global bookmark scope (fallback when account has none)
  instances: ["67890"]

layouts:                      # saved pane layouts (see :layout)
  dev:
    primary: instances
    secondary: events
    ratio: 0.5

layout_digests:               # global digest pins for layout import-from drift detection
  dev: "abc123…"

audit_retention_days: 90      # prune audit entries older than this on launch (0 = forever)

stats_enabled: false          # opt-in: persist session counters to disk
stats_endpoint: ""            # opt-in: where :stats post sends the local snapshot

fold_char: "+"                # prefix for folded pane labels when terminal is narrow
read_only: false              # blocks every mutating command-bar action this session
```

### Refresh resolution order

When a listView decides how often to tick:

1. `Accounts[active].RefreshOverrides[viewName]` — per-account, highest priority
2. `RefreshOverrides[viewName]` — config-wide
3. `Refresh` — global default
4. `2s` — built-in fallback

---

## TUI workflow

```bash
linode-tui                     # launch
linode-tui --view lke          # open directly into a resource view
linode-tui --layout dev        # launch with a named saved layout
linode-tui --no-layout         # launch without the auto-loaded default layout
linode-tui --read-only         # session-wide block on mutating actions
```

### Key bindings

| Key | Action |
|---|---|
| `:` | Open command bar |
| `?` | Toggle help overlay (then any key filters; `esc` clears filter) |
| `/` | Filter current view |
| `r` / `ctrl+r` | Refresh now |
| `ctrl+y` | Replay / undo most recent audit entry (only shown when the audit log has entries) |
| `enter` | Drill into selected row |
| `space` | Toggle row selection |
| `D` | Bulk-delete selected rows (one confirm) |
| `*` | Bookmark / unbookmark current row |
| `tab` | Cycle focus across split panes |
| `esc` | Cancel modal / clear filter / pop drill-in |
| `ctrl+c` | Quit |
| `↑/↓` `j/k` | Move cursor |

### Layouts

A layout = which view goes in each of up to 4 panes plus the split ratios. You can:

- **Save** the current shape: `:layout save dev` (or `:layout save default` to make it auto-load)
- **Load** a saved one: `:layout load dev`
- **List** them with descriptions: `:layout list`
- **Rename / delete**: `:layout rename old new`, `:layout delete old`
- **Diff**: `:layout diff` (use the CLI for clean output)
- **Export / import** to a YAML file: `:layout export dev ~/dev.yaml`, `:layout import ~/dev.yaml`
- **Import from URL** with optional sha256 pin: `:layout import-from https://example.com/dev.yaml?sha256=…`
- **Share** a pinned URL: `:layout share dev https://example.com/dev.yaml` → opens a modal with the digest already appended
- **Pin** an existing URL: `:layout pin dev https://example.com/dev.yaml` → same idea, prints the import-ready URL

### Splitting and folding

`:split <view>` adds a secondary pane next to the current one. `:unsplit` collapses back to one pane. The split ratio is per-pair and remembered. When the terminal is too small, panes fold into a `+secondary +tertiary` divider; tune the thresholds with `fold_width_secondary`, `fold_width_tertiary`, `fold_height_quaternary`, and change the marker with `:fold-char ⤬` (or `:fold-char reset`).

---

## Command bar verbs

### Resource navigation

| Verb | Effect |
|---|---|
| `:linodes` / `:instances` / `:inst` | Switch to Linodes view |
| `:databases` / `:dbaas` | DBaaS |
| `:lke` / `:k8s` | LKE clusters |
| `:volumes` / `:vol` | Block Storage volumes |
| `:nodebalancers` / `:nb` | NodeBalancers |
| `:firewalls` / `:fw` | Firewalls |
| `:images` / `:img` | Images |
| `:domains` / `:dns` | Domains |
| `:vpcs` / `:vpc` | VPCs |
| `:placementgroups` / `:pg` | Placement Groups |
| `:stackscripts` / `:ss` | StackScripts |
| `:objectstorage` / `:s3` | Object Storage |
| `:events` / `:ev` | Events feed |
| `:watchlist` / `:starred` | Bookmarked rows across all kinds, with drift detection |
| `:fanout_instances` (etc.) | Multi-account fan-out views — list all bookmarked accounts' resources |
| `:open <resource> [id]` | Inline JSON view of one resource in a detail modal |
| `:diff snapshot <resource> <id> [@N]` | Compare current resource JSON against a stored snapshot |

### Accounts and themes

| Verb | Effect |
|---|---|
| `:account` | List configured accounts |
| `:account <name>` | Switch active account (re-resolves token, rebuilds client) |
| `:theme <name>` | Switch theme live: dark / light / dracula / solarized-light |
| `:theme list` | Show themes with color swatches |
| `:theme account <name> <theme>` | Set per-account theme override |

### Refresh

| Verb | Effect |
|---|---|
| `:refresh` | Show current global + override settings in a modal |
| `:refresh <dur>` | Set global refresh (e.g. `:refresh 5s`) |
| `:refresh <view> <dur>` | Set per-view override (`:refresh events 2s`) |
| `:refresh <view> off` | Clear a per-view override |
| `:refresh defaults` | Apply the opinionated preset (events=2s, instances=5s, images=60s, …) |

### Layout, panes, and ratios

| Verb | Effect |
|---|---|
| `:layout save\|load\|list\|delete\|rename` `<name>` | Manage saved layouts |
| `:layout export <name> <path> [--json]` | Write a layout to a file |
| `:layout import <path>` | Load a layout from a file |
| `:layout import-from <url> [name]` | Fetch a layout over HTTPS, with optional sha256 pin |
| `:layout pin <name> <url>` | Print the import-ready URL with sha256 appended |
| `:layout share <name> <url> [open]` | Same idea, optionally launch the URL in the browser |
| `:pane <slot> <view>` | Swap a single pane (primary / secondary / tertiary / quaternary) |
| `:split <view>` / `:unsplit` | Two-pane split / collapse |
| `:fold-char <ch\|reset>` | Set or clear the folded-pane prefix |

### Bookmarks

| Verb | Effect |
|---|---|
| `:bookmark` | Show current scope (`global` or `account=<name>`) + usage |
| `:bookmark list` | Counts per resource kind |
| `:bookmark scope global\|account` | Switch where new `*` marks are stored |
| `:bookmark export <path>` | Write current bookmarks to YAML |
| `:bookmark import <path> [merge]` | Replace (default) or union with existing |
| `:bookmark migrate [--dry-run]` | Move global bookmarks into the active account |
| `:bookmark mv <kind> <from-id> <to-id>` | Rename a bookmark in-place |
| `:bookmark clear <kind>` | Drop all bookmarks for one kind (typed confirm) |

### Audit

| Verb | Effect |
|---|---|
| `:audit` / `:audit tail [n] [account=<name>] [--err]` | Recent entries |
| `:audit recent [n] [--err] [--no-marker]` | Styled view with colored dots, bold for today |
| `:audit grep <pattern> [--err] [account=<name>]` | Substring filter across action/kind/id/label/err |
| `:audit purge <dur> [kind]` | Drop entries older than `<dur>`, optionally filtered by kind |
| `:audit clear` | Wipe the entire log (typed confirm) |
| `:undo` / `ctrl+y` | Inspect or execute the inverse of the most recent mutating action |

### Misc

| Verb | Effect |
|---|---|
| `:doctor [<section>] [group=<g>] [--json] [fix]` | Run health checks; `fix` cleans orphan .tmp files |
| `:validate` | Re-run validate-config warnings against in-memory cfg |
| `:config show` / `:config path` | View redacted config / path |
| `:cache size` / `:cache prune <subdir\|all>` | Inspect and trim `~/.cache/linode-tui` |
| `:tools dir` / `:tools upgrade [kind]` / `:tools relocate <dir>` | External tool management |
| `:new linode\|nodebalancer\|volume\|vpc\|lke` | Open a create form |
| `:read-only` | Toggle the session-wide mutation block |
| `:stats` / `:stats post` / `:stats reset [all]` | View / send / clear local counters |

---

## CLI subcommands

The CLI surface is intentionally tiny — everything else lives behind the `:`
command palette in the TUI. Headless reads (list/get with table/csv/json
output) are out of scope; the official
[linode-cli](https://github.com/linode/linode-cli) covers that ground.

```bash
linode-tui version
linode-tui doctor                                      # all checks
linode-tui doctor --strict                             # exit non-zero on optional warnings
linode-tui doctor --quiet                              # hide successes
linode-tui doctor --section token --section op         # filter by check name
linode-tui doctor --group tools --group runtime        # filter by group
linode-tui doctor --json                               # machine-readable
linode-tui doctor --watch 5s                           # repeat; ctrl+c exits
linode-tui doctor --fix                                # remove orphan *.tmp files
linode-tui completion bash|zsh|fish|pwsh               # print completion script
linode-tui install-completion [shell]                  # write it to the standard location
```

In-TUI equivalents for the old subcommands: `:audit`, `:layout`, `:bookmark`,
`:cache`, `:config`, `:validate`, `:replay-last`, `:replay-from`, `:stats`.

> First run with no token? If a linode-cli config exists at
> `~/.config/linode-cli`, the TUI offers to import its accounts automatically.

---

## Bookmarks, layouts, and the audit log

These three local stores power most of the workflow tools added on top of the resource views.

### Bookmarks

Star (`*`) any row in any view to mark it. Bookmarked rows:

- Sort to the top of their view
- Show up in the **`:watchlist`** synthetic view, fanned out across all kinds in parallel
- Get a JSON snapshot taken at bookmark time; the watchlist's `Δ` column lights up when the live JSON differs

Scope: global by default. When you switch to per-account scope (`:bookmark scope account`), reads and writes use `accounts[<name>].bookmarks` instead. Migrate one to the other with `:bookmark migrate`; doctor warns when both layers are non-empty (`bookmark-scope` check).

### Layouts

A named layout captures up to 4 pane assignments + split ratios. Use them as workspace templates ("the prod dashboard", "the on-call view"). Share them by serving the YAML over HTTPS with `?sha256=<digest>` pinned in the URL:

1. `:layout fingerprint dev` → get the sha256
2. Append to your URL: `https://example.com/dev.yaml?sha256=<digest>`
3. Recipient runs `:layout import-from <url>` — verified before save

A digest is persisted (per-account and globally) so a re-fetch from an unpinned URL warns when upstream changed.

### Audit log

Every mutating action — create, delete, configure, layout edit, bookmark move, etc. — appends one JSON line to `~/.cache/linode-tui/audit.log`. The log:

- Rotates at 2 MiB (`.log` → `.log.1`, single generation kept)
- Auto-prunes entries older than `audit_retention_days` on launch (default 90, set to 0 to keep forever)
- Surfaces a one-line "recent: …" banner at startup with the last 3 entries colored by age/status
- Powers `ctrl+y` (replay-last), `:undo`, `:replay-from <date>`, and the watchlist drift markers

Useful gestures (in the TUI):

```
:audit grep <substring> --err          # what failed recently
:audit grep <substring> account=prod   # what touched prod
:replay-last execute                   # auto-undo last create
:replay-from 2026-04-01 execute
```

---

## Health checks

`linode-tui doctor` (or `:doctor` in the TUI) runs a set of grouped checks:

| Group | Check | What it watches |
|---|---|---|
| `config` | `config` | Config file parses and resolves |
| `config` | `read-only` | `read_only: true` is set (informational) |
| `tools` | `op` / `k9s` / `lazysql` / `ssh` | External binary on PATH |
| `runtime` | `token` | LINODE_TOKEN / account.Token / op_ref resolves |
| `runtime` | `cache` | `~/.cache/linode-tui` exists and is writable; shows total size |
| `runtime` | `audit-log` | Warns when `audit.log` >80% of 2 MiB rotation threshold |
| `runtime` | `stale-cache` | Orphan `.tmp` files left by interrupted writes (run `--fix` to clean) |
| `runtime` | `refresh-default` | `refresh: 0` falls back silently to 2s |
| `runtime` | `refresh` | `refresh_overrides` keys all map to registered views |
| `runtime` | `refresh-collision` | Same view has different account vs global durations |
| `runtime` | `bookmark-scope` | Both global and account bookmarks are non-empty (one is shadowed) |
| `layout` | `layout-digests` | Account vs global digest disagree, or only one layer pinned |

Each failed/optional check carries an inline `Suggestion` string in both the CLI text output and the TUI modal — copy-paste hints like `brew install 1password-cli` or `export LINODE_TOKEN=…`.

---

## Caching and storage

| Path | What's in it |
|---|---|
| `~/.config/linode-tui/config.yaml` | All configuration |
| `~/.cache/linode-tui/audit.log` | Mutating-action audit log (rotates at 2 MiB) |
| `~/.cache/linode-tui/audit.log.1` | Previous generation after rotation |
| `~/.cache/linode-tui/layouts/<name>.yaml` | Cached YAML from each `:layout import-from` |
| `~/.cache/linode-tui/snapshots/<kind>/<id>/` | Resource snapshots per bookmark (max 10 versions) |
| `~/.cache/linode-tui/debug.log` | Debug log when `--debug` is set |
| `~/.cache/linode-tui/stats.json` | Persisted session counters (only when `stats_enabled: true`) |

Inspect: `:cache size`. Prune one slice: `:cache prune layouts`. Wipe everything: `:cache prune all` (typed confirm).

---

## Telemetry

Off by default. When you opt in with `stats_enabled: true`, the TUI keeps in-memory counters of common actions (`linode:create`, `audit:pruned_today`, etc.) and persists them to disk on update.

If you also set `stats_endpoint: https://example.com/stats`, the `:stats post` command sends a snapshot:

```json
{
  "counters":       { … session counters … },
  "build":          {"version": "v0.1.0", "commit": "abc", "os": "linux", "arch": "amd64"},
  "uptime_seconds": 1234,
  "config_signals": {
    "refresh_overrides": 3,
    "saved_layouts":     5,
    "accounts":          2,
    "bookmark_kinds":    4
  },
  "timestamp": "2026-05-12T12:00:00Z"
}
```

No tokens, no account names, no host data. Only counts and aggregate config shape. The post is manual — there's no background loop.

---

## Recipes

### "I want to see my prod fleet at a glance"

```bash
linode-tui                                  # in the TUI:
:account prod
:linodes        # then * on the rows that matter
:fanout_instances    # if you have multiple prod accounts
:layout save prod-dashboard
:layout save default                         # auto-load on next launch
```

### "I want to know if anyone touched my LKE cluster"

```
:lke              # then * on it
```
Then every refresh, the watchlist Δ column lights up when the live JSON differs from the bookmarked snapshot. `:diff snapshot lke <id> @1` to see what changed.

### "I want my CI to fail if config is broken"

```bash
linode-tui doctor --strict --quiet
linode-tui doctor --section token --strict --quiet
```

### "Watch health checks on a dashboard"

```bash
linode-tui doctor --watch 30s --json --no-color | your-dashboard
```

### "Share a layout with my teammates"

```
# you (in the TUI):
:layout export-all ./layouts
# upload layouts/dev.yaml somewhere over HTTPS
:layout pin dev https://example.com/layouts/dev.yaml
# → outputs: https://example.com/layouts/dev.yaml?sha256=…

# them:
:layout import-from https://example.com/layouts/dev.yaml?sha256=…
```

### "Quickly wipe a dev account before another e2e run"

In the TUI, with the dev account active:

```
:clear-account dry-run    # preview
:clear-account            # typed-username confirm, then deletes everything
# refuses any account whose name contains "prod"
```
