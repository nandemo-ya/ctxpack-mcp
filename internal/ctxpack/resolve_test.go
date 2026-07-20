package ctxpack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCtxpack writes an executable stub that prints the given `--version`
// output, and returns its path.
func fakeCtxpack(t *testing.T, dir, versionOutput string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a shell script")
	}

	path := filepath.Join(dir, binaryName)
	script := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(versionOutput) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// isolate points PATH at an empty directory and clears CTXPACK_BIN, so a real
// ctxpack on the developer's machine cannot influence the result.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv(EnvBinary, "")
}

func codeOf(t *testing.T, err error) Code {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not a *ctxpack.Error", err)
	}
	return e.Code
}

func TestResolveUsesEnvOverride(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := fakeCtxpack(t, dir, "ctxpack 0.4.0")
	t.Setenv(EnvBinary, path)

	got, err := new(Resolver).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Path != path {
		t.Errorf("path = %q, want %q", got.Path, path)
	}
	if want := (Version{0, 4, 0}); got.Version != want {
		t.Errorf("version = %v, want %v", got.Version, want)
	}
}

func TestResolveRejectsBrokenEnvOverride(t *testing.T) {
	isolate(t)
	// A working install on PATH must not rescue a bad override: silently
	// ignoring it would hide the misconfiguration.
	pathDir := t.TempDir()
	fakeCtxpack(t, pathDir, "ctxpack 0.4.0")
	t.Setenv("PATH", pathDir)
	t.Setenv(EnvBinary, filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := new(Resolver).Resolve(context.Background())
	if err == nil {
		t.Fatal("Resolve succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeNotInstalled {
		t.Errorf("code = %q, want %q", got, CodeNotInstalled)
	}
	if !strings.Contains(err.Error(), EnvBinary) {
		t.Errorf("message %q does not name %s", err.Error(), EnvBinary)
	}
}

func TestResolveRejectsNonExecutableEnvOverride(t *testing.T) {
	isolate(t)
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit on windows")
	}
	path := filepath.Join(t.TempDir(), "ctxpack")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv(EnvBinary, path)

	_, err := new(Resolver).Resolve(context.Background())
	if err == nil {
		t.Fatal("Resolve succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeNotInstalled {
		t.Errorf("code = %q, want %q", got, CodeNotInstalled)
	}
}

func TestResolveFindsBinaryOnPath(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := fakeCtxpack(t, dir, "ctxpack 0.4.0")
	t.Setenv("PATH", dir)

	got, err := new(Resolver).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Path != path {
		t.Errorf("path = %q, want %q", got.Path, path)
	}
}

func TestResolveProbesFallbackDirsWhenPathMisses(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := fakeCtxpack(t, dir, "ctxpack 0.4.0")

	r := &Resolver{FallbackDirs: []string{filepath.Join(t.TempDir(), "empty"), dir}}
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Path != path {
		t.Errorf("path = %q, want %q", got.Path, path)
	}
}

func TestResolveReportsMissingBinary(t *testing.T) {
	isolate(t)

	r := &Resolver{FallbackDirs: []string{filepath.Join(t.TempDir(), "empty")}}
	_, err := r.Resolve(context.Background())
	if err == nil {
		t.Fatal("Resolve succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeNotInstalled {
		t.Errorf("code = %q, want %q", got, CodeNotInstalled)
	}
	// The message is the only guidance a user gets through their MCP client.
	for _, want := range []string{"brew install atani/tap/ctxpack", EnvBinary} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}

func TestResolveRejectsOldVersion(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	fakeCtxpack(t, dir, "ctxpack 0.3.9")
	t.Setenv("PATH", dir)

	_, err := new(Resolver).Resolve(context.Background())
	if err == nil {
		t.Fatal("Resolve succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeVersionUnsupported {
		t.Errorf("code = %q, want %q", got, CodeVersionUnsupported)
	}
	for _, want := range []string{"0.3.9", MinVersion.String()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}

func TestResolveAcceptsNewerVersion(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	fakeCtxpack(t, dir, "ctxpack 1.0.0")
	t.Setenv("PATH", dir)

	if _, err := new(Resolver).Resolve(context.Background()); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestResolveCachesSuccessButNotFailure(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	r := &Resolver{FallbackDirs: []string{dir}}

	// A missing binary must not poison the resolver: installing ctxpack while
	// the server runs should work without restarting the MCP client.
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve succeeded before the fixture existed")
	}

	path := fakeCtxpack(t, dir, "ctxpack 0.4.0")
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve after install: %v", err)
	}
	if got.Path != path {
		t.Fatalf("path = %q, want %q", got.Path, path)
	}

	// Once resolved, removing the binary does not re-trigger discovery.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Errorf("Resolve did not use the cached result: %v", err)
	}
}

func TestResolveReportsUnreadableVersion(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	fakeCtxpack(t, dir, "ctxpack unknown")
	t.Setenv("PATH", dir)

	_, err := new(Resolver).Resolve(context.Background())
	if err == nil {
		t.Fatal("Resolve succeeded, want an error")
	}
	if got := codeOf(t, err); got != CodeVersionUnsupported {
		t.Errorf("code = %q, want %q", got, CodeVersionUnsupported)
	}
}

func TestCheckExecutableRejectsADirectory(t *testing.T) {
	isolate(t)
	// A directory named ctxpack on PATH must not be mistaken for the binary.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, binaryName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	r := &Resolver{FallbackDirs: []string{dir}}
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve succeeded, want an error")
	} else if got := codeOf(t, err); got != CodeNotInstalled {
		t.Errorf("code = %q, want %q", got, CodeNotInstalled)
	}
}
