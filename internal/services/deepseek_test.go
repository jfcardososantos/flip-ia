package services

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"flip-ai/internal/models"
)

type deepSeekTestRoundTripper func(*http.Request) (*http.Response, error)

func (fn deepSeekTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestDeepSeekHeadersMatchImportedBrowserIdentity(t *testing.T) {
	headers := DeepSeekHeaders(models.DeepSeekAuth{Cookie: "session=ok", Token: "token"}, StoredWebSession{
		UserAgent: "Mozilla/5.0 Chrome/140.0.0.0 Safari/537.36",
	}, nil)

	if headers["x-client-platform"] != "web" || headers["x-client-version"] != "2.0.2" {
		t.Fatalf("browser identity has inconsistent client metadata: %#v", headers)
	}
	if headers["accept-charset"] != "UTF-8" || headers["x-client-locale"] == "" {
		t.Fatalf("required DeepSeek metadata is missing: %#v", headers)
	}
}

func TestDeepSeekHeadersAllowVersionOverride(t *testing.T) {
	t.Setenv("DEEPSEEK_CLIENT_VERSION", "2.1.0")
	headers := DeepSeekHeaders(models.DeepSeekAuth{Cookie: "session=ok", Token: "token"}, StoredWebSession{
		Headers: map[string]string{"x-client-version": "1.0.0"},
	}, map[string]string{"x-client-version": "1.1.0"})
	if headers["x-client-platform"] != "android" || headers["x-client-version"] != "2.1.0" {
		t.Fatalf("unexpected overridden mobile identity: %#v", headers)
	}
}

func TestCreateDeepSeekSessionAcceptsNestedResponseShape(t *testing.T) {
	originalClient := GlobalHTTPClient
	t.Cleanup(func() { GlobalHTTPClient = originalClient })
	GlobalHTTPClient = &http.Client{Transport: deepSeekTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		body := `{"code":0,"msg":"","data":{"biz_data":{"chat_session":{"id":"nested-session"}}}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	sessionID, err := CreateDeepSeekSession(models.DeepSeekAuth{Cookie: "session=ok", Token: "token"}, StoredWebSession{}, nil)
	if err != nil || sessionID != "nested-session" {
		t.Fatalf("nested session response was not accepted: id=%q err=%v", sessionID, err)
	}
}

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

func TestParseDeepSeekExpertPromotesTerminalThinkOnlyAnswer(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"p":"response/fragments","o":"APPEND","v":[{"type":"THINK","content":"resposta recuperada"}]}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	}, "\n")

	result := ParseDeepSeekStreamMode(bytes.NewBufferString(stream), true)
	if result.Content != "resposta recuperada" || result.ReasoningText != "" {
		t.Fatalf("terminal THINK-only answer was not promoted: %+v", result)
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
