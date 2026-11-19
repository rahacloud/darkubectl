# 03 · Apps and app details

```text
+ Apps / team-a / All --------------------------+ Selected -------------------+
| NAME          NAMESPACE  STATE       DESIRED  | billing-api                 |
| > billing-api prod       not ready         2  | prod / cluster-west         |
|   billing-api staging    healthy           1  | git_repo_url                |
|   session-db  prod       healthy           1  | Updated 5m ago              |
+-----------------------------------------------+-----------------------------+
| 3 apps  Enter details  / filter  s sort  n namespace  r refresh             |
+-----------------------------------------------------------------------------+

+ Apps / team-a / prod / billing-api ----------------------------------------+
| [Summary] Usage                                    Last success 12:31:08  |
| billing-api · not ready                                                  |
| Namespace  prod     Cluster  cluster-west                                |
| Replicas   2        Plan     Standard                                    |
| CPU        125m / 500m     RAM  384 MiB / 1000M                         |
| Image      registry.example.test/team/billing:2026.10.4                  |
| ] Usage   a actions   Esc Apps                                          |
+----------------------------------------------------------------------------+

+ Apps / team-a / prod / billing-api ----------------------------------------+
| Summary [Usage] · billing-api                     Last query 12:31:08     |
| CPU             125m / 500m                                              |
| RAM             384 MiB / 1000M                                          |
| Disk            No current sample / 20 GiB                               |
| Partial snapshot · unavailable metrics are marked below                  |
| [/] previous / next view   r refresh   Esc apps                           |
+----------------------------------------------------------------------------+

+ Pods / team-a / prod / billing-api -----------------+
| Console login required to list pods.                |
| Run darkubectl login, then relaunch the TUI.        |
| Esc apps                                            |
+-----------------------------------------------------+
```

## Apps

- The `n` namespace picker lists All plus the tenant's complete project inventory, keyed by namespace ID so same-name namespaces remain distinct. Include empty projects, cluster and app counts. If `ListNamespaces` fails, derive options from the single already-loaded Apps snapshot and label that empty namespaces are unavailable. Keep that fallback synchronized if the Apps snapshot arrives after the picker request. Selecting a namespace filters the local snapshot only; it triggers no list request. A narrow left rail contains equal-height Tenant and Pages panes beside the Apps and selected-item detail workspace. Summary, Usage, Scaling, Logs, Builds, Env, Terminal, Domains and History belong to the selected app's drilldown. Summary includes the selected app's pod/container snapshot and outbound IP; Scaling shows the replica and autoscaler configuration.
- Use `Client.ListApps` once per tenant refresh and share its snapshot with the namespace fallback, picker and Overview. Keep loaded rows keyed by app UUID. Use the namespace ID for filtering. Preserve rows with incomplete metadata and label missing fields.
- Default sort is app name, namespace name, namespace ID, then UUID. `s` opens a chooser for name, namespace, state and last updated, with both directions. Missing data sorts after known data. Filter literal text over app name, namespace name, state label and creation method, with no remote requests.
- At minimum width show Name, Namespace and State. Add Desired and Type first as width permits, then the Preview pane at 100+ columns. Show plan or request sizing, image and domain in Preview when those fields exist. Format sizing using the source units or the existing CLI convention. Layout changes never trigger a new fetch.
- Use `App.State.Text`, then `StateType`, then Unknown. Use `IsManagedService` and `CreationMethod`. `App.Replicas` is desired scale. Keep zero distinct from unavailable when the API omits the field. If a typed scalar cannot distinguish missing from zero, confirm fixture behavior or add a minimal presence-aware projection without breaking existing CLI outputs.
- Preserve the selected UUID across sort and refresh. If it disappears, clamp the cursor to the nearest remaining row. An already-open detail that gets a target-specific 404 shows **App no longer available** and offers a return to the refreshed list. It never jumps to a same-name app.

## App view contracts

