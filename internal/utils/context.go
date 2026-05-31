package utils

import (
	"mimoproxy/internal/models"
	"os"
	"strconv"
	"strings"
)

// ContextLimits controls how much history is sent to Xiaomi per request.
type ContextLimits struct {
	MaxMessages        int
	MaxChars           int
	MaxToolResultChars int
}

func ContextLimitsFromEnv(agentMode bool) ContextLimits {
	limits := ContextLimits{
		MaxMessages:        envInt("MAX_CONTEXT_MESSAGES", 80),
		MaxChars:           envInt("MAX_CONTEXT_CHARS", 4000000),
		MaxToolResultChars: envInt("MAX_TOOL_RESULT_CHARS", 32000),
	}
	if agentMode {
		if v := envInt("AGENT_MAX_MESSAGES", 20); v > 0 {
			limits.MaxMessages = v
		}
		if v := envInt("AGENT_MAX_CONTEXT_CHARS", 100000); v > 0 {
			limits.MaxChars = v
		}
		if v := envInt("AGENT_MAX_TOOL_RESULT_CHARS", 6000); v > 0 {
			limits.MaxToolResultChars = v
		}
	}
	return limits
}

func AgentFastModeEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_FAST_MODE")))
	if v == "false" || v == "0" {
		return false
	}
	return true // default on
}

func AgentSequentialToolsEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_SEQUENTIAL_TOOLS")))
	return v == "true" || v == "1"
}

// TrimMessagesForProxy keeps system prompt + recent turns and caps large tool outputs.
func TrimMessagesForProxy(messages []models.Message, limits ContextLimits) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]models.Message, 0, len(messages))
	var system []models.Message
	var rest []models.Message
	for _, m := range messages {
		if m.Role == "system" {
			system = append(system, truncateMessageContent(m, limits.MaxToolResultChars))
		} else {
			rest = append(rest, truncateMessageContent(m, limits.MaxToolResultChars))
		}
	}
	out = append(out, system...)
	if limits.MaxMessages > 0 && len(rest) > limits.MaxMessages {
		rest = rest[len(rest)-limits.MaxMessages:]
	}
	out = append(out, rest...)
	return out
}

func truncateMessageContent(m models.Message, maxChars int) models.Message {
	if maxChars <= 0 {
		return m
	}
	switch v := m.Content.(type) {
	case string:
		m.Content = truncateString(v, maxChars)
	case []interface{}:
		parts := make([]interface{}, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, truncateString(s, maxChars))
				continue
			}
			if block, ok := item.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if t, ok := block["text"].(string); ok {
						block["text"] = truncateString(t, maxChars)
					}
				}
				parts = append(parts, block)
				continue
			}
			parts = append(parts, item)
		}
		m.Content = parts
	}
	return m
}

func truncateString(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "\n...[truncated for proxy speed]"
}

// FormatToolsAsInstructionsCompact is a shorter tools block for agent/IDE latency.
func FormatToolsAsInstructionsCompact(tools []models.Tool, toolChoice string) string {
	if len(tools) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n# Tools — call with <tool_call>{\"name\":\"fn\",\"arguments\":{...}}</tool_call>\n")
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		fn := tool.Function
		sb.WriteString("- ")
		sb.WriteString(fn.Name)
		if fn.Description != "" {
			sb.WriteString(": ")
			if len(fn.Description) > 120 {
				sb.WriteString(fn.Description[:120])
				sb.WriteString("…")
			} else {
				sb.WriteString(fn.Description)
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("After planning, emit tool_call XML immediately. Do not stop with only a plan.\n")
	if toolChoice == "required" || toolChoice == "any" {
		sb.WriteString("tool_choice: you MUST call a tool this turn.\n")
	}
	return sb.String()
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
