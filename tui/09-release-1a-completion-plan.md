# 09 · Release 1A completion plan

## Goal and boundary

Complete the read-only interactive shell so a user can launch the TUI, choose a tenant, find the right app, inspect its Summary, refresh safely, and recover from loading or request failures. Preserve the existing mock profile as the repeatable offline path.

Release 1A remains a read-only shell for data access. The already implemented Restart and Delete lifecycle actions remain gated by `--allow-writes`, use their confirmation UI, and are covered here by a regression/security audit. The current browser also includes a read-only pod snapshot in Summary and a read-only Scaling view for replica/HPA settings. Scaling writes, disk, domain, Env, Terminal, Logs, Builds, global pages, and additional mutations remain later-release work. No credentials or secret values may enter model state, logs, errors, or rendered output.

## Starting point

The progress report records the launch path, tenant and namespace selection, Apps tree, fuzzy filtering, sorting, Summary with pod snapshots and scrolling, the read-only Scaling view, mock profile, contextual Restart/Delete, help modal, C1 keyboard/style audit, C2 request lifecycle, and C3 Summary semantics as implemented. Reconcile this document with the current source before starting each slice: the progress report can lag recent commits.

## Work sequence

### 1A-C1 · Audit the 1A contract and close keyboard gaps

**Status: Complete.** The responsive footer and help modal now share a state-aware hint registry; app-tree paging, modal key isolation, printable filter input, action scoping, and visual consistency are covered by focused model tests. `[` / `]` cycles Summary, Usage, and Scaling without changing app selection.

- Compare the key handling in `internal/tui` with the input contract in [02 Shell and context](02-shell-and-context.md), including global keys, each pane, inline filter, picker, sort UI, help modal, and action confirmations.
- Audit the visual system across those same states: selected and focused rows, active panes, headings, borders, status glyphs/colors, filter/sort indicators, empty/loading/error copy, modal spacing, and footer hints. Make selection/highlight behavior consistent while keeping keyboard focus distinct from the currently selected app where those differ.
- Keep presentation ready for later themes: route colors and reusable styles through a small semantic palette/theme boundary (for example, selected, focus, accent, muted, healthy, warning, error, border), avoid scattering raw color choices through rendering logic, and keep layout/interaction code independent of a specific palette. Do not add theme switching or a theme picker in 1A.
- Check `NO_COLOR`, terminal background assumptions, glyph fallback/readability, narrow widths, and text contrast when reviewing the shared palette. Preserve the current visual identity while making a future alternate theme a palette change rather than a renderer rewrite.
- Make a single source of truth for keys available in the current focus/state. Render that registry in the footer and help modal; disabled or unavailable features must not advertise active shortcuts.
- Preserve both conventional keys and vim-style alternatives where specified. Keep text entry literal for printable keys; prioritize the innermost active input/modal before global shortcuts.
- Ensure focus movement and `[`/`]` app-view navigation cannot accidentally expand/collapse the namespace tree, change tenant, or trigger a write. Follow the app-view contract for unavailable views (skip unavailable entries or clearly explain them; do not land on a dead destination).
- Add model-level coverage for each focus/overlay state and for key collisions, including no selected app, namespace selection, filtered results, and narrow-terminal resize.

**Acceptance:** Every enabled shortcut is discoverable in the current footer/help; every documented key routes correctly for each relevant focus; printable input is preserved; no absent feature appears actionable; both key styles behave consistently. Highlight, focus, status and modal styles are visually consistent, and colors/styles are accessed through semantic tokens so a later theme can be introduced without changing view logic.

### 1A-C2 · Make request state explicit and race-safe

**Status: Complete.** Each dataset now has an explicit request state and request/scope stamp. Request-scoped contexts cancel superseded work; reducers reject stale tenant, app, generation, and request IDs. Apps retain same-scope rows on refresh failure, detail and Usage run independently, and stale snapshots expose last-success time and retry guidance. Focused model tests cover state transitions, Profile retry, empty/partial namespaces, tenant A→B→A, selection cancellation, stale results, and Usage failure isolation.

