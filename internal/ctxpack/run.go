package ctxpack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout bounds a single ctxpack run. ctxpack times out its own URL
// fetches after 20 seconds, so this only fires when the process hangs.
const DefaultTimeout = 60 * time.Second

// maxCapturedOutput caps how much process output is quoted back in an error.
// Enough to identify the problem, short enough not to flood the context window.
const maxCapturedOutput = 400

// waitDelay bounds how long Wait lingers after the process is killed. Output is
// captured through pipes, and Wait blocks until every writer closes them, so
// anything still holding a descriptor would otherwise keep this call alive long
// past its deadline and make the timeout meaningless.
const waitDelay = 2 * time.Second

// Runner executes the ctxpack CLI. Do not copy a Runner after first use.
type Runner struct {
	// Resolver finds the binary. The zero value is usable.
	Resolver Resolver
	// Timeout overrides DefaultTimeout.
	Timeout time.Duration
}

// Options are the flags shared by the packing commands.
type Options struct {
	// Query moves sections relevant to it toward the top of the output.
	Query string
	// NoRecord skips recording the run in the cumulative savings history.
	NoRecord bool
}

func (o Options) args() []string {
	var args []string
	if o.Query != "" {
		args = append(args, "--query", o.Query)
	}
	if o.NoRecord {
		args = append(args, "--no-record")
	}
	return args
}

// Pack extracts context from a URL or local file.
func (r *Runner) Pack(ctx context.Context, source string, opts Options) (json.RawMessage, error) {
	if strings.TrimSpace(source) == "" {
		return nil, &Error{Code: CodeUsage, Message: "source is empty; pass a URL or a path to a local HTML or Markdown file"}
	}
	args := append([]string{source, "--json"}, opts.args()...)
	return r.runJSON(ctx, "", args...)
}

// PackContent cleans HTML or Markdown supplied by the caller, which ctxpack
// trusts as already rendered.
func (r *Runner) PackContent(ctx context.Context, content string, opts Options) (json.RawMessage, error) {
	if content == "" {
		return nil, &Error{Code: CodeUsage, Message: "content is empty; pass the HTML or Markdown to clean"}
	}
	args := append([]string{"-", "--json"}, opts.args()...)
	return r.runJSON(ctx, content, args...)
}

// Stats reports cumulative token savings.
func (r *Runner) Stats(ctx context.Context) (json.RawMessage, error) {
	return r.runJSON(ctx, "", "stats", "--json")
}

// ResetStats erases the savings history and returns ctxpack's confirmation
// line. This command has no JSON mode.
func (r *Runner) ResetStats(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "", "reset", "--yes")
	return strings.TrimSpace(out), err
}

// runJSON runs ctxpack and returns its stdout, verified to be JSON.
func (r *Runner) runJSON(ctx context.Context, stdin string, args ...string) (json.RawMessage, error) {
	out, err := r.run(ctx, stdin, args...)
	if err != nil {
		return nil, err
	}
	if !json.Valid([]byte(out)) {
		return nil, &Error{
			Code: CodeUnexpectedOutput,
			Message: fmt.Sprintf(
				"ctxpack did not return JSON, which usually means an unexpected version. Output was: %s",
				truncate(out)),
		}
	}
	return json.RawMessage(out), nil
}

// run executes ctxpack and translates process failures into *Error.
func (r *Runner) run(ctx context.Context, stdin string, args ...string) (string, error) {
	binary, err := r.Resolver.Resolve(ctx)
	if err != nil {
		return "", err
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary.Path, args...)
	cmd.WaitDelay = waitDelay
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr == nil {
		return stdout.String(), nil
	}

	// Our own deadline, not the caller's cancellation.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return "", &Error{
			Code:      CodeTimeout,
			Message:   fmt.Sprintf("ctxpack did not finish within %s", timeout),
			Retriable: true,
			err:       runErr,
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return "", &Error{
			Code:      CodeRuntime,
			Message:   fmt.Sprintf("could not run ctxpack: %v", runErr),
			Retriable: true,
			err:       runErr,
		}
	}
	return "", exitError(exitErr.ExitCode(), stderr.String(), runErr)
}

// exitError maps ctxpack's documented exit codes onto machine-readable codes.
func exitError(code int, stderr string, cause error) *Error {
	detail := strings.TrimSpace(stderr)

	switch code {
	case 1:
		return &Error{
			Code:      CodeRuntime,
			Message:   withDetail("ctxpack hit a network or runtime error; retrying may work.", detail),
			Retriable: true,
			err:       cause,
		}
	case 2:
		return &Error{
			Code:    CodeUsage,
			Message: withDetail("ctxpack rejected the request; check the source path or URL.", detail),
			err:     cause,
		}
	case 3:
		// Upstream's hint tells a shell user to pipe a rendered DOM into
		// `ctxpack -`. Through MCP the equivalent move is pack_content, so say
		// that instead: this is the one error an agent can fully recover from.
		return &Error{
			Code: CodeJSRendering,
			Message: withDetail(
				"This page requires JavaScript rendering, so ctxpack found no readable content. "+
					"Fetch it with a JavaScript-capable tool (a browser or your own fetch tool), "+
					"then pass the rendered HTML to the pack_content tool.", detail),
			err: cause,
		}
	default:
		return &Error{
			Code:    CodeRuntime,
			Message: withDetail(fmt.Sprintf("ctxpack exited with an unexpected status %d.", code), detail),
			err:     cause,
		}
	}
}

func withDetail(message, detail string) string {
	if detail == "" {
		return message
	}
	return message + " ctxpack said: " + truncate(detail)
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxCapturedOutput {
		return s
	}
	return s[:maxCapturedOutput] + "… (truncated)"
}
