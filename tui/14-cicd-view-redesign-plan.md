# CI/CD view redesign plan

Status: proposed; design only. No implementation changes are included in this
plan. Mock values are fictional.

## Goal

Rename the pane 4 `Builds` tab to `CI/CD` and make its content match the
selected app's `build_method`. Give deployment setup and mode context priority
at the top of the page. Put the Darkube build table under an explicit
`History` heading, and shorten commit titles in that table to fit the pane.

## Mode selection

Use the app's `build_method` as the source of truth:

| `build_method` | Page content |
| --- | --- |
| `webhook` | Darkube deployment summary, repository/build configuration, then the Darkube build history table and selected build output. |
| `gitlabci` | Custom CI/CD explanation and app-specific deploy information. Do not render the Darkube build history table or Darkube build actions. |
| Missing or unrecognized | A clear unknown-mode state. Do not infer the mode from Git repository presence, image source, autodeploy flag, or token availability; do not render Darkube history. |

The `CI/CD` tab remains available across app types so users can understand
which deployment path is configured. “If the type is not Darkube do not render
it” is interpreted as hiding the Darkube-only history/table and actions for
Custom CI/CD, while keeping the contextual CI/CD page. If the intent is to
hide the entire tab for Custom CI/CD, adjust this before implementation.

## Page structure

### Darkube (`webhook`)

1. Page header and deployment mode.
2. Repository and configuration facts available from the selected app:
   repository, provider when known, branch, autodeploy state, builder, and
   Dockerfile/context/workdir and build arguments when present. Missing values
   display as `—`. Show argument names and mask values by default; support
   deliberate reveal/copy only if the source provides them, and clear any
   revealed values when the app changes.
3. `History` section with the existing newest-first build table.
4. Selecting a row continues to show the build's full commit message,
   metadata, and output in the detail state.

Build row commit titles are single-line and truncated with an ellipsis based
on the actual inner pane width. Keep status and title visible first; shorten
secondary metadata next. The full commit message remains available in detail.

### Custom CI/CD (`gitlabci`)

1. Page header identifies Custom CI/CD.
2. Briefly explain that the app's own pipeline builds/pushes an image and
   deploys it using the app ID and deploy token.
3. Show the app ID, with a copy action.
4. If a deploy token can be obtained through an existing supported client
   path, show it masked by default with deliberate reveal/copy actions. Never
   fetch or expose it automatically on page entry; clear revealed state when
   changing app. If the existing path is unavailable, show a direct, useful
   explanation rather than inventing a token source.
5. Do not show Darkube build history, output, retry, stop, or build-latest
   actions for this mode.

### Unknown mode

Show that the deployment method could not be determined and identify the
missing/unrecognized method if safe. Keep the page useful without guessing or
requesting Darkube builds.

## Preview

Darkube-backed app:

```text
╭─ [4] Summary Usage Scaling Logs [CI/CD] Env ─────────────────────────╮
│ ↻ DARKUBE                                                            │
│ Repository  gitlab.example/team/ara       Branch  main                │
│ Auto deploy On push                     Builder Dockerfile            │
│ Dockerfile  ./Dockerfile                 Context .                    │
│ Build args  NODE_ENV=•••••• · API_URL=••••••                         │
│                                                                      │
│ History                                             30 most recent   │
│ › ✓ Fix authentication timeout…  #123 · main · a1b2c3d  10:42 · 1m  │
│   ✓ Add deployment health check…  #122 · main · e4f5a6b  09:54 · 2m  │
│   ◌ Update staging configuration… #121 · staging · c7d8e9f  running │
│                                                                      │
│ Enter output · r refresh · a actions                                 │
╰──────────────────────────────────────────────────────────────────────╯
```

Custom CI/CD app:

```text
╭─ [4] Summary Usage Scaling Logs [CI/CD] Env ─────────────────────────╮
│ ⚙ CUSTOM CI/CD                                                       │
│ App ID       8a4d…c921                                      y copy   │
│ Deploy token ••••••••••••••••••                       v reveal c copy│
│                                                                      │
│ Deployment is triggered by your CI pipeline using the app ID and     │
│ deploy token.                                                        │
│                                                                      │
│ y copy app ID · v reveal token · c copy token                        │
╰──────────────────────────────────────────────────────────────────────╯
```

The mockup uses an abbreviated app ID and a masked token placeholder. No real
credential appears in the page or in this plan.

Unknown or absent method:

```text
╭─ [4] Summary Usage Scaling Logs [CI/CD] Env ─────────────────────────╮
│ ? DEPLOYMENT TYPE UNKNOWN                                             │
│ This app's deployment method could not be determined.                │
│ Method  —                                                           │
│ Darkube build history is unavailable until the deployment type is    │
│ known.                                                     r refresh │
╰──────────────────────────────────────────────────────────────────────╯
```

## Interaction and implementation boundaries

- Keep the current pane selection, keyboard/mouse tab selection, and
  view-specific footer behavior; rename all visible `Builds` references in
  the tab/help text to `CI/CD`.
- In Darkube mode, retain existing build list/detail behavior and write-mode
  gates. Only show Darkube build actions in this mode.
- In Custom CI/CD mode, copy/reveal actions are view-specific and must not
  include the token in logs, errors, debug text, or automatic clipboard writes.
- When the selected app or tenant changes, cancel/stale-guard pending reads and
  clear selected-build and revealed-token state.
- Keep the existing build APIs scoped to Darkube mode; do not make build-list
  requests for Custom CI/CD or unknown mode.
- Use the existing palette and glyph conventions. Preserve readable status
  text in addition to color/glyphs.

## Acceptance criteria

1. Pane tab and Quick Help call the view `CI/CD`.
2. `webhook` shows deployment facts followed by a `History` heading and a
   width-aware, shortened commit-title table.
3. Selecting a Darkube build still exposes its full commit message and output.
4. `gitlabci` shows custom deployment guidance and app ID, and does not render
   Darkube history or actions.
5. Unknown mode is explicit and never guesses based on unrelated fields.
6. Mode changes on app navigation do not leak stale history or revealed
   credentials.
7. Narrow pane widths preserve mode, status, and commit title while truncating
   metadata gracefully.
8. Darkube build arguments are included when available; their values are
   masked until deliberately revealed.

## Scope for the implementation pass

After approval, inspect the existing app fields and CI-config/deploy-token
client path, implement the mode-specific page, update help/docs, and verify
with fictional mock fixtures for Darkube, Custom CI/CD, unknown mode, narrow
width, app switching, and read-only/allow-writes states. Do not perform live
write operations as part of this UI work.
