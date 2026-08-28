package services

import (
	"strings"
	"testing"

	"flip-ai/internal/models"
)

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
	payload, err := buildChatGPTConversationBody("gpt-4o", messages)
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
	content, _ := first[0]["content"].(string)
	if !strings.Contains(content, "Be concise.") {
		t.Errorf("system instruction not embedded in first message: %q", content)
	}
}
