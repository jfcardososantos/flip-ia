/*
 * File: tool.go
 * Project: mimoproxy
 * Created: 2026-04-29
 */

package utils

import (
	"encoding/json"
	"fmt"
	"mimoproxy/internal/models"
	"regexp"
	"strconv"
	"strings"
)

/**
 * Converts OpenAI tool definitions into textual instructions for the system prompt.
 */
func FormatToolsAsInstructions(tools []models.Tool) string {
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n# External Tools\n\n")
	sb.WriteString("You have access to the following tools. To execute a tool, you MUST use the exact XML tag `<tool_call>` with a JSON payload inside. NEVER wrap the JSON in Markdown code blocks (like ```json).\n\n")
	sb.WriteString("Format:\n")
	sb.WriteString("<tool_call>\n{\"name\": \"function_name\", \"arguments\": {\"arg1\": \"value1\"}}\n</tool_call>\n\n")
	sb.WriteString("CRITICAL RULES:\n")
	sb.WriteString("1. If you need to use a tool, output ONLY the `<tool_call>` block. Do NOT include any normal text explaining what you are doing. Do NOT output conversational text in the same response as a tool call.\n")
	sb.WriteString("2. You can only use ONE tool per response.\n")
	sb.WriteString("3. Wait for the tool result before proceeding to the next step.\n")
	sb.WriteString("4. You MUST use one of the exact tool names listed below. Never invent a new tool name.\n")
	sb.WriteString("5. If you want shell-style operations like `head`, `cat`, `ls`, `find`, or `sed`, use the `bash` tool if it exists instead of inventing those names as tools.\n\n")
	sb.WriteString("6. Do NOT say things like 'let me inspect', 'I'll explore', 'first I will check', or describe a future action. If inspection or execution is needed, call a tool immediately.\n")
	sb.WriteString("7. If tools are available and the task requires reading files, searching code, running commands, or inspecting project structure, prefer a tool call over conversational text.\n")
	sb.WriteString("8. Only answer in plain text when you are actually done or when no tool is needed.\n\n")
	sb.WriteString(buildToolCompatibilityHints(tools))
	sb.WriteString("Available tools:\n")

	for _, tool := range tools {
		if tool.Type == "function" {
			funcDef := tool.Function
			sb.WriteString(fmt.Sprintf("\n- %s: %s\n", funcDef.Name, funcDef.Description))
			params, _ := json.Marshal(funcDef.Parameters)
			sb.WriteString(fmt.Sprintf("  Parameters: %s\n", string(params)))
		}
	}

	return sb.String()
}

func buildToolCompatibilityHints(tools []models.Tool) string {
	available := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.Type == "function" {
			available[tool.Function.Name] = true
		}
	}

	var hints []string
	if available["read"] {
		hints = append(hints, "- Use `read` for reading a file's contents. Prefer it over inventing names like `read_file`, `open_file`, or `cat_file`.")
	}
	if available["glob"] {
		hints = append(hints, "- Use `glob` to discover files, enumerate project structure, or match files by pattern.")
	}
	if available["grep"] {
		hints = append(hints, "- Use `grep` to search inside files for symbols, strings, functions, routes, or code patterns.")
	}
	if available["edit"] {
		hints = append(hints, "- Use `edit` for targeted file modifications. Prefer it over inventing names like `apply_patch`, `modify_file`, or `replace_in_file`.")
	}
	if available["write"] {
		hints = append(hints, "- Use `write` to create a new file or fully replace a file's contents.")
	}
	if available["bash"] {
		hints = append(hints, "- Use `bash` for shell commands such as `npm install`, `git status`, `ls`, `head`, `tail`, `cat`, `find`, `sed`, `python`, `node`, and test commands.")
	}
	if available["task"] {
		hints = append(hints, "- Use `task` when a subtask or delegated investigation is useful.")
	}
	if available["webfetch"] {
		hints = append(hints, "- Use `webfetch` to retrieve a webpage or external documentation URL.")
	}
	if available["background_process"] {
		hints = append(hints, "- Use `background_process` for long-running commands you should not block on.")
	}
	if available["agent_manager"] {
		hints = append(hints, "- Use `agent_manager` only when multi-agent coordination is genuinely needed.")
	}

	if len(hints) == 0 {
		return ""
	}

	return "Compatibility hints:\n" + strings.Join(hints, "\n") + "\n\n"
}

/**
 * Parses XML-style tool calls from a text string and converts them to OpenAI format.
 */
