# Activity page plan

Status: proposed. Activity remains a disabled Pages entry until implemented. All examples are fictional.

## Layout

Activity replaces the Apps tree and details while keeping the Tenant and Pages rail. Selecting Apps again restores its selection and view. Activity starts on Alerts with Firing selected. Alerts belong to the selected tenant; notifications belong to the account and do not inherit a tenant or namespace filter.

```text
 Darkubectl / team-a / Activity                                  ⇅ Started ↓
╭─ [1] TENANTS ──╮ ╭─ ACTIVITY ──────────────────────────────────────────────────╮
│ › team-a       │ │ [ Alerts ]  Notifications       Tenant: team-a              │
│   team-b       │ │ Firing ▾  / filter              Updated 10:42               │
├─ [2] PAGES ────┤ ├─ [3] Alerts ──────────────────┬─ [4] Alert details ────────┤
│ ▦ Apps         │ │ STATUS     SEVERITY  STARTED     ALERT │ ⚠ High CPU usage           │
│ ∿ Activity     │ │ › ● Firing  ✖ Critical Nov 19 10:30 CPU │ Status    ● Firing          │
│ ▱ Tickets      │ │   ● Firing  ▲ Warning  Nov 19 10:35 Memory │ Severity  ✖ Critical       │
│ ▤ DB           │ │                               │ Instance  billing-api       │
│ ◇ Registry     │ │                               │ Condition CPU > 90%         │
│ ¤ Billing      │ │                               │ Started   Nov 19, 10:30     │
│                │ │                               │ Description                 │
│                │ │                               │ CPU exceeded threshold.     │
╰────────────────╯ ╰───────────────────────────────┴─────────────────────────────╯
 1/2 rail · 3 alerts · 4 details   h/l tabs · f status · s sort · / filter · r refresh
```

```text
 Darkubectl / team-a / Activity                                  ⇅ Time ↓
╭─ [1] TENANTS ──╮ ╭─ ACTIVITY ──────────────────────────────────────────────────╮
│ › team-a       │ │ Alerts  [ Notifications ]       Account-wide · latest 50    │
├─ [2] PAGES ────┤ │ All ▾  / filter                 Updated 10:43               │
│ ▦ Apps         │ ├─ [3] Notifications ───────────┬─ [4] Message ──────────────┤
│ ∿ Activity     │ │ › ✉ Unread  Maintenance       │ ✉ Unread                    │
│ ▱ Tickets      │ │     Nov 19, 10:30 · platform  │ Platform maintenance        │
│ ▤ DB           │ │   ✓ Read    Usage report      │ Nov 19, 09:00 · platform    │
│ ◇ Registry     │ │     Nov 19, 09:00 · billing   │                              │
│ ¤ Billing      │ │                               │ Maintenance begins tonight. │
╰────────────────╯ ╰───────────────────────────────┴─────────────────────────────╯
 1/2 rail · 3 notifications · 4 details  h/l tabs · f status · s sort · / filter · r refresh
```

Use the existing purple selection and focused-border styles. Apps and Activity are enabled when this slice ships; other pages stay disabled. Page heading, active tab, scope and filter stay visible as the list scrolls.

| Meaning | Glyph and label | Theme color |
| --- | --- | --- |
| Firing alert | `● Firing` | Danger `#FB7185` |
| Resolved alert | `✓ Resolved` | Success `#4ADE80` |
| Other or missing status | `?` and raw status, or `Unknown` | Muted `#B3AEC2` |
| Critical severity | `▲ Critical` | Danger `#FB7185` |
| Warning severity | `▲ Warning` | Number `#FBBF24` |
| Info severity | `◆ Info` | Accent `#B9A3FF` |
| Other or missing severity | `?` and raw severity, or `Unknown` | Muted `#B3AEC2` |
| Unread notification | `✉ Unread` | Accent `#B9A3FF` |
| Read notification | `✓ Read` | Muted `#B3AEC2` |

Match known labels case-insensitively for presentation; preserve unknown values. Color severity glyph and text rather than the whole row. Keep the selection legible, falling back to its foreground where needed. Always show status text alongside glyphs, including with `NO_COLOR`.

