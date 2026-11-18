# 02 · Shell, keyboard and context

```text
+ Darkubectl / team-a / Apps / prod / billing-api -----------------------------+
| [1] TENANTS  │ [3] APPS                     │ [4] SUMMARY                   |
| › team-a     │ ▾ prod · 2 apps              │ SUMMARY · billing-api         |
|              │ ├── billing-api  not ready   │ State      not ready           |
| [2] PAGES    │ └── gateway      healthy     │ Desired    2                   |
| ● Apps       │                              │                               |
| · Activity   │                              │                               |
| · Tickets    │                              │                               |
| · Resources  │                              │                               |
| · Registry   │                              │                               |
| · Billing    │                              │                               |
| · Account    │                              │                               |
+--------------+------------------------------+-------------------------------+
| Tab cycle panes   t tenant   n namespace   / filter   ? help   q quit        |
+-----------------------------------------------------------------------------+
```

## Launch and tenant resolution

- Register `darkubectl tui` with no positional arguments. Live mode supports inherited `--config`, `--token`, `--org` and `--base-url`. `--mock-file PATH` selects the local fixture mode. Reject an explicit non-table `-o` with a usage error. Require TTY input and output before terminal initialization, fixture loading, config resolution or network access. Help should work through a pipe.
- Fixture mode parses exactly one versioned YAML document with known fields only. It skips config/auth resolution and client construction, rejects explicit connection flags, and displays a persistent `MOCK PROFILE · OFFLINE` label. Environment credentials are ignored. The fixture schema contains no secret or write-action fields, is capped at 1 MiB and 1000 apps, and reports parser errors without echoing YAML values.
- The sample fixture is [`demo/mock-profile.yaml`](../demo/mock-profile.yaml). Run it with `go run . tui --mock-file demo/mock-profile.yaml`. Version 1 accepts either one top-level `tenant` with `apps`, or a `tenants` list where each tenant has its own name, apps, and optional namespace inventory. The Tenant pane always lists available tenants; `t` or focus Tenant to move through the list, then press Enter to switch. Selecting one loads only its fixture apps and namespaces. Each app requires a unique ID, a name, namespace ID/name, and nonnegative desired replicas; state, cluster, type, image, tag, and domain are optional. IDs are unique within each tenant. An empty apps list is valid.
- Preserve config path precedence (flag → `DARKUBE_CONFIG` → default), existing flag/env/file/default behavior for org and base URL, and REST auth selection (API key → Console access). Console access uses `DARKUBE_ACCESS_TOKEN`, then a refresh token via the existing resolver. Never prompt for credentials inside the TUI.
- With a configured org, start Apps promptly and request Profile independently. If Profile fails, leave the tenant and Apps usable. Show account as unavailable and label tenant options sourced from config as **Local config**. If Profile succeeds, use its organization slugs. A configured org missing from the profile must be labeled unverified until a successful tenant read or an access error.
- With no org, show tenant choices in the sidebar after Profile. If Profile fails, offer configured local slugs. If neither source yields a choice, show setup guidance using `darkubectl whoami`, `--org` and `darkubectl login`, with retry and quit. A successful empty Profile and a failed Profile have distinct messages. Missing credentials reuse the existing setup error and exit without entering the alternate screen.
- Tenant changes apply only to this run. On selection, immediately change the scope label and invalidate prior data, cursors, filters, app details, logs, requests and overlays. Reset namespace to All. If the new tenant denies access, keep that tenant displayed with retry and context selection.
- `n` opens a fuzzy namespace picker with All plus project rows keyed by namespace ID, cluster and name. Load the full inventory lazily through `ListNamespaces`; if unavailable (such as with API-key credentials), derive namespaces from the already-loaded app snapshot and mark that empty projects are omitted. Show empty namespaces. Mock profiles can provide an explicit `namespaces` inventory to preview empty projects offline. Choosing a namespace scopes the tree and clears the app filter. Keep Apps and the selected app Summary in the two main panes, with the navigation rail on the left. On a namespace row, Left/`h` collapses and Right/`l` expands it. On an app row, Right/`l` opens Summary; Left/`h` collapses its namespace. From Summary, Left/`h` returns to Apps. From the navigation rail, Right/`l` moves to Apps. `Tab`/`Shift+Tab` cycles focus across all panes.
- The left navigation rail has two equal-height panes: Tenant above Pages. Tenant always shows the available tenant list; `t` or `1` focuses it, Up/Down moves through tenants, and Enter switches. If the list exceeds the pane height, moving the cursor scrolls the visible rows. `1`, `2`, `3` and `4` focus Tenant, Pages, Apps and the selected-item detail workspace respectively. Apps is the only enabled page; Activity, Tickets, Resources, Registry, Billing and Account are listed as disabled until implemented. Summary, Usage, Pods, Logs, Builds, Env, Terminal, Domains and History are app-specific views available from a selected app, not global pages.

