# ctxpack-mcp

[![CI](https://github.com/nandemo-ya/ctxpack-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/nandemo-ya/ctxpack-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/nandemo-ya/ctxpack-mcp?sort=semver)](https://github.com/nandemo-ya/ctxpack-mcp/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/nandemo-ya/ctxpack-mcp)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**MCP server for [ctxpack](https://github.com/atani/ctxpack), token-aware context extraction for AI agents.**

Agents burn context on navigation, scripts, cookie banners, and footers. ctxpack strips that and returns compact Markdown with a measured token saving. This server puts it in front of any MCP client, so an agent can pack a page before reading it instead of pulling raw HTML into its window.

```
MCP client (Claude Code, Codex, Gemini CLI, Cursor, …)
  → ctxpack-mcp        (this server)
    → ctxpack          (the CLI, installed separately)
```

It is a thin wrapper. ctxpack owns the extraction; this server owns process execution, input mapping, and turning ctxpack's exit codes into errors an agent can act on.

## Requirements

- **ctxpack 0.4.0 or newer.** Install it separately (see below). 0.4.0 is the release that started reporting JavaScript-rendered pages instead of returning empty content, and this server depends on that.
- Nothing else at runtime. The server is a single static binary.

## Install

### Homebrew

```bash
brew install --cask nandemo-ya/tap/ctxpack-mcp
```

The cask declares `depends_on formula: "atani/tap/ctxpack"`, so this installs both binaries.

### Go

```bash
go install github.com/nandemo-ya/ctxpack-mcp/cmd/ctxpack-mcp@latest
brew install atani/tap/ctxpack   # or see the ctxpack README for other options
```

### Prebuilt binaries

Download an archive for your platform from [Releases](https://github.com/nandemo-ya/ctxpack-mcp/releases) and put `ctxpack-mcp` on your `PATH`. macOS, Linux (both arm64 and x86_64), and Windows x64 are published.

## Configure your MCP client

Every client runs the same binary over stdio; only the config file differs.

### Claude Code

```bash
claude mcp add ctxpack -- ctxpack-mcp
```

Or install the plugin, which carries the same configuration:

```
/plugin marketplace add nandemo-ya/ctxpack-mcp
/plugin install ctxpack@nandemo-ya
```

The plugin ships configuration only — install the binaries first, as above. Restart Claude Code afterwards; MCP servers from a plugin are registered when a session starts.

Or add it to `.mcp.json` in your project to share it with the repository:

```json
{
  "mcpServers": {
    "ctxpack": {
      "command": "ctxpack-mcp"
    }
  }
}
```

### Codex CLI

In `~/.codex/config.toml`:

```toml
[mcp_servers.ctxpack]
command = "ctxpack-mcp"
```

### Gemini CLI

In `~/.gemini/settings.json`:

```json
{
  "mcpServers": {
    "ctxpack": {
      "command": "ctxpack-mcp"
    }
  }
}
```

### Cursor

In `~/.cursor/mcp.json`, or `.cursor/mcp.json` for one project:

```json
{
  "mcpServers": {
    "ctxpack": {
      "command": "ctxpack-mcp"
    }
  }
}
```

## Tools

| Tool | What it does |
|------|--------------|
| `pack` | Extract compact context from a URL or a local HTML/Markdown file |
| `pack_content` | Clean HTML or Markdown you already hold, without fetching |
| `stats` | Report cumulative token savings across recorded runs |
| `reset_stats` | Erase the savings history |

`pack` and `pack_content` both accept `query` (moves sections relevant to your task toward the top, dropping nothing) and `no_record` (skip the savings history for this run).

Results are ctxpack's JSON, passed through untouched:

```json
{
  "ok": true,
  "source": { "url": "https://example.com/docs", "fetched_at": "..." },
  "title": "Example Docs",
  "content": { "format": "markdown", "text": "..." },
  "stats": {
    "raw_html_tokens": 42100,
    "final_tokens": 7800,
    "saved_tokens": 34300,
    "reduction_percent": 81.5
  }
}
```

## Errors

Failures come back as tool errors carrying a code, so an agent can branch on them:

| Code | Meaning | Retriable |
|------|---------|-----------|
| `runtime_error` | Network or runtime failure inside ctxpack | yes |
| `usage_error` | Bad source path or URL | no |
| `js_rendering_required` | The page needs JavaScript before it has readable content | no |
| `timeout` | ctxpack did not finish in time | yes |
| `unexpected_output` | ctxpack returned something this server cannot parse, usually a version mismatch | no |
| `ctxpack_not_installed` | No ctxpack binary was found | no |
| `ctxpack_version_unsupported` | The installed ctxpack is older than 0.4.0 | no |

`js_rendering_required` is the one an agent can recover from on its own: fetch the page with a JavaScript-capable tool, then hand the rendered HTML to `pack_content`. The error message says so.

## Troubleshooting

**"ctxpack was not found" even though it is installed.** MCP clients launched from a GUI — desktop apps and IDE extensions — start child processes with a minimal `PATH` that often omits `/opt/homebrew/bin`. The server probes the usual install directories, but the reliable fix is to name the binary outright:

```json
{
  "mcpServers": {
    "ctxpack": {
      "command": "ctxpack-mcp",
      "env": { "CTXPACK_BIN": "/opt/homebrew/bin/ctxpack" }
    }
  }
}
```

`CTXPACK_BIN` takes precedence over `PATH`. If it points at something that is not executable, the server says so rather than quietly searching elsewhere.

**The server starts but every call fails.** That is deliberate. A missing ctxpack does not stop the server from starting, so your client can show you the install instructions instead of failing to connect.

## Development

```bash
go build ./...
go test ./...          # no ctxpack needed; the exec layer runs against fixtures
gofmt -l . && go vet ./...
```

Integration tests run against a real ctxpack and are behind a build tag:

```bash
go test -tags integration ./...
```

The Claude Code plugin manifests have their own check:

```bash
claude plugin validate . --strict         # the marketplace
claude plugin validate plugin --strict    # the plugin
```

They also run weekly in CI against the minimum supported ctxpack and the newest upstream release. This server's contract is entirely upstream's JSON shape and exit codes, so an upstream change can break it with no change here.

Design notes are in [`docs/design.md`](docs/design.md).

## Acknowledgments

All of the hard part belongs to [**ctxpack**](https://github.com/atani/ctxpack) by [@atani](https://github.com/atani): the readability extraction, the chrome filtering, the query-based section reordering, the token estimation, and the cumulative savings history. This repository adds a protocol adapter on top and nothing else. If ctxpack is useful to you, star that project rather than this one.

ctxpack's roadmap mentions a native MCP mode. If upstream ships one, this project will either be contributed there or step aside — staying thin is partly about keeping that option cheap.

Issues about extraction quality — content wrongly dropped, sections ordered oddly, token counts that look off — belong upstream. Issues about tool shapes, error codes, client configuration, or binary discovery belong here.

## License

MIT, matching ctxpack.
