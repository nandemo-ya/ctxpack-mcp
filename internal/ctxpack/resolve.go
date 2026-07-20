package ctxpack

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// EnvBinary names the environment variable that overrides binary discovery.
const EnvBinary = "CTXPACK_BIN"

// binaryName is the executable to look for on PATH.
const binaryName = "ctxpack"

// versionTimeout bounds `ctxpack --version`, which is a local process that
// does no I/O beyond writing a line.
const versionTimeout = 10 * time.Second

// defaultFallbackDirs are probed when PATH lookup fails. GUI-launched MCP
// clients start child processes with a minimal PATH that omits the Homebrew
// prefix, which is the most common reason a working install looks missing.
var defaultFallbackDirs = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
}

// Binary is a resolved ctxpack installation.
type Binary struct {
	Path    string
	Version Version
}

// Resolver finds the ctxpack binary and confirms it is new enough. The zero
// value is ready to use, and it is safe for concurrent use.
type Resolver struct {
	// FallbackDirs overrides the directories probed after PATH lookup fails.
	FallbackDirs []string

	mu     sync.Mutex
	cached *Binary
}

// Resolve returns the ctxpack binary to run, checking its version on first use.
//
// Successful resolutions are cached; failures are not, so a user who installs
// ctxpack while the server is running recovers on the next tool call instead of
// having to restart the client.
func (r *Resolver) Resolve(ctx context.Context) (Binary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached != nil {
		return *r.cached, nil
	}

	path, err := r.findPath()
	if err != nil {
		return Binary{}, err
	}

	version, err := queryVersion(ctx, path)
	if err != nil {
		return Binary{}, err
	}

	if version.Less(MinVersion) {
		return Binary{}, &Error{
			Code: CodeVersionUnsupported,
			Message: fmt.Sprintf(
				"ctxpack %s at %s is too old; this server needs %s or newer, "+
					"which is the release that started reporting JavaScript-rendered pages instead of returning empty content. "+
					"Upgrade with `brew upgrade ctxpack`.",
				version, path, MinVersion),
		}
	}

	binary := Binary{Path: path, Version: version}
	r.cached = &binary
	return binary, nil
}

// findPath locates the binary without running it.
func (r *Resolver) findPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvBinary)); override != "" {
		// An explicit override that does not work is a configuration mistake,
		// so report it instead of quietly searching elsewhere.
		if err := checkExecutable(override); err != nil {
			return "", &Error{
				Code:    CodeNotInstalled,
				Message: fmt.Sprintf("%s is set to %q, which is not an executable file: %v", EnvBinary, override, err),
				err:     err,
			}
		}
		return override, nil
	}

	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}

	dirs := r.FallbackDirs
	if dirs == nil {
		dirs = defaultFallbackDirs
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, binaryName)
		if checkExecutable(candidate) == nil {
			return candidate, nil
		}
	}

	return "", &Error{
		Code: CodeNotInstalled,
		Message: "ctxpack was not found. Install it with `brew install atani/tap/ctxpack`, " +
			"or see https://github.com/atani/ctxpack for other options. " +
			"If it is already installed, set " + EnvBinary + " to the full path of the binary: " +
			"MCP clients launched from a GUI often run with a minimal PATH that omits the install directory.",
	}
}

// checkExecutable reports whether path is a regular file the process can run.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	// Windows has no executable bit; extension handling is left to the OS.
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

// queryVersion runs `ctxpack --version` and parses its output.
func queryVersion(ctx context.Context, path string) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return Version{}, &Error{
			Code:      CodeNotInstalled,
			Message:   fmt.Sprintf("could not run %s --version: %v", path, err),
			Retriable: true,
			err:       err,
		}
	}

	version, err := parseVersionOutput(string(out))
	if err != nil {
		return Version{}, &Error{
			Code:    CodeVersionUnsupported,
			Message: fmt.Sprintf("could not read the version from %s: %v", path, err),
			err:     err,
		}
	}
	return version, nil
}
