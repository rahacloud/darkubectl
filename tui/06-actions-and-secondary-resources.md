# 06 · Actions and secondary resources

## App actions · Phase 2

Phase 2 adds explicit session-level write opt-in via proposed `darkubectl tui --allow-writes`. This enables eligible menu entries. Server authorization still determines whether the operation is permitted. Add restart and scale first. Read actions remain available in read-only sessions.

```text
+ Actions / team-a / prod / billing-api ---------------------------+
| > Open current logs                                              |
|   Open build output                                              |
|   Scale app                                                      |
|   Restart app                                                    |
+------------------------------------------------------------------+

+ Review scale -----------------------------------------------------+
| Tenant team-a       Namespace prod       App billing-api          |
| App ID 9f17c819-1a75-4d62-a4b8-3c76065a390d                       |
| Desired replicas: 2 -> 1                                          |
| [Cancel]   Apply change                                           |
| Tab choose   Enter select   Esc cancel                            |
+-------------------------------------------------------------------+
```

1. Open the action menu with `a`. Enter opens a form where parameters are needed, then a review modal. Review shows tenant, namespace, app name, stable ID and the concrete diff or impact. Initial focus is Cancel. A navigation key or stale selection cannot submit.
2. Capture the exact target and the reviewed preconditions. Before sending the write, re-read the target and revalidate using existing operation guards. If reviewed fields, target scope or eligibility changed, invalidate the review and require a new one. A preflight read cannot prevent a concurrent server change unless the API exposes a conditional update, so keep the gap small and use conditional writes only when supported.
3. Run a single submission in flight per target. Store its scope with the operation and keep the scope fixed until it resolves. Suppress repeat keypresses and automatic mutation retries. A timeout or interrupt after dispatch is **Outcome unknown**. Offer an explicit read-back or reinspection, rather than resending automatically. Ctrl+C may exit after restoring the terminal, with a plain outcome-unknown message if applicable.
4. Record **Submitted**, **Verified**, **Rejected** or **Outcome unknown** as appropriate. Refresh the affected data plus relevant app lists. A normal post-write refresh error must not recast a verified write as failed. Restart acceptance is not rollout completion. Use the old-pod tracking logic when verification is supported and label limitations when it is unavailable.

| Action | Implementation dependency |
| --- | --- |
| Restart | Use `Client.RestartApp` and the semantics of `rollout restart`. Review the impact on all pods. If Console access is available, capture the old pod identities and use the existing rollout readiness check. |
| Scale | Reuse nonnegative replica validation and the shared field-edit/read-back flow behind `runAppChange`. Show current versus desired and explicitly describe a scale-to-zero impact. |
| Autoscale and image | Follow scale/restart. Extract the existing CLI validation and verification with the diff for limits or image. Avoid introducing a scheduler or a generic JSON editor. |
| Build start, retry and stop | Follow the read-only Builds tab. Reuse Git-app eligibility and build status checks. Confirm the exact build ID where applicable and identify start as building **and deploying** the current branch head when using the existing CLI operation. Retry and build start can mutate via GET. |

App deletion, namespace removal and destructive registry or storage operations require their own implementation brief. Any later destructive flow needs an exact target review, typed confirmation for irreversible actions, and explicit handling of known platform behavior, such as Helm releases surviving app deletion. No core release shortcut invokes these flows.

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
- Terminal handoff is a later explicit action available only with verified Console auth and an exact app/pod/container. Reuse the existing terminal operation and Bubble Tea's supported terminal suspend/resume flow. Pause requests that follow the selection, restore the screen after failure or completion and refresh the chosen app. Arbitrary exec, copying files, tunnels and credential-revealing commands remain CLI workflows.
