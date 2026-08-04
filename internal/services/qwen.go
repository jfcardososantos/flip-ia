package services

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"flip-ai/internal/models"
)

var qwenWebBaseURL = "https://chat.qwen.ai"

type qwenWebModelProfile struct {
	AutoThinking   bool
	AutoSearch     bool
	ThinkingFormat string
	SupportsUsage  bool
}

var qwenWebRuntime = struct {
	sync.RWMutex
	frontendVersion string
	modelProfiles   map[string]qwenWebModelProfile
}{
	frontendVersion: "0.2.81",
	modelProfiles:   make(map[string]qwenWebModelProfile),
}

func setQwenWebFrontendVersion(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}
	qwenWebRuntime.Lock()
	qwenWebRuntime.frontendVersion = version
	qwenWebRuntime.Unlock()
}

func currentQwenWebFrontendVersion() string {
	if configured := strings.TrimSpace(os.Getenv("QWEN_WEB_VERSION")); configured != "" {
		return configured
	}
	qwenWebRuntime.RLock()
	version := qwenWebRuntime.frontendVersion
	qwenWebRuntime.RUnlock()
	if version == "" {
		return "0.2.81"
	}
	return version
}

func setQwenWebModelProfiles(profiles map[string]qwenWebModelProfile) {
	copyProfiles := make(map[string]qwenWebModelProfile, len(profiles))
	for model, profile := range profiles {
		copyProfiles[strings.ToLower(strings.TrimSpace(model))] = profile
	}
	qwenWebRuntime.Lock()
	qwenWebRuntime.modelProfiles = copyProfiles
	qwenWebRuntime.Unlock()
}

func currentQwenWebModelProfile(model string) qwenWebModelProfile {
	qwenWebRuntime.RLock()
	profile := qwenWebRuntime.modelProfiles[strings.ToLower(strings.TrimSpace(model))]
	qwenWebRuntime.RUnlock()
	return profile
}

type QwenWebError struct {
	StatusCode int
	Body       string
}

