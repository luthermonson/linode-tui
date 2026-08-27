# Contributing to linode-tui

Thanks for your interest! This project is a Bubble Tea-based TUI for the Linode API. Most contributions land in `tui/views/` (new resource views) or `tools/` (new external tool integrations).

## Quick start

```bash
git clone https://github.com/luthermonson/linode-tui && cd linode-tui
go test ./...
go vet ./...
go build ./cmd/linode-tui
LINODE_TOKEN=... ./linode-tui
```

Go 1.26+ required (see `go.mod`).

## What goes where

| Area | Path |
|---|---|
| CLI entrypoint, flags | `cli/` |
| Config file & defaults | `config/` |
| linodego client wrapper, token resolution | `linode/` |
| 1Password `op` shell-out | `onepassword/` |
| External tool runner (k9s, lazysql, …), lazy-install pipeline | `tools/` |
| Bubble Tea root model, modals, forms | `tui/` |
| Resource views (one file per resource) | `tui/views/` |

See `AGENTS.md` for the architectural cheat-sheet — it's written for automated coding agents but humans get the same overview.

## Adding a resource view

1. Create `tui/views/<resource>.go`. Register a `name` and one or more aliases.
2. Define columns, `Lister`, `Rower`, `Matcher`, and an `IDFn` (enables bulk select).
3. Add per-row `Actions` (delete, etc.) and/or `KeyHandlers` (forms).
4. Hot keys re-use the standard set: `/` filter, `y` detail, `space`/`D` bulk, `d` delete, `enter` drill.
5. Run `go vet ./...` and `go test ./tui/views/...`.

## Adding a create / configure flow

1. Implement the `subform` interface (`Init / Update / View / Done / Result / Err`).
2. Use `huh.NewForm` with typed `Validate` funcs; lazy-load required data via `tea.Cmd`s in `Init`.
3. Register in `dispatchNew` (for `:new <resource>`) or wire to a view key handler.
4. Add a happy-path test in `tui/forms_test.go` using `httptest`.

## Tests

- `go test ./...` runs everything (HTTP-mocked).
- `LINODE_TUI_LIVE=1 go test ./livetest/...` runs the read-only suite against a real Linode account using `LINODE_TOKEN`. Skip unless you have a dev account configured.
- `LINODE_TUI_LIVE=1 LINODE_TUI_LIVE_MUTATE=1 go test ./livetest/...` additionally runs `livetest/mutate_test.go`, which creates and deletes real resources (e.g. spins up and tears down a Linode). Both env vars are required; only set this on a throwaway/dev account.

## Commit style

- Conventional-style prefixes are welcome (`feat:`, `fix:`, `refactor:`) but not required.
- Keep commits scoped — one feature or fix per commit.
- No `Co-Authored-By: Claude` / `Generated with Claude Code` footers in commits or PRs.

## Code style

- `gofmt` / `goimports` clean; CI enforces `golangci-lint`.
- Errors bubble up; no `log.Fatal` outside `cmd/`.
- Don't reach for new top-level deps without discussion — we deliberately picked the Charm + linodego + urfave stack.
- Themed colors only via `tui/theme`; never hard-coded.
- Destructive actions always go through a confirm modal.

## Releasing

Tags on `main` (`git tag vX.Y.Z && git push origin vX.Y.Z`) run GoReleaser via `.github/workflows/release.yml`. The resulting GitHub release is created **as a draft** (`release.draft: true` in `.goreleaser.yml`) — a maintainer needs to review it and click "Publish release" before it's visible. Artifacts, `checksums.txt`, and SBOMs are attached to the draft.

A Homebrew tap formula is planned but currently disabled: the `brews` block in `.goreleaser.yml` is commented out until a `HOMEBREW_TAP_TOKEN` secret (able to push to the separate `homebrew-tap` repo) is configured.

To preview a build locally without touching GitHub:

```bash
goreleaser release --snapshot --clean
```