func ParseToolCalls(text string) (string, []models.ToolCall) {
	var toolCalls []models.ToolCall
	cleanText := text

	// Regex to find <tool_call>{...}</tool_call>
	toolCallRegex := regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	matches := toolCallRegex.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		jsonStr := strings.TrimSpace(match[1])
		// Remove potential markdown wrappers like ```json ... ``` inside the tag
		jsonStr = regexp.MustCompile("(?s)^```[a-z]*\n").ReplaceAllString(jsonStr, "")
		jsonStr = regexp.MustCompile("(?s)\n```$").ReplaceAllString(jsonStr, "")
		jsonStr = strings.TrimSpace(jsonStr)

		var toolCallData struct {
			Name      string      `json:"name"`
			Arguments interface{} `json:"arguments"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &toolCallData); err == nil && toolCallData.Name != "" {
			var argsStr string
			switch v := toolCallData.Arguments.(type) {
			case string:
				argsStr = v
			default:
				b, _ := json.Marshal(v)
				argsStr = string(b)
			}

			toolCalls = append(toolCalls, models.ToolCall{
				ID:   "call_" + GenerateID(),
				Type: "function",
				Function: models.ToolFunction{
					Name:      toolCallData.Name,
					Arguments: argsStr,
				},
			})
			cleanText = strings.Replace(cleanText, match[0], "", 1)
		} else {
			// Try alternate format: <tool_name>JSON_ARGS (closing tag optional)
			altRegex := regexp.MustCompile(`(?s)<(\w+)>(.*)`)
			altMatch := altRegex.FindStringSubmatch(jsonStr)
			if len(altMatch) >= 3 {
				toolName := altMatch[1]
				argsStr := strings.TrimSpace(altMatch[2])
				// Remove trailing tag if present (e.g. </read_file>)
				closeTag := fmt.Sprintf("</%s>", toolName)
				argsStr = strings.TrimSuffix(strings.TrimSpace(argsStr), closeTag)
				argsStr = strings.TrimSpace(argsStr)
				
				toolCalls = append(toolCalls, models.ToolCall{
					ID:   "call_" + GenerateID(),
					Type: "function",
					Function: models.ToolFunction{
						Name:      toolName,
						Arguments: argsStr,
					},
				})
				cleanText = strings.Replace(cleanText, match[0], "", 1)
			}
		}
	}

	// Robustness check for whole JSON or JSON in Markdown block
	if len(toolCalls) == 0 {
		trimmedText := strings.TrimSpace(text)
		
		// Extract json from markdown block if present
		jsonBlockRegex := regexp.MustCompile(`(?s)\x60\x60\x60(?:json)?\s*({.*?})\s*\x60\x60\x60`)
		jsonMatch := jsonBlockRegex.FindStringSubmatch(trimmedText)
		if len(jsonMatch) >= 2 {
			trimmedText = jsonMatch[1]
		}
		
		if strings.HasPrefix(trimmedText, "{") && strings.HasSuffix(trimmedText, "}") {
			var toolCallData struct {
				Name      string      `json:"name"`
				Arguments interface{} `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(trimmedText), &toolCallData); err == nil && toolCallData.Name != "" && toolCallData.Arguments != nil {
				var argsStr string
				switch v := toolCallData.Arguments.(type) {
				case string:
					argsStr = v
				default:
					b, _ := json.Marshal(v)
					argsStr = string(b)
				}

				toolCalls = append(toolCalls, models.ToolCall{
					ID:   "call_" + GenerateID(),
					Type: "function",
					Function: models.ToolFunction{
						Name:      toolCallData.Name,
						Arguments: argsStr,
					},
				})
				
				// If we successfully parsed a fallback tool call, we clear the text
				// so the conversational text doesn't leak out as content.
				cleanText = ""
			}
		}
	}

	return strings.TrimSpace(cleanText), toolCalls
}

func NormalizeToolCalls(toolCalls []models.ToolCall, availableTools []models.Tool) []models.ToolCall {
	if len(toolCalls) == 0 {
		return toolCalls
	}

	available := make(map[string]models.ToolDefinition, len(availableTools))
	if len(availableTools) > 0 {
		for _, tool := range availableTools {
			if tool.Type == "function" {
				available[tool.Function.Name] = tool.Function
			}
		}
	}

	for i := range toolCalls {
		toolCalls[i].Function.Arguments = RepairToolArguments(toolCalls[i].Function.Arguments)

		if len(available) == 0 {
			continue
		}

		name := toolCalls[i].Function.Name
		if _, ok := available[name]; ok {
			continue
		}

		if normalized, ok := normalizeToolAlias(name, toolCalls[i].Function.Arguments, available); ok {
			toolCalls[i].Function.Name = normalized.Name
			toolCalls[i].Function.Arguments = normalized.Arguments
		}
	}

	return toolCalls
}

