package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nandemo-ya/ctxpack-mcp/internal/ctxpack"
)

// fakeCtxpack installs a fake ctxpack that answers --version and otherwise runs
// body, and returns a runner wired to it.
func fakeCtxpack(t *testing.T, body string) *ctxpack.Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a shell script")
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv(ctxpack.EnvBinary, "")

	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'ctxpack 0.4.0'; exit 0; fi\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ctxpack"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	return &ctxpack.Runner{Resolver: ctxpack.Resolver{FallbackDirs: []string{dir}}}
}

// connect wires a client to the server over an in-memory transport.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, fakeCtxpack(t, `printf '%s' '{"ok":true}'`))
}

func connectWith(t *testing.T, runner *ctxpack.Runner) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := newWithRunner("test", runner).Connect(ctx, serverTransport, nil)
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

func TestPackReturnsCtxpackJSONAsStructuredContent(t *testing.T) {
	const output = `{"ok":true,"title":"Example","stats":{"saved_tokens":34300}}`
	session := connectWith(t, fakeCtxpack(t, `printf '%s' '`+output+`'`))

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "pack",
		Arguments: map[string]any{"source": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("call pack: %v", err)
	}
	if res.IsError {
		t.Fatalf("pack failed: %s", text(res))
	}

	// Text content carries the same payload, so clients that ignore
	// structuredContent still see the result.
	if got := text(res); got != output {
		t.Errorf("text content = %s, want %s", got, output)
	}

	var structured map[string]any
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &structured); err != nil {
		t.Fatalf("structured content is not an object: %v", err)
	}
	if structured["title"] != "Example" {
		t.Errorf("structured content = %v, want the upstream fields preserved", structured)
	}
}

func TestToolErrorsCarryTheirCode(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		tool     string
		args     map[string]any
		wantCode string
		mentions string
	}{
		{
			name:     "javascript page",
			body:     "echo 'needs javascript' >&2\nexit 3",
			tool:     "pack",
			args:     map[string]any{"source": "https://app.example.com"},
			wantCode: "js_rendering_required",
			mentions: "pack_content",
		},
		{
			name:     "missing file",
			body:     "echo 'file not found' >&2\nexit 2",
			tool:     "pack",
			args:     map[string]any{"source": "/nope.html"},
			wantCode: "usage_error",
		},
		{
			name:     "network failure",
			body:     "echo 'network error' >&2\nexit 1",
			tool:     "stats",
			args:     map[string]any{},
			wantCode: "runtime_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := connectWith(t, fakeCtxpack(t, tt.body))

			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      tt.tool,
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatalf("call %s: transport error %v, want a tool error", tt.tool, err)
			}
			// Failures travel as tool output, not protocol errors, so the model
			// can read the code and act on it.
			if !res.IsError {
				t.Fatalf("isError = false, want true")
			}

			var structured map[string]any
			if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &structured); err != nil {
				t.Fatalf("structured content is not an object: %v", err)
			}
			if structured["code"] != tt.wantCode {
				t.Errorf("code = %v, want %q", structured["code"], tt.wantCode)
			}
			if _, ok := structured["retriable"]; !ok {
				t.Error("structured content has no retriable field")
			}
			if tt.mentions != "" && !strings.Contains(text(res), tt.mentions) {
				t.Errorf("message %q does not mention %q", text(res), tt.mentions)
			}
		})
	}
}

func TestResetStatsReturnsConfirmationText(t *testing.T) {
	session := connectWith(t, fakeCtxpack(t, "echo 'Reset 12 recorded run(s).'"))

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "reset_stats",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call reset_stats: %v", err)
	}
	if res.IsError {
		t.Fatalf("reset_stats failed: %s", text(res))
	}
	if got := text(res); got != "Reset 12 recorded run(s)." {
		t.Errorf("text = %q", got)
	}
}

func TestMissingCtxpackIsReportedPerCallNotAtStartup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(ctxpack.EnvBinary, "")

	// The server must still connect and list tools, so the client can show the
	// user how to install ctxpack instead of failing to start.
	runner := &ctxpack.Runner{Resolver: ctxpack.Resolver{FallbackDirs: []string{filepath.Join(t.TempDir(), "empty")}}}
	session := connectWith(t, runner)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 4 {
		t.Errorf("got %d tools, want 4", len(tools.Tools))
	}

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "stats",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call stats: %v", err)
	}
	if !res.IsError {
		t.Fatal("isError = false, want true")
	}
	if !strings.Contains(text(res), "brew install atani/tap/ctxpack") {
		t.Errorf("message %q does not tell the user how to install ctxpack", text(res))
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return data
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

func TestPackContentPassesContentThrough(t *testing.T) {
	// The fixture records stdin so a dropped pipe fails loudly here rather than
	// showing up as an empty pack later.
	dir := t.TempDir()
	sink := filepath.Join(dir, "stdin")
	session := connectWith(t, fakeCtxpack(t, "/bin/cat > "+sink+`; printf '%s' '{"ok":true}'`))

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "pack_content",
		Arguments: map[string]any{"content": "<h1>hi</h1>", "query": "greeting"},
	})
	if err != nil {
		t.Fatalf("call pack_content: %v", err)
	}
	if res.IsError {
		t.Fatalf("pack_content failed: %s", text(res))
	}

	got, err := os.ReadFile(sink)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	if string(got) != "<h1>hi</h1>" {
		t.Errorf("ctxpack received %q on stdin", got)
	}
}
