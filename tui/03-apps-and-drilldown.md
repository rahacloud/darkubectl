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

```

## Apps

- The `n` namespace picker lists All plus the tenant's complete project inventory, keyed by namespace ID so same-name namespaces remain distinct. Include empty projects, cluster and app counts. If `ListNamespaces` fails, derive options from the single already-loaded Apps snapshot and label that empty namespaces are unavailable. Keep that fallback synchronized if the Apps snapshot arrives after the picker request. Selecting a namespace filters the local snapshot only; it triggers no list request. A narrow left rail contains equal-height Tenant and Pages panes beside the Apps and selected-item detail workspace. Summary, Resources, Logs, Builds, Env, Domains and Ports belong to the selected app's drilldown. Terminal is available from the app action menu. Summary includes the selected app's pod/container snapshot and outbound IP.
- Use `Client.ListApps` once per tenant refresh and share its snapshot with the namespace fallback, picker and Overview. Keep loaded rows keyed by app UUID. Use the namespace ID for filtering. Preserve rows with incomplete metadata and label missing fields.
- Default sort is app name, namespace name, namespace ID, then UUID. `s` opens a chooser for name, namespace, state and last updated, with both directions. Missing data sorts after known data. Filter literal text over app name, namespace name, state label and creation method, with no remote requests.
- At minimum width show Name, Namespace and State. Add Desired and Type first as width permits, then the Preview pane at 100+ columns. Show plan or request sizing, image and domain in Preview when those fields exist. Format sizing using the source units or the existing CLI convention. Layout changes never trigger a new fetch.
- Use `App.State.Text`, then `StateType`, then Unknown. Use `IsManagedService` and `CreationMethod`. `App.Replicas` is desired scale. Keep zero distinct from unavailable when the API omits the field. If a typed scalar cannot distinguish missing from zero, confirm fixture behavior or add a minimal presence-aware projection without breaking existing CLI outputs.
- Preserve the selected UUID across sort and refresh. If it disappears, clamp the cursor to the nearest remaining row. An already-open detail that gets a target-specific 404 shows **App no longer available** and offers a return to the refreshed list. It never jumps to a same-name app.

## App view contracts

For a selected app, the detail views are Summary, Resources, Logs, Builds, Env, Domains and Ports. Pod and container status stays in Summary; Logs keeps its exact pod/container target picker. There is no separate Pods view or pod detail page planned. `[` moves to the previous view and `]` to the next; navigation wraps at the ends without changing the selected app in the Apps tree. Summary and Resources support PgUp/PgDn scrolling while keeping their tabs and section titles visible; Summary also keeps its Identity and Name rows visible. Domains shows external hosts with DNS and platform-certificate status, the ingress CNAME target, DNS challenge CNAME names and targets, the platform subdomain label and cluster suffix, and supported SSL, HTTPS redirect, and HTTP/2 fields. Write mode stages external domain additions/removals, platform subdomain set/replace/clear, and ingress settings for one diff review before save. DNS challenge records can be copied individually.

| View | Contract |
| --- | --- |
| Summary · 1A | Start from the selected list row and refresh with `GetAppTyped(id)`. Show state and description, tenant, namespace/cluster, selected app ID, desired replicas, plan/resources, creation method, image or known source type, domains and update time when present. In Deployment, map `build_method=webhook` to `↻ Darkube` and `build_method=gitlabci` to `⚙ Custom CI/CD`; otherwise show `? Unknown` rather than infer from Git/image type or token presence. Never retain the token value. When the API returns them, also show Service type, primary Port Service, Internal Address (including its selected service port when unambiguous), Service External IP, readiness probe, command and input names. Resolve Outbound IP from `GET /ip-management/api/egress-route/`, preferring a matching namespace route, then an all-namespaces route, then the cluster default. The future Ports view can list every port mapping. Display allowlisted fields and separate observations from configured values; never retain or display input values. |
| Resources | Combine plan, CPU/RAM limits, disk size, replicas, autoscaling, current usage and the existing per-pod history charts. In write mode, navigate configuration with arrows or Vim keys, `e` edits a selected value and toggles autoscaling, `s` reviews a diff, and confirmation saves. CPU/RAM edits apply only to dynamic plans; fixed plans set those values. Disk can only grow, and only when already configured. Replica count is editable while autoscaling is off; min/max replicas and CPU target are editable while it is on. `m` cycles metrics, `w` cycles 1h, 6h and 24h windows, and `p` cycles all pods and individual pods. Disk Usage appears only when the app has a configured disk. The chart keeps per-pod colors and capacity reference lines. PageUp/PageDown scrolls through the page. Billing exposes organization wallets and invoice totals without app attribution, so Resources must not present those totals as per-app cost. |
| Builds | Show recent Git builds and sanitized output. With `--allow-writes`, offer confirmed build-and-deploy latest, retry selected build, and stop selected running build; keep acceptance separate from observed results. |
| Env | Separate Plain and Secret sections. Always show plain values. Secrets are hidden initially; `v` reveals or hides the selected secret and `y` copies the selected plain or secret value. With `--allow-writes`, `e` edits a value, `+` adds plain, `A` adds secret, and `d` stages deletion. Each row marks staged additions, edits and removals. Press `s` to review the old/new diff, with secret values masked, then confirm once to save the batch; `x` discards staged changes. Saving updates the app environment and restarts its pods. |
| Terminal | Open Terminal from the app action menu (`a`, then `t`), choose an exact pod and container, and attach using the existing terminal transport. The session prints app/pod/container and control guidance to stdout. Ctrl+D exits, Ctrl+C is passed to the remote process, Ctrl+L clears the screen; normal terminal copy/paste shortcuts work through the PTY. This is a remote app shell, not an arbitrary local command runner. |

If Console access is absent, keep Summary accessible and mark Terminal unavailable. A socket auth failure gets a re-login hint. A timeout, an empty result and missing credentials each get distinct states. Render pod data as a snapshot. Keep WebSocket watching optional and deferred.

## Domains and History · Phase 2 reads

```text
+ Details / team-a / prod / gateway -------------------------------------+
| Summary [Resources] Logs Builds Env Domains Ports                     |
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
