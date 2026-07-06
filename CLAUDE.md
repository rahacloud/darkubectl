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
  - `POST /api/v1/token/` — `{email,password}` + TOTP → `{access,refresh}`.
  - `POST /api/v1/token/refresh/` — `{refresh}` → `{access}`.
  - `wss://api.hamravesh.com/ws/aexec/?app_id=&pod_name=&container_name=` with
    `Sec-WebSocket-Protocol: terminal, <jwt-access>, <org>` and
    `Origin: https://console.hamravesh.com`.
- The JWT is a Console SimpleJWT access token: short-lived (~8h) and **IP-bound**
  (an `ip` claim), so it must be minted on the machine that connects. The
  refresh token is long-lived.
- DRF conventions: list envelope `{count,next,previous,results}`; error envelope
  `{detail,success,code}`. Numeric-looking fields can be strings with units
  (`ram_limit:"500M"`, `cpu_request:"250m"`).

## Provisional — `TODO(protocol)`, needs a live capture to confirm

Three things are best-guesses; do not treat them as verified:

1. `internal/auth` — the 2FA request shape. Assumed: TOTP in the `x-otp` header
   with `{email,password}` in the body (the header is in the API's CORS
   allowlist). Confirm from a console-login network capture.
2. `internal/wsexec` — the exec frame encoding. `encodeInput`/`encodeResize`/
   `decodeOutput` assume raw text in/out and a `{"resize":{"cols","rows"}}`
   control frame. Symptom of a wrong guess: the terminal **attaches but does
   nothing** (the server waits for a correct init/resize before spawning the
   PTY). Capture real frames with `darkubectl exec … --debug` or the console's
   DevTools → Network → WS → Messages tab, then fix those three functions.
3. REST accepting `Bearer` — the console uses JWT for REST, so it is
   high-confidence, but the scheme name (`Bearer` vs `JWT`) is set in one
   place: `client.BearerToken` in `internal/client/client.go`.

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