**Implementation target:** Replace the current collection of loosely related `loading`/`err` booleans with explicit per-dataset lifecycle state. The current effect/result seams are `load`, `loadProfile`, `loadNamespaces`, and `loadSelectedSummary` in `internal/tui/browser.go`; result messages currently carry only partial tenant/app identity. `cmd/tui.go` already passes contexts into its client adapters, but `RunBrowser` closes over one root context instead of passing a request-scoped context through each effect.

1. **Define the lifecycle model.** Add a typed status (`Idle`, `Loading`, `Ready`, `Empty`, `Refreshing`, `Stale`, `Failed`, `Unavailable`) and a state record with `RequestID`, scope identity, `StartedAt`, `LastSuccessAt`, and sanitized error/retry information. Keep payloads in their typed snapshots. Use `Empty` only for a successful empty response; represent a filter with zero matches as a view projection over a successful snapshot.
2. **Give every request a stable key and currentness stamp.** Cover account Profile; tenant-scoped Apps and namespace inventory; selected-app detail keyed by tenant plus app ID; and Usage metrics keyed by tenant plus app ID. Each effect gets a monotonically increasing ID. Tenant changes advance a tenant-scope generation; changing the selected app advances the detail/usage generation. Applying a result requires its generation and request ID to still match. This prevents an older same-tenant refresh or A→B→A app selection from winning merely because its tenant/app ID matches again.
3. **Cancel obsolete work where the adapter supports it.** Change browser dependencies to receive the request context rather than hiding the root context in closures. Cancel requests on tenant switch, selected-app change for detail/usage, superseding refresh, and program exit. Continue to reject stale result messages even when cancellation races with a completed response. Permit at most one request per dataset key; manual refresh remains the only refresh policy in 1A.
4. **Keep the datasets independent.** Apps, Profile, namespace inventory, Summary detail, and Usage each transition separately. A namespace fallback built from Apps is explicitly partial and says empty projects may be missing; it is not a successful complete inventory. A failed Usage query must not mark Summary detail failed. An unavailable capability (for example, namespace inventory not exposed to this auth mode) is distinct from a transient request failure.
5. **Render states without discarding useful data.** First load shows Loading then Ready/Empty/Failed/Unavailable. Refresh keeps the same-scope snapshot visible and shows Refreshing; success replaces it and updates `LastSuccessAt`; failure retains it as Stale with the last-success time and retry hint. On tenant change clear old tenant payloads immediately and show the new scope loading. Detail and Usage follow the selected stable app ID independently. `r` reissues data needed by the focused view (Summary detail and Usage independently when Summary is active), while tenant/Profile retry remains scoped to Profile.
6. **Test reducers and race behavior deterministically.** Inject typed result messages directly for request IDs and scope generations; use controllable fake dependencies/channels for context cancellation. Cover Profile retry, Apps first load/empty/refresh/stale, namespace complete/empty/fallback/unavailable, Summary and Usage independently, out-of-order duplicate refreshes, tenant A→B and A→B→A, app A→B→A, selection changes while detail is loading, and stale results after cancellation. No sleeps or live API are needed for reducer tests.

**Acceptance:** No result from an old scope or superseded request can overwrite current UI. Every dataset visibly distinguishes loading, ready, empty, refreshing, stale, failed, and unavailable where applicable. Same-scope stale data remains visible with its last-success time; new scopes never show old tenant data. Retries are explicit and duplicate in-flight work is suppressed.

### 1A-C3 · Correct Summary data semantics and refresh behavior

