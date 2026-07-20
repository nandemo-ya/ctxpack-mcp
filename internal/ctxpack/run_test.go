package ctxpack

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func retriableOf(t *testing.T, err error) bool {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not a *ctxpack.Error", err)
	}
	return e.Retriable
}

// fixture is a fake ctxpack plus the files recording how it was called.
type fixture struct {
	runner    *Runner
	argsFile  string
	stdinFile string
}

// scriptedCtxpack installs a fake ctxpack that answers `--version`, records its
// arguments and stdin, then runs body.
//
// PATH is deliberately emptied so binary resolution cannot find a real ctxpack,
// which means the script must call external commands by absolute path.
func scriptedCtxpack(t *testing.T, body string) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a shell script")
	}

	isolate(t)
	dir := t.TempDir()
	f := &fixture{
		argsFile:  filepath.Join(dir, "args"),
		stdinFile: filepath.Join(dir, "stdin"),
	}

	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'ctxpack 0.4.0'; exit 0; fi\n" +
		": > " + f.argsFile + "\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> " + f.argsFile + "; done\n" +
		"/bin/cat > " + f.stdinFile + "\n" +
		body + "\n"

	path := filepath.Join(dir, binaryName)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	f.runner = &Runner{Resolver: Resolver{FallbackDirs: []string{dir}}}
	return f
}

