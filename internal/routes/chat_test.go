package routes

import "testing"

func TestAgentLocationOnlyRegex(t *testing.T) {
	text := "/Users/jfcardososantos/Documents/alfst-homepage/src/app/budget/page.tsx 80 20"
	if !agentLocationOnlyRegex.MatchString(text) {
		t.Fatalf("expected location-only response to match")
	}

	final := "Alterei /Users/me/app/page.tsx e concluí os ajustes solicitados."
	if agentLocationOnlyRegex.MatchString(final) {
		t.Fatalf("expected normal final response not to match")
	}
}