func (e *QwenWebError) Error() string {
	return fmt.Sprintf("Qwen Web returned %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

func IsQwenWebModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "qwen-web" || strings.HasPrefix(model, "qwen-web/")
}

func ResolveQwenWebModel(model string) (string, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "qwen-web" {
		if configured := strings.TrimSpace(os.Getenv("QWEN_WEB_DEFAULT_MODEL")); configured != "" {
			return configured, true
		}
		if current := strings.TrimSpace(CurrentModelCatalog().Providers["qwen"].DefaultModel); current != "" {
			return current, true
		}
		return "qwen3.8-max", true
	}
	if strings.HasPrefix(model, "qwen-web/") {
		upstream := strings.TrimSpace(strings.TrimPrefix(model, "qwen-web/"))
		if upstream != "" && !strings.ContainsAny(upstream, " \t\r\n?#") {
			return upstream, true
		}
	}
	return "", false
}

func GetSelectedQwenSession() (StoredWebSession, error) {
	session, err := GetStoredWebSession("qwen")
	if err != nil {
		return StoredWebSession{}, err
	}
	if strings.TrimSpace(session.Cookie) == "" && strings.TrimSpace(WebSessionToken(session)) == "" {
		return StoredWebSession{}, errors.New("missing Qwen cookie jar or token")
	}
	return session, nil
}

func QwenRolloverTokenLimit() int {
	return intEnvOrDefault("QWEN_WEB_ROLLOVER_TOKENS", 850000)
}

func QwenHandoffCharLimit() int {
	return intEnvOrDefault("QWEN_WEB_HANDOFF_CHARS", 120000)
}

func QwenMessageHash(message models.Message) string {
	raw, _ := json.Marshal(message)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func IsQwenContextError(err error) bool {
	var webErr *QwenWebError
	if !errors.As(err, &webErr) {
		return false
	}
	body := strings.ToLower(webErr.Body)
	return webErr.StatusCode == http.StatusRequestEntityTooLarge ||
		strings.Contains(body, "context length") ||
		strings.Contains(body, "maximum context") ||
		strings.Contains(body, "max token") ||
		strings.Contains(body, "parent") && strings.Contains(body, "invalid")
}

func IsQwenTransientError(err error) bool {
	var webErr *QwenWebError
	if errors.As(err, &webErr) {
		return webErr.StatusCode == http.StatusRequestTimeout ||
			webErr.StatusCode == http.StatusConflict ||
			webErr.StatusCode == http.StatusTooEarly ||
			webErr.StatusCode == http.StatusTooManyRequests ||
			webErr.StatusCode >= http.StatusInternalServerError
	}
	body := strings.ToLower(err.Error())
	return strings.Contains(body, "empty stream") ||
		strings.Contains(body, "stream error") ||
		strings.Contains(body, "timeout") ||
		strings.Contains(body, "connection reset") ||
		strings.Contains(body, "unexpected eof")
}

func IsQwenAuthError(err error) bool {
	var webErr *QwenWebError
	if errors.As(err, &webErr) && (webErr.StatusCode == http.StatusUnauthorized || webErr.StatusCode == http.StatusForbidden) {
		return true
	}
	body := strings.ToLower(err.Error())
	return strings.Contains(body, "verification") || strings.Contains(body, "captcha") || strings.Contains(body, "login")
}

func QwenProxyStatus(err error) int {
	var webErr *QwenWebError
	if errors.As(err, &webErr) && webErr.StatusCode == http.StatusTooManyRequests {
		return http.StatusTooManyRequests
	}
	if IsQwenTransientError(err) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

func QwenWebChat(session StoredWebSession, upstreamModel string, state WebChatState, prompt, title string, thinking, search bool) (models.DeepSeekChatResult, WebChatState, error) {
	if strings.TrimSpace(prompt) == "" {
		return models.DeepSeekChatResult{}, state, errors.New("Qwen requires a non-empty prompt")
	}
	if state.ChatID == "" {
		chatID, err := createQwenChat(session, upstreamModel)
		if err != nil {
			return models.DeepSeekChatResult{}, state, err
		}
		state.ChatID = chatID
		state.ParentMessageID = ""
	}

	messageFID := qwenID()
	now := time.Now()
	profile := currentQwenWebModelProfile(upstreamModel)
	var parentParam interface{}
	if strings.TrimSpace(state.ParentMessageID) != "" {
		parentParam = state.ParentMessageID
	}
	featureConfig := map[string]interface{}{
		"thinking_enabled": thinking,
		"output_schema":    "phase",
		"research_mode":    "normal",
		"auto_thinking":    false,
		"thinking_mode":    "Fast",
		"auto_search":      profile.AutoSearch || search,
	}
	if thinking {
		featureConfig["thinking_mode"] = "Thinking"
		featureConfig["thinking_format"] = firstNonEmpty(profile.ThinkingFormat, "summary")
	}
	message := map[string]interface{}{
		"id":             nil,
		"fid":            messageFID,
		"parentId":       parentParam,
		"parent_id":      parentParam,
		"childrenIds":    []interface{}{},
		"role":           "user",
		"content":        prompt,
		"user_action":    "chat",
		"timestamp":      now.Unix(),
		"models":         []string{upstreamModel},
		"model":          "",
		"chat_type":      "t2t",
		"sub_chat_type":  "t2t",
		"feature_config": featureConfig,
		"extra": map[string]interface{}{
			"meta": map[string]interface{}{"subChatType": "t2t"},
		},
	}
	payload := map[string]interface{}{
		"stream":             true,
		"version":            "2.1",
		"incremental_output": true,
		"chatId":             state.ChatID,
		"chat_id":            state.ChatID,
		"chat_mode":          "normal",
		"model":              upstreamModel,
		"parentId":           state.ParentMessageID,
		"parent_id":          parentParam,
		"messages":           []interface{}{message},
		"timestamp":          now.Unix(),
	}
	if profile.SupportsUsage {
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}

	response, err := qwenRequest(session, http.MethodPost, "/api/chat/completions?chat_id="+state.ChatID, payload)
	if err != nil {
		return models.DeepSeekChatResult{}, state, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		return models.DeepSeekChatResult{}, state, &QwenWebError{StatusCode: response.StatusCode, Body: string(body)}
	}

	result, parentID, err := parseQwenStream(response.Body)
	if err != nil {
		return models.DeepSeekChatResult{}, state, err
	}
	if parentID != "" {
		state.ParentMessageID = parentID
		result.MessageID = parentID
	}
	state.ChatID = strings.TrimSpace(state.ChatID)
	state.Model = upstreamModel
	if title != "" {
		state.Title = title
	}
	_ = updateQwenChat(session, state)
	return result, state, nil
}

func createQwenChat(session StoredWebSession, model string) (string, error) {
	payload := map[string]interface{}{
		"chatId":     "",
		"models":     []string{model},
		"project_id": "",
		"timestamp":  time.Now().UnixMilli(),
		"chat_type":  "t2t",
		"chat_mode":  "normal",
	}
	response, err := qwenRequest(session, http.MethodPost, "/api/chats/new", payload)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", &QwenWebError{StatusCode: response.StatusCode, Body: string(body)}
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("invalid Qwen create-chat response: %w", err)
	}
	if id := findStringByKey(envelope, "id"); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("Qwen create-chat response did not contain an id: %s", strings.TrimSpace(string(body)))
}

func updateQwenChat(session StoredWebSession, state WebChatState) error {
	if state.ChatID == "" {
		return nil
	}
	payload := map[string]interface{}{}
	if state.Title != "" {
		payload["title"] = state.Title
	}
	if state.ParentMessageID != "" {
		payload["currentId"] = state.ParentMessageID
		payload["currentResponseIds"] = []string{state.ParentMessageID}
	}
	if len(payload) == 0 {
		return nil
	}
	response, err := qwenRequest(session, http.MethodPost, "/api/chats/"+state.ChatID, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 256<<10))
		return &QwenWebError{StatusCode: response.StatusCode, Body: string(body)}
	}
	return nil
}

