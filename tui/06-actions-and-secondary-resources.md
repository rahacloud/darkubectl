# 06 · Actions and secondary resources

## App actions · Phase 2

The contextual `a` menu contains app lifecycle actions and Terminal. Stop is not shown because Darkubectl has no distinct app-stop command. Resources combines plan, CPU/RAM sizing, replica and autoscaler settings with current usage and history. Write mode stages supported resource changes for diff review and confirmation. Billing endpoints expose tenant wallets and invoices, without app-level attribution. Image changes and deployment stay in Builds, and host changes stay in Domains. Logs and Env are detail views; Terminal opens from the action menu so the remote session can take over the user's terminal.

Live actions require explicit session-level write opt-in with `darkubectl tui --allow-writes`. Server authorization still determines whether an operation is permitted. Read-only app views remain available without write opt-in. Restart always opens a Yes/No confirmation. Delete requires typing the exact app name and then activating a separate Delete app button by mouse click or Tab/Enter; Cancel is focused first. Before either write, re-read the app by UUID and verify its name and ID still match the reviewed target. Mock mode offers the same menu and confirmation UI, simulates restart, and removes a deleted app only from the current in-memory preview; it never calls the API or changes the fixture file.

```text
+ Actions / team-a / prod / billing-api ---------------------------+
| > >_ Terminal                                                    |
|   ↻ Restart                                                      |
|   ✖ Delete                                                       |
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

## DB, Registry and Tickets · Phase 3

```text
+ DB / team-a ------------------------------------------------------+
| NAME          ENGINE       STATUS    DETAIL       NODES   SIZE     |
| > prod-pg      PostgreSQL 16 ready   healthy      3       2c/4G/80G|
|   analytics   ClickHouse  24.8      provisioning 1       4c/8G/100G|
| Enter details   r refresh   Esc back                               |
+-------------------------------------------------------------------+

+ Registry / team-a / team-images ----------------------------------+
| ▾ hamdocker / billing-api                                        |
| >   release-53, latest                                            |
|     release-51                                                    |
|         sha256:af013… (untagged)                                  |
| Digest     sha256:af0132e…  ·  Size  83 MiB                      |
| Tags       release-53, latest                                     |
| Enter load/detail   d delete image/digest   t remove one tag       |
+-------------------------------------------------------------------+

+ Tickets / team-a -------------------------------------------------+
| > 284   Open     Ingress certificate renewal                      |
|   243   Closed   Database plan question                           |
| Enter conversation  Esc back                                      |
+-------------------------------------------------------------------+
```

- DB lists tenant managed databases through the existing `get databases` contract. Show engine/version, status/detail, node count and per-node CPU, RAM and disk. Structured output includes the returned database, namespace and cluster IDs plus managed status. Connection endpoints and credentials are not part of the current CLI response and must not be fabricated. Open details only for fields returned by the API. This page is read-only.
- Registry is grouped as registry → image → manifest. Each manifest row displays all tags that point to it, so aliases such as `release-53` and `latest` appear together and cannot be mistaken for separate images. Untagged manifests show a shortened digest in the explorer; details show the full digest, size, push time, tags, and reference. Load registries and image names on page entry, then load manifests on demand when an image is opened. `d` deletes the selected image or manifest after a target review and typing `delete`; `t` opens the manifest's tag list and removes only the selected tag after confirmation. Image deletion removes every manifest and tag; manifest deletion removes that manifest and its tags; tag deletion preserves the manifest. Credentials remain CLI-only. Registry creation is unavailable until a supported API operation is found. Retention and prune remain CLI workflows.
- Tickets starts with tenant ticket list and read-only conversation. Load detail on demand, sanitize rich text, and leave attachments in the CLI. Create/reply/close/rate are separate designs with draft handling and confirmation appropriate to each external effect.
- Terminal is available from the app action menu and requires Console auth plus an exact app/pod/container. Reuse the existing terminal operation and Bubble Tea's terminal suspend/resume flow. Print the app, pod and container with Ctrl+D/Ctrl+C/Ctrl+L guidance to stdout before attaching. Restore the screen after failure or completion. Arbitrary local command execution, copying files, tunnels and credential-revealing commands remain CLI workflows. In mock mode, the same picker and target banner work offline; shell commands are disabled.
