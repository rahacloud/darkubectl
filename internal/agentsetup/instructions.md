## Darkubectl

- Darkube uses tenant for organization, namespace for project, and app for a managed application. Never guess the tenant.
- When the account, membership, or tenant is unclear, call whoami. Prefer a matching typed MCP tool; if none fits, use the generic darkubectl MCP tool. Use the CLI only when MCP is unavailable or does not expose the operation.
- Typed MCP tools may select a tenant for one call, but all calls keep the server's local configuration, credential, and API endpoint. Do not switch to the CLI to get around an MCP access denial.
- Inspect current state before making a requested change. Use dryRun where supported, review the target and proposed change, then read the affected resource back; an accepted request or HTTP 202 alone does not prove it persisted.
- Follow the MCP server's tool schemas and access instructions. Reads are enabled by default; secret reads require --allow-secrets and writes require --allow-writes, which also enables secret access. Never work around denied or blocked operations.
- Only pass --yes for a user-requested change after checking its exact target; it bypasses interactive prompts.
- Treat API keys, tokens, and secret environment values as sensitive. Do not expose them in chat, logs, or generated files; access a secret only when the user's request requires it and the configured MCP access permits it.
- Do not use delete as routine cleanup or a name-conflict fix. If app deletion is requested, verify the exact tenant and app; deletion is asynchronous and may leave an orphaned Helm release. Check get orphans before recreating an app with a blocked name.
- Creating apps or namespaces and opening pod terminals require a Console JWT; an account API key cannot perform them. Namespace creation also requires a name prefixed by the tenant slug, and deleting a populated namespace removes its workloads. Explain credential or target constraints before proceeding.
- Use Darkubectl's schemas, help, and repository guidance for platform behavior. Do not infer unsupported Darkube behavior from Kubernetes conventions.
