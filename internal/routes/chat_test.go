package routes

import (
	"encoding/json"
	"strings"
	"testing"

	"flip-ai/internal/models"
)

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

func TestAdaptSystemPromptForMiMoAgentIdentity(t *testing.T) {
	prompt := "You are Hermes Agent.\nUse browser tools to complete the task."
	adapted := adaptSystemPromptForMiMo(prompt, true)
	lower := strings.ToLower(adapted)
	if strings.Contains(lower, "you are hermes") {
		t.Fatalf("expected Hermes identity claim to be neutralized, got %q", adapted)
	}
	if !strings.Contains(adapted, "external automation client") {
		t.Fatalf("expected adapter note, got %q", adapted)
	}
	if !strings.Contains(adapted, "user-authorized") {
		t.Fatalf("expected authorization note, got %q", adapted)
	}
}

func TestExtractPathOnlyResponse(t *testing.T) {
	text := "/Users/jfcardososantos/Documents/alfst-homepage/src/app/budget/page.tsx /Users/jfcardososantos/Documents/alfst-homepage/src/app/contact/page.tsx"
	paths := extractPathOnlyResponse(text)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}

	withLocations := "/Users/jfcardososantos/Documents/alfst-homepage/src/app/budget/page.tsx 80 30 /Users/jfcardososantos/Documents/alfst-homepage/src/app/contact/page.tsx 80 30"
	paths = extractPathOnlyResponse(withLocations)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths with locations, got %d", len(paths))
	}
	if strings.Contains(paths[0], "80") || strings.Contains(paths[1], "80") {
		t.Fatalf("expected returned paths without line/column, got %+v", paths)
	}

	final := "Concluí ajustes em /Users/me/app/page.tsx e /Users/me/app/contact/page.tsx."
	if paths := extractPathOnlyResponse(final); len(paths) != 0 {
		t.Fatalf("expected normal final response not to be path-only, got %+v", paths)
	}
}

func TestSynthesizePathReadToolCalls(t *testing.T) {
	result := parsedMimoChat{
		CleanText:    "/tmp/app/page.tsx /tmp/app/contact/page.tsx",
		FinishReason: "stop",
	}
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "read",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filePath": map[string]interface{}{"type": "string"},
				},
			},
		},
	}}

	out := synthesizePathReadToolCalls(result, tools, nil)
	if out.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %s", out.FinishReason)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(out.ToolCalls))
	}
	if out.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("unexpected tool name: %s", out.ToolCalls[0].Function.Name)
	}
	if !strings.Contains(out.ToolCalls[1].Function.Arguments, "contact/page.tsx") {
		t.Fatalf("unexpected arguments: %s", out.ToolCalls[1].Function.Arguments)
	}
}

func TestSynthesizeReadCommandToolCalls(t *testing.T) {
	result := parsedMimoChat{
		CleanText:    "sed -n '82,95p' /Users/jfcardososantos/Documents/alfst-homepage/src/app/budget/page.tsx Read budget page lines 82-95 sed -n '84,97p' /Users/jfcardososantos/Documents/alfst-homepage/src/app/contact/page.tsx Read contact page lines 84-97",
		FinishReason: "stop",
	}
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "read",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filePath": map[string]interface{}{"type": "string"},
				},
			},
		},
	}}

	out := synthesizePathReadToolCalls(result, tools, nil)
	if out.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %s", out.FinishReason)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[0].Function.Arguments, "budget/page.tsx") {
		t.Fatalf("unexpected first arguments: %s", out.ToolCalls[0].Function.Arguments)
	}
	if !strings.Contains(out.ToolCalls[1].Function.Arguments, "contact/page.tsx") {
		t.Fatalf("unexpected second arguments: %s", out.ToolCalls[1].Function.Arguments)
	}
}

func TestParseMimoChatBodyAttributedToolCall(t *testing.T) {
	body := strings.NewReader("event: message\n" +
		"data: {\"content\":\"O ambiente execute_code usa Python isolado.\\n<tool_call name=\\\"terminal\\\">{\\\"command\\\":\\\"source ~/composio-venv-uv/bin/activate && python -V && deactivate\\\",\\\"timeout\\\":30}</tool_call>\"}\n\n")

	result := parseMimoChatBody(body, "test", "mimo", "query", nil, false)
	if result.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %s with clean text %q", result.FinishReason, result.CleanText)
	}
	if result.CleanText != "" {
		t.Fatalf("expected empty clean text for tool call response, got %q", result.CleanText)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Function.Name != "terminal" {
		t.Fatalf("unexpected tool name: %s", result.ToolCalls[0].Function.Name)
	}
	if !strings.Contains(result.ToolCalls[0].Function.Arguments, "composio-venv-uv") {
		t.Fatalf("unexpected arguments: %s", result.ToolCalls[0].Function.Arguments)
	}
}

