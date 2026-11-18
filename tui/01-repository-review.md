# 01 · Repository review and integration points

Darkubectl is a Go CLI for the Hamravesh Darkube platform. Its core domain is account → tenant (organization) → namespace (Darkube project) → app. Apps cover both workloads and managed services. It supports REST inspection and app changes, WebSocket-based pod discovery and terminals, logs, builds, registries, storage, tickets, CLI automation and an MCP facade. Orphan reconciliation is a separate CLI flow backed by local `kubectl`.

```text
main.go
  +-- cmd.NewApp (urfave/cli/v3)
        +-- existing CLI commands
        +-- describe -i / agent setup -> internal/tui
        +-- tui (proposed) ----------> internal/tui
                 command adapters ---> internal/client    (REST)
                                  +--> internal/auth      (Console JWT)
                                  +--> internal/appstate  (pod WebSocket)
```

| Existing area | What to reuse or account for |
| --- | --- |
| [`main.go`](https://github.com/rahacloud/darkubectl/blob/15da4b9792a7280086d99abe74e8093d08d8d41b/main.go), [`cmd/app.go`](https://github.com/rahacloud/darkubectl/blob/15da4b9792a7280086d99abe74e8093d08d8d41b/cmd/app.go) | Register the opt-in command in `NewApp`. Reuse config and auth resolution. Existing `buildClient` requires a tenant, so bootstrap via the global path or an explicit-org constructor that shares its resolution. |
| [`internal/tui`](https://github.com/rahacloud/darkubectl/tree/15da4b9792a7280086d99abe74e8093d08d8d41b/internal/tui), `internal/output` | `RunDescribe` already provides a searchable viewport. The agent setup wizard is a second Bubble Tea v2 model with key-routing tests. Reuse patterns and palette tokens as appropriate. The describe screen flattens a raw object and should not be the new Summary projection. |
| `internal/config`, `internal/auth`, `cmd/session.go` | Layered config, JWT resolution and refresh. REST uses an API key first, then Console JWT. Console access is an independent capability when both are configured. |
| `internal/client` | Typed and raw app reads, paginated lists, logs, builds, Profile, namespace inventory, alerts, notifications and shared update behavior. It owns transport and response decoding. |
| `internal/appstate`, `internal/wsexec`, `internal/kube` | Pod discovery is a Console JWT WebSocket. Interactive shell is another WebSocket. Local `kubectl` use belongs to orphan reconciliation. |
| `cmd/mutate.go`, `cmd/rollout.go`, `cmd/build.go` | Existing field edits use read-modify-write and read-back, restart can track old pod names, and build operations have Git-app guards. Extract shared operations when writes are implemented. |
| `cmd/mcp.go`, `internal/agentsetup` | MCP runs CLI calls in subprocesses and classifies unknown paths as writes. Explicitly block `tui` as an interactive command so it stays blocked even when MCP writes are enabled. |

The repo pins Go 1.27, Bubble Tea v2, Bubbles v2 and Lipgloss v2. Use the checked-in versions and the repository’s CI and lint conventions.

## What the draft needed to settle

| Draft idea | Repository finding | Implementation decision |
| --- | --- | --- |
| Tenant picker from `whoami` | `Client.Profile` is account-wide. The repo confirms JWT support and records API-key support as unverified. `get tenants` reads local settings. | A failed Profile read leaves a configured tenant usable. Offer local tenant slugs as an explicitly labeled fallback and confirm actual access with Apps. |
| Namespace selector | The full namespace endpoint needs JWT according to the repo. App-derived namespaces omit empty projects. Names can collide across clusters. | Use numeric ID internally. Show inventory provenance. Reuse App rows for the fallback to avoid duplicate pagination. |
| Ready/desired and dashboard health | Typed `App.Replicas` is desired. REST has no typed ready count or reliable universal severity taxonomy. `wait` recognizes `healthy`. | Render platform state. Summarize documented categories plus Other and Unknown. Pod readiness comes from Pod data. |
| Logs as a live stream | `AppLogs` reads a windowed REST tail. The CLI resolves Console access before reading, and discovers targets through the pod socket unless one is supplied. | Keep CLI credential parity, poll with a bounded buffer and mark possible gaps. Avoid automatic pod fallback when the selected pod disappears. |
| Builds and recent changes | Builds are Git-app scoped. `UpdatedAt` is an app modification timestamp. | Fetch builds only when opening a Git app's tab, and label recent changes as app updates. |
| Activity deep links | Notifications are account-wide and the typed payload has no tenant field. Alerts are tenant-scoped but expose text and an action link, not a typed app ID. | Label the feed scope and jump only after verifying an exact app ID. |
| Read-only based on HTTP verb | Build retry and build-latest/deploy change state via GET. | Expose only explicitly selected read operations to Phase 1. |
| Mutation success means done | Field edits can receive HTTP 202 before persistence. Restart acceptance and rollout completion are separate. | Use the established read-back or rollout logic and give ambiguous completion its own status. |

This is a source review. API behavior marked unverified in the repo stays unverified here. The design and its release checks require no live tenant or credentials.
