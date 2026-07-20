# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project state

Pre-implementation. The repository currently holds only `docs/design.md`, which is authoritative — read it before writing code. Implementation is tracked in issues #2 through #7, which are ordered and carry explicit dependencies:

1. #2 Go module + MCP server skeleton (tools registered as stubs)
2. #3 CI (test + lint)
3. #4 ctxpack binary resolution + version check
4. #5 The four tools + error mapping
5. #6 Unit tests + upstream drift detection job
6. #7 Release automation, Homebrew, README, flip repo public

The repository is private and will be published as OSS (#7).

## What this project is

A thin MCP server that wraps the [ctxpack](https://github.com/atani/ctxpack) CLI (token-aware context extraction for AI agents). It is Go, uses the official `modelcontextprotocol/go-sdk` (v1.x — not `mark3labs/mcp-go`), and speaks stdio only.

```text
MCP client → ctxpack-mcp (Go, os/exec) → ctxpack CLI (user-installed, >= 0.4.0)
```

Each tool call spawns one short-lived `ctxpack --json` process. No daemon, no shared state.

## Constraints that are easy to violate

- **Stay thin.** ctxpack owns extraction, cleaning, scoring, and token estimation. This server owns process execution, input mapping, and error translation. Do not add extraction logic here, and do not work around upstream bugs — fix them upstream.
- **Always pass `--json`.** ctxpack's Markdown output mode is not exposed; MCP structured content replaces it. `-o FILE` is not exposed either.
- **Never bundle or auto-install the ctxpack binary.** Users install it themselves. A Go library import is impossible anyway: upstream keeps all logic under `internal/`.
- **Do not fail server startup when ctxpack is missing.** Register tools normally and return an actionable error at call time, so the client can show install instructions instead of a silent connection failure.
- **Binary resolution order is `CTXPACK_BIN` → `PATH` → probe `/opt/homebrew/bin` and `/usr/local/bin`.** The fallback probe is not optional: GUI-launched MCP clients start children with a minimal `PATH` that omits the Homebrew prefix, which is the most common failure mode for CLI-backed MCP servers on macOS.
- **Minimum ctxpack version is 0.4.0**, the release that introduced the exit-3 contract this server depends on.

## Tools and error mapping

Four tools: `pack` (URL or local file), `pack_content` (raw HTML/Markdown via stdin), `stats`, `reset_stats`. Full parameter tables and MCP annotations are in `docs/design.md`.

ctxpack's exit codes map to structured errors: 1 → `runtime_error` (retriable), 2 → `usage_error`, 3 → `js_rendering_required`. Exit 3 matters most — the error message must tell the agent to fetch the page with a JavaScript-capable tool and pass the rendered HTML to `pack_content`. That recovery loop is why `pack_content` exists.

Wrapper-level errors: `ctxpack_not_installed`, `ctxpack_version_unsupported`, `timeout` (60s child kill), `unexpected_output` (stdout is not valid JSON, usually a version mismatch).

## Development commands

The Go module does not exist yet; these are the commands CI will run once #2 lands.

```bash
go build ./...
go test ./...
go test -run TestName ./internal/...   # single test
gofmt -l .                             # must output nothing
go vet ./...
```

Tests must pass with no ctxpack installed. Point `CTXPACK_BIN` at a fixture script that emits canned stdout/stderr and exit codes rather than shelling out to the real binary. The env override exists for restricted-`PATH` clients and doubles as the seam that makes the exec layer testable.

A real ctxpack (0.4.0) is installed locally at `/opt/homebrew/bin/ctxpack` for manual verification.

## CI conventions

Follow upstream ctxpack: pin actions to commit SHAs with version comments, use `go-version-file: go.mod` so the toolchain never drifts from the module minimum, and default workflow `permissions` to `contents: read`.

A separate integration job installs real ctxpack via `go install github.com/atani/ctxpack/cmd/ctxpack@VERSION` and runs weekly against both the minimum supported version and `@latest`. This server's contract is entirely upstream's JSON schema and exit codes, so an upstream release can break it with no change on our side.

## Upstream relationship

atani/ctxpack's roadmap lists "MCP/server mode for agent frameworks". If upstream ships one, this project may be contributed upstream or deprecated. Staying thin keeps every outcome cheap — weigh that before adding surface area.
