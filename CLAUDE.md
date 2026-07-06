# CLAUDE.md

Guidance for AI assistants (Claude Code) working in this repository. Human
usage lives in `README.md`; this file is the engineering/agent context.

## What this is

`darkubectl` — a kubectl-like CLI for the Hamravesh **Darkube** platform. Go
1.26, built on `urfave/cli/v3` (not Cobra). It talks to the Hamravesh REST API
and an exec websocket.

## Build / test / lint

```sh
go build ./...
go test -race ./...
golangci-lint run ./...   # must report 0 issues before committing
gofmt -w .
```

CI (`.github/workflows/ci.yml`) runs `golangci-lint` (v2.12.2) and
`go test -race` on push to `main` and on PRs, using the Go version from
`go.mod`.

## Architecture

- `cmd/` — the `urfave/cli/v3` command tree. Root + shared helpers in `app.go`;
  one file per command group (`get`, `describe`, `scale`, `patch`, `delete`,
  `login`, `exec`, `terminal`, `config`). `session.go` holds exec/terminal
  helpers. Auth is resolved once in `app.go`'s `resolveAuth`.
- `internal/client` — REST client on `resty/v3`. `Auth` is either `APIKey(...)`
  or `BearerToken(...)`; every request also sends `X-Organization`.
- `internal/auth` — Console JWT mint/refresh (SimpleJWT).
- `internal/wsexec` — exec websocket transport on `coder/websocket`.
- `internal/output` — table / JSON / YAML rendering plus the colorized
  `describe` view (`lipgloss`; auto-degrades to plain text when not a TTY).
- `internal/tui` — interactive Bubble Tea viewers (the `describe -i` viewer).
- `internal/config` — koanf-based config: `~/.darkube/config.yaml` layered with
  `DARKUBE_*` env vars (`DARKUBE_ORG` → `current-tenant`, etc.), written 0600.

## The Darkube API (reverse-engineered — official docs are not reachable)

- Base: `https://api.hamravesh.com` — public IP, call it **without** a proxy.
  The `*.darkube.app` hosts resolve to a private IP and are not usable directly.
- Every request is tenant-scoped by `X-Organization: <org-slug>` (403
  `permission_denied` without it). Tenant = organization; namespace = project;
  app = app.
- REST auth is either scheme: `Authorization: Api-key <k>` **or**
  `Authorization: Bearer <jwt>`.
- Key endpoints:
  - `GET /api/v2/darkube/apps/?limit=&offset=&fields=` — list; `GET/PATCH/DELETE
    /api/v2/darkube/apps/<uuid>/` for one app.
  - `GET /api/v1/darkube/plans/` (global), `.../certificates/`.
  - `POST /api/v1/darkube/apps/` — **create** (v1, not v2). Body needs `svc`
    `{type,ports}`, `custom_config`, `builder`, `ssl_challenge_type`,
    `organization` (numeric id, from an app's v1 detail), `namespace` (int),
    `plan`. Requires JWT (user context); the Api-key 500s. See
    `client.buildCreatePayload`.
  - `POST /api/v1/token/` — `{email,password}` + TOTP → `{access,refresh}`.
  - `POST /api/v1/token/refresh/` — `{refresh}` → `{access}`.
  - `wss://api.hamravesh.com/ws/aexec/?app_id=&pod_name=&container_name=` with
    `Sec-WebSocket-Protocol: terminal, <jwt-access>, <org>` and
    `Origin: https://console.hamravesh.com`.
  - `wss://api.hamravesh.com/ws/app-pods/?app_id=` with subprotocol
    `json, <jwt-access>, <org>` — streams pods as JSON; the **only** source of
    pod names (REST `state.pods` is empty; `/ws/app-state/` carries only
    aggregate replica counts). Parsed in `internal/appstate`.
- The JWT is a Console SimpleJWT access token: short-lived (~8h) and **IP-bound**
  (an `ip` claim), so it must be minted on the machine that connects. The
  refresh token is long-lived.
- DRF conventions: list envelope `{count,next,previous,results}`; error envelope
  `{detail,success,code}`. Numeric-looking fields can be strings with units
  (`ram_limit:"500M"`, `cpu_request:"250m"`).

## Confirmed vs unverified

Confirmed against a live session:

- **2FA login** — `POST /api/v1/token/` with `{email,password}` and the TOTP in
  the `x-otp` header works (`darkubectl login`).
- **Exec frame protocol** — the Kubernetes remotecommand channel protocol:
  binary frames prefixed with a 1-byte channel id (0 stdin, 1 stdout, 2 stderr,
  3 exit-status → ends the session, 4 resize as `{"Width","Height"}`). See the
  channel constants in `internal/wsexec`.
- **REST `Bearer` auth** — `Authorization: Bearer <jwt>` works for the whole REST
  API (the `ip` claim is not enforced for REST). The Api-key and the Console
  login are different principals with different per-app access, so the login is
  the full-access path.

All the reverse-engineered surfaces are now confirmed. `create app` requires the
JWT (the Api-key lacks the user context and 500s); its numeric `organization`
field is resolved from an existing app's v1 detail (`client.OrganizationID`).

## Conventions

- Keep `golangci-lint` at **0 issues**. `.golangci.yml` runs `default: all` with
  a few opinionated linters disabled and scoped exclusions — the comments there
  explain each; prefer fixing code over adding disables.
- Prefer fixing the real finding; when a rule is a false positive, use a
  targeted `//nolint:<linter> // reason` rather than disabling it globally.
- Markdown is linted by markdownlint (`.markdownlint.jsonc`, `MD013` off). Run
  `npx markdownlint-cli2 <file>` after editing docs.
- Never persist secrets beyond the 0600 config file; never log tokens (the
  `--debug` frame dumper logs terminal I/O, not credentials).
- Commit messages end with the trailer
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