func RepairToolArguments(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}

	// Remove markdown code fences when the model wraps JSON incorrectly.
	raw = regexp.MustCompile("(?s)^```[a-zA-Z]*\\s*").ReplaceAllString(raw, "")
	raw = regexp.MustCompile("(?s)\\s*```$").ReplaceAllString(raw, "")
	raw = strings.TrimSpace(raw)

	// Already valid JSON.
	var probe interface{}
	if json.Unmarshal([]byte(raw), &probe) == nil {
		return raw
	}

	// Extract the first balanced JSON object/array if extra braces or trailing text were emitted.
	if candidate := extractBalancedJSON(raw); candidate != "" {
		if json.Unmarshal([]byte(candidate), &probe) == nil {
			return candidate
		}
	}

	// Common model glitch: one or more extra closing braces at the end.
	trimmed := raw
	for strings.HasSuffix(trimmed, "}") || strings.HasSuffix(trimmed, "]") {
		trimmed = strings.TrimSpace(trimmed[:len(trimmed)-1])
		if balanced := extractBalancedJSON(trimmed); balanced != "" {
			if json.Unmarshal([]byte(balanced), &probe) == nil {
				return balanced
			}
		}
	}

	return raw
}

func extractBalancedJSON(raw string) string {
	start := strings.IndexAny(raw, "{[")
	if start == -1 {
		return ""
	}

	var stack []rune
	inString := false
	escaped := false

	for i, r := range raw[start:] {
		switch {
		case escaped:
			escaped = false
			continue
		case r == '\\' && inString:
			escaped = true
			continue
		case r == '"':
			inString = !inString
			continue
		case inString:
			continue
		case r == '{' || r == '[':
			stack = append(stack, r)
		case r == '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return strings.TrimSpace(raw[start : start+i])
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return strings.TrimSpace(raw[start : start+i+1])
			}
		case r == ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return strings.TrimSpace(raw[start : start+i])
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return strings.TrimSpace(raw[start : start+i+1])
			}
		}
	}

	return ""
}

