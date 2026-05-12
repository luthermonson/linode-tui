# Contributing to linode-tui

Thanks for your interest! This project is a Bubble Tea-based TUI for the Linode API. Most contributions land in `internal/tui/views/` (new resource views) or `internal/tools/` (new external tool integrations).

## Quick start

```bash
git clone https://github.com/linode/tui && cd tui
go test ./...
go vet ./...
go build ./cmd/linode-tui
LINODE_TOKEN=... ./linode-tui
```

Go 1.26+ required (see `go.mod`).

## What goes where

| Area | Path |
|---|---|
| CLI entrypoint, flags, `clear-account` | `internal/cli/` |
| Config file & defaults | `internal/config/` |
| linodego client wrapper, token resolution | `internal/linode/` |
| 1Password `op` shell-out | `internal/onepassword/` |
| External tool runner (k9s, lazysql, …), lazy-install pipeline | `internal/tools/` |
| Bubble Tea root model, modals, forms | `internal/tui/` |
| Resource views (one file per resource) | `internal/tui/views/` |

See `AGENTS.md` for the architectural cheat-sheet — it's written for automated coding agents but humans get the same overview.

## Adding a resource view

1. Create `internal/tui/views/<resource>.go`. Register a `name` and one or more aliases.
2. Define columns, `Lister`, `Rower`, `Matcher`, and an `IDFn` (enables bulk select).
3. Add per-row `Actions` (delete, etc.) and/or `KeyHandlers` (forms).
4. Hot keys re-use the standard set: `/` filter, `y` detail, `space`/`D` bulk, `d` delete, `enter` drill.
5. Run `go vet ./...` and `go test ./internal/tui/views/...`.

## Adding a create / configure flow

1. Implement the `subform` interface (`Init / Update / View / Done / Result / Err`).
2. Use `huh.NewForm` with typed `Validate` funcs; lazy-load required data via `tea.Cmd`s in `Init`.
3. Register in `dispatchNew` (for `:new <resource>`) or wire to a view key handler.
4. Add a happy-path test in `internal/tui/forms_test.go` using `httptest`.

## Tests

- `go test ./...` runs everything (HTTP-mocked).
- `LINODE_TUI_LIVE=1 go test ./internal/livetest/...` runs against a real Linode account using `LINODE_TOKEN`. Read-only — never mutating. Skip unless you have a dev account configured.

## Commit style

- Conventional-style prefixes are welcome (`feat:`, `fix:`, `refactor:`) but not required.
- Keep commits scoped — one feature or fix per commit.
- No `Co-Authored-By: Claude` / `Generated with Claude Code` footers in commits or PRs.

## Code style

- `gofmt` / `goimports` clean; CI enforces `golangci-lint`.
- Errors bubble up; no `log.Fatal` outside `cmd/`.
- Don't reach for new top-level deps without discussion — we deliberately picked the Charm + linodego + urfave stack.
- Themed colors only via `internal/tui/theme`; never hard-coded.
- Destructive actions always go through a confirm modal.

## Releasing

Tags on `main` produce GoReleaser binaries plus a Homebrew tap formula. To preview locally:

```bash
goreleaser release --snapshot --clean
```
