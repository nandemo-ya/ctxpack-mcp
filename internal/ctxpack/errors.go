// Package ctxpack locates and runs the ctxpack CLI.
package ctxpack

import "fmt"

// Code identifies a failure mode so agents can branch on it without parsing
// the message text.
type Code string

const (
	// CodeNotInstalled means no ctxpack binary could be found.
	CodeNotInstalled Code = "ctxpack_not_installed"
	// CodeVersionUnsupported means the installed ctxpack predates the behavior
	// this server relies on.
	CodeVersionUnsupported Code = "ctxpack_version_unsupported"
	// CodeRuntime is a network or runtime failure inside ctxpack (exit 1).
	CodeRuntime Code = "runtime_error"
	// CodeUsage is a bad request: a missing file or a malformed source (exit 2).
	CodeUsage Code = "usage_error"
	// CodeJSRendering means the page needs a JavaScript-capable fetcher before
	// ctxpack can read it (exit 3). Recoverable through the pack_content tool.
	CodeJSRendering Code = "js_rendering_required"
	// CodeTimeout means ctxpack outlived this server's patience.
	CodeTimeout Code = "timeout"
	// CodeUnexpectedOutput means stdout was not the JSON this server expects,
	// which usually points at a version mismatch.
	CodeUnexpectedOutput Code = "unexpected_output"
)

// Error is a failure with a machine-readable code. Tool handlers surface these
// to the model, so Message must say what to do next, not just what went wrong.
type Error struct {
	Code      Code
	Message   string
	Retriable bool

	err error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.err }
