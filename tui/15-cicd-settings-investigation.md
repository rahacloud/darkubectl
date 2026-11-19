# CI/CD settings editing investigation

Date: 2026-11-19. Scope: investigation and local TUI implementation for editing
the Darkube settings shown in pane 4. No app configuration was changed.

## Findings

- The selected app's `build_method` selects Darkube (`webhook`) or Custom
  CI/CD (`gitlabci`). The CI/CD view shows App ID and a masked deploy token
  for either mode, and shows build history for Darkube (`webhook`).
- On the v1 app detail endpoint, live `OPTIONS` marks the following fields
  writable, but does not guarantee that a particular value is stored:

| Field | Metadata | Existing CLI apply |
| --- | --- | --- |
| `git_repo_url` | string, maximum length 1024 | Yes |
| `git_branch_name` | string, maximum length 128 | Yes |
| `git_provider_type` | `Github` or `Gitlab` | Yes |
| `git_build_dockerfile` | string, maximum length 128 | Yes |
| `git_build_context` | string, maximum length 128 | Yes |
| `git_build_workdir` | string, maximum length 128 | Yes |
| `builder` | `dockerfile` or `heroku_buildpacks` | Yes |
| `autodeploy_on_git_push` | boolean | Yes |
| `git_build_args` | writable `field`; item schema unspecified | No |
| `build_method` | `gitlabci` or `webhook` | Yes |

- Live v1 detail for the Darkube-managed `lms-landing` app returned four
  build arguments, each an object with string `key` and `value` fields. The
  v2 app list returned the same shape for this app and two custom CI/CD apps.
  Only structure and counts were recorded; values were not recorded. This
  confirms the **read** shape, not that the server accepts the same shape on
  write, its validation rules, or the effects of saving it.
- Argument values are returned by the API, not represented as vault-backed
  secret environment variables. They can contain sensitive values and should
  remain masked in the TUI by default. The current view already handles
  `{key,value}` when displaying and revealing them.
- `cmd/apply.go` maps the scalar settings to the full app object, but skips
  empty strings; it cannot clear one through this spec, and does not include
  build arguments. `internal/client/update.go` performs a v1 full-object PUT
  after reading and normalizing the app. `cmd/mutate.go` explains why an
  accepted 202 needs read-back. The new typed client update path uses a
  read-modify-write, checks the staged baseline, and verifies the stored values.
- The CI/CD editor keeps staged settings and its cursor separate from Env,
  preserving build-list navigation and build output state.

## Implemented editor

When writes are enabled, select a configuration field and press `e` or Enter
to edit it inline. The view shows repository, branch, Dockerfile path, build
context, workdir, auto-deploy, and build arguments; Provider and Builder are
left out of the view. Build arguments support add, edit-value and delete while
keeping their original order. `t` stages a switch between Darkube and Custom
CI/CD. Changes remain staged until reviewed with `s` and confirmed with
`y`/Enter; `x` discards them. Review output masks argument values. The app ID
and masked deploy token remain visible in either deployment mode.

The save binds tenant and app ID, rereads the current app, checks that the
deployment mode and settings still match the original snapshot, then updates
the staged fields on the fresh object. A changed field or mode produces a
conflict instead of silently overwriting someone else's change. It preserves
unrelated fields and validates known choices and lengths. Because the backend
accepts a whole-object PUT, another writer can still race between the last
read and PUT unless the API offers a conditional update; such support has not
been established.

Re-read after saving and compare the intended fields, including argument
entries and deployment mode, before reporting them stored. A successful
read-back verifies stored configuration, not that a build ran or deployment
completed. Never show argument values in diffs, errors or logs. Clear staged
and revealed data on target changes.

## Evidence boundaries and remaining questions

1. Metadata says fields are writable; live changes to each Git setting and
   argument add/edit/delete have **not** been exercised. The observed argument
   list shape is not a confirmed write contract.
2. It is unknown whether changing any setting immediately starts a build,
   causes a rollout, updates a webhook, or only affects the next build.
3. It is unknown whether argument names must be unique, whether empty values
   or deletions are accepted, and whether order is meaningful.
4. Provider/repository changes may require a connected Git integration; the
   create path documents that prerequisite. Whether changing an existing app
   requires extra console calls is not established.
5. Public console source maps could not be reached in this environment. The
   editor's exact console write flow and build-argument validation therefore
   remain unverified.
6. A controlled write/read-back on a specifically authorized disposable
   Git-backed app, followed by build/rollout observation, is needed to close
   these questions. Do not use a production app for the experiment.

The full repository build passes after this implementation. Tests and live
write behavior, including mode switching, remain unverified.
