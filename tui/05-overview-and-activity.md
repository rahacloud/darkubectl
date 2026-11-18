# 05 · Overview, alerts and notifications

```text
+ Overview / team-a / All ----------------------------------------------+
| Apps 12     Healthy 8     Not ready 2     Other 1     Unknown 1       |
| Firing alerts: 2 (tenant)     Unread: 3 (account, latest 50)          |
+ Recently updated apps ------------------------------------------------+
| > prod / billing-api          not ready                   5m ago      |
|   staging / gateway           healthy                    18m ago      |
+-----------------------------------------------------------------------+
| Enter app  :alerts firing alerts  :notifications feed  r refresh      |
+-----------------------------------------------------------------------+

+ Activity / team-a / All ---------------------------------------------+
| [Alerts: tenant]     Notifications: account                          |
| STATUS   SEVERITY   ALERT                   TARGET                   |
| > firing critical   Elevated errors         billing-api              |
| resolved warning    Storage response        Target unverified        |
| Enter details  / filter  h/l tabs  r refresh  Esc back               |
+----------------------------------------------------------------------+

+ Activity / Notifications ---------------------------------------------+
| Account-wide feed     Latest 50      Tenant attribution unavailable   |
| > unread   Platform maintenance         10:30                         |
|   read     Usage report                  Yesterday                    |
| Enter message  / filter  h/l tabs  r refresh  Esc back                |
+-----------------------------------------------------------------------+
```

## Overview

- Derive counts and recent app rows from the shared Apps snapshot. Apply the namespace filter to app counts and rows. Map `healthy` to Healthy, `not_ready` to Not ready, missing type to Unknown and remaining types to Other. Display platform state text on rows. Order the five most recent apps by parsed `UpdatedAt`, skipping missing or invalid timestamps and using UUID as tie-breaker. Treat the time as an app update.
- Keep separate freshness, errors and retry behavior for Apps, Alerts and Notifications. Refresh loads the visible datasets, sharing identical in-flight work. An unavailable feed displays its status instead of a numeric zero. No per-app build requests from Overview.
- Alert counts remain tenant-wide and notifications account-wide regardless of namespace selection. Show the scope near each count. Unread count covers the latest 50 fetched notifications. It is neither a backend total nor a tenant count.

## Activity

- Alerts uses `Client.Alerts` with active org; defaults to firing using `Alert.IsFiring`. Offer All/Firing/Resolved, using the platform status text for rows. Show severity, name, affected target text and start/end when available. Enter opens bounded details. Label raw target strings as reported data.
- Notifications uses the account-level client and `Notifications(ctx, 50)`. Provide All/Unread. Identify rows by slug. Convert HTML descriptions to plain text and sanitize before rendering. Keep links inert. Opening an item performs no mark-read request. A change of tenant must not relabel these items as belonging to that tenant.
- Provide **Open app** for an alert only if the pinned API/CLI supplies a verified app reference or a fixture-backed parser for an expected action-link format yields an exact app UUID present in the current tenant. Check origin/path before parsing any link and never navigate the URL itself. Do not match based on a display name. If an exact target is outside the active namespace, prompt before changing namespace scope. Ship alert details without a link until the reference format is verified.
- Filter loaded rows locally by useful displayed fields. Use independent load/error states in the two tabs, so a tenant alert error cannot suppress account notifications. A denied tenant-scoped query shows an access failure.