**Status: Implementation complete.** `App.Replicas` remains an `int`; `ReplicasPresent` is populated by JSON decoding for list and detail payloads. Mock fixtures require non-null replicas and preserve explicit zero. Summary verifies the selected UUID, distinguishes unknown from zero, puts `svc.internalAddress` in its own row with an unambiguous service port, and keeps custom/external/service external addresses under Domains. It conditionally renders the primary Service port, External IP, readiness probe, command and input names; it does not list every port. Input values are discarded. Focused TUI, JSON-shape, fixture, and build checks pass; the endpoint-level `httptest` cannot be run in this sandbox because it denies localhost listeners.

**Dependency:** Complete C2 first so Summary and Usage results have independent request state and stale-result rejection. C3 changes what the typed data means and how the Summary presents it; it does not add new data endpoints.

1. **Preserve replica presence without breaking existing consumers.** `client.App.Replicas` is currently an `int`, so JSON omission becomes `0`; CLI code also reads that integer directly. First establish API behavior from checked-in/controlled response fixtures. If omission is meaningful, keep the existing numeric field for compatibility and add an explicit presence bit in the typed app model, populated by JSON decoding for both `ListApps` and `GetAppTyped`. Treat absent and `null` as unknown, and explicit `0` as known zero. Avoid changing unrelated CLI output or create-spec replica types.
2. **Make fixture input honest.** The mock YAML struct currently also uses an `int`, so an omitted `replicas` entry becomes zero. Require the field in the fixture contract, reject both omission and `null`, and keep explicit zero valid. Set the typed presence marker when converting the mock fixture. Update the schema/docs and add absent, null, zero, and positive-value fixtures.
3. **Bind Summary to the exact selected app.** Continue fetching with `GetAppTyped(ctx, id)`; never resolve by name. When the response arrives, require both the requested tenant/app generation and the returned app ID to match the selected target. A 404, mismatched ID, or decode failure remains a detail error and must not fall back to another app with the same name in another namespace. Cover duplicate names across namespaces and selection changes during a request.
4. **Define the display rules for partial fields.** Desired replicas show the integer only when known, including `0`; unknown renders as `-` (or the agreed empty marker). Empty plan retains the Plan label without a fabricated value. CPU/RAM/Disk show measured `used / configured total` only when a valid sample exists; when samples are absent, show the configured total alone. Partial metric results remain per-resource and must not imply that missing samples equal zero. Keep labels and values visually distinct.
5. **Verify summary projections against API-shaped fixtures.** Cover state text/type fallback, description, plan and resource totals, timestamps, image, creation method, zero/unknown replicas, and missing/partial/full metric samples. For Domains, deduplicate case-insensitively and preserve clickable OSC 8 links; confirm which typed API fields carry custom, external, and internal hosts before adding any projection, and test each supported source rather than inferring a host from an app name.
6. **Keep refresh selection stable and scoped.** Apps refresh preserves selection by stable ID and retains same-scope rows on failure (C2 state handling). Summary refresh reads the exact selected ID; changing selected app invalidates the previous Summary/Usage immediately. A successful detail response updates only that app, and a failed detail/usage request does not overwrite valid data from a different selection.

**Acceptance:** Summary distinguishes unknown replicas from explicitly zero without changing existing CLI integer behavior; exact-ID detail reads cannot resolve to a duplicate-name app; fixture data obeys the same presence contract; configured totals and measured usage are never confused; missing optional fields and domains render according to the explicit display rules.

### 1A-C4 · Finish terminal lifecycle and long-session behavior

**Status: Implementation complete.** Live API clients obtain their credential from a serialized provider for every request. API keys remain static; `DARKUBE_ACCESS_TOKEN` remains explicit and is never replaced by a refresh token; configured refresh credentials renew the access JWT within one minute of expiry. Refresh failures and rejected credentials show recovery guidance without displaying the token or raw refresh error. Resizing below 60×20 now displays the size prompt while preserving the active modal/filter and selection. Bubble Tea owns alternate-screen and terminal restoration; local PTY checks confirmed entry followed by restoration on `q` and Ctrl+C. Its `Run` teardown also invokes terminal restoration on errors. Resize behavior is covered by model tests; an interactive PTY resize was unavailable.

