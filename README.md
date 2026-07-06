# darkubectl

Kubectl-like access to the Hamravesh [Darkube](https://darkube.app) platform.

`darkubectl` talks to the Hamravesh API (`https://api.hamravesh.com`) to list and
manage your Darkube resources from the command line. Tenants map to Darkube
**organizations**, namespaces to **projects**, and apps to Kubernetes **apps**.

## Install

```sh
go install github.com/rahacloud/darkubectl@latest
```

Or build locally:

```sh
go build -o darkubectl .
```

## Authentication

Authentication uses a Hamravesh **account API key** plus an active **tenant**
(organization). Every request is sent as:

```
Authorization: Api-key <token>
X-Organization: <tenant-slug>
```

Configure them once:

```sh
darkubectl config set-token <your-api-key>
darkubectl config use-tenant <org-slug>     # e.g. rahacloud
```

Config is stored at `~/.darkube/config.yaml` (override with `$DARKUBE_CONFIG`).
Values can also be supplied via environment or flags, which take precedence:

| Setting  | Flag          | Environment      | Config key       |
| -------- | ------------- | ---------------- | ---------------- |
| Token    | `--token`     | `DARKUBE_TOKEN`  | `token`          |
| Tenant   | `--org`/`-n`  | `DARKUBE_ORG`    | `current-tenant` |
| Base URL | `--base-url`  | `DARKUBE_BASE_URL` | `base-url`     |
| Config   | `--config`    | `DARKUBE_CONFIG` | —                |

## Usage

```sh
# Tenants (organizations)
darkubectl get tenants
darkubectl config use-tenant talaland

# Apps
darkubectl get apps                    # table
darkubectl get apps -o wide            # + cluster, RAM, CPU, domain, id
darkubectl get apps -o json
darkubectl describe app <name|id>      # full object (YAML)

# Other resources
darkubectl get namespaces              # projects (derived from apps)
darkubectl get certificates
darkubectl get plans

# Mutations (all prompt for confirmation; pass -y to skip)
darkubectl scale app <name|id> --replicas 3
darkubectl patch app <name|id> -p '{"ram_limit": "1024M"}'
darkubectl delete app <name|id>
```

Output format is controlled by `-o/--output`: `table` (default), `wide`, `json`,
`yaml`, or `name`. Scope any single command to a different tenant with `-n <org>`.

## Development

```sh
go build ./...
golangci-lint run ./...
```

The codebase is layered:

- `internal/client` — Darkube API client (built on [resty](https://resty.dev)).
- `internal/config` — layered config loading (file + env) via [koanf](https://github.com/knadh/koanf).
- `internal/output` — table / JSON / YAML rendering.
- `cmd` — the [urfave/cli](https://cli.urfave.org) command tree.