func ExtractTerminalToolContent(toolCalls []models.ToolCall) (string, []models.ToolCall) {
	if len(toolCalls) == 0 {
		return "", toolCalls
	}

	var parts []string
	filtered := make([]models.ToolCall, 0, len(toolCalls))

	for _, call := range toolCalls {
		name := strings.TrimSpace(strings.ToLower(call.Function.Name))
		if !isTerminalPseudoTool(name) {
			filtered = append(filtered, call)
			continue
		}

		if text := extractTerminalText(call.Function.Arguments); text != "" {
			parts = append(parts, text)
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n")), filtered
}

func isTerminalPseudoTool(name string) bool {
	switch name {
	case "attempt_completion", "finish", "final", "final_answer", "done", "respond", "complete", "attemptcomplete":
		return true
	default:
		return false
	}
}

func extractTerminalText(rawArgs string) string {
	if strings.TrimSpace(rawArgs) == "" {
		return ""
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal([]byte(rawArgs), &asMap); err == nil {
		for _, key := range []string{"final_output", "result", "response", "answer", "content", "text", "message"} {
			if val, ok := asMap[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
	}

	var asString string
	if err := json.Unmarshal([]byte(rawArgs), &asString); err == nil {
		return strings.TrimSpace(asString)
	}

	return strings.TrimSpace(rawArgs)
}

func normalizeToolAlias(name string, rawArgs string, available map[string]models.ToolDefinition) (models.ToolFunction, bool) {
	name = strings.TrimSpace(strings.ToLower(name))

	var args map[string]interface{}
	_ = json.Unmarshal([]byte(rawArgs), &args)

	buildBash := func(command string) (models.ToolFunction, bool) {
		payload, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			return models.ToolFunction{}, false
		}
		return models.ToolFunction{Name: "bash", Arguments: string(payload)}, true
	}
	buildMapped := func(toolName string, payload map[string]interface{}) (models.ToolFunction, bool) {
		if _, ok := available[toolName]; !ok {
			return models.ToolFunction{}, false
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return models.ToolFunction{}, false
		}
		return models.ToolFunction{Name: toolName, Arguments: string(b)}, true
	}
	firstAny := func(keys ...string) interface{} {
		for _, key := range keys {
			if val, ok := args[key]; ok {
				return val
			}
		}
		return nil
	}
	copyIfPresent := func(dst map[string]interface{}, srcKeys ...string) {
		for _, key := range srcKeys {
			if val, ok := args[key]; ok {
				dst[key] = val
				return
			}
		}
	}

	pathKeys := []string{"path", "file_path", "filepath", "file"}
	firstString := func(keys ...string) string {
		for _, key := range keys {
			if val, ok := args[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
		return ""
	}
	firstInt := func(keys ...string) int {
		for _, key := range keys {
			switch v := args[key].(type) {
			case float64:
				return int(v)
			case string:
				if n, err := strconv.Atoi(v); err == nil {
					return n
				}
			}
		}
		return 0
	}

	switch name {
	case "read_file", "readfile", "open_file", "view_file", "cat_file":
		path := firstString(pathKeys...)
		if path == "" {
			return models.ToolFunction{}, false
		}
		payload := map[string]interface{}{"file_path": path}
		copyIfPresent(payload, "offset", "start_line", "line_start")
		copyIfPresent(payload, "limit", "end_line", "line_end")
		return buildMapped("read", payload)
	case "list_files", "list_dir", "list_directory", "find_files", "glob_search", "search_files_by_pattern":
		path := firstString("path", "dir", "directory")
		pattern := firstString("pattern", "glob", "query")
		if pattern == "" {
			if path != "" {
				pattern = strings.TrimRight(path, "/") + "/**/*"
			} else {
				pattern = "**/*"
			}
		}
		payload := map[string]interface{}{"pattern": pattern}
		if path != "" {
			payload["path"] = path
		}
		return buildMapped("glob", payload)
	case "search_code", "search_files", "ripgrep", "grep_search", "find_in_files":
		pattern := firstString("pattern", "query", "search", "text", "regex")
		path := firstString("path", "dir", "directory")
		if pattern == "" {
			return models.ToolFunction{}, false
		}
		payload := map[string]interface{}{"pattern": pattern}
		if path != "" {
			payload["path"] = path
		}
		copyIfPresent(payload, "include", "exclude")
		return buildMapped("grep", payload)
	case "write_file", "create_file", "save_file", "overwrite_file":
		path := firstString(pathKeys...)
		content, _ := firstAny("content", "text", "contents", "value").(string)
		if path == "" {
			return models.ToolFunction{}, false
		}
		payload := map[string]interface{}{"file_path": path, "content": content}
		return buildMapped("write", payload)
	case "edit_file", "modify_file", "replace_in_file", "apply_patch":
		path := firstString(pathKeys...)
		if path == "" {
			return models.ToolFunction{}, false
		}
		payload := map[string]interface{}{"file_path": path}
		for _, key := range []string{"old_string", "new_string", "replace_all", "instructions", "patch", "content"} {
			if val, ok := args[key]; ok {
				payload[key] = val
			}
		}
		return buildMapped("edit", payload)
	case "fetch_url", "browse_url", "open_url", "web_fetch":
		target := firstString("url", "href", "target")
		if target == "" {
			return models.ToolFunction{}, false
		}
		payload := map[string]interface{}{"url": target}
		return buildMapped("webfetch", payload)
	case "delegate_task", "subtask", "run_subtask":
		payload := map[string]interface{}{}
		for _, key := range []string{"prompt", "task", "description", "goal"} {
			if val, ok := args[key]; ok {
				payload[key] = val
				break
			}
		}
		return buildMapped("task", payload)
	case "run_in_background", "long_running_command", "background_command":
		payload := map[string]interface{}{}
		for _, key := range []string{"command", "cmd", "description", "workdir"} {
			if val, ok := args[key]; ok {
				payload[key] = val
			}
		}
		return buildMapped("background_process", payload)
	case "manage_agents", "spawn_agent", "multi_agent":
		return buildMapped("agent_manager", args)
	case "head":
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		path := firstString(pathKeys...)
		if path == "" {
			return models.ToolFunction{}, false
		}
		lines := firstInt("lines", "n", "count")
		if lines <= 0 {
			lines = 20
		}
		return buildBash(fmt.Sprintf("head -n %d %s", lines, strconv.Quote(path)))
	case "tail":
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		path := firstString(pathKeys...)
		if path == "" {
			return models.ToolFunction{}, false
		}
		lines := firstInt("lines", "n", "count")
		if lines <= 0 {
			lines = 20
		}
		return buildBash(fmt.Sprintf("tail -n %d %s", lines, strconv.Quote(path)))
	case "cat":
		if _, ok := available["read"]; ok {
			path := firstString(pathKeys...)
			if path != "" {
				return buildMapped("read", map[string]interface{}{"file_path": path})
			}
		}
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		path := firstString(pathKeys...)
		if path == "" {
			return models.ToolFunction{}, false
		}
		return buildBash(fmt.Sprintf("cat %s", strconv.Quote(path)))
	case "ls":
		if _, ok := available["glob"]; ok {
			path := firstString("path", "dir", "directory")
			if path == "" {
				path = "."
			}
			return buildMapped("glob", map[string]interface{}{"pattern": strings.TrimRight(path, "/") + "/**/*", "path": path})
		}
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		path := firstString("path", "dir", "directory")
		if path == "" {
			path = "."
		}
		return buildBash(fmt.Sprintf("ls -la %s", strconv.Quote(path)))
	case "pwd":
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		return buildBash("pwd")
	case "find":
		if _, ok := available["glob"]; ok {
			path := firstString("path", "dir", "directory")
			pattern := firstString("pattern", "name", "query", "glob")
			if path == "" {
				path = "."
			}
			if pattern == "" {
				pattern = "**/*"
			}
			return buildMapped("glob", map[string]interface{}{"pattern": pattern, "path": path})
		}
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		path := firstString("path", "dir", "directory")
		if path == "" {
			path = "."
		}
		return buildBash(fmt.Sprintf("find %s", strconv.Quote(path)))
	case "sed", "awk", "npm", "node", "python", "pytest", "go", "git", "cargo", "pnpm", "yarn":
		if _, ok := available["bash"]; !ok {
			return models.ToolFunction{}, false
		}
		command := firstString("command", "cmd")
		if command == "" {
			command = name
			if value := firstString("args", "arguments"); value != "" {
				command += " " + value
			}
		}
		return buildBash(command)
	}

	return models.ToolFunction{}, false
}

/**
 * Converts an OpenAI message back to a string format that MiMo understands.
 */
func FormatMessageForMiMo(message models.Message) string {
	var parts []string

	// Handle tool results (as a separate message or as parts)
	if message.Role == "tool" {
		contentStr := ""
		switch v := message.Content.(type) {
		case string:
			contentStr = v
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "text" {
						if text, ok := m["text"].(string); ok {
							contentStr += text
						}
					}
				}
			}
		}
		return fmt.Sprintf("\n<tool_result>\n%s\n</tool_result>\n\n[SYSTEM REMINDER: If you still need to inspect files, run commands, search code, or take any action, respond ONLY with a `<tool_call>` block using one exact tool name from the available tools list. Do NOT narrate intent like 'let me check'. If no action is needed, answer normally in plain text.]\n", contentStr)
	}

	// Handle normal content and complex parts
	if message.Content != nil {
		switch v := message.Content.(type) {
		case string:
			parts = append(parts, v)
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					mType, _ := m["type"].(string)
					switch mType {
					case "text":
						if text, ok := m["text"].(string); ok {
							parts = append(parts, text)
						}
					case "reasoning":
						if text, ok := m["text"].(string); ok {
							parts = append(parts, fmt.Sprintf("<think>%s</think>", text))
						}
					case "tool_use":
						name, _ := m["name"].(string)
						input := m["input"]
						inputBytes, _ := json.Marshal(input)
						parts = append(parts, fmt.Sprintf("<tool_call>{\"name\": \"%s\", \"arguments\": %s}</tool_call>", name, string(inputBytes)))
					case "tool_result":
						content, _ := m["content"].(string)
						parts = append(parts, fmt.Sprintf("<tool_result>%s</tool_result>", content))
					}
				}
			}
		}
	}

	// Handle tool calls
	if len(message.ToolCalls) > 0 {
		for _, tc := range message.ToolCalls {
			if tc.Type == "function" {
				var args interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				argsBytes, _ := json.Marshal(args)
				parts = append(parts, fmt.Sprintf("<tool_call>{\"name\": \"%s\", \"arguments\": %s}</tool_call>", tc.Function.Name, string(argsBytes)))
			}
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n"))
}