- Exercise startup, alternate-screen entry, normal quit, error exit, and Ctrl+C. Ensure terminal modes and cursor state are restored on every exit path.
- Handle resize while browsing and while editing/filtering or showing a modal. Preserve active scope, focus, selection, and entered text; below the minimum size, show the resize prompt without losing the session.
- Review token refresh for long-running sessions using existing auth resolution. Serialize refresh, avoid logging credentials, and surface explicit recovery guidance for expired credentials. Do not fall through from an expired explicit token to a different credential source.
- Add PTY coverage for key exit/restore paths and resize where available. Add fake-clock or deterministic adapter coverage for refresh serialization and expiry; if environment restrictions prevent PTY/network checks, record the exact boundary.

**Acceptance:** The terminal is restored after all tested exits; resizing does not corrupt layout or discard state; supported refresh credentials continue safely, and expired explicit credentials yield actionable guidance.

### 1A-C5 · Reconcile lifecycle actions, CLI/MCP boundaries, and docs

**Status: Implementation complete.** Restart and Delete are the only live TUI writes and both require `--allow-writes`; each re-reads the exact app ID and checks its name before dispatch. Restart and Delete confirmations have tested Cancel paths; Delete requires exact case-sensitive name entry and a separate activation. Submission guards and busy-state input handling prevent duplicate mutation calls, with no automatic retry after an ambiguous adapter error. Mock actions invoke no live adapters and leave the fixture bytes unchanged. MCP tests keep `tui --allow-writes` blocked. README and TUI progress/contract docs describe these boundaries. Full live destructive acceptance was not performed.

- Regression-check Restart and Delete modal copy, glyphs, focus order, No/Cancel paths, exact-name typing, separate Delete activation, and duplicate-submission prevention in mock and live-adapter tests.
- Verify mock actions stay in-memory and never write the fixture. Verify live writes require `--allow-writes`, re-read and bind to the exact app ID/name, and use no automatic mutation retry after an ambiguous outcome.
- Verify `tui` remains blocked by MCP policy even with write opt-in, and inspect the TUI command's reachable API methods for unintended writes or secret reveal.
- Update [08 Progress](08-progress.md), the README usage section, and any contract docs to distinguish implemented behavior, automated evidence, user-verified mock behavior, and unverified live acceptance.

**Acceptance:** Only the approved Restart/Delete paths can write; confirmations cannot be bypassed by navigation or repeated input; mock mode is offline and fixture-preserving; documentation matches source and evidence.

## Definition of done

- All five slices above meet their acceptance criteria, with code and docs reviewed together.
- Run focused tests for each state reducer/adapter boundary, then `go build ./...`, `go test ./...` (including race coverage if supported by the repository's normal workflow), the configured linter, Markdown lint, and `git diff --check`. Do not claim a blocked environment-dependent check passed; record the exact failed command and boundary.
- Exercise the mock profile interactively at minimum terminal dimensions, normal dimensions, and a wide terminal; verify Tenant, Pages, Apps, Details focus, filter, sort, help, refresh, and action-confirmation mock behavior.
- Verify one live read-only path with an authorized account and configured tenant if credentials are available. Treat local build, tests, mock acceptance, live API acceptance, and release/deployment as separate evidence.
- Update the progress report only after acceptance evidence exists. Mark Release 1A complete only when every remaining item is either closed or explicitly re-scoped with the user's agreement.

## Suggested implementation order

1. Contract/key audit and focused regression tests (1A-C1).
2. Request state and stale-result protection (1A-C2).
3. Replica presence, exact-ID Summary data, and partial-value presentation (1A-C3).
4. Terminal lifecycle, resize, and token refresh (1A-C4).
5. Action/security audit, docs, and full acceptance evidence (1A-C5).

Each slice should be a separately reviewable commit. If the audit finds an item already complete, record the source/test evidence and close it without reimplementing it.
