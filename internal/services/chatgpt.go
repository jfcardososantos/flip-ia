package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"flip-ai/internal/models"
)

const chatGPTWebBaseURL = "https://chatgpt.com"

type ChatGPTError struct {
	StatusCode int
	Body       string
}

func (e *ChatGPTError) Error() string {
	return fmt.Sprintf("ChatGPT Web returned %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

// ChatGPTWebModel maps a supported model alias to the selector (slug) accepted by
// the ChatGPT Web backend.
func ResolveChatGPTWebModel(model string) (string, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case model == "chatgpt-web":
		if configured := strings.TrimSpace(os.Getenv("CHATGPT_WEB_DEFAULT_MODEL")); configured != "" {
			return configured, true
		}
		return "gpt-4o", true
	case strings.HasPrefix(model, "chatgpt-web/"):
		upstream := strings.TrimSpace(strings.TrimPrefix(model, "chatgpt-web/"))
		if upstream != "" && !strings.ContainsAny(upstream, " \t\r\n?#") &&
			!strings.Contains(upstream, "..") && !strings.Contains(upstream, "/") {
			return upstream, true
		}
	case strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o"):
		if !strings.ContainsAny(model, " /\\?#") && !strings.Contains(model, "..") {
			return model, true
		}
	}
	return "", false
}

func IsChatGPTWebModel(model string) bool {
	_, ok := ResolveChatGPTWebModel(model)
	return ok
}

func GetSelectedChatGPTSession() (StoredWebSession, error) {
	session, err := GetStoredWebSession("chatgpt")
	if err != nil {
		return StoredWebSession{}, err
	}
	if strings.TrimSpace(session.Cookie) == "" {
		return StoredWebSession{}, errors.New("missing ChatGPT cookie jar")
	}
	return session, nil
}

func IsChatGPTAuthError(err error) bool {
	var chatErr *ChatGPTError
	if errors.As(err, &chatErr) && (chatErr.StatusCode == http.StatusUnauthorized || chatErr.StatusCode == http.StatusForbidden) {
		return true
	}
	body := strings.ToLower(err.Error())
	return strings.Contains(body, "unauthorized") || strings.Contains(body, "authentication") ||
		strings.Contains(body, "cloudflare") || strings.Contains(body, "turnstile") || strings.Contains(body, "captcha")
}

func IsChatGPTTransientError(err error) bool {
	var chatErr *ChatGPTError
	if errors.As(err, &chatErr) {
		return chatErr.StatusCode == http.StatusTooManyRequests || chatErr.StatusCode >= http.StatusInternalServerError
	}
	body := strings.ToLower(err.Error())
	return strings.Contains(body, "empty stream") || strings.Contains(body, "timeout") ||
		strings.Contains(body, "connection reset") || strings.Contains(body, "unexpected eof")
}

func ChatGPTProxyStatus(err error) int {
	var chatErr *ChatGPTError
	if errors.As(err, &chatErr) && chatErr.StatusCode == http.StatusTooManyRequests {
		return http.StatusTooManyRequests
	}
	if IsChatGPTTransientError(err) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

// ChatGPTAccessToken resolves the access token for the stored session. The token
// from the imported session is preferred; if it is empty (or stale), the token is
// fetched from the official /api/auth/session endpoint using the raw cookie jar.
func ChatGPTAccessToken(session StoredWebSession) (string, error) {
	if token := strings.TrimSpace(WebSessionToken(session)); token != "" {
		return token, nil
	}
	req, err := http.NewRequest(http.MethodGet, chatGPTWebBaseURL+"/api/auth/session", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", chatGPTWebBaseURL)
	req.Header.Set("Referer", chatGPTWebBaseURL+"/")
	req.Header.Set("Cookie", strings.TrimSpace(session.Cookie))
	if ua := strings.TrimSpace(session.UserAgent); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &ChatGPTError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var sessions []struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(body, &sessions); err != nil {
		return "", fmt.Errorf("invalid ChatGPT session response: %w", err)
	}
	for _, entry := range sessions {
		if token := strings.TrimSpace(entry.AccessToken); token != "" {
			return token, nil
		}
	}
	return "", errors.New("ChatGPT session response did not include an access token (session may be invalid)")
}

func ChatGPTHeaders(session StoredWebSession, accessToken string) map[string]string {
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	if strings.TrimSpace(session.UserAgent) != "" {
		userAgent = session.UserAgent
	}
	origin := chatGPTWebBaseURL
	if strings.TrimSpace(session.Origin) != "" {
		origin = strings.TrimSpace(session.Origin)
	}
	headers := map[string]string{
		"Accept":        "text/event-stream",
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + accessToken,
		"Origin":        origin,
		"Referer":       origin + "/",
		"User-Agent":    userAgent,
		"oai-language":  strings.TrimSpace(os.Getenv("CHATGPT_WEB_LANGUAGE")),
	}
	if strings.TrimSpace(headers["oai-language"]) == "" {
		delete(headers, "oai-language")
	}
	if org := strings.TrimSpace(
		os.Getenv("CHATGPT_WEB_ORGANIZATION")); org != "" {
		headers["OpenAI-Organization"] = org
	}
	for _, key := range []string{"accept-language", "x-openai-source", "openai-intent"} {
		if value := strings.TrimSpace(session.Headers[key]); value != "" {
			headers[key] = value
		}
	}
	return headers
}

func buildChatGPTConversationBody(model string, messages []models.Message) (map[string]interface{}, error) {
	var apiMessages []map[string]interface{}
	var systemParts []string
	// Streams consumed: last assistant content + reasoning so reasoning models
	// restart correctly, plus any pending tool calls.
	var lastAssistant map[string]interface{}
	for _, message := range messages {
		text := ExtractText(message.Content, false)
		switch message.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			apiMessages = append(apiMessages, map[string]interface{}{
				"role": "user", "content": text, "author": map[string]interface{}{"role": "user"},
			})
		case "assistant":
			apiMessages = append(apiMessages, map[string]interface{}{
				"role": "assistant", "content": text, "author": map[string]interface{}{"role": "assistant"},
			})
			if len(apiMessages) > 0 {
				lastAssistant = apiMessages[len(apiMessages)-1]
			}
		case "tool", "function":
			apiMessages = append(apiMessages, map[string]interface{}{
				"role": "system", "content": "Tool result: " + text, "author": map[string]interface{}{"role": "system"},
			})
		default:
			return nil, fmt.Errorf("ChatGPT Web does not support message role %s", message.Role)
		}
	}
	_ = lastAssistant

	if len(apiMessages) == 0 {
		return nil, errors.New("ChatGPT requires at least one user message")
	}
	if len(systemParts) > 0 {
		constraint := " (System instructions are embedded above and must be followed.)"
		apiMessages[0]["content"] = strings.Join(systemParts, "\n\n") + "\n\n" + apiMessages[0]["content"].(string) + constraint
	}
	return map[string]interface{}{
		"action":                        "next",
		"messages":                      apiMessages,
		"model":                         model,
		"parent_message_id":             "",
		"conversation_id":               "",
		"sidebar_conversation_id":       nil,
		"stream":                        true,
		"timezone_offset_min":           -240,
		"history_and_training_disabled": true,
	}, nil
}

func ChatGPTWebChat(session StoredWebSession, model string, messages []models.Message) (models.DeepSeekChatResult, error) {
	if strings.TrimSpace(model) == "" {
		return models.DeepSeekChatResult{}, errors.New("ChatGPT requires a non-empty model")
	}
	accessToken, err := ChatGPTAccessToken(session)
	if err != nil {
		return models.DeepSeekChatResult{}, err
	}
	payload, err := buildChatGPTConversationBody(model, messages)
	if err != nil {
		return models.DeepSeekChatResult{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return models.DeepSeekChatResult{}, err
	}
	headers := ChatGPTHeaders(session, accessToken)

	if ChatGPTBrowserRelayAvailable() {
		resp, relayErr := ChatGPTBrowserRelayRequest(session, http.MethodPost, chatGPTWebBaseURL+"/backend-api/conversation", raw, headers)
		if relayErr == nil {
			return chatGPTParseRelayResponse(resp)
		}
		// The relay was present but could not complete the request (e.g. the
		// authenticated tab is not ready). Fall through to direct HTTP so the
		// request is still attempted when no security challenge is present.
		return chatGPTExecuteDirect(session, raw, headers)
	}

	return chatGPTExecuteDirect(session, raw, headers)
}

func chatGPTExecuteDirect(session StoredWebSession, raw []byte, headers map[string]string) (models.DeepSeekChatResult, error) {
	req, err := http.NewRequest(http.MethodPost, chatGPTWebBaseURL+"/backend-api/conversation", bytes.NewReader(raw))
	if err != nil {
		return models.DeepSeekChatResult{}, err
	}
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	req.Header.Set("Cookie", strings.TrimSpace(session.Cookie))
	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return models.DeepSeekChatResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return models.DeepSeekChatResult{}, &ChatGPTError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return parseChatGPTStream(resp.Body)
}

func chatGPTParseRelayResponse(resp *http.Response) (models.DeepSeekChatResult, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return models.DeepSeekChatResult{}, &ChatGPTError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return parseChatGPTStream(resp.Body)
}

func parseChatGPTStream(reader io.Reader) (models.DeepSeekChatResult, error) {
	var result models.DeepSeekChatResult
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	sawContent := false
	sawEvent := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			MessageType string `json:"message_type"`
			Content     struct {
				MessageType string `json:"message_type"`
				Text        string `json:"text"`
				Parts       []struct {
					ContentType string `json:"content_type"`
					Text        string `json:"text"`
					Step        string `json:"step"`
				} `json:"parts"`
			} `json:"content"`
			Error interface{} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		sawEvent = true
		if event.Error != nil {
			return models.DeepSeekChatResult{}, fmt.Errorf("ChatGPT stream error: %v", event.Error)
		}
		messageType := strings.ToLower(event.MessageType)
		if event.Content.MessageType != "" {
			messageType = strings.ToLower(event.Content.MessageType)
		}
		switch messageType {
		case "content":
			text := event.Content.Text
			if strings.TrimSpace(text) == "" {
				for _, part := range event.Content.Parts {
					if part.ContentType == "text" && strings.TrimSpace(part.Text) != "" {
						text += part.Text
					}
				}
			}
			if strings.TrimSpace(text) != "" {
				result.Content += text
				sawContent = true
			}
		case "reasoning", "analysis", "thinking", "computer_use.chat_context":
			text := event.Content.Text
			if strings.TrimSpace(text) == "" {
				for _, part := range event.Content.Parts {
					if strings.TrimSpace(part.Text) != "" {
						text += part.Text
					}
				}
			}
			if strings.TrimSpace(text) != "" {
				result.ReasoningText += text
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return models.DeepSeekChatResult{}, err
	}
	if !sawEvent {
		return models.DeepSeekChatResult{}, errors.New("ChatGPT returned an empty stream")
	}
	result.Usage.CompletionTokens = len(result.Content+result.ReasoningText) / 4
	_ = sawContent
	return result, nil
}
