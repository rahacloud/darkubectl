# CLAUDE.md

Guidance for AI assistants (Claude Code) working in this repository. Human usage lives in `README.md`; this file is the engineering/agent context.

## What this is

`darkubectl` — a kubectl-like CLI for the Hamravesh **Darkube** platform. Go 1.26, built on `urfave/cli/v3` (not Cobra). It talks to the Hamravesh REST API and an exec websocket.

## Build / test / lint

```sh
go build ./...
go test -race ./...
golangci-lint run ./...   # must report 0 issues before committing
gofmt -w .
```

CI (`.github/workflows/ci.yml`) runs `golangci-lint` (v2.12.2) and `go test -race` on push to `main` and on PRs, using the Go version from `go.mod`.

## Architecture

- `cmd/` — the `urfave/cli/v3` command tree. Root + shared helpers in `app.go`; one file per command group (`get`, `describe`, `scale`, `patch`, `delete`, `login`, `exec`, `terminal`, `config`). `session.go` holds exec/terminal helpers. Auth is resolved once in `app.go`'s `resolveAuth`.
- `internal/client` — REST client on `resty/v3`. `Auth` is either `APIKey(...)` or `BearerToken(...)`; every request also sends `X-Organization`.
- `internal/auth` — Console JWT mint/refresh (SimpleJWT).
- `internal/wsexec` — exec websocket transport on `coder/websocket`.
- `internal/output` — table / JSON / YAML rendering plus the colorized `describe` view (`lipgloss`; auto-degrades to plain text when not a TTY).
- `internal/tui` — interactive Bubble Tea viewers (the `describe -i` viewer).
- `internal/kube` — reads Deployments out of a cluster by shelling out to `kubectl` (no client-go dependency; inherits the user's kubeconfig, context and OIDC exec-plugin auth) and diffs them against the app list. Backs `get orphans`. `Diff` is pure and unit-tested; only `Deployments` touches the outside world.
- `internal/config` — koanf-based config: `~/.darkube/config.yaml` layered with `DARKUBE_*` env vars (`DARKUBE_ORG` → `current-tenant`, etc.), written 0600.

## The Darkube API (reverse-engineered — official docs are not reachable)

- Base: `https://api.hamravesh.com` — public IP, call it **without** a proxy. The `*.darkube.app` hosts resolve to a private IP and are not usable directly.
- Every request is tenant-scoped by `X-Organization: <org-slug>` (403 `permission_denied` without it). Tenant = organization; namespace = project; app = app.
- REST auth is either scheme: `Authorization: Api-key <k>` **or** `Authorization: Bearer <jwt>`.
- Key endpoints:
  - `GET /api/v2/darkube/apps/?limit=&offset=&fields=` — list; `GET/PATCH/DELETE /api/v2/darkube/apps/<uuid>/` for one app.
  - `GET /api/v1/darkube/plans/` (global), `.../certificates/`.
  - `GET /api/v1/darkube/namespaces/` — paginated DRF list of projects, with `id`, `name` and the nested `cluster`. Needs a **JWT**; the Api-key is rejected, which is why `NamespacesFromApps` exists as a fallback. Prefer `client.Namespaces`, which tries this first: the derived list can only contain namespaces that already hold an app, so a freshly created empty project is invisible to it — exactly when you need its id in order to create the first app in it.
  - `POST /api/v1/darkube/apps/` — **create** (v1, not v2). Body needs `svc` `{type,ports}`, `custom_config`, `builder`, `ssl_challenge_type`, `organization` (numeric id, from an app's v1 detail), `namespace` (int), `plan`. Requires JWT (user context); the Api-key 500s. See `client.buildCreatePayload`.
  - `GET /api/v1/darkube/apps/<uuid>/app_log/` — container logs. Query: `pod_name`, `container_name`, `previous` (the crashed instance, like `kubectl logs -p`), and an index window `from_index`/`to_index` anchored by `reference_index`. The window is absolute, not time-based; the server clamps an out-of-range window to the end, so the tail is `from_index=20000000` (the console's own sentinel) and `to_index=20000000+N`. Response is `{"logs": {"<rfc3339>": "<text>"}, "reference": <int>}` — an **object**, so ordering is lost in JSON and has to be restored by sorting the keys. Entries can hold several physical lines separated by ` \n `.
  - `POST /api/v1/token/` — `{email,password}` + TOTP → `{access,refresh}`.
  - `POST /api/v1/token/refresh/` — `{refresh}` → `{access}`.
  - `wss://api.hamravesh.com/ws/aexec/?app_id=&pod_name=&container_name=` with `Sec-WebSocket-Protocol: terminal, <jwt-access>, <org>` and `Origin: https://console.hamravesh.com`.
  - `wss://api.hamravesh.com/ws/app-pods/?app_id=` with subprotocol `json, <jwt-access>, <org>` — streams pods as JSON; the **only** source of pod names (REST `state.pods` is empty; `/ws/app-state/` carries only aggregate replica counts). Parsed in `internal/appstate`.
- **App detail fields worth knowing** (from `GET .../apps/<uuid>/`). All of these *can* be set on `POST` — and only there, since PATCH 500s:
  - `disk` — `{partitions:[{display_name,mount_path,sub_path}], set_fsgroup, size_in_Gi, storage_class_name}`. `storage_class_name` is **required**: omitting it returns 500, not a validation error. `rawfile-btrfs` is valid on c11 and c13.
  - `svc` — `{type, ports:{<name>:{containerPort,servicePort,nodePort,protocol}}}` plus read-only `internalAddress` / `externalIP`. Omit `ports` and the app exposes nothing.
  - `envs` / `secret_envs` — lists of `{name, value}`. The key is `name`, not `key`; `key` returns a bare 400 `invalid` with an empty `detail`.
  - `trigger_deploy_token` — the app's CI deploy token, the `--token` half of `darkube deploy --app-id <id> --token <tok>`. Read-only as far as anyone has tried, present on the v2 detail, and stored nowhere in the cluster, so the API is the only way to wire up a pipeline without the console. `get deploy-token` prints it; it is deliberately **not** a field on the `App` struct, or `get apps -o json` would dump every app's token.
  - `namespace.cluster.has_capacity_to_create_app` — whether the cluster will accept a new app at all. Worth checking before a create: as of 2026-08-11 `hamravesh-c13` reports `false` while `hamravesh-c11` reports `true`.
- The JWT is a Console SimpleJWT access token: short-lived (~8h) and **IP-bound** (an `ip` claim), so it must be minted on the machine that connects. The refresh token is long-lived.
- DRF conventions: list envelope `{count,next,previous,results}`; error envelope `{detail,success,code}`. Numeric-looking fields can be strings with units (`ram_limit:"500M"`, `cpu_request:"250m"`).

## Confirmed vs unverified

Confirmed against a live session:

- **2FA login** — `POST /api/v1/token/` with `{email,password}` and the TOTP in the `x-otp` header works (`darkubectl login`).
- **Exec frame protocol** — the Kubernetes remotecommand channel protocol: binary frames prefixed with a 1-byte channel id (0 stdin, 1 stdout, 2 stderr, 3 exit-status → ends the session, 4 resize as `{"Width","Height"}`). See the channel constants in `internal/wsexec`.
- **REST `Bearer` auth** — `Authorization: Bearer <jwt>` works for the whole REST API (the `ip` claim is not enforced for REST). The Api-key and the Console login are different principals with different per-app access, so the login is the full-access path.
- **`GET /api/v1/darkube/namespaces/`** — works with a JWT, 200 with the full project list including empty projects. Confirmed 2026-08-11.
- **`GET .../app_log/`** — confirmed against running apps, including a pod that was not Ready. This is the only diagnostic that works on a failing container: the exec websocket 403s unless the pod is Ready, so `logs` is what you reach for when an app will not start.
- **The websocket subprotocol is not a selector.** `/ws/aexec/`, `/ws/app-pods/` and `/ws/app-state/` each accept `terminal`, `json`, `logs` or `log` interchangeably — only the 2nd (token) and 3rd (org) values matter. Unknown `/ws/…` paths return 500 on the handshake rather than 404, so a 500 there means "no such route", not an outage.

- **`POST` accepts the full app shape** — `svc.ports`, `disk` and `envs` all persist when sent at creation. Verified 2026-08-11 by creating throwaway apps and reading each field back, including a `rawfile-btrfs` PVC that bound and came up healthy on c11.

Known broken, confirmed 2026-08-11:

- **`PATCH /api/v{1,2}/darkube/apps/<uuid>/` returns 500** with an empty body for every field tried — a scalar (`readiness_probe_path`), `replicas`, and the nested `svc`. Both API versions behave identically, so the v1-for-writes rule that rescues `create` does not apply. This makes `patch app` and `scale app` non-functional and makes `POST` the only chance to get an app's configuration right: fixing one afterwards means delete-and-recreate.
- **Delete is asynchronous and can orphan the release.** After `DELETE` the app leaves the list immediately, but its Helm release is removed separately and may not be removed at all. Recreating under the same name then fails: `TerminatingAppException` ("try again in a minute") while the delete is in flight, and `SameHelmReleaseNameExists` indefinitely if the release was left behind. There is no way out through the API — no `force`/`overwrite`/`adopt_existing` flag on create, no `helm_release_name` override, and no release endpoint (all 404). Clearing an orphan needs the Darkube console or Hamravesh support. **This is the common case, not the edge case**: of the thirteen Deployments in `talaland-dev` on 2026-08-18, ten were orphans — every app deleted from that namespace had left its release running, some for over a week. `get orphans` exists to find them, since neither the API nor the cluster shows the divergence on its own. The API distinguishes the two collisions itself: `DuplicateReleaseAndNamespaceException` means a live app really does hold the name, `SameHelmReleaseNameExists` means only a release does — `cmd.explainCreateError` turns both into English guidance on stderr.

Every reverse-engineered surface is confirmed except PATCH, noted above. `create app` requires the JWT (the Api-key lacks the user context and 500s); its numeric `organization` field is resolved from an existing app's v1 detail (`client.OrganizationID`).

## Conventions

- Keep `golangci-lint` at **0 issues**. `.golangci.yml` runs `default: all` with a few opinionated linters disabled and scoped exclusions — the comments there explain each; prefer fixing code over adding disables.
- Prefer fixing the real finding; when a rule is a false positive, use a targeted `//nolint:<linter> // reason` rather than disabling it globally.
- Markdown is linted by markdownlint (`.markdownlint.jsonc`, `MD013` off). Run `npx markdownlint-cli2 <file>` after editing docs.
- **Markdown is written one paragraph per line**, `README.md` and this file included. Do not hard-wrap prose: each paragraph, and each list item including its continuation, is a single physical line, however long. `MD013` is off precisely so this is allowed. Reflowing to a column width produces a diff touching every line of a paragraph for a one-word change, which is what this avoids. Code fences, tables and the HTML header block are exempt — leave their line structure alone.
- Never persist secrets beyond the 0600 config file; never log tokens (the `--debug` frame dumper logs terminal I/O, not credentials).
- Commit messages end with the trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