func (f *fixture) args(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(f.argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func (f *fixture) stdin(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.stdinFile)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	return string(data)
}

func (f *fixture) ran() bool {
	_, err := os.Stat(f.argsFile)
	return err == nil
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestPackPassesJSONThroughAndBuildsArgs(t *testing.T) {
	f := scriptedCtxpack(t, `printf '%s' '{"ok":true,"title":"Example"}'`)

	got, err := f.runner.Pack(context.Background(), "https://example.com", Options{Query: "pricing", NoRecord: true})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if string(got) != `{"ok":true,"title":"Example"}` {
		t.Errorf("output = %s, want the fixture JSON verbatim", got)
	}

	want := []string{"https://example.com", "--json", "--query", "pricing", "--no-record"}
	if args := f.args(t); !equalArgs(args, want) {
		t.Errorf("args = %q, want %q", args, want)
	}
}

func TestPackOmitsUnsetFlags(t *testing.T) {
	f := scriptedCtxpack(t, `printf '%s' '{"ok":true}'`)

	if _, err := f.runner.Pack(context.Background(), "page.html", Options{}); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	want := []string{"page.html", "--json"}
	if args := f.args(t); !equalArgs(args, want) {
		t.Errorf("args = %q, want %q", args, want)
	}
}

func TestPackContentSendsContentOnStdin(t *testing.T) {
	f := scriptedCtxpack(t, `printf '%s' '{"ok":true}'`)

	const content = "<h1>hello</h1>\n<p>body</p>"
	if _, err := f.runner.PackContent(context.Background(), content, Options{}); err != nil {
		t.Fatalf("PackContent: %v", err)
	}
	// ctxpack reads the document from stdin, so a dropped pipe would silently
	// pack nothing at all.
	if got := f.stdin(t); got != content {
		t.Errorf("stdin = %q, want %q", got, content)
	}

	want := []string{"-", "--json"}
	if args := f.args(t); !equalArgs(args, want) {
		t.Errorf("args = %q, want %q", args, want)
	}
}

func TestStatsAndResetStatsInvokeSubcommands(t *testing.T) {
	f := scriptedCtxpack(t, `printf '%s' '{"runs":3}'`)
	if _, err := f.runner.Stats(context.Background()); err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if args := f.args(t); !equalArgs(args, []string{"stats", "--json"}) {
		t.Errorf("stats args = %q", args)
	}

	// reset has no JSON mode upstream, so the confirmation line is returned as
	// text rather than parsed.
	reset := scriptedCtxpack(t, `echo 'Reset 12 recorded run(s).'`)
	out, err := reset.runner.ResetStats(context.Background())
	if err != nil {
		t.Fatalf("ResetStats: %v", err)
	}
	if out != "Reset 12 recorded run(s)." {
		t.Errorf("output = %q", out)
	}
	if args := reset.args(t); !equalArgs(args, []string{"reset", "--yes"}) {
		t.Errorf("reset args = %q", args)
	}
}

func TestExitCodesMapToCodes(t *testing.T) {
	tests := []struct {
		name      string
		exit      int
		stderr    string
		want      Code
		retriable bool
		mentions  string
	}{
		{
			name:      "network failure",
			exit:      1,
			stderr:    "ctxpack: network error (retriable): no such host",
			want:      CodeRuntime,
			retriable: true,
			mentions:  "no such host",
		},
		{
			name:     "missing file",
			exit:     2,
			stderr:   "ctxpack: file not found: /nope.html",
			want:     CodeUsage,
			mentions: "/nope.html",
		},
		{
			name:   "javascript page",
			exit:   3,
			stderr: "ctxpack: page appears to require JavaScript rendering",
			want:   CodeJSRendering,
			// The recovery path has to name the tool that can finish the job.
			mentions: "pack_content",
		},
		{
			name:     "unexpected status",
			exit:     7,
			stderr:   "ctxpack: kaboom",
			want:     CodeRuntime,
			mentions: "7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := scriptedCtxpack(t, "echo '"+tt.stderr+"' >&2\nexit "+strconv.Itoa(tt.exit))

			_, err := f.runner.Pack(context.Background(), "https://example.com", Options{})
			if err == nil {
				t.Fatal("Pack succeeded, want an error")
			}
			if got := codeOf(t, err); got != tt.want {
				t.Errorf("code = %q, want %q", got, tt.want)
			}
			if got := retriableOf(t, err); got != tt.retriable {
				t.Errorf("retriable = %v, want %v", got, tt.retriable)
			}
			if !strings.Contains(err.Error(), tt.mentions) {
				t.Errorf("message %q does not mention %q", err.Error(), tt.mentions)
			}
		})
	}
}

func TestMalformedJSONIsReported(t *testing.T) {
	f := scriptedCtxpack(t, `printf '%s' 'not json at all'`)

	_, err := f.runner.Stats(context.Background())
	if err == nil {
		t.Fatal("Stats succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeUnexpectedOutput {
		t.Errorf("code = %q, want %q", got, CodeUnexpectedOutput)
	}
	if !strings.Contains(err.Error(), "not json at all") {
		t.Errorf("message %q does not quote the offending output", err.Error())
	}
}

func TestTimeoutKillsTheProcess(t *testing.T) {
	// Absolute path: the fixture runs with an emptied PATH.
	f := scriptedCtxpack(t, "/bin/sleep 30")
	f.runner.Timeout = 200 * time.Millisecond

	start := time.Now()
	_, err := f.runner.Pack(context.Background(), "https://example.com", Options{})
	if err == nil {
		t.Fatal("Pack succeeded, want a timeout")
	}
	if got := codeOf(t, err); got != CodeTimeout {
		t.Errorf("code = %q, want %q", got, CodeTimeout)
	}
	if !retriableOf(t, err) {
		t.Error("timeout should be retriable")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Pack took %s; the child was not killed", elapsed)
	}
}

func TestCallerCancellationIsNotATimeout(t *testing.T) {
	f := scriptedCtxpack(t, "/bin/sleep 30")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := f.runner.Pack(ctx, "https://example.com", Options{})
	if err == nil {
		t.Fatal("Pack succeeded, want cancellation")
	}
	// A client that cancels should not be told ctxpack was too slow.
	var e *Error
	if errors.As(err, &e) && e.Code == CodeTimeout {
		t.Errorf("cancellation reported as %q", e.Code)
	}
}

func TestEmptyInputsAreRejectedWithoutRunning(t *testing.T) {
	f := scriptedCtxpack(t, `printf '%s' '{"ok":true}'`)

	if _, err := f.runner.Pack(context.Background(), "   ", Options{}); err == nil {
		t.Error("Pack accepted a blank source")
	} else if got := codeOf(t, err); got != CodeUsage {
		t.Errorf("code = %q, want %q", got, CodeUsage)
	}

	if _, err := f.runner.PackContent(context.Background(), "", Options{}); err == nil {
		t.Error("PackContent accepted empty content")
	} else if got := codeOf(t, err); got != CodeUsage {
		t.Errorf("code = %q, want %q", got, CodeUsage)
	}

	if f.ran() {
		t.Error("ctxpack was executed despite invalid input")
	}
}

func TestLongOutputIsTruncatedInErrors(t *testing.T) {
	// A ctxpack that floods stdout must not flood the model's context with it.
	f := scriptedCtxpack(t, `/bin/dd if=/dev/zero bs=1 count=5000 2>/dev/null | /usr/bin/tr '\0' 'x'`)

	_, err := f.runner.Stats(context.Background())
	if err == nil {
		t.Fatal("Stats succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeUnexpectedOutput {
		t.Fatalf("code = %q, want %q", got, CodeUnexpectedOutput)
	}
	if len(err.Error()) > maxCapturedOutput+300 {
		t.Errorf("error message is %d bytes; truncation did not apply", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("message %q does not say it was truncated", err.Error())
	}
}

func TestErrorUnwrapsToTheProcessFailure(t *testing.T) {
	f := scriptedCtxpack(t, "exit 2")

	_, err := f.runner.Pack(context.Background(), "x", Options{})
	if err == nil {
		t.Fatal("Pack succeeded, want an error")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Error("the process failure is not reachable through errors.As")
	}
}
