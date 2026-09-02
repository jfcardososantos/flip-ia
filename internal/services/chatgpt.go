package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

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
		if current := strings.TrimSpace(CurrentModelCatalog().Providers["chatgpt"].DefaultModel); current != "" {
			return current, true
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

type chatGPTWebModelInfo struct {
	Slug        string
	Title       string
	Description string
	MaxTokens   int
	Default     bool
}

func fetchChatGPTWebModels(ctx context.Context) ([]chatGPTWebModelInfo, string, error) {
	session, err := GetSelectedChatGPTSession()
	if err != nil {
		return nil, "", err
	}
	accessToken, err := ChatGPTAccessToken(session)
	if err != nil {
		return nil, "", err
	}
	endpoint := strings.TrimSpace(os.Getenv("CHATGPT_MODELS_URL"))
	if endpoint == "" {
		endpoint = chatGPTWebBaseURL + "/backend-api/models?history_and_training_disabled=false"
	}
	headers := ChatGPTHeaders(session, accessToken)
	headers["Accept"] = "application/json"

	var response *http.Response
	if ChatGPTBrowserRelayAvailable() {
		response, err = ChatGPTBrowserRelayRequestContext(ctx, session, http.MethodGet, endpoint, nil, headers)
	}
	if response == nil && ctx.Err() == nil {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if requestErr != nil {
			return nil, endpoint, requestErr
		}
		for key, value := range headers {
			if value != "" {
				request.Header.Set(key, value)
			}
		}
		request.Header.Set("Cookie", strings.TrimSpace(session.Cookie))
		response, err = GlobalHTTPClient.Do(request)
	}
	if err != nil {
		return nil, endpoint, err
	}
	if response == nil {
		return nil, endpoint, errors.New("ChatGPT model discovery returned no response")
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if readErr != nil {
		return nil, endpoint, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, endpoint, &ChatGPTError{StatusCode: response.StatusCode, Body: string(body)}
	}
	models, parseErr := parseChatGPTWebModels(body)
	return models, endpoint, parseErr
}

func parseChatGPTWebModels(body []byte) ([]chatGPTWebModelInfo, error) {
	var envelope struct {
		DefaultModelSlug string `json:"default_model_slug"`
		DefaultModel     string `json:"default_model"`
		Models           []struct {
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			MaxTokens   int      `json:"max_tokens"`
			Tags        []string `json:"tags"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid ChatGPT model response: %w", err)
	}
	defaultModel := firstNonEmpty(envelope.DefaultModelSlug, envelope.DefaultModel)
	seen := make(map[string]bool)
	models := make([]chatGPTWebModelInfo, 0, len(envelope.Models))
	for _, item := range envelope.Models {
		slug := strings.TrimSpace(item.Slug)
		if slug == "" || seen[slug] || strings.ContainsAny(slug, " \t\r\n?#/") || strings.Contains(slug, "..") {
			continue
		}
		seen[slug] = true
		isDefault := slug == defaultModel || containsString(item.Tags, "default")
		models = append(models, chatGPTWebModelInfo{
			Slug: slug, Title: strings.TrimSpace(item.Title), Description: strings.TrimSpace(item.Description),
			MaxTokens: item.MaxTokens, Default: isDefault,
		})
	}
	if len(models) == 0 {
		return nil, errors.New("ChatGPT Web returned an empty model list")
	}
	if defaultModel == "" {
		models[0].Default = true
	}
	return models, nil
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

func IsChatGPTConversationError(err error) bool {
	var chatErr *ChatGPTError
	if !errors.As(err, &chatErr) {
		return false
	}
	body := strings.ToLower(chatErr.Body)
	return chatErr.StatusCode == http.StatusBadRequest ||
		chatErr.StatusCode == http.StatusNotFound ||
		chatErr.StatusCode == http.StatusConflict ||
		strings.Contains(body, "conversation") ||
		strings.Contains(body, "parent_message") ||
		strings.Contains(body, "parent message")
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
	type chatGPTAuthSession struct {
		AccessToken string `json:"accessToken"`
	}
	var sessions []chatGPTAuthSession
	if len(bytes.TrimSpace(body)) > 0 && bytes.TrimSpace(body)[0] == '[' {
		if err := json.Unmarshal(body, &sessions); err != nil {
			return "", fmt.Errorf("invalid ChatGPT session response: %w", err)
		}
	} else {
		var session chatGPTAuthSession
		if err := json.Unmarshal(body, &session); err != nil {
			return "", fmt.Errorf("invalid ChatGPT session response: %w", err)
		}
		sessions = append(sessions, session)
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

func chatGPTPrompt(messages []models.Message) (string, error) {
	var systemParts []string
	var turns []string
	for _, message := range messages {
		text := strings.TrimSpace(ExtractText(message.Content, false))
		switch message.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			if text != "" {
				turns = append(turns, "User:\n"+text)
			}
		case "assistant":
			if len(message.ToolCalls) > 0 {
				raw, _ := json.Marshal(message.ToolCalls)
				turns = append(turns, "Assistant tool call:\n"+string(raw))
			} else if text != "" {
				turns = append(turns, "Assistant:\n"+text)
			}
		case "tool", "function":
			if text != "" {
				turns = append(turns, "Tool result:\n"+text)
			}
		default:
			return "", fmt.Errorf("ChatGPT Web does not support message role %s", message.Role)
		}
	}
	parts := make([]string, 0, len(systemParts)+len(turns))
	if len(systemParts) > 0 {
		parts = append(parts, strings.Join(systemParts, "\n\n"))
	}
	parts = append(parts, turns...)
	prompt := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if prompt == "" {
		return "", errors.New("ChatGPT requires at least one non-empty message")
	}
	return prompt, nil
}

func buildChatGPTConversationBody(model string, state WebChatState, messages []models.Message) (map[string]interface{}, error) {
	prompt, err := chatGPTPrompt(messages)
	if err != nil {
		return nil, err
	}
	parentMessageID := strings.TrimSpace(state.ParentMessageID)
	if parentMessageID == "" {
		parentMessageID = qwenID()
	}
	var conversationID interface{}
	if strings.TrimSpace(state.ChatID) != "" {
		conversationID = strings.TrimSpace(state.ChatID)
	}
	_, timezoneOffsetSeconds := time.Now().Zone()
	return map[string]interface{}{
		"action": "next",
		"messages": []map[string]interface{}{{
			"id":          qwenID(),
			"author":      map[string]interface{}{"role": "user"},
			"create_time": float64(time.Now().UnixMilli()) / 1000,
			"content":     map[string]interface{}{"content_type": "text", "parts": []string{prompt}},
			"metadata":    map[string]interface{}{},
		}},
		"model":                   model,
		"parent_message_id":       parentMessageID,
		"conversation_id":         conversationID,
		"sidebar_conversation_id": conversationID,
		"stream":                  true,
		"timezone_offset_min":     -timezoneOffsetSeconds / 60,
	}, nil
}

func ChatGPTWebChat(session StoredWebSession, model string, state WebChatState, messages []models.Message) (models.DeepSeekChatResult, WebChatState, error) {
	if strings.TrimSpace(model) == "" {
		return models.DeepSeekChatResult{}, state, errors.New("ChatGPT requires a non-empty model")
	}
	accessToken, err := ChatGPTAccessToken(session)
	if err != nil {
		return models.DeepSeekChatResult{}, state, err
	}
	payload, err := buildChatGPTConversationBody(model, state, messages)
	if err != nil {
		return models.DeepSeekChatResult{}, state, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return models.DeepSeekChatResult{}, state, err
	}
	headers := ChatGPTHeaders(session, accessToken)

	var result models.DeepSeekChatResult
	if ChatGPTBrowserRelayAvailable() {
		resp, relayErr := ChatGPTBrowserRelayRequest(session, http.MethodPost, chatGPTWebBaseURL+"/backend-api/conversation", raw, headers)
		if relayErr == nil {
			result, err = chatGPTParseRelayResponse(resp)
		} else {
			result, err = chatGPTExecuteDirect(session, raw, headers)
		}
	} else {
		result, err = chatGPTExecuteDirect(session, raw, headers)
	}
	if err != nil {
		return models.DeepSeekChatResult{}, state, err
	}
	if result.ConversationID != "" {
		state.ChatID = result.ConversationID
	}
	if result.MessageID != "" {
		state.ParentMessageID = result.MessageID
	}
	state.Model = model
	return result, state, nil
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
		// A Cloudflare mitigation (challenge/block) must be surfaced so the
		// caller can route the request through the authenticated browser relay
		// (which solves the captcha) instead of the direct HTTP transport.
		if isChatGPTCloudflareChallenge(resp.Header, string(body)) {
			body = []byte("Cloudflare blocked the direct HTTP request" + cloudflareChallengeDetail(resp.Header, string(body)) +
				"; open chatgpt.com in Chrome and use the relay, or solve the captcha and reimport the session. Original: " + string(body))
		}
		return models.DeepSeekChatResult{}, &ChatGPTError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return parseChatGPTStream(resp.Body)
}

func isChatGPTCloudflareChallenge(header http.Header, body string) bool {
	mitigated := strings.ToLower(strings.TrimSpace(header.Get("Cf-Mitigated")))
	if mitigated == "challenge" || strings.Contains(mitigated, "challenge") {
		return true
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "cloudflare") && (strings.Contains(lower, "challenge") || strings.Contains(lower, "captcha") || strings.Contains(lower, "turnstile") || strings.Contains(lower, "blocked") || strings.Contains(lower, "verify you are human")) {
		return true
	}
	for _, marker := range []string{"you have been blocked", "verify you are human", "cf-chl-", "turnstile", "access denied", "blocked by cloudflare"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func cloudflareChallengeDetail(header http.Header, body string) string {
	var detail []string
	if ray := strings.TrimSpace(header.Get("Cf-Ray")); ray != "" {
		detail = append(detail, " (cf-ray "+ray+")")
	}
	if strings.Contains(strings.ToLower(body), "turnstile") {
		detail = append(detail, " [turnstile]")
	}
	return strings.Join(detail, " ")
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
			Type           string `json:"type"`
			MessageType    string `json:"message_type"`
			ConversationID string `json:"conversation_id"`
			Message        *struct {
				ID     string `json:"id"`
				Author struct {
					Role string `json:"role"`
				} `json:"author"`
				Content struct {
					ContentType string        `json:"content_type"`
					Text        string        `json:"text"`
					Parts       []interface{} `json:"parts"`
				} `json:"content"`
			} `json:"message"`
			Content struct {
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
		if event.ConversationID != "" {
			result.ConversationID = event.ConversationID
		}
		if event.Message != nil && strings.EqualFold(event.Message.Author.Role, "assistant") {
			text := chatGPTContentText(event.Message.Content.Text, event.Message.Content.Parts)
			contentType := strings.ToLower(event.Message.Content.ContentType)
			if event.Message.ID != "" {
				result.MessageID = event.Message.ID
			}
			if text != "" {
				if strings.Contains(contentType, "thought") || strings.Contains(contentType, "reason") {
					result.ReasoningText = text
				} else {
					// Native conversation events contain the full message-so-far, not a delta.
					result.Content = text
					sawContent = true
				}
			}
			continue
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
	if !sawContent && strings.TrimSpace(result.ReasoningText) == "" {
		return models.DeepSeekChatResult{}, errors.New("ChatGPT stream ended without assistant content")
	}
	result.Usage.CompletionTokens = len(result.Content+result.ReasoningText) / 4
	return result, nil
}

func chatGPTContentText(text string, parts []interface{}) string {
	if strings.TrimSpace(text) != "" {
		return text
	}
	var out strings.Builder
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			out.WriteString(value)
		case map[string]interface{}:
			if candidate, ok := value["text"].(string); ok {
				out.WriteString(candidate)
			} else if candidate, ok := value["content"].(string); ok {
				out.WriteString(candidate)
			}
		}
	}
	return out.String()
}
