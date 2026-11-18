# 08 · TUI progress report

**Status:** Experimental implementation in progress. Release 1A is incomplete; the remaining releases are planned. This report describes source behavior and local checks, not a release or deployment.

## Implemented

- `darkubectl tui` launches an alternate-screen, read-only browser. It requires terminal input and output, a configured or explicitly supplied tenant, and the existing REST credentials. It rejects positional arguments and non-table output. Help is available without a terminal.
- The command resolves configuration and authentication through the existing tenant-scoped client. Its sole TUI data operation currently lists apps through `Client.ListApps`; it does not offer app writes or secret-reveal actions.
- Apps are grouped by namespace with tree connectors. Namespace rows can be collapsed or expanded with Enter or Left/Right and `h`/`l`; Up/Down and `j`/`k` move through visible rows. Home/End and `g`/`G` jump to the first/last visible row.
- Selecting an app opens a summary from the list snapshot. Manual refresh reloads the list, preserves the selected app ID when possible, and keeps old rows with a stale warning when the refresh fails. The UI has a help panel, terminal size prompt, cell-aware app-name alignment, status glyphs, colors, and reverse-video selection. `NO_COLOR` disables foreground colors.
- MCP classifies `tui` as blocked, including when writes are enabled.

## Remaining for release 1A

- Tenant discovery, Profile failure fallback, session-only tenant switching, and namespace inventory/filtering are not implemented. Startup currently requires a selected tenant.
- Search/filter, sort selection, jump picker, contextual action registry, full focus handling, and the complete 1A keyboard contract remain to be implemented.
- Summary uses the app list snapshot and does not refresh a selected app by ID. It omits several planned fields and does not distinguish omitted desired-replica values from zero.
- The state model does not yet implement all planned loading, empty, refreshing, stale, failed and unavailable states, last-success timestamps, or request generation tracking.
- Long-session token refresh, complete terminal capability handling, and the planned 1A fixture, terminal resize and restoration checks remain open. The terminal color and glyph appearance should be checked in real terminals.

## Later releases

Pods, logs, builds, overview, activity, mutations, registry, tickets, and the broader resource browser have not been implemented. Their design and acceptance criteria remain in [03 Apps](03-apps-and-drilldown.md) through [07 Implementation and verification](07-implementation-and-verification.md).

## Verification recorded so far

- `go build ./...` and `go test -race ./...` passed; the tests include MCP classification and browser model behavior using synthetic data.
- `darkubectl tui --help` was run without an interactive terminal. No live API or interactive terminal acceptance was recorded.
- `golangci-lint run ./...` passed with zero issues using the repository's v2.14.0 version built with Go 1.27. The preinstalled binary was incompatible, so the compatible binary was built separately in `/tmp`.
- `markdownlint-cli2 'tui/*.md'` passed with zero issues.