func qwenRequest(session StoredWebSession, method, path string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, qwenWebBaseURL+path, body)
	if err != nil {
		return nil, err
	}
	for key, value := range qwenHeaders(session) {
		request.Header.Set(key, value)
	}
	request.Header.Set("X-Request-Id", qwenID())
	return GlobalHTTPClient.Do(request)
}

func qwenHeaders(session StoredWebSession) map[string]string {
	userAgent := strings.TrimSpace(session.UserAgent)
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	}
	origin := strings.TrimSpace(session.Origin)
	if origin == "" {
		origin = qwenWebBaseURL
	}
	referer := strings.TrimSpace(session.Referer)
	if referer == "" {
		referer = qwenWebBaseURL + "/"
	}
	headers := map[string]string{
		"Accept":       "application/json, text/event-stream",
		"Content-Type": "application/json",
		"Cookie":       strings.TrimSpace(session.Cookie),
		"Origin":       origin,
		"Referer":      referer,
		"User-Agent":   userAgent,
		"Version":      currentQwenWebFrontendVersion(),
		"source":       "web",
		"Timezone":     qwenEnvOrDefault("QWEN_WEB_TIMEZONE", time.Now().Format("Mon Jan 02 2006 15:04:05 GMT-0700")),
	}
	if token := strings.TrimSpace(WebSessionToken(session)); token != "" &&
		envBoolDefault("QWEN_WEB_USE_AUTHORIZATION", false) {
		headers["Authorization"] = "Bearer " + token
	}
	allowed := map[string]bool{
		"accept-language": true,
		"timezone":        true,
		"source":          true,
		"x-request-id":    true,
		"x-xsrf-token":    true,
		"x-csrf-token":    true,
	}
	for key, value := range session.Headers {
		if allowed[strings.ToLower(strings.TrimSpace(key))] && strings.TrimSpace(value) != "" {
			headers[key] = value
		}
	}
	return headers
}

