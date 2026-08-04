package services

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestParseDuckDuckGoResults(t *testing.T) {
	document, err := xhtml.Parse(strings.NewReader(`
		<html><body><div class="result results_links">
		<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">The Go documentation</a>
		<a class="result__snippet">Official documentation for the Go programming language.</a>
		</div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	results := parseDuckDuckGoResults(document, 5)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %+v", results)
	}
	if results[0].URL != "https://go.dev/doc/" || results[0].Title != "The Go documentation" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
	if !strings.Contains(results[0].Snippet, "Official documentation") {
		t.Fatalf("unexpected snippet: %q", results[0].Snippet)
	}
}

func TestCleanSearchResultURLRejectsUnsafeScheme(t *testing.T) {
	if result := cleanSearchResultURL("javascript:alert(1)"); result != "" {
		t.Fatalf("unsafe URL was accepted: %q", result)
	}
}
