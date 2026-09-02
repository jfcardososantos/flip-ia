package services

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"flip-ai/internal/models"
)

type chatGPTRoundTripper func(*http.Request) (*http.Response, error)

func (fn chatGPTRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestResolveChatGPTWebModel(t *testing.T) {
	cases := map[string]string{
		"chatgpt-web":             "gpt-4o",
		"chatgpt-web/gpt-5":       "gpt-5",
		"chatgpt-web/o3":          "o3",
		"chatgpt-web/gpt-4o-mini": "gpt-4o-mini",
	}
	for input, want := range cases {
		got, ok := ResolveChatGPTWebModel(input)
		if !ok || got != want {
			t.Errorf("ResolveChatGPTWebModel(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	for _, bad := range []string{"", "default", "gpt 5", "chatgpt-web/../x", "3.5"} {
		if _, ok := ResolveChatGPTWebModel(bad); ok {
			t.Errorf("ResolveChatGPTWebModel(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestResolveChatGPTWebModelAliasUsesEnv(t *testing.T) {
	t.Setenv("CHATGPT_WEB_DEFAULT_MODEL", "gpt-5")
	got, ok := ResolveChatGPTWebModel("chatgpt-web")
	if !ok || got != "gpt-5" {
		t.Fatalf("alias did not honor CHATGPT_WEB_DEFAULT_MODEL: %q, %v", got, ok)
	}
	t.Setenv("CHATGPT_WEB_DEFAULT_MODEL", "")
}

func TestIsChatGPTWebModel(t *testing.T) {
	if !IsChatGPTWebModel("chatgpt-web") || !IsChatGPTWebModel("chatgpt-web/o3") {
		t.Fatal("expected chatgpt-web models to be recognized")
	}
	if IsChatGPTWebModel("qwen-web") || IsChatGPTWebModel("kimi-k3") {
		t.Fatal("unexpected model recognized as ChatGPT")
	}
}

func TestChatGPTAccessTokenAcceptsObjectSessionResponse(t *testing.T) {
	originalClient := GlobalHTTPClient
	GlobalHTTPClient = &http.Client{Transport: chatGPTRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"accessToken":"token-from-object"}`)),
			Request:    request,
		}, nil
	})}
	t.Cleanup(func() { GlobalHTTPClient = originalClient })

	token, err := ChatGPTAccessToken(StoredWebSession{Cookie: "session=ok"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "token-from-object" {
		t.Fatalf("token = %q", token)
	}
}

func TestParseChatGPTStreamReadsContentAndReasoning(t *testing.T) {
	input := "event: response.created\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"message_type\":\"content\",\"content\":{\"text\":\"Hello \"}}\n\n" +
		"data: {\"message_type\":\"reasoning\",\"content\":{\"text\":\"thinking\"}}\n\n" +
		"data: {\"message_type\":\"content\",\"content\":{\"parts\":[{\"content_type\":\"text\",\"text\":\"world\"}]}}\n\n" +
		"data: [DONE]\n\n"
	result, err := parseChatGPTStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseChatGPTStream error: %v", err)
	}
	if result.Content != "Hello world" {
		t.Errorf("content = %q; want %q", result.Content, "Hello world")
	}
	if result.ReasoningText != "thinking" {
		t.Errorf("reasoning = %q; want %q", result.ReasoningText, "thinking")
	}
}

func TestParseChatGPTStreamEmptyReturnsError(t *testing.T) {
	if _, err := parseChatGPTStream(strings.NewReader(": keep-alive\n\n")); err == nil {
		t.Fatal("expected empty stream to return an error")
	}
}

func TestBuildChatGPTConversationBodyEmbedsSystem(t *testing.T) {
	messages := []models.Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "Hello"},
	}
	payload, err := buildChatGPTConversationBody("gpt-4o", WebChatState{}, messages)
	if err != nil {
		t.Fatalf("buildChatGPTConversationBody error: %v", err)
	}
	if got := payload["model"]; got != "gpt-4o" {
		t.Errorf("model = %v", got)
	}
	first, _ := payload["messages"].([]map[string]interface{})
	if len(first) == 0 {
		t.Fatal("expected at least one message")
	}
	contentMap, _ := first[0]["content"].(map[string]interface{})
	parts, _ := contentMap["parts"].([]string)
	content := strings.Join(parts, "")
	if !strings.Contains(content, "Be concise.") {
		t.Errorf("system instruction not embedded in first message: %q", content)
	}
}

func TestBuildChatGPTConversationBodyContinuesConversation(t *testing.T) {
	state := WebChatState{ChatID: "conv_1", ParentMessageID: "msg_1"}
	payload, err := buildChatGPTConversationBody("gpt-4o", state, []models.Message{{Role: "user", Content: "Next"}})
	if err != nil {
		t.Fatal(err)
	}
	if payload["conversation_id"] != "conv_1" || payload["parent_message_id"] != "msg_1" {
		t.Fatalf("conversation linkage missing: %+v", payload)
	}
	if _, exists := payload["history_and_training_disabled"]; exists {
		t.Fatal("conversation history must not be forcibly disabled")
	}
}

func TestParseChatGPTNativeConversationStream(t *testing.T) {
	input := "data: {\"conversation_id\":\"conv_1\",\"message\":{\"id\":\"msg_1\",\"author\":{\"role\":\"assistant\"},\"content\":{\"content_type\":\"text\",\"parts\":[\"Hel\"]}}}\n\n" +
		"data: {\"conversation_id\":\"conv_1\",\"message\":{\"id\":\"msg_1\",\"author\":{\"role\":\"assistant\"},\"content\":{\"content_type\":\"text\",\"parts\":[\"Hello\"]}}}\n\n" +
		"data: [DONE]\n\n"
	result, err := parseChatGPTStream(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Hello" || result.ConversationID != "conv_1" || result.MessageID != "msg_1" {
		t.Fatalf("unexpected native stream result: %+v", result)
	}
}

func TestParseChatGPTWebModelsUsesAccountResponse(t *testing.T) {
	models, err := parseChatGPTWebModels([]byte(`{
		"default_model_slug":"gpt-current",
		"models":[
			{"slug":"gpt-current","title":"GPT Current","max_tokens":200000},
			{"slug":"o-next","description":"Reasoning model","max_tokens":300000},
			{"slug":"gpt-current","title":"duplicate"},
			{"slug":"invalid/model"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("model count = %d: %+v", len(models), models)
	}
	if models[0].Slug != "gpt-current" || !models[0].Default || models[0].MaxTokens != 200000 {
		t.Fatalf("unexpected default model: %+v", models[0])
	}
	if models[1].Slug != "o-next" || models[1].Description != "Reasoning model" {
		t.Fatalf("unexpected second model: %+v", models[1])
	}
}

func TestIsChatGPTCloudflareChallenge(t *testing.T) {
	header := make(map[string][]string)
	header["Cf-Mitigated"] = []string{"challenge"}
	header["Cf-Ray"] = []string{"abc123-GRU"}

	blockedBody := `<html><title>Sorry, you have been blocked</title><body>You are unable to access chatgpt.com. Please verify you are human.</body></html>`
	if !isChatGPTCloudflareChallenge(header, blockedBody) {
		t.Error("expected Cf-Mitigated challenge to be detected")
	}

	if !isChatGPTCloudflareChallenge(make(map[string][]string), blockedBody) {
		t.Error("expected blocked.html to be detected")
	}

	if isChatGPTCloudflareChallenge(make(map[string][]string), `{"error":"some_json_error"}`) {
		t.Error("plain JSON error should not be a Cloudflare challenge")
	}

	if detail := cloudflareChallengeDetail(header, "turnstile html"); !strings.Contains(detail, "abc123-GRU") || !strings.Contains(detail, "turnstile") {
		t.Errorf("unexpected detail: %q", detail)
	}
}