func parseQwenStream(reader io.Reader) (models.DeepSeekChatResult, string, error) {
	var result models.DeepSeekChatResult
	var messageID string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	sawEvent := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "" || line == "[DONE]" {
			continue
		}
		var event interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		sawEvent = true
		if id := findStringByKey(event, "message_id"); id != "" {
			messageID = id
		} else if id := findStringByKey(event, "response_id"); id != "" {
			messageID = id
		}
		content, reasoning := qwenEventText(event)
		result.Content += content
		result.ReasoningText += reasoning
		if usage := findMapByKey(event, "usage"); usage != nil {
			result.Usage.PromptTokens = int(numberValue(usage["prompt_tokens"]))
			result.Usage.CompletionTokens = int(numberValue(usage["completion_tokens"]))
			result.Usage.TotalTokens = int(numberValue(usage["total_tokens"]))
		}
		if failed, detail := qwenEventFailure(event); failed {
			return models.DeepSeekChatResult{}, "", errors.New("Qwen stream error: " + detail)
		}
	}
	if err := scanner.Err(); err != nil {
		return models.DeepSeekChatResult{}, "", err
	}
	if !sawEvent {
		return models.DeepSeekChatResult{}, "", errors.New("Qwen returned an empty stream")
	}
	if messageID == "" {
		return models.DeepSeekChatResult{}, "", errors.New("Qwen stream did not provide a response_id")
	}
	if result.Usage.CompletionTokens == 0 {
		result.Usage.CompletionTokens = len(result.Content+result.ReasoningText) / 4
	}
	return result, messageID, nil
}

func qwenEventText(event interface{}) (string, string) {
	root, _ := event.(map[string]interface{})
	if root == nil {
		return "", ""
	}
	if choices, ok := root["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			for _, key := range []string{"delta", "message"} {
				if item, ok := choice[key].(map[string]interface{}); ok {
					return qwenTextFromMap(item)
				}
			}
		}
	}
	if data, ok := root["data"].(map[string]interface{}); ok {
		if choices, ok := data["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					return qwenTextFromMap(delta)
				}
			}
		}
		return qwenTextFromMap(data)
	}
	return qwenTextFromMap(root)
}

func qwenTextFromMap(item map[string]interface{}) (string, string) {
	content, _ := item["content"].(string)
	reasoning, _ := item["reasoning_content"].(string)
	if reasoning == "" {
		reasoning, _ = item["thinking_content"].(string)
	}
	phase, _ := item["phase"].(string)
	if phase == "think" || phase == "thinking" || phase == "thinking_summary" {
		if reasoning == "" {
			reasoning = content
			content = ""
		}
	}
	if delta, ok := item["delta"].(string); ok {
		if phase == "think" || phase == "thinking" || phase == "thinking_summary" {
			reasoning += delta
		} else {
			content += delta
		}
	}
	return content, reasoning
}

func qwenEventFailure(event interface{}) (bool, string) {
	root, _ := event.(map[string]interface{})
	if root == nil {
		return false, ""
	}
	if success, ok := root["success"].(bool); ok && !success {
		detail := findStringByKey(root, "details")
		if detail == "" {
			detail = findStringByKey(root, "message")
		}
		if detail == "" {
			detail = "unknown upstream error"
		}
		return true, detail
	}
	if rawError, ok := root["error"]; ok && rawError != nil {
		detail := ""
		switch value := rawError.(type) {
		case string:
			detail = value
		case map[string]interface{}:
			detail = findStringByKey(value, "details")
			if detail == "" {
				detail = findStringByKey(value, "message")
			}
			if detail == "" {
				encoded, _ := json.Marshal(value)
				detail = string(encoded)
			}
		}
		if detail == "" {
			detail = "unknown upstream error"
		}
		return true, detail
	}
	return false, ""
}

func findStringByKey(value interface{}, key string) string {
	switch item := value.(type) {
	case map[string]interface{}:
		if raw, ok := item[key].(string); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
		for _, child := range item {
			if found := findStringByKey(child, key); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, child := range item {
			if found := findStringByKey(child, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func findMapByKey(value interface{}, key string) map[string]interface{} {
	switch item := value.(type) {
	case map[string]interface{}:
		if raw, ok := item[key].(map[string]interface{}); ok {
			return raw
		}
		for _, child := range item {
			if found := findMapByKey(child, key); found != nil {
				return found
			}
		}
	case []interface{}:
		for _, child := range item {
			if found := findMapByKey(child, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func numberValue(value interface{}) float64 {
	switch item := value.(type) {
	case float64:
		return item
	case json.Number:
		number, _ := item.Float64()
		return number
	}
	return 0
}

func qwenID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func intEnvOrDefault(key string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}

func qwenEnvOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
