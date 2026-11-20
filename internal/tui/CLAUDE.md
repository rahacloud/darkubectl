# TUI engineering guidelines

These instructions apply to `internal/tui/` and complement the repository root
`CLAUDE.md` and `CONTRIBUTING.md`. The browser is in `browser/`, shared visual
tokens are in `theme/`, and the describe viewer and agent setup wizard are
separate programs. Command adapters and authentication live in `cmd/`.

## Design and interaction

- For substantial changes, inspect existing behavior and prepare a reviewable
  design with ASCII previews before implementation. Cover narrow and wide
  terminals, navigation, loading, empty, stale and error states, and the effects
  of each action. Treat proposals as proposals until their behavior is verified.
- Keep related operational data in the existing detail view; add a page or tab
  only when the workflow warrants it. Display the tenant, namespace and exact
  target clearly wherever a user can inspect or change infrastructure.
- Make every workflow usable with the keyboard. Keep help and footer hints
  contextual, accurate and in step with key handlers. Mouse support supplements
  keyboard navigation; keep rendering and hit-test geometry in sync. Text input
  and overlays receive keys before global shortcuts.
- Use the existing theme and Hamravesh console palette: violet accents, muted
  borders and restrained semantic colors. Let the terminal own its background.
  Convey focus, status and errors with text or symbols as well as color; honor
  `NO_COLOR`. Use terminal cell widths for layout, truncation and scrolling.
- Keep layouts legible on resize and small terminals. Preserve text selection
  and copying where possible. Treat Persian, mixed-direction text, Unicode and
  untrusted control characters carefully; verify actual terminal rendering and
  preserve the original text when submitting user input.

## Infrastructure and secret safety

- Live mode is read-only unless the user starts it with `--allow-writes`.
  Enforce this at the central callback boundary as well as in the UI. Hide
  unavailable write controls and shortcuts, including the pod terminal. Mock
  writes stay in memory and must never reach the API or modify the fixture.
- Bind each action to the selected tenant and resource ID when initiated.
  Show the target, intended change and meaningful consequences before a write;
  use confirmation appropriate to the operation. For destructive operations,
  require deliberate confirmation, including exact-name confirmation where
  supported. Prevent duplicate submissions while an operation is pending.
- Reuse existing auth, validation and command mutation paths. App field edits
  go through `runAppChange` and its shared prepared-change flow, including
  read-back verification. A successful HTTP response can mean acceptance only;
  report verification, rollout completion, failure and ambiguous outcomes
  accurately. Do not automatically retry a non-idempotent action after an
  ambiguous result. HTTP method alone does not establish that an API is read-only.
- Never expose credentials, tokens, secret values or database password
  references in logs, errors or structured output. Reveal and copy secrets only
  through explicit supported user actions. Sanitize remote text and errors before
  rendering them in a terminal. Use fictional data in fixtures and examples.
- Do not invent API capabilities or assume undocumented behavior. Use existing
  typed clients and established contracts; distinguish source evidence, mock
  behavior and live verification. Keep the TUI blocked through MCP.

## State and data

- Keep loading, refreshing, ready, empty, stale, failed and unavailable states
  distinct. A failed refresh may retain the last successful data for the same
  scope, marked stale with a retry hint; an initial failure is not an empty list.
- On tenant or selection changes, invalidate dependent data, overlays and
  requests immediately. Bind asynchronous results to request IDs and scope
  generations so late responses, including A → B → A, cannot appear under the
  wrong target. Keep independently loaded datasets and their errors separate.
- Fetch details only when needed. Preserve selection by stable ID when safe;
  do not silently substitute another resource when a target disappears. Treat
  missing values as unknown rather than zero and label account-wide versus
  tenant-scoped data accurately.
- Keep offline mock mode deterministic, visibly marked and independent of
  credentials and network access. Extend its synthetic fixtures when new
  workflows need representative data, including empty and failure cases.

## Verification

- Follow the root build, formatting, lint and test instructions. Add focused
  behavioral coverage for scope changes, stale responses, authorization,
  confirmation, failure and read-back where relevant. Tests must not contact
  the real API or read a developer's configuration.
- Check keyboard and mouse behavior, resize, small terminals, `NO_COLOR`, and
  visual alignment in an actual terminal for layout changes. Use mock mode for
  reproducible examples. Verify PTY handoff and restoration when changing the
  terminal workflow. Report automated checks, manual rendering and live API
  evidence separately; do not claim a check that was not performed.
