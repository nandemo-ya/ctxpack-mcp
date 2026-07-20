# ctxpack-mcp Design Document

ctxpack-mcp is a thin MCP (Model Context Protocol) server that wraps the [ctxpack](https://github.com/atani/ctxpack) CLI. It lets AI agents call ctxpack's token-aware context extraction as MCP tools instead of shelling out themselves.

Status: draft. Targets ctxpack >= 0.4.0.

## Goals

- Expose ctxpack's extraction pipeline (URL / local file / raw content → compact Markdown) as MCP tools with structured results.
- Stay thin: no extraction logic of our own. ctxpack owns the pipeline; this server owns process execution, input mapping, and error translation.
- Work with any MCP client over stdio: Claude Code, Codex CLI, Gemini CLI, Cursor, VS Code.
- Turn ctxpack's exit-code contract (especially exit 3, JavaScript-rendered pages) into structured errors that agents can recover from without human help.

## Non-goals

- Re-implementing or extending ctxpack's cleaning, scoring, or token estimation.
- Bundling the ctxpack binary. Users install it themselves (see [Dependency on ctxpack](#dependency-on-the-ctxpack-cli)).
- HTTP/SSE transport. stdio covers every target client today; we can add HTTP later if a hosted use case appears.
- File output (`-o FILE`). MCP results return inline; agents that want a file can write one.
- Markdown output mode. The server always runs ctxpack with `--json`; MCP structured content subsumes the human-oriented Markdown format.

## Architecture

```text
MCP client (Claude Code, Codex, ...)
  │  stdio (JSON-RPC)
  ▼
ctxpack-mcp (Go, single binary)
  │  os/exec
  ▼
ctxpack CLI (user-installed, >= 0.4.0)
```

- **Language:** Go, matching upstream ctxpack. Single static binary per platform.
- **MCP SDK:** [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) (official, v1.x, stable API). We chose it over `mark3labs/mcp-go`, which remains on v0.x with a moving API.
- **Transport:** stdio only.
- **Execution model:** each tool call spawns one short-lived `ctxpack` process with `--json`, parses stdout, and returns the parsed result. No daemon, no shared state.

## Dependency on the ctxpack CLI

ctxpack-mcp requires a user-installed ctxpack binary. This follows the common MCP pattern for CLI-backed servers (git, kubectl, and docker servers all assume a preinstalled CLI), and upstream leaves us no alternative: ctxpack keeps all logic under `internal/`, so a Go library import is impossible.

### Binary resolution

Resolution order:

1. `CTXPACK_BIN` environment variable, if set. Fail immediately if it points to a missing or non-executable file.
2. `exec.LookPath("ctxpack")` on `PATH`.
3. Fallback probe of well-known install locations: `/opt/homebrew/bin/ctxpack`, `/usr/local/bin/ctxpack`.

Step 3 exists because GUI-launched MCP clients (Claude Desktop, IDE extensions) often start child processes with a minimal `PATH` that omits Homebrew's prefix. This is the single most common failure mode for CLI-backed MCP servers on macOS.

If resolution fails, the server still starts and registers its tools, but every tool call returns an error that includes install instructions (`brew install atani/tap/ctxpack`). Failing at call time rather than startup keeps the server visible in the client's tool list, which gives the agent an error message it can show the user instead of a silent connection failure.

### Version compatibility

On first tool call, the server runs `ctxpack --version` and caches the result.

- Minimum supported version: **0.4.0**. Earlier versions returned exit 0 with near-empty content for JavaScript-rendered pages; 0.4.0 introduced the exit-3 contract this server depends on.
- Versions below the minimum produce an error naming the installed and required versions.
- Newer minor/patch versions are accepted. The wrapped surface (flags, JSON schema, exit codes) is upstream's documented compatibility contract.

## Tool surface

Four tools. Names are short because MCP clients already namespace them by server.

### `pack`

Extract compact, token-aware context from a URL or local HTML/Markdown file.

| Parameter | Type | Required | Maps to |
|-----------|------|----------|---------|
| `source` | string | yes | positional `SOURCE` (URL or local file path) |
| `query` | string | no | `--query` (moves task-relevant sections toward the top) |
| `no_record` | boolean | no | `--no-record` (skip cumulative stats recording) |

`source` accepts both `http(s)://` URLs and local HTML/Markdown file paths, mirroring the CLI. Restricting the tool to URLs would only narrow it against upstream for no gain in safety: any MCP client that grants file access already does so through its own permission model.

Invocation: `ctxpack SOURCE --json [--query TEXT] [--no-record]`

Result: ctxpack's JSON output passed through as structured content:

```json
{
  "ok": true,
  "source": { "url": "...", "fetched_at": "..." },
  "title": "...",
  "query": null,
  "content": { "format": "markdown", "text": "..." },
  "stats": {
    "raw_html_tokens": 42100,
    "clean_text_tokens": 7800,
    "final_tokens": 7800,
    "saved_tokens": 34300,
    "reduction_percent": 81.5
  }
}
```

Annotations: `readOnlyHint: true`, `openWorldHint: true` (fetches URLs).

### `pack_content`

Clean raw HTML or Markdown that the caller already holds. The server pipes the content to `ctxpack - --json` over stdin.

| Parameter | Type | Required | Maps to |
|-----------|------|----------|---------|
| `content` | string | yes | stdin |
| `query` | string | no | `--query` |
| `no_record` | boolean | no | `--no-record` |

This tool exists for one main scenario: `pack` fails with `js_rendering_required` (see [Error handling](#error-handling)), the agent fetches the page with its own JavaScript-capable tool, then passes the rendered DOM here for cleaning. ctxpack trusts stdin as already rendered, so the full pipeline still applies.

Annotations: `readOnlyHint: true`, `openWorldHint: false`.

### `stats`

Report cumulative token savings across all recorded runs.

No parameters. Invocation: `ctxpack stats --json`. Result: `{runs, raw_input_tokens, clean_text_tokens, final_tokens, saved_tokens, reduction_percent}` passed through.

Annotations: `readOnlyHint: true`.

### `reset_stats`

Reset the cumulative stats history (`~/.ctxpack/stats.jsonl`).

No parameters. Invocation: `ctxpack reset --yes`. The `--yes` flag is safe here because MCP clients gate destructive tools behind their own approval flow; the annotation below tells them to.

Annotations: `readOnlyHint: false`, `destructiveHint: true`.

## Error handling

ctxpack documents four exit codes. The server maps each to a structured MCP tool error so agents can branch without parsing prose:

| Exit code | Meaning (upstream) | MCP mapping |
|-----------|--------------------|-------------|
| 0 | success | normal result |
| 1 | network/runtime error, often retriable | error, `code: "runtime_error"`, `retriable: true` |
| 2 | usage error or missing input file | error, `code: "usage_error"`, `retriable: false` |
| 3 | page requires JavaScript rendering | error, `code: "js_rendering_required"`, `retriable: false`, plus recovery hint |

The exit-3 error message tells the agent exactly how to recover:

> This page requires JavaScript rendering. Fetch it with a JavaScript-capable tool (e.g. your browser or fetch tool), then pass the rendered HTML to the `pack_content` tool.

Additional failure modes introduced by the wrapper itself:

- **Binary not found** → `code: "ctxpack_not_installed"`, message includes `brew install atani/tap/ctxpack` and a pointer to `CTXPACK_BIN`.
- **Version too old** → `code: "ctxpack_version_unsupported"`, message names installed and minimum versions.
- **Process timeout** → the server kills the child after 60 seconds and returns `code: "timeout"`, `retriable: true`. ctxpack's own URL fetch timeout is 20 seconds, so 60 seconds only triggers on pathological hangs.
- **Malformed JSON on stdout** → `code: "unexpected_output"`, with the first part of stdout/stderr attached for debugging. This usually means a ctxpack version mismatch.

## Development and CI

### Testing strategy

Unit tests run without ctxpack installed. Tests point `CTXPACK_BIN` at a fixture script that emits canned stdout, stderr, and exit codes, which covers argument construction, JSON passthrough, stdin piping, every exit-code mapping, timeouts, and malformed output. The `CTXPACK_BIN` override exists for GUI clients with a restricted `PATH`, and it doubles as the seam that makes the exec layer testable.

Because this server's contract is entirely upstream's JSON schema and exit codes, an upstream release can break it without any change on our side. A separate integration job installs real ctxpack on Linux via `go install github.com/atani/ctxpack/cmd/ctxpack@VERSION` (no Homebrew, so it finishes in seconds) and exercises the tools against both the minimum supported version and `@latest`. It runs weekly on a schedule as well as on pull requests, so a new upstream release surfaces on its own rather than in a user's bug report.

### CI workflows

`ci.yml` runs on pushes to `main` and on pull requests, with two jobs:

- **test** — `go test ./...` across `ubuntu-latest` and `macos-latest`.
- **lint** — `gofmt -l` and `go vet ./...`.

Both use `go-version-file: go.mod` so the toolchain never drifts from the module's declared minimum. Actions are pinned to commit SHAs with version comments, and workflow `permissions` default to `contents: read`. These conventions follow upstream ctxpack, which also runs `pinact` to keep SHA pins current.

Release automation (goreleaser, release-please) lands with the first release rather than up front; see [Distribution](#distribution).

## Distribution

Distribution is layered, generic-first. MCP is a cross-client standard and ctxpack itself is agent-agnostic, so nothing in the core path may assume a specific client.

### Layer 1 — binary distribution (initial release scope)

- GitHub Releases with multi-platform builds via goreleaser (macOS arm64/x64, Linux arm64/x64, Windows x64).
- Homebrew: `brew install nandemo-ya/tap/ctxpack-mcp`, with the formula declaring `depends_on "atani/tap/ctxpack"` so one command installs both binaries.
- `go install github.com/nandemo-ya/ctxpack-mcp@latest` for Go users.
- README documents per-client setup for Claude Code, Codex CLI, Gemini CLI, and Cursor. The config is the same everywhere: run the `ctxpack-mcp` binary over stdio.

### Layer 2 — MCP Registry (post-release)

Publish a `server.json` to the official [MCP Registry](https://registry.modelcontextprotocol.io) for cross-client discovery.

### Layer 3 — Claude Code plugin (post-release, convenience only)

Add a `.claude-plugin/` manifest to this repository so Claude Code users can install via the plugin marketplace. The plugin only ships MCP configuration; it cannot ship binaries, so Layer 1 remains a prerequisite. Other clients are unaffected.

## Upstream relationship

atani/ctxpack's roadmap lists "MCP/server mode for agent frameworks". If upstream ships a native MCP mode, we will evaluate whether to contribute this implementation upstream, keep maintaining it independently, or deprecate it in favor of upstream. Until then, this project fills the gap, and staying thin keeps every one of those outcomes cheap.
