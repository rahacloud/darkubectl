# 08 · TUI progress report

**Status:** Experimental implementation in progress. Release 1A is incomplete; the remaining releases are planned. This report describes source behavior and local checks, not a release or deployment.

## Implemented

- `darkubectl tui` launches an alternate-screen, read-only browser. It requires terminal input and output. Live mode needs existing REST credentials and resolves a tenant from flags/config or the account chooser; fixture mode uses only local YAML and needs neither. The command rejects positional arguments and non-table output. Help is available without a terminal.
- Live mode resolves configuration and authentication through the existing client adapters. It loads account Profile for discovery and lists apps through a tenant-scoped `Client.ListApps`. Fixture mode branches before config/auth resolution and client construction, and renders only the mock profile's apps. Neither mode offers app writes or secret-reveal actions.
- Live mode loads account Profile independently of Apps. With no tenant, the TUI offers Profile memberships; if Profile fails, it offers local config slugs. A configured tenant remains usable if Profile fails, and is marked unverified if Profile succeeds without that tenant. The `t` modal switches tenant for this run only and clears prior app data before loading the new scope.
- `n` opens a fuzzy namespace picker. It lazily loads complete inventory through `Client.ListNamespaces`, keeps distinct namespace IDs, shows per-namespace app counts including empty projects, and scopes the Apps tree without a second app request. If inventory is unavailable, it falls back to namespaces in the loaded app snapshot and labels the omission of empty projects. Mock files may include tenant-specific `namespaces` for offline previews of empty projects.
- The tenant chooser is a centered modal with fixed-width rows and a visible check glyph for the active tenant. The header shows a tenant breadcrumb, adding namespace and app only in app details, with a pulsing blue live marker that becomes an animated spinner during Profile or Apps requests; mock mode retains an offline marker.
- `/` opens an inline app filter after the breadcrumb. It applies case-insensitive fuzzy subsequence matching as text is entered; `namespace/appname` scopes both fields, while an app-only query searches all namespaces. Up/Down moves through filtered rows without a commit step; Enter opens the selected match, and Esc cancels editing or clears the query.
- `--mock-file PATH` loads a strict version-1 YAML fixture, with either one tenant or multiple tenant-specific app snapshots. Unknown fields and multiple documents are rejected; input is capped at 1 MiB and 1000 total apps. Parser errors omit YAML snippets. The browser labels the source `MOCK PROFILE · OFFLINE`; `demo/mock-profile.yaml` contains two fictional tenants for exercising session-only tenant switching.
- Apps are grouped by namespace with tree connectors and per-namespace app counts (counts reflect current filter matches). Namespace rows can be collapsed or expanded with Enter or Left/Right and `h`/`l`; Up/Down and `j`/`k` move through visible rows. Home/End and `g`/`G` jump to the first/last visible row.
- `s` opens sort selection for name, namespace, state, and last updated. Up/Down selects a field and Left/Right sets ascending or descending order. Sorting preserves the selected app ID.
- Apps and Summary remain visible as side-by-side panes. Up/Down moves the selected app and updates its summary from the loaded snapshot. The summary heading includes the app name; CPU, RAM, and applicable disk usage appear indented under Plan with icons. When usage samples are unavailable, show the configured total without a fabricated used value. Timestamps render in local time, and creation methods use an icon with a short label. Summary reads actual CPU/RAM/disk metrics against plan capacity when available. It shows internal and external hosts together under Domains, with OSC 8 clickable links and multiple hosts on separate aligned rows. Right/`l` or Enter on an app focuses Summary and fetches the typed app by UUID; Left/`h` from Summary returns to the tree. On a namespace row, Left/`h` collapses and Right/`l` expands it. The contextual key hints stay pinned to the bottom row.
- Focusing Summary refreshes its typed app by ID in live mode, while fixture mode uses its local app snapshot. The summary shows tenant, namespace/cluster, ID, state and description, desired replicas, plan/resources, creation method, image, domains, and update time. `r` refreshes the focused view. Failed detail reads retain the last known data and show an error; a successful 404 remains a detail error and never resolves by name.
- Manual list refresh preserves the selected app ID when possible, and keeps old rows with a stale warning when the refresh fails. The UI has a help panel, terminal size prompt, cell-aware app-name alignment, status glyphs, colors, and reverse-video selection. `NO_COLOR` disables foreground colors.
- MCP classifies `tui` as blocked, including when writes are enabled.

## Remaining for release 1A

- Startup discovers tenants from Profile or falls back to local config.
- Jump picker, contextual action registry, full focus handling, and the complete 1A keyboard contract remain to be implemented.
- Summary still cannot distinguish an omitted desired-replica value from zero because the shared typed app field has no presence information.
- The state model does not yet implement all planned loading, empty, refreshing, stale, failed and unavailable states, last-success timestamps, or request generation tracking.
- Long-session token refresh, complete terminal capability handling, and terminal resize/restoration checks remain open. The user has verified the multi-tenant mock in an interactive terminal; live API acceptance remains unverified.

## Later releases

Pods, logs, builds, overview, activity, mutations, registry, tickets, and the broader resource browser have not been implemented. Their design and acceptance criteria remain in [03 Apps](03-apps-and-drilldown.md) through [07 Implementation and verification](07-implementation-and-verification.md).

## Verification recorded so far

- Before the offline profile change, `go build ./...` and `go test -race ./...` passed; those results cover the earlier implementation state.
- `darkubectl tui --help` was run without an interactive terminal. The user verified the multi-tenant mock in an interactive terminal; no live API acceptance was recorded.
- Current offline profile validation: `go build ./...`, `go test ./internal/tui`, and the focused mock-mode CLI flag test passed. The broader `go test ./cmd ./internal/tui` run is blocked by the sandbox denying localhost listeners in an existing `httptest` case (`cmd/wait_internal_test.go`). Markdown lint and `git diff --check` pass. The tenant discovery and fuzzy-filter changes compile with `go build ./...`; the filter has not yet been exercised interactively, and the live API flow remains unverified.
- Mock profile tests load the checked-in demo file, exercise offline rendering, and reject unknown fields and multiple YAML documents. The user confirmed the multi-tenant mock runs and looks good in an interactive terminal.
- `golangci-lint run ./...` passed with zero issues using the repository's v2.14.0 version built with Go 1.27. The preinstalled binary was incompatible, so the compatible binary was built separately in `/tmp`.
- `markdownlint-cli2 'tui/*.md'` passed with zero issues.
