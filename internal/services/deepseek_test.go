package services

import (
	"bytes"
	"strings"
	"testing"

	"flip-ai/internal/models"
)

func TestParseDeepSeekExpertFragmentStreamKeepsThinkingOutOfContent(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"v":{"response":{"message_id":"expert_msg_2","fragments":[{"type":"THINK","content":"analisando "}]}}}`,
		`data: {"p":"response/fragments/-1/content","v":"o pedido"}`,
		`data: {"p":"response/fragments","o":"APPEND","v":[{"type":"RESPONSE","content":""}]}`,
		`data: {"p":"response/fragments/-1/content","v":"bom dia"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	}, "\n")

	result := ParseDeepSeekStreamMode(bytes.NewBufferString(stream), true)
	if result.MessageID != "expert_msg_2" {
		t.Fatalf("unexpected message id: %q", result.MessageID)
	}
	if result.ReasoningText != "analisando o pedido" {
		t.Fatalf("unexpected reasoning: %q", result.ReasoningText)
	}
	if result.Content != "bom dia" {
		t.Fatalf("expected only final response as content, got %q", result.Content)
	}
}

func TestParseDeepSeekDataSkipsFinishedStatus(t *testing.T) {
	result := models.DeepSeekChatResult{}

	parseDeepSeekData(`{"p":"response/status","v":"FINISHED"}`, &result)
	parseDeepSeekData(`{"p":"response/content","v":"ok"}`, &result)

	if result.Content != "ok" {
		t.Fatalf("expected FINISHED status to be ignored, got %q", result.Content)
	}
}

func TestParseDeepSeekDataReadsExpertOpenAIDeltas(t *testing.T) {
	result := models.DeepSeekChatResult{}

	parseDeepSeekData(`{"id":"expert_msg_1","choices":[{"delta":{"reasoning_content":"analisando "}}]}`, &result)
	parseDeepSeekData(`{"choices":[{"delta":{"content":"resposta"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`, &result)

	if result.MessageID != "expert_msg_1" {
		t.Fatalf("unexpected message id: %q", result.MessageID)
	}
	if result.ReasoningText != "analisando " || result.Content != "resposta" {
		t.Fatalf("unexpected expert result: %+v", result)
	}
	if result.Usage.TotalTokens != 14 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestParseDeepSeekDataTreatsReasoningPathAsReasoning(t *testing.T) {
	result := models.DeepSeekChatResult{}
	parseDeepSeekData(`{"p":"response/reasoning_content","v":"pensando"}`, &result)

	if result.ReasoningText != "pensando" || result.Content != "" {
		t.Fatalf("unexpected reasoning-path result: %+v", result)
	}
}

func TestParseDeepSeekDataSkipsStatusText(t *testing.T) {
	result := models.DeepSeekChatResult{}

	parseDeepSeekData(`{"p":"status","v":"almost done"}`, &result)
	parseDeepSeekData(`{"p":"response/content","v":"done"}`, &result)

	if strings.Contains(result.Content, "almost done") {
		t.Fatalf("expected status text to be ignored, got %q", result.Content)
	}
	if result.Content != "done" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestParseDeepSeekDataReadsNestedInitialContent(t *testing.T) {
	result := models.DeepSeekChatResult{}

	parseDeepSeekData(`{"p":"response/content","v":{"content":"O"}}`, &result)
	parseDeepSeekData(`{"p":"response/content","v":"la"}`, &result)

	if result.Content != "Ola" {
		t.Fatalf("expected nested first token to be preserved, got %q", result.Content)
	}
}

func TestParseDeepSeekDataReadsArrayContent(t *testing.T) {
	result := models.DeepSeekChatResult{}

	parseDeepSeekData(`{"p":"response/content","v":[{"text":"O"},"la","FINISHED"]}`, &result)

	if result.Content != "Ola" {
		t.Fatalf("expected array content to be preserved without status, got %q", result.Content)
	}
}

func TestDeepSeekWebModelTypeRoutesOfficialFamilies(t *testing.T) {
	tests := map[string]string{
		"deepseek-v4-flash": "default",
		"deepseek-v5-flash": "default",
		"deepseek-v4-pro":   "expert",
		"deepseek-expert":   "expert",
	}
	for model, want := range tests {
		if got := DeepSeekWebModelType(model); got != want {
			t.Errorf("DeepSeekWebModelType(%q) = %q; want %q", model, got, want)
		}
	}
}
