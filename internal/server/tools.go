package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nandemo-ya/ctxpack-mcp/internal/ctxpack"
)

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

// handlers binds the tools to one ctxpack runner, which caches binary
// resolution across calls.
type handlers struct {
	runner *ctxpack.Runner
}

// Handlers return an 'any' output so ctxpack's JSON is passed through verbatim.
// Inferring an output schema from a Go struct would bind this server to a
// snapshot of the upstream schema and break on every upstream field addition.

func addTools(s *mcp.Server, runner *ctxpack.Runner) {
	h := &handlers{runner: runner}

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
	}, h.pack)

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
	}, h.packContent)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stats",
		Title:       "Show cumulative token savings",
		Description: "Report cumulative token savings across every recorded ctxpack run.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: ptr(false),
		},
	}, h.stats)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "reset_stats",
		Title:       "Reset cumulative token savings",
		Description: "Erase the cumulative ctxpack savings history. This cannot be undone.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: ptr(true),
			OpenWorldHint:   ptr(false),
		},
	}, h.resetStats)
}

func (h *handlers) pack(ctx context.Context, _ *mcp.CallToolRequest, in packInput) (*mcp.CallToolResult, any, error) {
	out, err := h.runner.Pack(ctx, in.Source, ctxpack.Options{Query: in.Query, NoRecord: in.NoRecord})
	return jsonResult(out, err)
}

func (h *handlers) packContent(ctx context.Context, _ *mcp.CallToolRequest, in packContentInput) (*mcp.CallToolResult, any, error) {
	out, err := h.runner.PackContent(ctx, in.Content, ctxpack.Options{Query: in.Query, NoRecord: in.NoRecord})
	return jsonResult(out, err)
}

func (h *handlers) stats(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	out, err := h.runner.Stats(ctx)
	return jsonResult(out, err)
}

func (h *handlers) resetStats(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	out, err := h.runner.ResetStats(ctx)
	if err != nil {
		return errorResult(err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: out}},
	}, nil, nil
}

// jsonResult returns ctxpack's JSON as both text and structured content, so a
// client that ignores structuredContent still sees the result.
func jsonResult(out json.RawMessage, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return errorResult(err)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(out)}},
		StructuredContent: out,
	}, nil, nil
}

// errorResult reports a failure as tool output rather than a protocol error, so
// the model can read the code and recover — js_rendering_required in
// particular has a documented next step.
func errorResult(err error) (*mcp.CallToolResult, any, error) {
	var e *ctxpack.Error
	if !errors.As(err, &e) {
		// Not ours to classify: let the SDK turn it into a tool error.
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: e.Error()}},
		StructuredContent: map[string]any{
			"code":      string(e.Code),
			"message":   e.Message,
			"retriable": e.Retriable,
		},
	}, nil, nil
}

func ptr[T any](v T) *T { return &v }