func TestShouldRetryAgentToolCallForPortugueseActionIntent(t *testing.T) {
	result := parsedMimoChat{
		CleanText: `Além disso, notei que o prompt positivo no workflow está vazio.
Vou verificar se o script está realmente substituindo o texto do prompt antes de enviar.
Vou reexecutar com o timeout estendido e monitorar o log do ComfyUI.`,
		FinishReason: "stop",
	}

	if !shouldRetryAgentToolCall(result, "auto") {
		t.Fatalf("expected retry for action-intent-only response")
	}
}

func TestShouldNotRetryAgentToolCallForCompletedPortugueseResponse(t *testing.T) {
	result := parsedMimoChat{
		CleanText:    "Concluí a verificação, ajustei o workflow e corrigi o prompt positivo vazio no generate.py.",
		FinishReason: "stop",
	}

	if shouldRetryAgentToolCall(result, "auto") {
		t.Fatalf("expected no retry for completed response")
	}
}

func TestShouldRetryAgentToolCallForMalformedExecuteCodePayload(t *testing.T) {
	result := parsedMimoChat{
		CleanText: `doc_content = docs_service.documents().get(documentId=doc_id).execute()
for titulo in titulos:
    format_requests.append({"updateParagraphStyle": {}})
docs_service.documents().batchUpdate(documentId=doc_id, body={"requests": format_requests}).execute()
print("Documento formatado")
", timeout: 30 }`,
		FinishReason: "stop",
	}

	if !shouldRetryAgentToolCall(result, "auto") {
		t.Fatalf("expected retry for malformed execute_code payload")
	}
}

func TestSynthesizeCodeExecutionToolCalls(t *testing.T) {
	text := `Vou executar com execute_code:
doc_content = docs_service.documents().get(documentId=doc_id).execute()
for titulo in titulos:
    format_requests.append({"title": titulo})
docs_service.documents().batchUpdate(documentId=doc_id, body={"requests": format_requests}).execute()
print("Documento formatado")
", timeout: 30 }`
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "execute_code",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code":    map[string]interface{}{"type": "string"},
					"timeout": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}}

	calls := synthesizeCodeExecutionToolCalls(text, tools)
	if len(calls) != 1 {
		t.Fatalf("expected one synthesized call, got %d", len(calls))
	}
	if calls[0].Function.Name != "execute_code" {
		t.Fatalf("unexpected tool: %s", calls[0].Function.Name)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("invalid synthesized arguments: %v", err)
	}
	code, _ := args["code"].(string)
	if !strings.HasPrefix(code, "doc_content =") || strings.Contains(code, "timeout:") {
		t.Fatalf("unexpected recovered code: %q", code)
	}
	if args["timeout"] != float64(30) {
		t.Fatalf("unexpected timeout: %#v", args["timeout"])
	}
}

func TestDeepSeekThinkingEnabledForCurrentOfficialModels(t *testing.T) {
	if deepSeekThinkingEnabled("deepseek-v4-flash") {
		t.Fatal("Flash should use the Web default non-thinking mode")
	}
	if !deepSeekThinkingEnabled("deepseek-v4-pro") {
		t.Fatal("Pro should use Web Expert thinking mode")
	}
}

func TestPrepareDeepSeekToolCallResultPreservesReasoning(t *testing.T) {
	result := prepareDeepSeekToolCallResult(models.DeepSeekChatResult{
		Content:       `<tool_call>{"name":"terminal","arguments":{"command":"pwd"}}</tool_call>`,
		ReasoningText: "preciso inspecionar o diretório antes de continuar",
	})

	if result.Content != "" {
		t.Fatalf("tool bridge content should be cleared, got %q", result.Content)
	}
	if result.ReasoningText != "preciso inspecionar o diretório antes de continuar" {
		t.Fatalf("reasoning_content must survive tool calls, got %q", result.ReasoningText)
	}
}

