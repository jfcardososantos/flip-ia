package routes

import (
	"testing"

	"flip-ai/internal/models"
	"flip-ai/internal/services"
)

func TestQwenUnsentMessagesSkipsAssistantAlreadyStoredUpstream(t *testing.T) {
	messages := []models.Message{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "do the work"},
		{Role: "assistant", Content: "I need a tool"},
		{Role: "tool", Content: "tool result"},
	}
	state := services.WebChatState{ChatID: "chat_1", ClientMessageCount: 2}
	pending := qwenUnsentMessages(messages, state)
	if len(pending) != 1 || pending[0].Role != "tool" {
		t.Fatalf("unexpected pending messages: %+v", pending)
	}
}

func TestQwenUnsentMessagesKeepsInitialConversation(t *testing.T) {
	messages := []models.Message{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "hello"},
	}
	pending := qwenUnsentMessages(messages, services.WebChatState{})
	if len(pending) != len(messages) {
		t.Fatalf("initial conversation was truncated: %+v", pending)
	}
}
