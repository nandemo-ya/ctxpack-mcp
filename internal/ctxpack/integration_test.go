//go:build integration

// These tests run against a real ctxpack installation instead of a fixture.
// This server's entire contract is upstream's JSON shape and exit codes, so an
// upstream release can break it with no change on our side: that is what these
// tests, and the scheduled workflow that runs them, are here to catch.
//
//	go test -tags integration ./internal/ctxpack
package ctxpack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// realRunner uses whatever ctxpack is installed, and fails the test if none is.
func realRunner(t *testing.T) *Runner {
	t.Helper()
	r := &Runner{}
	binary, err := r.Resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("no usable ctxpack: %v", err)
	}
	t.Logf("using ctxpack %s at %s", binary.Version, binary.Path)
	return r
}

// packResult is the subset of upstream's schema this server relies on. Missing
// or renamed fields here mean the tools stop returning usable results.
type packResult struct {
	OK     bool `json:"ok"`
	Source struct {
		URL       string `json:"url"`
		FetchedAt string `json:"fetched_at"`
	} `json:"source"`
	Title   *string `json:"title"`
	Content struct {
		Format string `json:"format"`
		Text   string `json:"text"`
	} `json:"content"`
	Stats struct {
		RawHTMLTokens    int     `json:"raw_html_tokens"`
		CleanTextTokens  int     `json:"clean_text_tokens"`
		FinalTokens      int     `json:"final_tokens"`
		SavedTokens      int     `json:"saved_tokens"`
		ReductionPercent float64 `json:"reduction_percent"`
	} `json:"stats"`
}

func decodePack(t *testing.T, raw json.RawMessage) packResult {
	t.Helper()
	var got packResult
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		// Unknown fields are additive and harmless, so report them without
		// failing: the point is to notice upstream moved, not to block on it.
		t.Logf("upstream JSON has fields this server does not model: %v", err)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode pack result: %v\nraw: %s", err, raw)
		}
	}
	return got
}

func TestRealPackLocalFile(t *testing.T) {
	got := decodePack(t, mustPack(t, "testdata/article.html"))

	if !got.OK {
		t.Error("ok = false")
	}
	if got.Content.Format != "markdown" {
		t.Errorf("content.format = %q, want markdown", got.Content.Format)
	}
	if !strings.Contains(got.Content.Text, "Token Budgets") {
		t.Errorf("content.text lost the article body: %q", got.Content.Text)
	}
	// Page chrome removal is the reason this tool exists.
	for _, noise := range []string{"We use cookies", "Related reading", "console.log", "Home About Contact"} {
		if strings.Contains(got.Content.Text, noise) {
			t.Errorf("content.text still contains %q", noise)
		}
	}
	if got.Stats.RawHTMLTokens <= got.Stats.FinalTokens {
		t.Errorf("stats show no savings: %+v", got.Stats)
	}
	if got.Stats.SavedTokens <= 0 || got.Stats.ReductionPercent <= 0 {
		t.Errorf("stats.saved_tokens/reduction_percent are unset: %+v", got.Stats)
	}
}

func mustPack(t *testing.T, source string) json.RawMessage {
	t.Helper()
	raw, err := realRunner(t).Pack(context.Background(), source, Options{NoRecord: true})
	if err != nil {
		t.Fatalf("Pack(%q): %v", source, err)
	}
	return raw
}

func TestRealPackContentOverStdin(t *testing.T) {
	const html = `<html><body><nav>menu</nav><h1>Heading</h1><p>Paragraph.</p></body></html>`

	raw, err := realRunner(t).PackContent(context.Background(), html, Options{NoRecord: true})
	if err != nil {
		t.Fatalf("PackContent: %v", err)
	}
	got := decodePack(t, raw)

	if !strings.Contains(got.Content.Text, "Heading") {
		t.Errorf("content.text = %q, want the heading", got.Content.Text)
	}
	if strings.Contains(got.Content.Text, "menu") {
		t.Errorf("content.text kept the nav: %q", got.Content.Text)
	}
}

func TestRealQueryReordersSections(t *testing.T) {
	raw, err := realRunner(t).Pack(context.Background(), "testdata/article.html",
		Options{Query: "pricing", NoRecord: true})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	got := decodePack(t, raw)

	pricing := strings.Index(got.Content.Text, "billed per million")
	intro := strings.Index(got.Content.Text, "Agents waste context")
	if pricing < 0 || intro < 0 {
		t.Fatalf("content.text lost a section: %q", got.Content.Text)
	}
	if pricing > intro {
		t.Errorf("--query did not move the matching section up:\n%s", got.Content.Text)
	}
}

func TestRealStatsShape(t *testing.T) {
	raw, err := realRunner(t).Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	var got struct {
		Runs             *int     `json:"runs"`
		SavedTokens      *int     `json:"saved_tokens"`
		ReductionPercent *float64 `json:"reduction_percent"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode stats: %v\nraw: %s", err, raw)
	}
	if got.Runs == nil || got.SavedTokens == nil || got.ReductionPercent == nil {
		t.Errorf("stats is missing a field this server reports: %s", raw)
	}
}

func TestRealMissingFileIsUsageError(t *testing.T) {
	_, err := realRunner(t).Pack(context.Background(), "testdata/does-not-exist.html", Options{NoRecord: true})
	if err == nil {
		t.Fatal("Pack succeeded on a missing file")
	}
	if got := codeOf(t, err); got != CodeUsage {
		t.Errorf("code = %q, want %q (upstream exit 2)", got, CodeUsage)
	}
}

func TestRealJavaScriptPageIsDetected(t *testing.T) {
	// The exit-3 contract is the reason this server requires ctxpack 0.4.0 and
	// the only failure an agent can recover from unaided, so it gets a real
	// server and a real fetch.
	const shell = `<!doctype html><html><head><title>App</title></head><body>` +
		`<div id="root"></div><script src="/app.js"></script>` +
		`<noscript>You need to enable JavaScript to run this app.</noscript></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(shell))
	}))
	defer srv.Close()

	_, err := realRunner(t).Pack(context.Background(), srv.URL, Options{NoRecord: true})
	if err == nil {
		t.Fatal("Pack succeeded on a JavaScript shell; upstream detection changed")
	}
	if got := codeOf(t, err); got != CodeJSRendering {
		t.Fatalf("code = %q, want %q (upstream exit 3)", got, CodeJSRendering)
	}
	// The recovery instruction is the whole value of this error.
	var e *Error
	if errors.As(err, &e) && !strings.Contains(e.Message, "pack_content") {
		t.Errorf("message does not point at pack_content: %s", e.Message)
	}
}

func TestRealVersionMeetsMinimum(t *testing.T) {
	binary, err := new(Resolver).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if binary.Version.Less(MinVersion) {
		t.Errorf("installed ctxpack %s is below the supported minimum %s", binary.Version, MinVersion)
	}
}
