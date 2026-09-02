package routes

import (
	"testing"

	"flip-ai/internal/models"
	"flip-ai/internal/services"
)

func TestChatGPTUnsentMessagesSkipsStoredAssistant(t *testing.T) {
	messages := []models.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "follow-up"},
	}
	pending := chatGPTUnsentMessages(messages, services.WebChatState{ChatID: "conv_1", ClientMessageCount: 1})
	if len(pending) != 1 || pending[0].Role != "user" || pending[0].Content != "follow-up" {
		t.Fatalf("unexpected pending messages: %+v", pending)
	}
}

func TestChatGPTUnsentMessagesKeepsToolResult(t *testing.T) {
	messages := []models.Message{
		{Role: "user", Content: "run it"},
		{Role: "assistant", ToolCalls: []models.ToolCall{{Function: models.ToolFunction{Name: "terminal", Arguments: `{}`}}}},
		{Role: "tool", Content: "done"},
	}
	pending := chatGPTUnsentMessages(messages, services.WebChatState{ChatID: "conv_1", ClientMessageCount: 1})
	if len(pending) != 2 || pending[0].Role != "assistant" || pending[1].Role != "tool" {
		t.Fatalf("unexpected pending tool messages: %+v", pending)
	}
}