## Input contract

| Focus and keys | Behavior |
| --- | --- |
| Apps browser · Up/Down or `k/j` | Move through the app tree and update the adjacent summary |
| Pane focus · `1`, `2`, `3`, `4` | Focus Tenant, Pages, Apps or the selected-item detail workspace. `Tab`/`Shift+Tab` cycles focus through the four panes. |
| Apps and Summary panes · Left/Right or `h/l` | On a namespace, Left/`h` collapses and Right/`l` expands. On an app, Right/`l` or Enter focuses Summary; Left/`h` collapses the namespace. From Summary, Left/`h` returns to Apps. From the navigation rail, Right/`l` moves to Apps. |
| Selected app · `[` / `]` | Move to the previous/next app-specific view: Summary, Usage, Pods, Logs, Builds, Env, Terminal, Domains, History. The sequence wraps at either end; unavailable views are skipped. |
| Navigation rail · `t`, Up/Down, Enter | Focus the always-visible tenant list, move through it and switch the session. Apps is the active page; planned pages are disabled. |
| Browse · `n` | Open the searchable namespace picker. Type to filter, use Up/Down to move, then Enter to choose All or a namespace. |
| Multi-control screen · Tab / Shift+Tab | Cycle focus among panes. Move focus to the nearest surviving control on resize. |
| List or viewport · Home/End or `g/G` | First/last row or top/bottom. Viewport also supports PageUp/PageDown. |
| List · `/`, Enter, Esc | Open an inline filter after the breadcrumb. Matches update as you type using case-insensitive fuzzy subsequence matching. `appname` searches app names across all namespaces; `namespace/appname` scopes both sides, allowing spaces around `/`. Up/Down leaves text entry and moves through the filtered tree; Enter focuses Summary for the selected app. Esc while editing restores the previous query, and Esc from the list clears a query. Ctrl+U clears while editing. |
| Browse · `a`, `r`, `?`, `q` | Open lifecycle actions for the selected item, refresh the active view, show help or quit. Hide unavailable actions from the menu. |
| App actions · `a`, `--allow-writes` | Live writes are disabled unless the flag is set. Restart always asks Yes/No. Delete requires the exact app name, then a click on the separate Delete app button; `Tab` then `Enter` is the keyboard equivalent. Mock mode simulates actions locally without API calls or fixture-file changes. |
| Modal or text input | Innermost component handles keys first. Printable shortcuts, including `q`, are literal in text fields. Esc closes the namespace picker or tenant list before changing view. Ctrl+C exits consistently and restores the terminal. |
| Drilldown · Enter, Esc | Open the focused row or control. Esc returns to the parent after overlays and filters have been handled. |

`a` opens the selected app's lifecycle menu. App-specific configuration stays in the related view; Logs, Builds and Terminal are navigated with `[` and `]`.

## Layout and data states

- At 60×20 or wider show a narrow left navigation rail, the Apps tree and Summary. Divide the rail into equal-height Tenant and Pages panes; the two main panes share the remaining width. Below the minimum, show a resize message and retain Esc/Ctrl+C. Keep the contextual key footer pinned to the final terminal row.
- Use terminal cell widths for truncation and scrolling, and use text markers for selection, active tab, health and stale status. Honor `NO_COLOR` and keep the full workflow navigable without a mouse. Check Persian, wide Unicode and combining characters against terminal limitations.
- Use independent `Loading`, `Ready`, `Empty`, `Refreshing`, `Stale`, `Failed` and `Unavailable` states per dataset. Keep same-scope results during refresh. A failed refresh with a prior success shows stale data plus the last successful time and a retry hint. New scope starts visibly empty/loading. A successful zero-item response and a filter with no matching rows have different copy.
- One layout and key source drives rendering, help and footer hints. Resizing, typing and moving a highlight never trigger a detail API request.
