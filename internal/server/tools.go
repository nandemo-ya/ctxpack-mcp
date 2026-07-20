package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errNotImplemented is returned until the ctxpack exec layer lands. Tools are
// registered regardless so clients always see the full tool list.
var errNotImplemented = errors.New("not implemented yet: the ctxpack exec layer is tracked in issue #5")

// packInput mirrors `ctxpack SOURCE [--query TEXT] [--no-record]`.
type packInput struct {
	Source   string `json:"source" jsonschema:"URL (http or https) or path to a local HTML or Markdown file"`
	Query    string `json:"query,omitempty" jsonschema:"Description of the task at hand; sections relevant to it move toward the top of the output. No content is dropped."`
	NoRecord bool   `json:"no_record,omitempty" jsonschema:"Skip recording this run in the cumulative savings history"`
}

// packContentInput mirrors `ctxpack - [--query TEXT] [--no-record]`, with the
// content piped over stdin.
type packContentInput struct {
	Content  string `json:"content" jsonschema:"Raw HTML or Markdown to clean"`
	Query    string `json:"query,omitempty" jsonschema:"Description of the task at hand; sections relevant to it move toward the top of the output. No content is dropped."`
	NoRecord bool   `json:"no_record,omitempty" jsonschema:"Skip recording this run in the cumulative savings history"`
}

// noInput is the empty parameter object for tools that take no arguments.
type noInput struct{}

// Handlers return an 'any' output so ctxpack's JSON is passed through verbatim.
// Inferring an output schema from a Go struct would bind this server to a
// snapshot of the upstream schema and break on every upstream field addition.

func addTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "pack",
		Title: "Pack a URL or file into compact context",
		Description: "Extract compact, token-aware context from a URL or a local HTML/Markdown file. " +
			"Prefer this over fetching a page directly: it strips navigation, ads, scripts, and other page chrome, " +
			"returns clean Markdown, and reports how many tokens were saved. " +
			"If the page turns out to need JavaScript rendering, this fails with js_rendering_required; " +
			"fetch the page with a JavaScript-capable tool and pass the resulting HTML to pack_content instead.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: ptr(true),
		},
	}, packHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "pack_content",
		Title: "Pack HTML or Markdown you already have",
		Description: "Clean raw HTML or Markdown that you already hold, without fetching anything. " +
			"Use this after pack reports js_rendering_required: render or fetch the page yourself, then pass the HTML here. " +
			"Content given to this tool is trusted as already rendered.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: ptr(false),
		},
	}, packContentHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stats",
		Title:       "Show cumulative token savings",
		Description: "Report cumulative token savings across every recorded ctxpack run.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: ptr(false),
		},
	}, statsHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "reset_stats",
		Title:       "Reset cumulative token savings",
		Description: "Erase the cumulative ctxpack savings history. This cannot be undone.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: ptr(true),
			OpenWorldHint:   ptr(false),
		},
	}, resetStatsHandler)
}

func packHandler(context.Context, *mcp.CallToolRequest, packInput) (*mcp.CallToolResult, any, error) {
	return nil, nil, errNotImplemented
}

func packContentHandler(context.Context, *mcp.CallToolRequest, packContentInput) (*mcp.CallToolResult, any, error) {
	return nil, nil, errNotImplemented
}

func statsHandler(context.Context, *mcp.CallToolRequest, noInput) (*mcp.CallToolResult, any, error) {
	return nil, nil, errNotImplemented
}

func resetStatsHandler(context.Context, *mcp.CallToolRequest, noInput) (*mcp.CallToolResult, any, error) {
	return nil, nil, errNotImplemented
}

func ptr[T any](v T) *T { return &v }