func TestSynthesizeRequiredZeroArgumentToolCall(t *testing.T) {
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name:       "get_current_time",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}}
	call, ok := synthesizeRequiredZeroArgumentToolCall("required", tools)
	if !ok || call.Function.Name != "get_current_time" || call.Function.Arguments != "{}" {
		t.Fatalf("unexpected synthesized tool call: %+v, ok=%v", call, ok)
	}
}

func TestSynthesizeRequiredToolCallRejectsRequiredArguments(t *testing.T) {
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "search",
			Parameters: map[string]interface{}{
				"type": "object", "required": []interface{}{"query"},
			},
		},
	}}
	if _, ok := synthesizeRequiredZeroArgumentToolCall("required", tools); ok {
		t.Fatal("must not invent arguments for a tool with required parameters")
	}
}

func TestFilterKimiAllowedToolCallsRejectsInventedTool(t *testing.T) {
	tools := []models.Tool{{Type: "function", Function: models.ToolDefinition{Name: "get_current_time"}}}
	calls := []models.ToolCall{
		{Type: "function", Function: models.ToolFunction{Name: "ipython", Arguments: `{}`}},
		{Type: "function", Function: models.ToolFunction{Name: "get_current_time", Arguments: `{}`}},
	}
	filtered := filterKimiAllowedToolCalls(calls, tools, "required")
	if len(filtered) != 1 || filtered[0].Function.Name != "get_current_time" {
		t.Fatalf("unexpected filtered calls: %+v", filtered)
	}
}

func TestKimiFalseToolRefusalTriggersRecovery(t *testing.T) {
	for _, text := range []string{
		"Infelizmente, não tenho acesso às suas credenciais do Google Docs.",
		"Não consigo criar documentos diretamente na sua conta.",
		"Infelizmente, não tenho a capacidade de criar documentos no Google Docs.",
		"Não posso executar ações em contas externas.",
		"I cannot access Google Slides or your credentials.",
	} {
		if !looksLikeKimiFalseToolRefusal(text) {
			t.Fatalf("expected refusal recovery for %q", text)
		}
	}
}

func TestSynthesizeKimiReadOnlyDiscoveryUsesAuthorizedTerminal(t *testing.T) {
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "terminal",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"command"},
			},
		},
	}}
	call, ok := synthesizeKimiReadOnlyDiscoveryToolCall(tools)
	if !ok || call.Function.Name != "terminal" || !strings.Contains(call.Function.Arguments, "find") {
		t.Fatalf("unexpected discovery call: %+v, ok=%v", call, ok)
	}
}

func TestSynthesizeKimiSkillReadFollowup(t *testing.T) {
	messages := []models.Message{{Role: "tool", Content: "./skills/unrelated/SKILL.md\n./skills/google-docs/SKILL.md\n./skills/google-slides/SKILL.md"}}
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "terminal",
			Parameters: map[string]interface{}{
				"properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"command"},
			},
		},
	}}
	call, ok := synthesizeKimiSkillReadFollowup(messages, tools)
	if !ok || call.Function.Name != "terminal" || !strings.Contains(call.Function.Arguments, "google-docs/SKILL.md") {
		t.Fatalf("unexpected skill-read call: %+v, ok=%v", call, ok)
	}
}

func TestSynthesizeKimiInitialSkillDiscovery(t *testing.T) {
	messages := []models.Message{{Role: "user", Content: "Utilize as skills com minhas credenciais para criar no Google Docs."}}
	tools := []models.Tool{{
		Type: "function",
		Function: models.ToolDefinition{
			Name: "terminal",
			Parameters: map[string]interface{}{
				"properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"command"},
			},
		},
	}}
	call, ok := synthesizeKimiInitialSkillDiscovery(messages, tools)
	if !ok || call.Function.Name != "terminal" || !strings.Contains(call.Function.Arguments, "find") {
		t.Fatalf("unexpected initial skill-discovery call: %+v, ok=%v", call, ok)
	}
}

func TestKimiAgentInstructionsDeclareHostToolsAsReal(t *testing.T) {
	instructions := kimiAgentAdapterInstructions()
	for _, required := range []string{"Hermes Agent", "Kilo Code", "skills", "credentials", "<tool_call>"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("missing %q in Kimi agent instructions", required)
		}
	}
}
