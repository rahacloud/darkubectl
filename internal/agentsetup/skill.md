---
name: darkubectl
description: Safely inspect and manage Hamravesh Darkube tenants, apps, builds, domains, and support data with darkubectl MCP tools or CLI.
---

<!-- darkubectl-agent-setup:skill -->
# Darkubectl

Use this skill for Hamravesh Darkube operations. Prefer the platform's typed MCP tools because their schemas describe supported arguments. If none fits, use the generic darkubectl MCP command tool. Use the CLI only when MCP is unavailable or does not expose the operation. Never switch paths to bypass an MCP denial or access policy.

## Platform model

Darkube is Hamravesh's application platform on Kubernetes. Its API calls are usually tenant-scoped: an organization is a tenant, a namespace is a project, and an app is a managed application. Never guess which tenant the user means.

## Workflow

1. If the account, membership, or tenant is unclear, call whoami and use its tenant list. Typed tools accept an optional tenant to select the organization for that call; this does not change the server's credential, config, or API endpoint.
2. Use the most specific tool available, such as list_apps, describe_app, get_app_spec, get_pods, app_logs, get_domains, or rollout_status. Use the generic darkubectl tool only for commands without a typed tool; pass its args as an argument list, not shell text.
3. Follow the MCP server's command access instructions. The generic tool applies policy to the resolved command; commands not classified as reads require write access, and terminal or non-returning commands remain blocked. A CLI path is not a way around an MCP denial.
4. Inspect the current resource before proposing or making a change. Use JSON or YAML output when parsing CLI results. Check schemas, command help, or repository guidance instead of inferring behavior from Kubernetes conventions.
5. Only make changes the user requested. Use dryRun when supported and review the target and diff before writing. Read the resource back afterward; a successful request or HTTP 202 response alone does not prove the platform stored the change.

## Access and credentials

The MCP server uses this machine's darkubectl configuration, credential, and API endpoint for every call. The optional tenant parameter on typed tools selects the organization for that call only.

MCP is read-only by default. The generic command tool permits reads unless a command may expose secrets; secret operations require --allow-secrets. Write tools are registered only when the server starts with --allow-writes, which also enables secret access. Commands needing a terminal or that run indefinitely remain blocked. Never bypass a denial by changing tools, credentials, or execution paths.

The MCP child has no interactive stdin. A generic command that prompts needs --yes; only include it for a user-requested change after checking its exact target.

Treat API keys, secret environment values, registry credentials, and deployment tokens as sensitive. Secret environment values are hidden by default. Do not print, log, or copy secrets into generated files; only pass or reveal a secret when the user's explicit task requires it and the configured MCP access permits it.

An account API key cannot create apps or namespaces or open pod terminals; those operations require a Console JWT. Namespace creation also requires its name to start with the tenant slug. If the available credential cannot perform the requested operation, explain the requirement instead of trying another credential path.

## Deletion and recovery

Do not use delete as routine cleanup or as a fix for an app-name conflict. If the user requests deletion, verify the exact tenant and target first. App deletion is asynchronous and may leave its Helm release behind; use get orphans to inspect releases before recreating an app whose name is blocked, and report only the state darkubectl confirms.

Namespace deletion is destructive: a populated namespace's workloads are removed with it. Inspect its apps first and do not rely on the API to protect them. Namespace deletion requires a Console JWT.
