# 10 · TUI browser refactor

## Goal

Give the app browser one package boundary and make its main responsibilities easy to find. Preserve command behavior, API semantics, mock mode, screen copy, keyboard behavior, and write safeguards.

## Before

The app browser lived in `internal/tui/browser.go` at about 3,200 lines. It combined the model, Bubble Tea event reducer, requests and cancellation, tenant/namespace/app navigation, input handling, and nearly all rendering. Related actions, history, request-state and mock-profile code shared `internal/tui` with two independent TUI programs: the describe viewer and agent setup wizard. `cmd/tui.go` was 247 lines and built the browser's live API adapters inline.

## After

The browser is now the `internal/tui/browser` package. Its main files are:

```text
internal/tui/browser/
  browser.go              exported browser API and options
  model.go                browser model and navigation/view state
  messages.go             typed asynchronous result messages
  update.go               Bubble Tea event dispatcher
  results.go              result application and scope checks
  keys.go                 app and focus keyboard handling
  overlay_keys.go         filter, sort and help input handling
  request_state.go        per-dataset request states
  request_registry.go     request IDs, cancellation and invalidation
  loaders.go              request creation and data loaders
  focus.go                focus and tenant navigation
  namespaces.go           namespace inventory and picker
  app_navigation.go       app-view cycling
  apps.go                 app sorting and visible selection
  tree.go                 app tree and filtering projection
  context.go              context pane rendering
  layout.go               app shell, panes and footer
  overlays.go             namespace and help overlays
  styles.go               shared rendering and terminal-safe text helpers
  summary.go              app Summary projection
  pods.go                 pod status view
  egress.go               outbound IP selection
  usage.go                current resource usage view
  usage_history.go        history loading, selection and rendering
  history_chart.go        chart scaling, plotting and quantities
  actions.go              action state, confirmation and writes
  actions_view.go         action dialogs and mouse geometry
  mock.go                 in-memory mock browser adapter
  mock_profile.go         strict fixture parsing and conversion
  *_test.go               tests grouped by browser behavior

cmd/
  tui.go                  flags, validation and launch
  tui_adapters.go         live browser adapters
  tui_usage.go            current and historical metric queries
  tui_auth.go             credential selection and refresh
```

The package contains 42 Go files including focused tests. `browser.go` is 82 lines, `update.go` 60, `results.go` 262, `keys.go` 239, and the largest production file is `summary.go` at 372 lines. The CLI command fell from 247 lines to 78; adapters and metric readers now have separate owners.

## Why `internal/tui/browser/`

The app browser is an independent Bubble Tea program with its own model, asynchronous data, mock fixture, navigation, and lifecycle actions. Putting it in a directory gives it a Go package boundary and keeps its private implementation separate from the describe viewer and agent setup wizard, which remain under `internal/tui`.

The move changes the import path, so browser tests and private helpers moved with the package and `cmd/tui.go` now imports `internal/tui/browser`. Existing exported entry point names and option/data types were retained. A browser-local escape key and footer-height constant replaced accidental dependencies on names declared by the separate describe viewer. `internal/output` remains shared.

## Work completed

- Moved browser implementation, mock profile, request state, actions, history, and related tests to `internal/tui/browser/`.
- Split the large browser source into model, messages, dispatch, results, key handling, requests, loaders, navigation, views, overlays and shared rendering files.
- Split action transition/write handling from action rendering, and history chart calculations from request/view behavior.
- Split the browser test file into focused app, navigation, tree, layout, summary, action and key tests. Updated test paths and fixtures to process batched commands and to account for the existing Summary/Usage/Pods cycle.
- Extracted live API adapter construction from `cmd/tui.go`; current and historical metric loops live in `cmd/tui_usage.go`.
- Added tests for pod readiness/restarts/previous container state and egress namespace/global/default route precedence.
- Kept describe and agent setup outside the browser package. Preserved write opt-in, confirmation, target revalidation, one-shot submission, MCP blocking, request scope checks, cancellation, fixture limits, and secret/input-value handling.

## Verification

- `go build ./...` passed.
- `go test ./internal/tui/browser -count=1` passed.
- `go test -race ./...` passed: 400 tests across 12 packages. The sandbox initially blocked local test listeners; the successful run used approved localhost access.
- `git diff --check` passed.
- `golangci-lint run ./...` could not start: the installed golangci-lint v2.12.2 was built with Go 1.26.2 and rejects this repository's Go 1.27 target. The repository guidance specifies v2.14.0.

No live Darkube writes or deployments were part of this refactor. The refactor is complete; remaining verification is to run the configured golangci-lint v2.14.0 in an environment whose binary supports Go 1.27.
