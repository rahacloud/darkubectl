# Builds view plan

Status: implemented in the working tree. `go build ./...` and `git diff --check`
pass. Automated tests, interactive terminal acceptance, live reads and live
writes were not run for this slice. Mock data below is fictional.

## Purpose and placement

Add Builds to pane 4, after Logs and before Env. Keep the selected app and the
other three panes visible. The tab is available for every selected app so an
unsupported or unknown creation method can be explained in place. A confirmed
Git repository app (`git_repo_url`) can load its recent builds. A known non-Git
app shows that Darkube builds are unavailable for its type and makes no build
request. If the creation method is missing, refresh app details before deciding;
if still unknown, explain that eligibility could not be determined.

## Layout and interaction

```text
╭─ [4] Summary Usage Scaling Logs [Builds] Env ───────────────────────╮
│ › ✕  Fix database connection                                        │
│      #819 · main · 8d91fab                 Nov 19 10:42 · 1m 20s    │
│   ✓  Add interactive gravity lab                                    │
│      #818 · main · 020ab88                 Nov 19 09:54 · 2m 04s    │
│   ◌  Update staging config                                          │
│      #817 · staging · 4ac213e              Nov 19 09:30 · running   │
╰─────────────────────────────────────────────────────────────────────╯
 q quit · ? help · a actions                 Enter output · r refresh
```

Follow the console's hierarchy using compact, two-line rows: commit message and
status glyph first, then build ID, branch, short commit, start time and duration.
At narrow widths, keep the status and commit message visible and truncate
secondary metadata with an ellipsis. Show missing values as `—`. Use `✓` for
succeeded, `✕` for failed, `◌` for initiated/running, `−` for canceled, and
`?` for unknown. Pair each glyph with its status text in the selected build's
detail and expose text in non-color mode; unknown statuses retain the API text.
Use green, red, lavender, muted and neutral colors respectively, with animation
limited to the existing loading indicator for active builds.

The list is newest first, initially limited to 30. Preserve selection by build
ID on refresh, clamping only when it disappears. Click selects a row and Enter
or a second click opens the selected build output. Scroll the list with normal
movement and page keys. Display loading, empty, unavailable, permission failure
and stale states distinctly; retain a same-app snapshot after a refresh failure.
Refresh is manual for the list.

```text
╭─ [4] Summary Usage Scaling Logs [Builds] Env ───────────────────────╮
│ ✕  Fix database connection                                          │
│ Build          819                Status       failed               │
│ Branch         main               Commit       8d91fab              │
│ Started        Nov 19 10:42       Duration     1m 20s               │
│                                                                     │
│ ── Build output ─────────────────────────────────────────────────── │
│ STEP 5/6: go build ./...                                            │
│ build exited with status 1                                          │
╰─────────────────────────────────────────────────────────────────────╯
 q quit · ? help · a actions   / search · n/N match · r refresh · Esc list
```

Enter fetches the selected ID from the list using `GetBuild`. Esc returns to
the same selection. Keep the selected commit message, glyph and metadata above
the output, like the console's selected-build screen; reserve the remaining
space for the scrollable output. Output supports scrolling, literal search, next/previous
match, copying retained output, and manual refresh. Reuse suitable Logs
rendering and selection patterns, but keep build output state separate from
app logs. Search and copy operate only on the selected build's retained output.
Keep all shortcuts in the footer and Quick Help, with view-specific hints.
An empty log, a missing build, and a failed read must remain distinct. Render
commit messages, errors and output as untrusted text, strip terminal control
sequences, bound retained output to 2 MiB, and mark truncation. Display only
bounded, explicitly handled error text; never dump raw error objects.

## Write mode

When `--allow-writes` is enabled for a confirmed Git app, `a` in Builds opens a
contextual action menu. The existing global app lifecycle menu remains accessible
outside Builds. Hide unavailable build actions and expose the same operations by
mouse and keyboard. In read-only mode, the build write actions are absent.

| Action | Availability and confirmation | Result |
| --- | --- | --- |
| Build and deploy latest commit | Git app; show tenant, namespace, app and known branch; explain the branch head is resolved by the server and a successful build deploys. Confirm with Cancel selected by default. | Call `BuildLatestCommit`. Show submission separately from build success and deployment. |
| Retry selected build | Build selected from this app's current list or its output screen; show app, build ID, branch and commit, and confirm with Cancel selected. | Call `RetryBuild` for that exact ID. The supplied console screenshot labels this as rebuilding and deploying that commit; confirm the API behavior before making the same promise in the TUI. |
| Stop selected build | Selected build has a freshly verified `initiated` or `running` status. Show app and build ID, then confirm with Cancel selected. | Call `StopBuild`; refresh detail/list and report the observed state. |

For every write, bind tenant, app ID and selected build ID before confirmation;
re-read the app and, for build-specific actions, verify the build still belongs
to that app's current list before submitting. For stop, refresh its status and
disable the operation if it has already finished or status is unknown. Prevent
duplicate submits while a write is in flight. A tenant/app switch or a changed
selection invalidates an open confirmation. Once submitted, keep the result
associated with the original target even if the user navigates away.

The start endpoint returns no build ID. Retry can also return no ID. Refresh the
list after submission and show newly observed builds as candidates, without
claiming a newly appearing build was caused by this request: another actor can
start one concurrently. If retry supplies an ID, verify it against a refreshed
list before selecting it. A failed or timed-out read after submission is an
unknown observation, not proof that the write failed. Never automatically retry
a write, including the GET endpoints that trigger builds. Report accepted,
rejected, or outcome unknown; a successful build and a completed deployment
require separate observed evidence.

## Implementation slices

1. Add list and detail reads to `BrowserOptions`, scoped client adapters, request
   state, navigation and mock fixtures. Reject stale results by tenant, app,
   build ID and request generation; cancel reads on target changes. Add the
   Builds tab and read-only list/output interactions.
2. Add build actions, explicit confirmation and write adapters. Keep mock
   operations in memory and visibly simulated. Maintain independent action
   state and prevent duplicate submits; implement post-submit observations.
3. Verify list/detail behavior with synthetic fixtures, delayed responses,
   app/tenant changes, narrow panes, mouse hits, clipboard and sanitized output.
   Cover read-only gating, confirmation targets, running/finished transitions,
   ambiguous responses and concurrent new builds. Exercise live writes only on
   an explicitly authorized disposable Git app; the existing code records the
   start, retry and stop routes as not yet exercised live.

## Source boundaries

`internal/client/builds.go` defines list, detail, retry, stop and branch-head
build-and-deploy operations. `cmd/build.go` confirms their CLI behavior and
shows that the start response does not identify the new build. `tui/04-logs-and-builds.md`
contains the initial read-only contract. The API read routes were previously
observed on Git-backed apps; write-route behavior and retry deployment effects
are not yet confirmed live.