For a selected app, the active detail views are Summary, Usage and Scaling, followed by planned Logs, Builds, Env, Terminal, Domains and History. Pod status is part of Summary. `[` moves to the previous view and `]` to the next; navigation wraps at the ends without changing the selected app in the Apps tree. Summary and Scaling support PgUp/PgDn scrolling while keeping their tabs and section titles visible; Summary also keeps its Identity and Name rows visible.

| View | Contract |
| --- | --- |
| Summary · 1A | Start from the selected list row and refresh with `GetAppTyped(id)`. Show state and description, tenant, namespace/cluster, selected app ID, desired replicas, plan/resources, creation method, image or known source type, domains and update time when present. When the API returns them, also show Service type, primary Port Service, Internal Address (including its selected service port when unambiguous), Service External IP, readiness probe, command and input names. Resolve Outbound IP from `GET /ip-management/api/egress-route/`, preferring a matching namespace route, then an all-namespaces route, then the cluster default. The future Ports view can list every port mapping. Display allowlisted fields and separate observations from configured values; never retain or display input values. |
| Usage | Show current resource samples and a scrollable historical bar chart for CPU Usage, CPU Throttle, RAM Usage, Network Receive and Network Send; show Disk Usage only when the app has a configured disk. Resource glyphs identify CPU, RAM and disk. `m` cycles metrics, `w` cycles 1h, 6h and 24h windows, and `p` cycles all pods and individual pods. Every pod keeps its own stable color and legend entry; colors identify pods, not severity. CPU, RAM and disk charts show a yellow horizontal CPU request, RAM limit or disk capacity line when available. The y axis labels resource values and the x axis labels local times. The scale spans zero to the higher of the reference and sampled peak. The chart fills and interpolates missing buckets for continuity. The API's available history may be shorter than the selected window. PageUp/PageDown scrolls through the chart and legend. Distinguish failed metrics, empty results and stale snapshots. `r` refreshes usage and history; `r` in Summary refreshes detail and current metrics independently. Autoscaling and disk growth controls remain future work. |
| Pods · 1B | On entry or refresh, obtain Console access and call `appstate.FetchPods` with base URL, org and app UUID. Use the existing settling logic for the initially stale frame. Show pod name, readiness, status/reason, restart count and age where available. |
| Pod detail · 1B | Enter opens the selected pod's container list, then Enter on a container opens explicit logs. Show present versus previous container state where available. Actions offer current or previous logs using the same target picker as the Logs tab. |
| Pod usage · later | Pull current CPU/memory only on demand after validating the existing `Insight` behavior and units used by `cmd/top.go`. Drop usage columns from the initial delivery. Avoid a per-row metrics fan-out. |
| Builds · later | Keep Git build history and build controls here. Image selection and deployment start belong in this view; show the exact image/tag and target before applying a deployment. |
| Env · later | List the selected app's environment variable names and values using the existing env data contract. Mask values by default; reveal one value only through a deliberate, temporary per-value action. Never persist or log values. |
| Terminal · later | Attach an interactive shell to an explicitly selected app pod/container using the existing terminal transport. Show the exact tenant, app, pod and container before attaching; restore the TUI cleanly on exit or failure. This is a remote app shell, not an arbitrary local command runner. |

If Console access is absent, keep Summary and Builds accessible and mark Pods and Terminal unavailable. A socket auth failure gets a re-login hint. A timeout, an empty result and missing credentials each get distinct states. Render pod data as a snapshot in 1B. Keep WebSocket watching optional and deferred.

## Domains and History · Phase 2 reads

```text
+ Details / team-a / prod / gateway -------------------------------------+
| Summary Usage Scaling Logs Builds Env Terminal [Domains] History       |
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
- Domain changes belong inside the Domains view, with the exact app and host shown before applying a change.
- History uses `AppHistory` scoped by UUID and an explicit window, initial default 24h. Show time, actor and allowlisted field names. On detail open, render old/new values only for individually reviewed fields such as replica count. Render unknown or sensitive value changes without their raw values.
- Env values appear only in the app's Env view, with sensitive values masked by default and temporary reveal explicitly requested. Exclude raw specs/manifests, permissions payloads and arbitrary app JSON from the TUI. For advanced inspection, retain the separate existing CLI experience.
