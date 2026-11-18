# 04 · Logs and builds

```text
+ Logs / team-a / prod / billing-api -----------------------------------------+
| billing-7f2 / main / Previous     Paused     Tail 100     Buffered 83       |
| 12:30:15 starting billing                                                   |
| 12:30:17 database connection refused                                        |
|          retry budget exhausted                                             |
| 12:30:18 exiting                                                            |
| Search: connection refused                 Match 1/1                        |
+-----------------------------------------------------------------------------+
| t target  p current/previous  / search  n/N match  r refresh  Esc back      |
+-----------------------------------------------------------------------------+

+ Builds / team-a / prod / billing-api ---------------------------------------+
| BUILD   STATUS    BRANCH    COMMIT    STARTED                               |
| > 819   failed    main      8d91fab   5m                                    |
|   818   succeeded main      020ab88   48m                                   |
| Enter build output  r refresh  Esc back                                     |
+-----------------------------------------------------------------------------+

+ Build 819 / billing-api / failed -------------------------------------------+
| main @ 8d91fab    Started 12:27:00    Ended 12:28:20                        |
| STEP 5/6: go build ./...                                                    |
| build exited with status 1                                                  |
| / search   n/N match   r refresh   Esc builds                               |
+-----------------------------------------------------------------------------+
```

## App logs

1. Resolve the target within the selected app. If entered from a pod/container row, use exactly that selection. From the Logs tab, use the latest snapshot or fetch Pods if none exists. With one pod and one known container, select the only pair and display it. Multiple pods or containers require a picker. For a pod with unavailable container metadata, show `main (CLI default)` as an explicit selectable assumption. A vanished pod retains its old label and offers the picker. Never silently move to another pod.
2. Require Console access as the existing CLI does. Use the normal REST credential for `Client.AppLogs` and pass app UUID, pod, container, tail and previous flag. Expose capability and permission failures separately. Previous mode stops follow and starts a new buffer. Empty previous logs means **No previous logs returned**.
3. Start with a 100-entry requested tail and follow off. `f` toggles follow for current mode. Poll every 3s after the prior request completes, with one request in flight. `r` coalesces with an in-flight same-target read. Pause stops scheduling and cancels the active poll while preserving the buffer. Leaving the view or changing target cancels all pending work.
4. Keep API entries intact for search and retention so stack traces remain together. Merge the newest response by exact timestamp for that target, sort by the timestamp contract used by the client, and replace text for an already retained timestamp. Bound retention to the newest 2,000 entries and 2 MiB of sanitized log text, evicting oldest whole entries. Truncate a single oversized entry and show a marker. Notify about eviction or possible gaps. An advancing, full tail with no overlap is one gap signal. Manual refresh merges into the same target's buffer.
5. Show **Following · 3s**, **Paused** or **Retrying · Ns**. On transient follow errors, retain stale content, back off to at most 30s and reset on recovery. On auth errors, stop follow and offer relaunch guidance. On scrolling away from the bottom, retain the viewport anchor and show a new-entry count while polling continues. End or `G` resumes bottom anchoring. If retention drops the anchored entry, move to the oldest available entry and show why.
6. `/` searches retained entries by case-insensitive literal text. Enter commits, Esc restores the prior query while editing, and `n/N` navigate matches when the viewport is focused. Use `w` to toggle wrap and show full timestamps via a toggle exposed in actions. Paused and Previous modes still allow manual refresh. Leave export, copy and command shortcuts for a separate design.

Use a separate target/cache key for tenant, app, pod, container and current/previous. Render all output as untrusted text. Remove terminal control sequences including OSC and embedded escapes, preserve ordinary line breaks and tabs, and render no remote hyperlinks. Bound retained/rendered text and avoid forwarding raw output or errors to diagnostics. App and build logs can contain secrets emitted by the workload. Keep them memory-only and clear them on target/tenant change or exit. Use streaming or a response cap if memory measurements reveal oversized API responses before decoding.

## Builds

- Gate the tab by `CreationMethodGitRepoURL`. For other known types show **No Darkube builds for this app type** without making a build request. Unknown creation method should ask for a Summary refresh. Fetch `ListBuilds(appID, 30)` on entry and refresh. Show build ID, state, branch, short commit and timestamps where present.
- Enter opens `GetBuild(buildID)` only for a build selected from that app's results. Retain build identity and selection on return. Render a sanitized commit summary and log. Turn structured `Errors` into bounded plain text from approved fields or a generic failure indicator, with no raw object dump. Keep rendered build output to the same 2 MiB limit with a visible truncation marker.
- Build output refresh is manual in 1B. Success, failure and cancellation remain distinct. Preserve an unknown platform status as text. Render empty history, a missing build log and access-denied states separately.