At 60×20, retain the rail and show one workspace: Enter opens details in place, Esc returns to the list. At wider sizes use the split preview; Enter or `4` focuses details, `3` focuses the list. `1` and `2` focus the rail. Below the existing minimum, show the resize message. Use terminal-cell-aware truncation. Do not reverse Persian strings; test real Persian fixture text and document terminal-specific limitations.

## Interaction and data

- `h`/`l` switches tabs; preserve each tab's filter, sort, selection and scroll position. `f` cycles Alerts through Firing, All and Resolved, and Notifications through All and Unread. `/` filters loaded rows locally; Esc exits filter entry. Support normal movement, paging and mouse selection; details scroll independently. Show contextual footer hints.
- Use the existing top-right header position for the active tab's sort indicator, replacing the Apps namespace and sort indicators while Activity is open. `s` opens the existing sort selection interaction; Up/Down chooses the field, Left/Right sets ascending or descending, Enter applies and Esc closes. Alerts offers Started, Severity, Status and Name, defaulting to Started descending. Notifications offers Time, Read status and Title, defaulting to Time descending. Returning to Apps restores its namespace and sort indicators and settings. Keep filter status in Activity, distinct from sorting.
- Fetch alerts via `Client.Alerts` for the selected tenant. Show name, status, severity, instance and start time as space permits. Details include available ID, owner, condition, descriptions, time window, start and end. Instance is reported text. Sort parsed start times with a stable ID tie-breaker; place missing or invalid times last in either direction and show raw values in details. For severity use Critical, Warning, Info, then unknown; for status use Firing, Resolved, then unknown. Reverse known values for descending without moving unknown values ahead of known values. Filter displayed fields.
- `Alert.IsFiring` currently treats every status except exact `resolved` as firing. Recognize known firing and resolved statuses explicitly for this page; show unknown statuses under All without claiming they are firing or resolved. Preserve raw values and confirm variants against fixtures before expanding the known set.
- Fetch notifications through the account client with `Notifications(ctx, 50)` and select by slug. Default to newest time first; sort only the loaded 50, with stable ties and invalid times last. Show title, read state, time and target type, plus description and available metadata in details. Convert HTML to bounded plain text, decode entities, strip control sequences and keep links inert. Opening a notification does not mark it read. Filters cover only the loaded 50; do not present a backend total.
- `r` refreshes the visible feed manually. Preserve selection by alert ID or notification slug; select the nearest row if it disappears. Handle missing or duplicate IDs safely rather than silently selecting a different item. No writes or background polling are needed.
- Omit Open app until a verified API reference or a fixture-backed parser of a validated action-link origin and path provides an exact app UUID present in the tenant. Do not infer identity from display names. Prompt before changing an active namespace filter.

## Loading and scope

Give each feed independent loading, refreshing, ready, empty, stale, failed and unavailable states. Show filter-specific empty messages. A failed refresh retains the same-scope snapshot, last-success time and retry guidance; an initial failure shows a bounded, sanitized error and retry. Unavailable is not zero, and one feed's failure must not suppress the other.

Tenant changes cancel and invalidate alert requests and immediately clear old tenant alerts and details. Load the new tenant when Alerts is visible. Use scope generations and request IDs to reject late responses, including A→B→A. Notifications remain account-scoped across tenant changes. Cancel pending reads on exit and reject responses older than a newer refresh.

## Implementation and acceptance

1. Add Activity page selection, responsive list/details, keyboard and mouse navigation, glyphs, fixtures and independent read adapters. Reuse account/tenant clients and existing request state. Mock mode stays offline.
2. Add filtering, details, sanitization and refresh, including cancellation, stale snapshots, scope generations and unknown status behavior.
3. Verify both feeds with fixtures covering empty and filtered states, each sort field and direction, missing timestamps, selection across sorting, restored Apps settings, severity/status variants, missing fields, HTML/control input, Persian text, long lines, `NO_COLOR`, minimum and wide terminals, mouse and keyboard focus, failed refreshes and delayed tenant switches. Inspect actual terminal rendering. Treat a live read-only smoke check, if authorized credentials are available, as separate evidence.

The data contract is in `internal/client/notifications.go` and current CLI presentation in `cmd/notifications.go`. This plan does not claim implementation or live verification.
