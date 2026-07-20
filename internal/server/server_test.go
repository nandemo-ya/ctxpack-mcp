package server

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires a client to the server over an in-memory transport.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := New("test").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func TestListToolsReportsEverySchemaAndAnnotation(t *testing.T) {
	res, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}

	// destructiveHint is meaningful only when readOnlyHint is false, so the
	// read-only tools deliberately leave it unset.
	want := []struct {
		name        string
		readOnly    bool
		destructive *bool
		openWorld   bool
		required    []string
	}{
		{name: "pack", readOnly: true, openWorld: true, required: []string{"source"}},
		{name: "pack_content", readOnly: true, openWorld: false, required: []string{"content"}},
		{name: "stats", readOnly: true, openWorld: false},
		{name: "reset_stats", readOnly: false, destructive: ptr(true), openWorld: false},
	}

	if len(res.Tools) != len(want) {
		t.Errorf("got %d tools, want %d", len(res.Tools), len(want))
	}

	for _, w := range want {
		tool, ok := tools[w.name]
		if !ok {
			t.Errorf("tool %q not registered", w.name)
			continue
		}
		if tool.Annotations == nil {
			t.Errorf("tool %q: no annotations", w.name)
			continue
		}
		if got := tool.Annotations.ReadOnlyHint; got != w.readOnly {
			t.Errorf("tool %q: readOnlyHint = %v, want %v", w.name, got, w.readOnly)
		}
		switch got := tool.Annotations.DestructiveHint; {
		case w.destructive == nil && got != nil:
			t.Errorf("tool %q: destructiveHint = %v, want unset", w.name, *got)
		case w.destructive != nil && (got == nil || *got != *w.destructive):
			t.Errorf("tool %q: destructiveHint = %v, want %v", w.name, got, *w.destructive)
		}
		if got := tool.Annotations.OpenWorldHint; got == nil || *got != w.openWorld {
			t.Errorf("tool %q: openWorldHint = %v, want %v", w.name, got, w.openWorld)
		}
		if tool.Description == "" {
			t.Errorf("tool %q: no description", w.name)
		}
		for _, field := range w.required {
			if !requires(tool, field) {
				t.Errorf("tool %q: %q is not a required input", w.name, field)
			}
		}
	}
}

// requires reports whether the tool's inferred input schema marks field as
// required. Optional fields carry `omitempty`, which is what keeps them out of
// this list, so a stray tag change would surface here.
func requires(tool *mcp.Tool, field string) bool {
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		return false
	}
	required, ok := schema["required"].([]any)
	if !ok {
		return false
	}
	for _, r := range required {
		if name, ok := r.(string); ok && name == field {
			return true
		}
	}
	return false
}

func TestCallToolReportsNotImplemented(t *testing.T) {
	session := connect(t)

	calls := []struct {
		tool string
		args map[string]any
	}{
		{tool: "pack", args: map[string]any{"source": "https://example.com"}},
		{tool: "pack_content", args: map[string]any{"content": "<h1>hi</h1>"}},
		{tool: "stats", args: map[string]any{}},
		{tool: "reset_stats", args: map[string]any{}},
	}

	for _, call := range calls {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      call.tool,
			Arguments: call.args,
		})
		if err != nil {
			t.Errorf("call %s: transport error %v, want a tool error", call.tool, err)
			continue
		}
		// The stub must fail as tool output, not as a protocol error, so the
		// model can read the reason instead of the client dropping the call.
		if !res.IsError {
			t.Errorf("call %s: isError = false, want true", call.tool)
		}
		if !strings.Contains(text(res), "not implemented") {
			t.Errorf("call %s: content %q does not explain the stub", call.tool, text(res))
		}
	}
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
