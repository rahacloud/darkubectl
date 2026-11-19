# 00 · Darkubectl TUI implementation brief

**Status:** Implementation in progress; release 1A is incomplete. See [progress report](08-progress.md) for shipped behavior, outstanding work, and verification. **Design baseline:** darkubectl `main` at [`15da4b9`](https://github.com/rahacloud/darkubectl/commit/15da4b9792a7280086d99abe74e8093d08d8d41b). All screens use fictional data.

Interactive TUI views follow the Hamravesh console palette; its colors and semantic roles are recorded in [Visual style](11-visual-style.md).

## Problem statement

A Darkube operator investigating an unhealthy app has to repeat tenant and app arguments across separate commands for app state, pods, logs and builds. The interactive describe viewer helps inspect a single object but does not maintain a navigation session across resources. Build an opt-in `darkubectl tui` that lets the operator discover a workload, verify its scope and move from symptom to evidence without repeating the target.

Use the filters and contextual help from [K9s](https://k9scli.io/topics/commands/), the list-to-details flow from LazyDocker and the keyboard discoverability from LazyGit. Darkube organizations, projects and apps define the screen hierarchy. Kubernetes credentials and a local `kubectl` executable are unnecessary for the core flow.

```text
+ Darkubectl / team-a / Apps / prod / billing-api -----------------------------+
| [1] TENANTS  │ [3] APPS                     │ [4] SUMMARY                   |
| › team-a     │ ▾ prod · 2 apps              │ SUMMARY · billing-api         |
|              │ ├── billing-api  not ready   │ State      not ready           |
| [2] PAGES    │ └── gateway      healthy     │ Desired    2                   |
| ▦ Apps       │                              │                               |
| ∿ Activity   │                              │                               |
| ▱ Tickets    │                              │                               |
| ▤ DB         │                              │                               |
| ◇ Registry   │                              │                               |
| ¤ Billing    │                              │                               |
+--------------+------------------------------+-------------------------------+
| 1-4 focus panes   n namespace   / filter   ? help   q quit                  |
+-----------------------------------------------------------------------------+
```

## Release boundaries

| Release | Deliverable | Exit condition |
| --- | --- | --- |
| 0 · Offline preview | Versioned YAML mock profile and read-only Apps tree | `darkubectl tui --mock-file demo/mock-profile.yaml` renders entirely from the file, with no API client or credentials |
| 1A · Read-only shell | Launch, context, Apps, Summary, Scaling overview, Outbound IP, navigation and error states | An API-key user with a configured tenant can find and open the correct app, even when Profile is unavailable |
| 1B · Read-only diagnostics | Pod snapshot in Summary, current and previous logs, Builds and build output | A Console-authenticated user can trace an unhealthy app to pod or build evidence, and the UI explains unavailable capabilities |
| 1C · Read-only triage | Overview, Alerts and Notifications | Each feed reports its own state and scope. Verified alert links open one exact app |
| 2 · App-specific workflows | Scaling controls, disk growth, Builds/Deploy, Domains and History, plus lifecycle actions such as restart | View-specific changes stay with their app view; lifecycle mutations use review, existing validation, target binding and post-submit verification |
| 3 · Broader navigation | DB and Tickets pages, Registry explorer with confirmed image/tag/digest deletion, plus app-scoped Env and Terminal access | Each workflow has a verified scope, auth model and independent acceptance checks |

Phase 0 provides a safe visual preview from local fictional data. Live mode is read-only unless `--allow-writes` is explicitly set; the lifecycle menu then offers confirmed Restart and Delete, and Env changes require confirmation. Env shows plain values, allows selected secret reveal and copies either kind of value. The app action menu opens Terminal for a selected pod/container; it does not become a local command runner. Bulk/spec operations, raw API, MCP, CI output, credential management, copying files and tunnels stay CLI workflows. Start on **Apps** for fastest time to diagnosis. Overview is a jump destination when 1C lands. Default refresh is manual for resource screens. Live log follow is opt-in per view.

## Read-only presentation rule

Apply one rule across every app view: when `--allow-writes` is off, hide all write-only affordances from the view, footer, and Help modal. This includes edit/add/delete/toggle/save/discard hints, add rows, edit prompts, and selection styling that exists only to suggest a value can be edited. Keep read-only actions and navigation visible, such as refresh, scrolling, copy, secret reveal, log target selection, and pod/build selection. Enforce the same boundary in key handlers; hiding a hint does not replace write guards. When writes are enabled, show the relevant controls only in the view and focus state where they work.

## Read and implement in order

| Spec | Use it to |
| --- | --- |
| [01 Repository review](01-repository-review.md) | Find real entry points, data sources and platform constraints |
| [02 Shell and context](02-shell-and-context.md) | Implement launch, keyboard routing, layout, startup and context picking |
| [03 Apps and drilldown](03-apps-and-drilldown.md) | Implement the core table, Summary, Resources, Logs, Env, Terminal access, Domains and Ports |
| [04 Logs and builds](04-logs-and-builds.md) | Implement target selection, bounded log follow and build inspection |
| [05 Overview and activity](05-overview-and-activity.md) | Implement health summaries, independently loaded feeds and exact drilldown |
| [06 Actions and secondary resources](06-actions-and-secondary-resources.md) | Implement phased mutations, resource screens and terminal handoff |
| [07 Implementation and verification](07-implementation-and-verification.md) | Split work into PRs and run the definition of done |
| [09 Release 1A completion plan](09-release-1a-completion-plan.md) | Close the remaining shell, state, Summary, terminal, and action-audit gaps |
| [12 Builds view plan](12-builds-view-plan.md) | Builds view implementation, write-action checks and remaining acceptance |
| [13 Activity page plan](13-activity-page-plan.md) | Proposed alerts and notifications layout, styling and behavior |
| [14 CI/CD view redesign](14-cicd-view-redesign-plan.md) | Rename Builds to CI/CD and show deployment-mode-specific setup and history |
| [15 CI/CD settings investigation](15-cicd-settings-investigation.md) | API evidence and proposed editing behavior for Darkube build settings |
| [16 DB page plan](16-db-page-plan.md) | Managed database inventory page design and data contract |

Treat mockups as layout and state references. The written interaction and data contracts define behavior. Use synthetic fixtures throughout implementation and verification.
