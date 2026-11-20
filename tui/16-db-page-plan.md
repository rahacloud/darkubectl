# DB page plan

Status: implemented. The list and detail UI fetch the selected database's DBaaS detail response; external and internal connection fields are shown together. The API's password field is a Vault path, not the credential, so the TUI links to the Console where retrieval requires TOTP. Examples below are fictional.

## Layout

Keep the Tenant and Pages rail. The DB workspace lists managed databases in the middle pane and shows the selected database in the details pane. Selecting a database updates details immediately; selecting another page preserves the DB selection and filter for the current tenant. Selecting DB in Pages activates it without Enter, consistent with the other enabled pages.

```text
 Darkubectl / team-a / DB                                      ⇅ Name ↑
╭─ [1] TENANTS ─╮ ╭─ [3] DATABASES ─────────────╮ ╭─ [4] DATABASE DETAILS ────────────╮
│ › team-a      │ │ / filter                    │ │ ▤ orders-db                       │
├─ [2] PAGES ───┤ │                             │ │                                   │
│ ▦ Apps        │ │ › ▤ orders-db  ●           │ │ Engine     PostgreSQL 15          │
│ ∿ Activity    │ │     PostgreSQL 15 · 3 nodes │ │ Status     ● Running              │
│ ▱ Tickets     │ │   ▤ cache-db   ▲           │ │ Detail     Healthy                │
│ ▤ DB          │ │     Redis 7 · 1 node        │ │ Role       Primary                │
│ ◇ Registry    │ │   ▤ archive-db ?           │ │ Plan       standard               │
│               │ │     PostgreSQL 14 · 1 node  │ │ Managed    Yes                    │
│               │ │                             │ │                                   │
│               │ │                             │ │ NODES · 3                         │
│               │ │                             │ │ ● orders-0   1000m  2 GB  30 GB  │
│               │ │                             │ │ ● orders-1   1000m  2 GB  30 GB  │
│               │ │                             │ │ ● orders-2   1000m  2 GB  30 GB  │
│               │ │                             │ │                                   │
│               │ │                             │ │ Namespace ID  123                 │
│               │ │                             │ │ Cluster ID    42                  │
╰───────────────╯ ╰─────────────────────────────╯ ╰───────────────────────────────────╯
 1/2 rail · 3 databases · 4 details   / filter · s sort · r refresh · ? help
```

Show the `▤` database glyph before each database name and its status glyph after the name, with status text in details. Display each node's own name, status, CPU, RAM and disk; do not assume all nodes have the same capacity. CPU is millicores, RAM is MB and disk is GB as specified by the client model. Missing values display `—`; only show a node count from the response and do not infer health from capacity. Include the database ID in details when present. Long fields and node lists scroll in the details pane. Namespace and cluster are IDs unless an existing matching lookup resolves them unambiguously; a lookup failure must not block the page.

## Connection detail API finding

The list route `GET /dbaas/api/v1/app/database/` omits connection data. A read-only request to `GET /dbaas/api/v1/app/database/{id}/` returns it. The response includes `connection` and `private_connection` for the master, optional standby variants, and a structured `connections` object grouped by `master`/`standby` and `external`/`private`. Each connection contains `username`, `password`, `database`, `host` and `port`. The detail response also adds resource sizing, creation time, status progress, external-access state and available actions. This route was confirmed against an existing database; no write was performed.

The details pane shows External and Internal fields together. When standby data is present, `m`/`b` selects the master or standby pair. It uses `connections` as the primary shape and falls back to the top-level compatibility fields only when needed. The API's `password` value is a Vault reference and retrieving the real password requires TOTP in the Console; never display or copy that reference as a password. Show a clickable Console URL built from the current tenant and database ID. `y` copies the external connection URI with the literal `REPLACE_PASSWORD` placeholder.

Treat every returned password field as a Vault path, not a credential. Do not reveal it or interpolate it into a URI. Show the Console link for TOTP protected retrieval. Mock profiles use synthetic connection values and never load real connection data. The page stays read only; `available_actions` does not by itself authorize exposing mutation controls.

The status vocabulary is not documented by the current client. Preserve and display the raw status and detail. Use green `●` only for values confirmed healthy, yellow `▲` for confirmed transitional or warning states, and red `✖` for confirmed failure states. Unknown or missing values use muted `?`; never infer status from role. Apply the same conservative rule independently to node status. Keep labels visible without color, and use the existing theme and focused selection styling.

At narrow widths, show the list and open details in place with Enter or `4`; Esc returns to the list. At wider widths show both panes, with `3` and `4` changing focus. Preserve the standard minimum-size message. Use terminal-cell-aware truncation, and retain full values in scrollable details where practical.

## Interaction and data

- Use the existing tenant-scoped `Client.ListDatabases` read through a browser adapter. The endpoint returns a bare array; the current contract has no pagination. Load on first activation and on tenant change while DB is active. `r` refreshes manually.
- `/` filters the loaded list locally by name, engine, version and status. Show an explicit no-matches state distinct from an empty tenant. Esc leaves filter entry using existing browser behavior.
- Use the existing header sort position and sort interaction. Offer Name, Engine and Status; default Name ascending. Sort case-insensitively, preserve unknown values, use stable identity as a tie-breaker, and place missing values last in either direction. Keep DB sorting separate from Apps and Activity.
- Support the existing movement, page scrolling and mouse selection conventions. Preserve the selected database by ID across refresh, filtering and sorting where possible; if it disappears, select the nearest visible row. Handle missing or duplicate IDs without merging entries or silently selecting a different database.
- Fetch connection details from the confirmed detail route only for the selected database, and keep them out of the list request. Do not include passwords in logs or errors. The mapped API has no investigated database write contract; there are no create, resize, restart or delete controls in this slice, including when writes are enabled.

## Loading and scope

Use the browser request-state conventions: initial loading glyph, ready, empty, failed, refreshing and stale. A failed refresh keeps the previous same-tenant data, shows that it is stale and offers `r` to retry. An initial failure shows a sanitized, bounded error and retry hint; it must not look like an empty result.

On tenant change, clear the old database list and details immediately, cancel or invalidate outstanding requests, and load the new tenant when DB is active. Use request IDs and scope generations to reject late responses, including A → B → A. Preserve list selection and filter across page switches within the same tenant, but do not carry a selected database into another tenant.

## Implementation and acceptance

1. Complete: added the typed tenant-scoped `GetDatabase` adapter and selection-scoped request state; external and internal fields display together, with a Console link for TOTP protected password retrieval.
2. Complete: mock mode uses synthetic connection values and makes no API call for details.
3. Build and diff checks are run for this implementation. Interactive layout and clipboard behavior still need a manual terminal pass; the live endpoint was separately confirmed read only.

The list response is defined in `internal/client/products.go`; the existing CLI table is in `cmd/products.go`. The detail endpoint is part of the undocumented console API and should remain isolated behind a typed client method.
