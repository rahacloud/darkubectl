# 03 · Apps and app details

```text
+ Apps / team-a / All --------------------------+ Selected -------------------+
| NAME          NAMESPACE  STATE       DESIRED  | billing-api                 |
| > billing-api prod       not ready         2  | prod / cluster-west         |
|   billing-api staging    healthy           1  | git_repo_url                |
|   session-db  prod       healthy           1  | Updated 5m ago              |
+-----------------------------------------------+-----------------------------+
| 3 apps  Enter details  / filter  s sort  :ns namespace  r refresh           |
+-----------------------------------------------------------------------------+

+ Apps / team-a / prod / billing-api ----------------------------------------+
| Summary [Pods] Logs Builds                          Last success 12:31:08  |
| POD              READY STATE            RESTARTS AGE                       |
| > billing-7f2    0/1   CrashLoopBackOff         7 13m                      |
|   billing-95d    1/1   Running                  0 12m                      |
| Enter containers   a actions   Esc apps                                    |
+----------------------------------------------------------------------------+

+ Pods / team-a / prod / billing-api -----------------+
| Console login required to list pods.                |
| Run darkubectl login, then relaunch the TUI.        |
| Esc apps                                            |
+-----------------------------------------------------+
```

## Apps

- Use `Client.ListApps` once per tenant refresh and share its snapshot with the namespace fallback, picker and Overview. Keep loaded rows keyed by app UUID. Use the namespace ID for filtering. Preserve rows with incomplete metadata and label missing fields.
- Default sort is app name, namespace name, namespace ID, then UUID. `s` opens a chooser for name, namespace, state and last updated, with both directions. Missing data sorts after known data. Filter literal text over app name, namespace name, state label and creation method, with no remote requests.
- At minimum width show Name, Namespace and State. Add Desired and Type first as width permits, then the Preview pane at 100+ columns. Show plan or request sizing, image and domain in Preview when those fields exist. Format sizing using the source units or the existing CLI convention. Layout changes never trigger a new fetch.
- Use `App.State.Text`, then `StateType`, then Unknown. Use `IsManagedService` and `CreationMethod`. `App.Replicas` is desired scale. Keep zero distinct from unavailable when the API omits the field. If a typed scalar cannot distinguish missing from zero, confirm fixture behavior or add a minimal presence-aware projection without breaking existing CLI outputs.
- Preserve the selected UUID across sort and refresh. If it disappears, clamp the cursor to the nearest remaining row. An already-open detail that gets a target-specific 404 shows **App no longer available** and offers a return to the refreshed list. It never jumps to a same-name app.

## Summary and Pods

| View | Contract |
| --- | --- |
| Summary · 1A | Start from the selected list row and refresh with `GetAppTyped(id)`. Show state and description, tenant, namespace/cluster, selected app ID, desired replicas, plan/resources, creation method, image or known source type, primary domain and update time when present. Display allowlisted fields and separate observations from configured values. |
| Pods · 1B | On entry or refresh, obtain Console access and call `appstate.FetchPods` with base URL, org and app UUID. Use the existing settling logic for the initially stale frame. Show pod name, readiness, status/reason, restart count and age where available. |
| Pod detail · 1B | Enter opens the selected pod's container list, then Enter on a container opens explicit logs. Show present versus previous container state where available. Actions offer current or previous logs using the same target picker as the Logs tab. |
| Pod usage · later | Pull current CPU/memory only on demand after validating the existing `Insight` behavior and units used by `cmd/top.go`. Drop usage columns from the initial delivery. Avoid a per-row metrics fan-out. |

If Console access is absent, keep Summary and Builds accessible and mark Pods unavailable. A socket auth failure gets a re-login hint. A timeout, an empty result and missing credentials each get distinct states. Render pod data as a snapshot in 1B. Keep WebSocket watching optional and deferred.

## Domains and History · Phase 2 reads

```text
+ Details / team-a / prod / gateway -------------------------------------+
| Summary  Pods  Logs  Builds  [Domains]  History                        |
| DOMAIN                     DNS           PLATFORM CERT                 |
| gateway.example.test       points here   exp. 2027-01-12               |
| api.example.test           unknown       unavailable                   |
| r refresh   Esc apps                                                   |
+------------------------------------------------------------------------+

+ History / team-a / prod / gateway -------------------------------------+
| Last 24h    TIME      ACTOR        CHANGED FIELDS                      |
| >           11:08     ops          replicas                            |
| Enter change detail   Esc back                                         |
+------------------------------------------------------------------------+
```

- Domains reuses the data shape behind `get domains`: `ExternalHosts`, `CertificateStatus` and tenant-aware DNS checking. Show partial check failures as unknown. Do not launch external TLS probes by default.
- History uses `AppHistory` scoped by UUID and an explicit window, initial default 24h. Show time, actor and allowlisted field names. On detail open, render old/new values only for individually reviewed fields such as replica count. Render unknown or sensitive value changes without their raw values.
- Exclude env values, raw specs/manifests, permissions payloads and arbitrary app JSON from the TUI. For advanced inspection, retain the separate existing CLI experience.
