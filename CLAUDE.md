# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A thin MCP server that exposes the [ctxpack](https://github.com/atani/ctxpack) CLI as agent tools. Go, official `modelcontextprotocol/go-sdk` (v1.x — not `mark3labs/mcp-go`), stdio only. Public, MIT.

```
MCP client → ctxpack-mcp → ctxpack CLI (user-installed, >= 0.4.0)
```

Design rationale lives in `docs/design.md`; read it before changing anything structural.

## How a tool call flows

```
internal/server/tools.go   handler, unmarshals typed input
  → internal/ctxpack/run.go   Runner method, builds args
    → resolve.go              Resolver finds the binary (cached after first success)
      → exec ctxpack --json
    ← stdout as json.RawMessage, or *ctxpack.Error with a Code
  ← jsonResult / errorResult
```

- `internal/ctxpack` owns everything about the CLI: discovery, version gating, execution, exit-code translation. It knows nothing about MCP.
- `internal/server` owns the MCP surface: schemas, annotations, and turning results or `*ctxpack.Error` into `CallToolResult`.
- `cmd/ctxpack-mcp` is wiring only: flags, version resolution, stdio transport.

One `Runner` is shared across calls so binary resolution happens once.

## Constraints that are easy to violate

- **Stay thin.** ctxpack owns extraction, cleaning, scoring, and token estimation. This server owns process execution, input mapping, and error translation. Do not add extraction logic, and do not work around upstream bugs — fix them upstream.
- **Always pass `--json`.** Markdown output mode is not exposed; MCP structured content replaces it. `-o FILE` is not exposed either.
- **Never bundle or auto-install ctxpack.** Users install it themselves. A Go library import is impossible anyway: upstream keeps all logic under `internal/`.
- **Do not fail server startup when ctxpack is missing.** Tools register regardless and report the problem at call time, so the client can show install instructions instead of a silent connection failure. There is a test for this.
- **Keep the fallback probe** of `/opt/homebrew/bin` and `/usr/local/bin` in resolution. GUI-launched MCP clients start children with a minimal `PATH`, which is the most common failure mode on macOS.
- **Minimum ctxpack is 0.4.0**, the release that introduced the exit-3 contract. `MinVersion` in `internal/ctxpack/version.go` and the pin in `upstream-drift.yml` must move together.

## Gotchas that cost time to rediscover

- **ctxpack writes errors to stderr as plain text even with `--json`.** On a non-zero exit, stdout is empty. Never try to parse stdout as JSON unless the exit code was 0.
- **`cmd.WaitDelay` is load-bearing.** Output is captured through pipes and `Wait` blocks until every writer closes them, so killing the process does not release a descriptor a surviving child holds. Without it the timeout does not actually bound a call.
- **Test fixtures run with `PATH` emptied** so resolution cannot find a real ctxpack. Fixture scripts must call external commands by absolute path (`/bin/cat`, `/bin/sleep`). A bare `cat` silently yields empty stdin instead of failing.
- **`destructiveHint` is only meaningful when `readOnlyHint` is false**, so read-only tools leave it unset rather than sending a redundant `false`.
- **Handlers return `any` as the output type** on purpose: no output schema is inferred, so ctxpack's JSON passes through verbatim instead of being pinned to a Go struct that breaks whenever upstream adds a field.
- **Failures are tool errors, not protocol errors** — `IsError` plus a `structuredContent` object carrying `code`, `message`, and `retriable`. A protocol error would hide the reason from the model.
- **`reset --yes` has no JSON mode**; it prints a confirmation line, so `reset_stats` returns text.

## Error codes

Exit 1 → `runtime_error` (retriable), 2 → `usage_error`, 3 → `js_rendering_required`, anything else → `runtime_error`. Wrapper-level: `timeout`, `unexpected_output`, `ctxpack_not_installed`, `ctxpack_version_unsupported`.

Exit 3 matters most: the message must tell the agent to fetch the page with a JavaScript-capable tool and pass the HTML to `pack_content`. That recovery loop is why `pack_content` exists, and a test asserts the message names it.

## Commands

```bash
go build ./...
go test ./...                          # hermetic; no ctxpack required
go test -run TestName ./internal/...   # single test
go test -tags integration ./...        # runs against a real ctxpack
gofmt -l . && go vet ./...             # lint job; gofmt must print nothing
goreleaser check                       # validate release config
goreleaser release --snapshot --clean --skip=publish   # full build, no publish
```

Unit tests fake ctxpack through `CTXPACK_BIN` and fixture scripts, so they never need the real binary. Integration tests do, and assert the upstream JSON fields and exit codes this server reads.

## CI

- `ci.yml` — tests on ubuntu and macOS, plus gofmt and vet.
- `upstream-drift.yml` — integration tests against the pinned minimum and the newest upstream release, weekly and on PRs. A break against the newest release does not block PRs but fails the scheduled run. This server's contract is entirely upstream's JSON and exit codes, so upstream can break it with no change here.
- Pin actions to commit SHAs with version comments; use `go-version-file: go.mod`; default `permissions` to `contents: read`.

## Releasing

Commits must follow conventional commits — release-please derives the version and changelog from them.

`release.yml` runs release-please and the goreleaser build in one workflow, because a tag pushed with the default `GITHUB_TOKEN` does not trigger other workflows. Merging the release PR cuts the tag and publishes in the same run.

Tags are plain semver (`v0.1.0`), unlike upstream's `ctxpack-v0.4.0`, so `go install ...@latest` resolves. Homebrew publishing uses `homebrew_casks` (goreleaser deprecated `brews`) into `nandemo-ya/homebrew-tap`, with `HOMEBREW_TAP_GITHUB_TOKEN` for cross-repo write.

The official MCP Registry listing is deferred, not forgotten; `docs/design.md` records why the Go binary makes it awkward.

## Claude Code plugin

The repository is its own marketplace. `.claude-plugin/marketplace.json` at the root points at `./plugin`, which holds `plugin/.claude-plugin/plugin.json` and `plugin/.mcp.json`. Publishing is a push to `main` — there is no registry step.

- **The MCP config must live in `plugin/.mcp.json`.** An `mcpServers` key inside `plugin.json` is accepted by the validator and then silently ignored: `claude plugin details` reports `MCP servers (0)`. That silence is the whole trap.
- **The plugin lives in `plugin/`, not the repository root.** A root plugin would ship the Go source and trip `--strict` on `CLAUDE.md`, which a plugin cannot load anyway.
- **Two version fields must agree** — `plugin.json` and the marketplace entry. release-please bumps both through `extra-files`; `claude plugin tag` checks the agreement.
- Verify with `claude plugin validate . --strict` and `claude plugin validate plugin --strict`. For an end-to-end check, point `CLAUDE_CONFIG_DIR` at a throwaway directory and install from a local path, so testing does not disturb a real configuration.

## Upstream relationship

atani/ctxpack's roadmap lists "MCP/server mode for agent frameworks". If upstream ships one, this project may be contributed there or deprecated. Staying thin keeps every outcome cheap — weigh that before adding surface area. Extraction-quality issues belong upstream; tool shapes, error codes, and client configuration belong here.
