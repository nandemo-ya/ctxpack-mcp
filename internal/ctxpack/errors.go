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
