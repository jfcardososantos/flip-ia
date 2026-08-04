package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveQwenWebModel(t *testing.T) {
	t.Setenv("QWEN_WEB_DEFAULT_MODEL", "")
	tests := map[string]string{
		"qwen-web":              "qwen3.8-max",
		"qwen-web/qwen3.7-plus": "qwen3.7-plus",
		"qwen-web/qwen3.8-max":  "qwen3.8-max",
		"qwen-web/qwen-future":  "qwen-future",
	}
	for input, want := range tests {
		got, ok := ResolveQwenWebModel(input)
		if !ok || got != want {
			t.Fatalf("ResolveQwenWebModel(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	if IsQwenWebModel("qwen3.7-plus") {
		t.Fatal("plain qwen model names must not be captured by the web adapter")
	}
}

func TestParseQwenStream(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"response.created":{"chat_id":"chat_1","parent_id":"user_1","response_id":"msg_123"}}`,
		`data: {"choices":[{"delta":{"phase":"think","content":"reason "}}]}`,
		`data: {"choices":[{"delta":{"content":"hello "}}]}`,
		`data: {"choices":[{"delta":{"content":"world"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
	result, messageID, err := parseQwenStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseQwenStream: %v", err)
	}
	if messageID != "msg_123" {
		t.Fatalf("message id = %q", messageID)
	}
	if result.ReasoningText != "reason " || result.Content != "hello world" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestQwenContextError(t *testing.T) {
	err := &QwenWebError{StatusCode: 400, Body: "maximum context length exceeded"}
	if !IsQwenContextError(err) {
		t.Fatal("expected context-length error to trigger rollover")
	}
}

func TestQwenWebChatCreatesVisibleConversationAndContinuesByResponseID(t *testing.T) {
	t.Setenv("QWEN_WEB_USE_AUTHORIZATION", "false")
	var createCalled, completionCalled, updateCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=qwen" {
			t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("web requests must not send authorization by default: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/chats/new":
			createCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"chat_1"}}`))
		case "/api/chat/completions":
			completionCalled = true
			var payload map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode completion payload: %v", err)
			}
			if payload["chat_id"] != "chat_1" || payload["model"] != "qwen3.7-plus" {
				t.Errorf("unexpected completion payload: %+v", payload)
			}
			if _, exists := payload["headers"]; exists {
				t.Errorf("client-only headers must not be serialized into the Qwen JSON payload: %+v", payload)
			}
			if r.Header.Get("X-Request-Id") == "" {
				t.Error("Qwen HTTP request is missing X-Request-Id")
			}
			if payload["parent_id"] != nil {
				t.Errorf("new chat parent_id = %#v; want null", payload["parent_id"])
			}
			if _, exists := payload["stream_options"]; exists {
				t.Errorf("stream_options must be omitted unless the model advertises usage support: %+v", payload)
			}
			messages, _ := payload["messages"].([]interface{})
			firstMessage, _ := messages[0].(map[string]interface{})
			if files, ok := firstMessage["files"].([]interface{}); !ok || len(files) != 0 {
				t.Errorf("Qwen user message must include an empty files array: %+v", firstMessage)
			}
			if firstMessage["parent_id"] != nil || firstMessage["parentId"] != nil {
				t.Errorf("new user message parents must be null: %+v", firstMessage)
			}
			featureConfig, _ := firstMessage["feature_config"].(map[string]interface{})
			if featureConfig["auto_thinking"] != false || featureConfig["thinking_mode"] != "Fast" {
				t.Errorf("unexpected modern feature config: %+v", featureConfig)
			}
			extra, _ := firstMessage["extra"].(map[string]interface{})
			meta, _ := extra["meta"].(map[string]interface{})
			if meta["subChatType"] != "t2t" {
				t.Errorf("missing Qwen message metadata: %+v", firstMessage)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"response.created\":{\"chat_id\":\"chat_1\",\"parent_id\":\"user_1\",\"response_id\":\"assistant_1\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"phase\":\"answer\",\"content\":\"ready\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"done\":true}\n\n"))
		case "/api/chats/chat_1":
			updateCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	previousBaseURL := qwenWebBaseURL
	qwenWebBaseURL = server.URL
	defer func() { qwenWebBaseURL = previousBaseURL }()

	result, state, err := QwenWebChat(
		StoredWebSession{Cookie: "session=qwen", Token: "token-1"},
		"qwen3.7-plus",
		WebChatState{Provider: "qwen", SessionKey: "project-1"},
		"continue",
		"Project",
		false,
		false,
	)
	if err != nil {
		t.Fatalf("QwenWebChat: %v", err)
	}
	if result.Content != "ready" || state.ChatID != "chat_1" || state.ParentMessageID != "assistant_1" {
		t.Fatalf("unexpected result/state: result=%+v state=%+v", result, state)
	}
	if !createCalled || !completionCalled || !updateCalled {
		t.Fatalf("missing upstream calls: create=%v completion=%v update=%v", createCalled, completionCalled, updateCalled)
	}
}

func TestQwenTransientErrorsAndProxyStatus(t *testing.T) {
	err := &QwenWebError{StatusCode: http.StatusInternalServerError, Body: `{"detail":"Internal server error"}`}
	if !IsQwenTransientError(err) || QwenProxyStatus(err) != http.StatusServiceUnavailable {
		t.Fatalf("Qwen 500 must be retryable and exposed as 503")
	}
	if IsQwenAuthError(err) {
		t.Fatal("generic Qwen 500 must not be reported as a verification failure")
	}
}

func TestQwenHeadersAuthorizationIsExplicitOptIn(t *testing.T) {
	session := StoredWebSession{
		Cookie: "session=qwen", Token: "token-1",
		Headers: map[string]string{"Timezone": "America/Bahia", "Version": "0.1.0"},
	}
	t.Setenv("QWEN_WEB_USE_AUTHORIZATION", "false")
	headers := qwenHeaders(session)
	if got := headers["Authorization"]; got != "" {
		t.Fatalf("authorization enabled by default: %q", got)
	}
	if got := headers["Version"]; got == "0.1.0" {
		t.Fatalf("stored session must not override the current frontend version: %q", got)
	}
	if got := headers["Timezone"]; !strings.Contains(got, "GMT-0300") || strings.Contains(got, "America/Bahia") {
		t.Fatalf("timezone header was not normalized to the browser format: %q", got)
	}

	t.Setenv("QWEN_WEB_USE_AUTHORIZATION", "true")
	if got := qwenHeaders(session)["Authorization"]; got != "Bearer token-1" {
		t.Fatalf("authorization opt-in = %q; want Bearer token-1", got)
	}
}
