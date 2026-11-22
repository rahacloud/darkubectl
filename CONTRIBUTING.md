# Contributing to darkubectl

Thank you for your interest in `darkubectl`. Issues and pull requests are welcome, and new findings about the Darkube API are the most valuable contribution, because nobody documents that API.

This guide tells you how to write a change that we can merge quickly. [`README.md`](README.md) tells you how to use the tool. [`CLAUDE.md`](CLAUDE.md) is the engineering reference: it holds the architecture and each fact that we know about the API, with the date of the confirmation. It has that name because AI assistants load it, but it is written for people too. Read the applicable section before you change a command.

## Before you start

- For a small fix, open a pull request directly.
- For a new command, a new flag on a write command, or a change in behavior, open an issue first. Tell us what you want to do with the tool, not only the command that you want.
- For a bug, give the command, the output, the result of `darkubectl version`, and the HTTP status if you have it. `darkubectl api METHOD PATH` shows the raw answer of the API.
- Remove tokens, API keys, passwords and the names of tenants that are not yours from each issue and pull request.

## Setup

You need the Go version in `go.mod` and [`golangci-lint`](https://golangci-lint.run) v2 (CI uses v2.14.0).

The recipes are in the `justfile`; [`just`](https://github.com/casey/just) is optional, and each recipe is one plain command that you can run by hand.

```sh
just check     # format check, golangci-lint (must report 0 issues), build, race tests
just fmt       # gofmt -w .
just lint-md   # only if you changed Markdown
```

The tests do not need a Hamravesh account and do not use the network. To try a command against the real API, you need your own Darkube tenant (see "Changes that touch the API").

## Project structure

| Path | Contents |
| --- | --- |
| `main.go` | Calls `cmd`. Nothing else. |
| `cmd/` | The command tree on `urfave/cli/v3`. One file for each command group. Shared helpers are in `app.go`. |
| `internal/client` | The REST client. Each API route is a method here, with its types. |
| `internal/auth` | Login and token refresh. |
| `internal/wsexec`, `internal/appstate` | The websocket transports (exec, and the stream of pods). |
| `internal/output` | Table, JSON and YAML output, and the colors of `describe`. |
| `internal/tui` | The interactive Bubble Tea views. |
| `internal/config` | The configuration file and the `DARKUBE_*` variables. |
| `internal/kube` | Reads Deployments through `kubectl` for `get orphans`. |
| `internal/agentsetup` | The plan and the file edits of `agent setup`. |

### Where a new feature goes

A feature usually has three parts:

1. **The route**, in `internal/client`: a method on `Client` and the types of the request and the answer. Put it in the file of its product (`registry.go`, `storage.go`), or make a new file. Code in `cmd/` does not build URLs.
2. **The command**, in `cmd/`: a `newXCommand()` function that returns a `*cli.Command`. Add a new top-level command to the list in `app.go`. Add a new resource of `get` to `get.go`.
3. **The policy for MCP**, in `cmd/mcp.go`: add a read-only command to `mcpPolicy`. A command that is not in the table counts as a write, and `darkubectl mcp` then refuses it without `--allow-writes`. If an AI agent must call the command as a typed tool, add it to `mcpToolSpecs` in `cmd/mcp_tools.go`.

Use the helpers that exist. Do not write the same flow again:

- `buildClient` gives a client for a command in a tenant. `newGlobalClient` is for the few routes that are not in a tenant (`whoami`, `get plans`, `get notifications`).
- `runAppChange` in `cmd/mutate.go` does each edit of one field of an app: the dry run, the diff, the confirmation, the `PUT`, and a second read to make sure that the server stored the value. A 202 from this API does not prove that. Add new edits there.
- `client.UpdateApp` does the read, the change and the write of an app. The API has no `PATCH`.
- `withAppCompletion` adds completion of app names to a command whose first argument is an app.

## Coding style

`golangci-lint` runs almost all linters (`default: all` in `.golangci.yml`), so most rules of style are automatic. These rules are not:

- **Follow kubectl.** If kubectl has a verb, a flag or an output format for the same operation, use its name and its behavior. Users must not learn a second vocabulary.
- **A write is safe by default.** Each command that changes something shows the change, asks for confirmation, and accepts `-y`/`--yes` and `--dry-run`. After the write, read the object again and fail if the value is not there.
- **Output.** Results go to stdout, and messages, prompts and warnings go to stderr. A command that lists something supports `-o json` and `-o yaml` through `internal/output`. Colors must stop when stdout is not a terminal.
- **Errors tell the user what to do.** The API frequently answers with a bare 500 or with Persian text. If you know the cause, give the cause and the command that corrects it (see `explainCreateError`).
- **Secrets.** Do not print or log a token, a password or the value of a secret variable unless the user asked for it with a flag. Do not write a secret to a file other than the configuration file (mode 0600). Do not add a secret field to a struct that `-o json` prints.
- **Comments give the reason.** Much of this code is the result of an experiment with the API. If a line looks wrong but is correct, write why, and the date of the observation if the behavior of the API can change.
- **Lint.** Correct the code before you disable a linter. For a false positive, use `//nolint:<linter> // reason` on that line. Do not disable a linter for the full project.
- **Dependencies.** Do not add a dependency for something that the standard library does. Tests use only the standard `testing` package.

## Tests

`go test -race ./...` must pass, and each change in behavior comes with a test.

- **A new route** gets a test in `internal/client` against an `httptest.Server`. The test examines the method, the path, the headers and the body of the request, and decodes an answer that you copied from the real API (with the secrets and names removed).
- **A new command** gets tests for its logic: how flags become a change, validation, and the text of the diff. Put the logic in functions that take values and return values, so that the test needs no server and no terminal.
- **A bug fix** gets a test that fails without the fix.
- **A new entry in `mcpPolicy` or `mcpToolSpecs`** is already covered: `TestMCPPolicyKeysAreCommands` and `TestMCPToolSpecsResolve` fail if a key does not name a real command or flag.
- **A change to the app spec** (`spec.go`, `apply.go`) must keep `TestSpecRoundTripIsANoOp` green. `get apps -o spec` and `apply` are inverses.
- **A merger in `internal/agentsetup`** must be idempotent. Run each case two times in the test, as the tests there do.

Conventions that the linters enforce: a test in the package under test is in a `*_internal_test.go` file, a test of the exported API is in a `*_test.go` file with the package name `<pkg>_test`, and each test calls `t.Parallel()`. Give a test a name that states the behavior, for example `TestRolloutDoneReportsRestartsOfNewPodsOnly`. A test must not call the real API or read the configuration of the developer.

## Changes that touch the API

The Darkube API has no public documentation, and it does not always do what its own metadata says. Thus a pull request that adds or changes a call must say how you know that the call works.

- Try the route first with `darkubectl api METHOD PATH [-d JSON]`. `OPTIONS` on a route shows its fields. The source maps of the console show the calls that the console makes; `CLAUDE.md` explains how to read them.
- Make writes only in your own tenant, on an app or a bucket that you made for the test. Delete it after the test.
- In the pull request, say which routes you called, what you saw, and on which date. Say also what you did not try.
- Add the finding to the API section of `CLAUDE.md`, with the date. Include the surprises: a field that the server ignores, a status that means something different, a route that needs a trailing slash.
- If you could not try a path, say so in the code and in the "What works, and what does not" section of `README.md`.

## Documentation

- Add a new command or flag to the "Usage" section of `README.md`.
- Update `CLAUDE.md` if you change the architecture or learn something about the API.
- Write Markdown with one paragraph on one line. Do not wrap prose at a column: a wrapped paragraph changes on each line for an edit of one word. Code blocks and tables keep their lines.

## Pull requests

1. Fork the repository and make a branch from `main`.
2. Make one logical change in each pull request. Put a refactor in its own pull request, before the feature that needs it.
3. Run the build, the tests and the linter. CI runs the same commands, and a pull request with a red CI does not get a review.
4. Write commit messages as [Conventional Commits](https://www.conventionalcommits.org): `feat(storage): create and remove buckets and access keys`, `fix(ticket): send an attachment's bare media type`. The types in use are `feat`, `fix`, `docs`, `test`, `style`, `build` and `chore`, and the scope is the command group or the package. In the body, give the reason for the change and what you observed.
5. In the description, say what the change does and why, show the output of the new command, and say how you tested it (see "Changes that touch the API"). Link the issue.
6. If a maintainer asks for a change, add commits to the same branch. We squash on merge if the history needs it.

A release is a `v*` tag that a maintainer pushes. You do not need to change a version number or a changelog.

## License

`darkubectl` has the [GPL-3.0](LICENSE) license. When you open a pull request, you agree that your contribution has the same license.
