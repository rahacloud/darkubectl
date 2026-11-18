# 07 · Implementation and verification

## Integration shape

```text
+ cmd/tui.go -------------------------------------------------------+
| TTY guard -> CLI config/auth -> scoped adapter -> run Bubble Tea |
+--------------------------------------+---------------------------+
                                       |
                       + internal/tui -v-------------------------+
                       | message -> Update -> model -> View      |
                       |                 |                        |
                       |                 +-> effect -> result msg |
                       +------------------------------------------+
```

- Keep CLI orchestration in `cmd/tui.go` and UI behavior in `internal/tui`. Pass typed, operation-specific dependencies across that boundary. A proposed `RunBrowser(ctx, deps)` owns the Bubble Tea program. Avoid an import cycle by mapping scope and credentials in the command adapters. Reuse `client.Client` and `appstate` rather than running subprocesses or parsing CLI output. Keep read and later write dependencies distinct.
- Snapshot auth inputs at launch. Construct the account client independently and tenant clients from an explicit org. Reuse the existing REST and Console auth choices. For long sessions with refresh credentials, serialize refresh and acquire current Console access when needed. For an expired explicit access token, show recovery guidance instead of falling through to a different credential. Never place raw credentials in models, effect messages, rendering or errors. Close clients and cancel operations on scope or program end as applicable.
- Keep network work out of `View` and the synchronous part of `Update`. A request key includes dataset and identifiers appropriate to its scope (tenant, namespace where server-relevant, app, build or log target). Attach both scope generation and monotonically increasing request ID to results. Apply only results matching the active generation and latest request for the key. Cancel irrelevant work on view exit, selection commit or scope change. Cap requests per effect and allow only one in-flight request per key. Keep only bounded snapshots tied to the current session.
- Prefer pure formatting and reducer functions for state and projection. Maintain one key/action registry per focus state. Drive footer/help from it. Store selected row by stable ID. Render text via bounded sanitization and cell-aware truncation. Use the alternate screen and ensure terminal restoration on normal exit, error and interrupt.
- Explicitly add `tui: accessBlocked` to `mcpPolicy`. Cover direct and generic dispatch with writes allowed. Keep `describe app -i`, `agent setup`, completion, batch CLI formats and root behavior compatible. Use existing lint configuration and Markdown conventions.

## Implementation tickets

Current implementation status and verification evidence are tracked in [08 Progress](08-progress.md). The tickets and acceptance criteria below remain the target, not a claim that their work is complete.

| PR | Work and dependencies | Ready to merge when |
| --- | --- | --- |
| 0 · Offline preview | Add strict bounded YAML profile parsing and a local demo fixture | The mock browser renders file data with a persistent offline label, ignores ambient credentials and makes no client/API calls |
| 1A-1 · Entry and shell | Register command, terminal guards, config/auth adapters, root model, navigation and MCP block | Help works without TTY, piped launch fails before API, quit restores terminal and both key styles route through one model |
| 1A-2 · Context and Apps | Implement Profile fallback, tenant and namespace pickers, paginated Apps, stable sorting and typed Summary | An API-key fixture can drill into the right duplicate-name app. Old tenant results never bleed into a new scope |
| 1B-1 · Pods and logs | Implement snapshot, exact target selection, previous mode, bounded follow and search | A crash-loop diagnosis works from Apps to previous logs. Console auth is explained and the view stays responsive under slow or failed requests |
| 1B-2 · Builds | Implement Git-only list and selected build detail using the shared log rendering where it fits | A failed Git build can be opened and searched. Ineligible rows cannot invoke build endpoints |
| 1C · Triage | Implement Overview and Activity with shared Apps and independent feeds | Failed feeds, bounded unread counts and verified or unsupported app jumps are distinguishable |
| 2 and 3 · Expansion | Implement [03](03-apps-and-drilldown.md) and [06](06-actions-and-secondary-resources.md) phase by phase | Each feature meets its scope, authorization and verification rules before being linked from the picker |

## Verification matrix

| Fixture or check | Expected result |
| --- | --- |
| Mock profile with valid, empty, duplicate-ID, unknown-field, multi-document and oversized input | Valid profile renders as offline; malformed profiles fail before the alternate screen and parser errors never echo fixture values |
| Mock mode with ambient credentials and explicit connection flags | Ambient credentials remain unused. Explicit connection flags are rejected; the API client constructor and network are never reached |
| Missing credentials, invalid config, non-TTY stdin/stdout, help via pipe, explicit JSON output | Stable plain CLI output or error, no alternate screen on invalid launch, no API on non-TTY path |
| API key + configured org + denied Profile, JWT-only session with no org, local fallbacks and zero tenants | Correct startup paths. No cross-tenant request or context persistence. JWT-only features carry accurate capability text. |
| Duplicate names across namespaces/clusters, removed UUID, pagination and empty views | Correct IDs, no incorrect fallback target, data empty and filter-empty separately, stable selection after updates |
| Slow tenant A response after tenant B becomes active, old app after navigating back, stale refresh failure | Scope/request IDs discard late results. Same-scope stale rows retain timestamp and retry hint. |
| Slow follow, pause/resume, rotated pod, previous empty, duplicate/out-of-window logs, huge record and terminal controls | No overlapping polling, no wrong target, retained buffer respects limits, multiline search and neutralized control text |
| Healthy versus unknown app state, non-Git app, partial feed failure, HTML and mixed-language activity | Accurate state labels, zero accidental build fetches, independent feed errors, readable plain text |
| Write route audit of 1A–1C and MCP invocation even with write opt-in | Only expected read methods/routes, including the auth refresh exchange. Zero calls to semantic mutation routes. TUI blocked through MCP. |
| Phase 2 cancel/review change/double submit/202/timeout | Cancel issues no write, review changes require a new confirmation, submit is single-shot, accepted is distinct from verified and ambiguous is never automatically retried |
| Keyboard in 60×16, 80×24 and 120×32, resize while editing, `NO_COLOR`, terminal restored after Ctrl+C | Footer matches active focus, keyboard alternatives work, no overflow or hidden scope, clean exit |

## Merge checklist

- Unit-test model messages and focus transitions with deterministic dependencies. Use HTTP and WebSocket fixtures for adapter and cancellation behavior. Use a fake clock for polling and backoff, and a PTY smoke test for startup, resizing and restoration. Use fixtures with fake credentials and synthetic logs.
- Run `go build ./...`, `go test -race ./...` and `golangci-lint run ./...` against the Go version in `go.mod`. Format edited Go files and run the repository’s Markdown lint on docs. Run the existing CLI and MCP regression tests alongside the new slices.
- Add README usage for each shipped release and record any observed differences in API capabilities. Mark fixture-only behavior explicitly. Keep this folder's proposed features synchronized with the shipped scope.
