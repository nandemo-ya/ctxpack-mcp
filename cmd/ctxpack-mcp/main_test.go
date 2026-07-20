package main

import "testing"

func TestResolveVersionPrefersLdflags(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "1.2.3"
	if got := resolveVersion(); got != "1.2.3" {
		t.Errorf("resolveVersion() = %q, want the injected version", got)
	}
}

func TestResolveVersionFallsBackWithoutLdflags(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	// Release builds inject a version; go install stamps one into the build
	// info; neither applies here, so this exercises the last resort.
	version = ""
	if got := resolveVersion(); got == "" {
		t.Error("resolveVersion() is empty; clients show this string as the server version")
	}
}
