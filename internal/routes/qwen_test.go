package routes

import (
	"strings"
	"testing"

	"flip-ai/internal/models"
	"flip-ai/internal/services"
)

func TestQwenUnsentMessagesReplaysNormalizedToolCallWithResult(t *testing.T) {
	messages := []models.Message{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "do the work"},
		{Role: "assistant", ToolCalls: []models.ToolCall{{Type: "function", Function: models.ToolFunction{Name: "terminal", Arguments: `{"command":"pwd"}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "tool result"},
	}
	state := services.WebChatState{ChatID: "chat_1", ClientMessageCount: 2}
	pending := qwenUnsentMessages(messages, state)
	if len(pending) != 2 || pending[0].Role != "assistant" || pending[1].Role != "tool" {
		t.Fatalf("unexpected pending messages: %+v", pending)
	}
}

func TestQwenUnsentMessagesSkipsOrdinaryAssistantAlreadyStoredUpstream(t *testing.T) {
	messages := []models.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "follow-up"},
	}
	pending := qwenUnsentMessages(messages, services.WebChatState{ChatID: "chat_1", ClientMessageCount: 1})
	if len(pending) != 1 || pending[0].Role != "user" {
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

func TestQwenAgentInstructionsRequireRealExecutionAndValidation(t *testing.T) {
	instructions := qwenAgentAdapterInstructions()
	for _, expected := range []string{"Hermes Agent", "Kilo Code", "inspect the workspace", "run relevant validation", "exactly one valid <tool_call>"} {
		if !strings.Contains(instructions, expected) {
			t.Fatalf("missing %q in Qwen agent instructions", expected)
		}
	}
}

func TestQwenActionTaskWithoutToolCallTriggersRecovery(t *testing.T) {
	result := parsedMimoChat{CleanText: "Pronto, implementei a alteração solicitada.", FinishReason: "stop"}
	messages := []models.Message{{Role: "user", Content: "Implemente no projeto e rode os testes."}}
	if !shouldRetryQwenAgentToolCall(result, "auto", messages) {
		t.Fatal("expected a claimed code change without a tool call to trigger recovery")
	}
}

func TestQwenFinalAnswerAfterToolResultDoesNotForceAnotherTool(t *testing.T) {
	result := parsedMimoChat{CleanText: "Concluído; os testes passaram.", FinishReason: "stop"}
	messages := []models.Message{
		{Role: "user", Content: "Corrija no projeto e rode os testes."},
		{Role: "assistant", ToolCalls: []models.ToolCall{{Function: models.ToolFunction{Name: "apply_patch", Arguments: `{"patch":"change"}`}}}},
		{Role: "tool", Content: "PASS"},
	}
	if shouldRetryQwenAgentToolCall(result, "auto", messages) {
		t.Fatal("a final answer following a tool result must be allowed")
	}
}

func TestQwenReadOnlyDiscoveryIsNotEvidenceOfCodeChange(t *testing.T) {
	result := parsedMimoChat{CleanText: "Pronto, corrigi tudo.", FinishReason: "stop"}
	messages := []models.Message{
		{Role: "user", Content: "Implemente no projeto e rode os testes."},
		{Role: "assistant", ToolCalls: []models.ToolCall{{Function: models.ToolFunction{Name: "terminal", Arguments: `{"command":"pwd; find . -maxdepth 3 -type f"}`}}}},
		{Role: "tool", Content: "./main.go"},
	}
	if !shouldRetryQwenAgentToolCall(result, "auto", messages) {
		t.Fatal("read-only discovery must not allow a false completion claim")
	}
}

func TestQwenMutationCommandIsEvidenceOfCodeChange(t *testing.T) {
	messages := []models.Message{
		{Role: "user", Content: "Altere o arquivo no projeto."},
		{Role: "assistant", ToolCalls: []models.ToolCall{{Function: models.ToolFunction{Name: "terminal", Arguments: `{"command":"apply_patch < /tmp/change.patch"}`}}}},
		{Role: "tool", Content: "Done"},
	}
	if !qwenHasWorkspaceMutation(messages) {
		t.Fatal("apply_patch command should count as workspace mutation")
	}
}

func TestSynthesizeWorkspaceDiscoveryUsesAvailableTerminal(t *testing.T) {
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "terminal",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
					"timeout": map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"command"},
			},
		},
	}}
	call, ok := synthesizeWorkspaceDiscoveryToolCall(tools)
	if !ok || call.Function.Name != "terminal" || !strings.Contains(call.Function.Arguments, "find .") {
		t.Fatalf("unexpected discovery call: %+v, ok=%v", call, ok)
	}
}

func TestFilterAllowedToolCallsForQwenRejectsInventedCommand(t *testing.T) {
	tools := []models.Tool{{Type: "function", Function: models.ToolDefinition{Name: "terminal"}}}
	calls := []models.ToolCall{
		{Type: "function", Function: models.ToolFunction{Name: "python", Arguments: `{}`}},
		{Type: "function", Function: models.ToolFunction{Name: "terminal", Arguments: `{"command":"pwd"}`}},
	}
	filtered := filterAllowedToolCalls(calls, tools, "auto")
	if len(filtered) != 1 || filtered[0].Function.Name != "terminal" {
		t.Fatalf("unexpected filtered calls: %+v", filtered)
	}
}
