# 02 · Shell, keyboard and context

```text
+ Darkubectl ---------------------------------------------------------------+
| Apps / billing-api        team-a / prod        account@example.test       |
| Read only                 Last success 12:31:08                           |
+ Detail -------------------------------------------------------------------+
| [Summary]  Pods  Logs  Builds                                             |
| State       not ready                                                     |
| Desired     2                                                             |
+---------------------------------------------------------------------------+
| Esc apps  Left/Right or h/l tabs  r refresh  a actions  ? help            |
+---------------------------------------------------------------------------+

+ Select tenant --------------------------------------+
| Filter: team                                        |
| > team-a             Profile          current       |
|   team-b             Profile                        |
| Enter switch session   Esc cancel                   |
+-----------------------------------------------------+
```

## Launch and tenant resolution

- Register `darkubectl tui` with no positional arguments or local v1 flags. Support inherited `--config`, `--token`, `--org` and `--base-url`. Reject an explicit non-table `-o` with a usage error. Require TTY input and output before terminal initialization, config resolution or network access. Help should work through a pipe.
- Preserve config path precedence (flag → `DARKUBE_CONFIG` → default), existing flag/env/file/default behavior for org and base URL, and REST auth selection (API key → Console access). Console access uses `DARKUBE_ACCESS_TOKEN`, then a refresh token via the existing resolver. Never prompt for credentials inside the TUI.
- With a configured org, start Apps promptly and request Profile independently. If Profile fails, leave the tenant and Apps usable. Show account as unavailable and label tenant options sourced from config as **Local config**. If Profile succeeds, use its organization slugs. A configured org missing from the profile must be labeled unverified until a successful tenant read or an access error.
- With no org, show the chooser after Profile. If Profile fails, offer configured local slugs. If neither source yields a choice, show setup guidance using `darkubectl whoami`, `--org` and `darkubectl login`, with retry and quit. A successful empty Profile and a failed Profile have distinct messages. Missing credentials reuse the existing setup error and exit without entering the alternate screen.
- Tenant changes apply only to this run. On selection, immediately change the scope label and invalidate prior data, cursors, filters, app details, logs, requests and overlays. Reset namespace to All. If the new tenant denies access, keep that tenant displayed with retry and context selection.
- Namespace picker offers All plus project rows keyed by namespace ID. Load complete inventory lazily through `ListNamespaces`, with app-derived fallback for unavailable inventory. Mark the fallback and its omission of empty projects. Keep different IDs for same-name namespaces, showing cluster or ID. Changing namespace returns to the root list with an empty filter. Back from app detail restores list selection, sort, filter and scroll where the selected ID still exists.

## Input contract

| Focus and keys | Behavior |
| --- | --- |
| List or picker · Up/Down or `k/j` | Move the cursor one row |
| Detail tab strip · Left/Right or `h/l` | Switch tabs in display order, stopping at either end |
| Multi-control screen · Tab / Shift+Tab | Cycle focus among visible controls. Move focus to the nearest surviving control on resize. |
| List or viewport · Home/End or `g/G` | First/last row or top/bottom. Viewport also supports PageUp/PageDown. |
| List · `/`, Enter, Esc | Edit a case-insensitive substring filter live. Enter commits and Esc restores the previous filter. Esc with a committed filter clears it before leaving the list. |
| Browse · `:` or Ctrl+P | Open the same searchable jump panel. `:` accepts exact aliases on Enter and otherwise uses the selected match. Ctrl+P also searches scoped, already-loaded apps. No arbitrary command parsing. |
| Browse · `a`, `r`, `?`, `q` | Scope-specific actions, active view refresh, contextual help and quit. Hide unavailable bindings from the footer. |
| Modal or text input | Innermost component handles keys first. Printable shortcuts, including `q`, are literal in text fields. Esc closes the top overlay before changing view. Ctrl+C exits consistently and restores the terminal. |
| Drilldown · Enter, Esc | Open the focused row or control. Esc returns to the parent after overlays and filters have been handled. |

Jump entries are fixed to shipped views and actions. Phase 1 aliases are `app`/`apps`, `ctx`/`tenant`, `ns`/`namespace` and, in 1C, `overview`, `alerts` and `notifications`. App jumps show namespace for disambiguation and stay within active tenant and namespace. Jump matching uses no remote search. `a` uses the same action definitions as help and shows only applicable read actions in Phase 1.

## Layout and data states

- At 100+ columns show an optional list preview rendered from the row already in memory. At 60–99 columns show a single list or detail pane. Below 60×16 minimum (width or height), show a resize message and retain Esc/Ctrl+C. Reserve one header line for scope and one footer line for current actions even in compact mode.
- Use terminal cell widths for truncation and scrolling, and use text markers for selection, active tab, health and stale status. Honor `NO_COLOR` and keep the full workflow navigable without a mouse. Check Persian, wide Unicode and combining characters against terminal limitations.
- Use independent `Loading`, `Ready`, `Empty`, `Refreshing`, `Stale`, `Failed` and `Unavailable` states per dataset. Keep same-scope results during refresh. A failed refresh with a prior success shows stale data plus the last successful time and a retry hint. New scope starts visibly empty/loading. A successful zero-item response and a filter with no matching rows have different copy.
- One layout and key source drives rendering, help and footer hints. Resizing, typing and moving a highlight never trigger a detail API request.
