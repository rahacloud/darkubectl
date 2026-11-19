# 06 · Actions and secondary resources

## App actions · Phase 2

The contextual `a` menu is for app lifecycle actions: Restart and Delete. Stop is not shown because Darkubectl has no distinct app-stop command. Scaling currently shows replica and autoscaler settings read-only; scaling mutations need a dedicated confirmed flow. Resource usage and history stay in Usage, image changes and deployment in Builds, and host changes in Domains. Logs, build output, Env and Terminal are views, not action-menu entries.

Live actions require explicit session-level write opt-in with `darkubectl tui --allow-writes`. Server authorization still determines whether an operation is permitted. Read-only app views remain available without write opt-in. Restart always opens a Yes/No confirmation. Delete requires typing the exact app name and then activating a separate Delete app button by mouse click or Tab/Enter; Cancel is focused first. Before either write, re-read the app by UUID and verify its name and ID still match the reviewed target. Mock mode offers the same menu and confirmation UI, simulates restart, and removes a deleted app only from the current in-memory preview; it never calls the API or changes the fixture file.

```text
+ Actions / team-a / prod / billing-api ---------------------------+
| > Restart app                                                    |
|   Delete app                                                     |
+------------------------------------------------------------------+

+ ↻ Restart --------------------------------------------------------+
| Restart pods for billing-api?                                     |
| [ Cancel ]   [ ↻ Restart ]                                         |
| ←/→ choose · Enter confirm · Esc cancel                            |
+-------------------------------------------------------------------+

+ ✖ Delete ---------------------------------------------------------+
| Delete billing-api permanently?                                   |
| Type the exact app name:                                          |
| > billing-api                                                      |
| Cannot be undone.                                                  |
| [ Cancel ]   [ ✖ Delete ]                                          |
| Click ✖ Delete or Tab/Enter · Esc cancel                           |
+-------------------------------------------------------------------+
```

1. Open the action menu with `a`. Restart uses an explicit Yes/No confirmation. Delete additionally requires an exact app-name match and a distinct Delete app button. Initial focus is Cancel/No. A navigation key or stale selection cannot submit.
2. Capture the exact target and the reviewed preconditions. Before sending the write, re-read the target and revalidate using existing operation guards. If reviewed fields, target scope or eligibility changed, invalidate the review and require a new one. A preflight read cannot prevent a concurrent server change unless the API exposes a conditional update, so keep the gap small and use conditional writes only when supported.
3. Run a single submission in flight per target. Store its scope with the operation and keep the scope fixed until it resolves. Suppress repeat keypresses and automatic mutation retries. A timeout or interrupt after dispatch is **Outcome unknown**. Offer an explicit read-back or reinspection, rather than resending automatically. Ctrl+C may exit after restoring the terminal, with a plain outcome-unknown message if applicable.
4. Record **Submitted**, **Verified**, **Rejected** or **Outcome unknown** as appropriate. Refresh the affected data plus relevant app lists. A normal post-write refresh error must not recast a verified write as failed. Restart acceptance is not rollout completion. Use the old-pod tracking logic when verification is supported and label limitations when it is unavailable.

| Action | Implementation dependency |
| --- | --- |
| Restart · implemented | Use `Client.RestartApp` and the semantics of `rollout restart`. Require Yes/No confirmation. If Console access is available, capture old pod identities and use the existing rollout readiness check. |
| Stop app · unavailable | Darkubectl exposes no distinct app-stop command. Do not silently implement it as scale-to-zero. Add only after confirming platform semantics. |
| Delete app · implemented | Use `Client.DeleteApp` after exact-name typing and separate Delete button activation. Keep the exact target review and typed confirmation; explain that platform cleanup may finish asynchronously. |

Build start, retry and stop remain inside the app's Builds view. Namespace removal and destructive registry or storage operations require their own implementation brief. Any destructive flow needs an exact target review, typed confirmation for irreversible actions, and explicit handling of known platform behavior, such as Helm releases surviving app deletion. No core release shortcut invokes these flows.

## Resources, Registry and Tickets · Phase 3

```text
+ Resources / team-a -----------------------------------------------+
| Find resource:                    Namespaces   Certificates       |
| > Disks                          Databases    Storage             |
|   Marketplace services           Members      Billing             |
|   Orphaned workloads                                              |
| Enter browse   Esc back                                           |
+-------------------------------------------------------------------+

+ Registry / team-a / team-images ----------------------------------+
| Registries / team-images / billing-api                            |
| > release-52    sha256:78b...                                     |
|   release-51    sha256:221...                                     |
| Enter tag details  Esc images                                     |
+-------------------------------------------------------------------+

+ Tickets / team-a -------------------------------------------------+
| > 284   Open     Ingress certificate renewal                      |
|   243   Closed   Database plan question                           |
| Enter conversation  Esc back                                      |
+-------------------------------------------------------------------+
```

- Resources starts with Namespace and certificate inventory. Add remaining types in vertical slices using existing `get` commands as data contracts. Keep marketplace services tied to the app model. Treat billing and member data as separately authorized. Orphan reconciliation is opt-in and requires local `kubectl` plus an explicit Kubernetes context verified against the Darkube scope.
- Registry starts with registry → image → tag and read-only detail, including GC rules where exposed. Reuse existing listing and pagination paths. Registry passwords and credential output remain CLI-only. Any future prune or delete must preview exact scope and distinguish preview from execution.
- Tickets starts with tenant ticket list and read-only conversation. Load detail on demand, sanitize rich text, and leave attachments in the CLI. Create/reply/close/rate are separate designs with draft handling and confirmation appropriate to each external effect.
- The app-scoped Terminal view is planned for a later release and requires verified Console auth plus an exact app/pod/container. Reuse the existing terminal operation and Bubble Tea's supported terminal suspend/resume flow. Pause requests that follow the selection, restore the screen after failure or completion and refresh the chosen app. Arbitrary local command execution, copying files, tunnels and credential-revealing commands remain CLI workflows.
