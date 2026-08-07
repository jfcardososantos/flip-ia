package services

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flip-ai/internal/models"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const deepSeekBaseURL = "https://chat.deepseek.com"

var ErrDeepSeekPoWRequired = errors.New("DeepSeek web requires a proof-of-work challenge response for this request")
var ErrDeepSeekEmptyResponse = errors.New("DeepSeek returned no final content after internal recovery")

func IsDeepSeekModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "deepseek" || strings.HasPrefix(model, "deepseek-")
}

// DeepSeekWebModelType maps official model families to the selector used by
// chat.deepseek.com. Newly discovered non-Pro models follow the Web default,
// while Pro models use Expert mode.
func DeepSeekWebModelType(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "-pro") || strings.Contains(model, "expert") {
		return "expert"
	}
	return "default"
}

func ValidateDeepSeekAuthInput(rawCookie string, token string) (models.DeepSeekAuth, error) {
	rawCookie = strings.TrimSpace(rawCookie)
	token = cleanEnvValue(token)

	if token == "" {
		token = extractDeepSeekUserToken(rawCookie)
	}
	if rawCookie == "" {
		return models.DeepSeekAuth{}, errors.New("missing DeepSeek cookie jar")
	}
	if token == "" {
		return models.DeepSeekAuth{}, errors.New("missing DeepSeek userToken from localStorage")
	}

	return models.DeepSeekAuth{
		Cookie: rawCookie,
		Token:  token,
	}, nil
}

func GetSelectedDeepSeekAuth() (models.DeepSeekAuth, error) {
	_, auth, err := GetSelectedDeepSeekSession()
	return auth, err
}

func GetSelectedDeepSeekSession() (StoredWebSession, models.DeepSeekAuth, error) {
	session, err := GetStoredWebSession("deepseek")
	if err != nil {
		return StoredWebSession{}, models.DeepSeekAuth{}, err
	}
	auth, err := ValidateDeepSeekAuthInput(session.Cookie, WebSessionToken(session))
	if err != nil {
		return StoredWebSession{}, models.DeepSeekAuth{}, err
	}
	return session, auth, nil
}

func DeepSeekHeaders(auth models.DeepSeekAuth, session StoredWebSession, customHeaders map[string]string) map[string]string {
	userAgent := "DeepSeek/1.0.13 Android/35"
	if strings.TrimSpace(session.UserAgent) != "" {
		userAgent = strings.TrimSpace(session.UserAgent)
	}
	origin := deepSeekBaseURL
	if strings.TrimSpace(session.Origin) != "" {
		origin = strings.TrimSpace(session.Origin)
	}
	referer := deepSeekBaseURL + "/"
	if strings.TrimSpace(session.Referer) != "" {
		referer = strings.TrimSpace(session.Referer)
	}

	headers := map[string]string{
		"accept":          "application/json",
		"accept-encoding": "gzip",
		"authorization":   "Bearer " + auth.Token,
		"content-type":    "application/json",
		"cookie":          auth.Cookie,
		"host":            "chat.deepseek.com",
		"origin":          origin,
		"referer":         referer,
		"user-agent":      userAgent,
	}

	for key, value := range session.Headers {
		headers[strings.ToLower(strings.TrimSpace(key))] = value
	}
	for _, key := range []string{"accept-language", "user-agent", "origin", "referer"} {
		if val, ok := customHeaders[key]; ok && strings.TrimSpace(val) != "" {
			headers[key] = val
		}
	}
	if pow := strings.TrimSpace(os.Getenv("DEEPSEEK_POW_RESPONSE")); pow != "" {
		headers["x-ds-pow-response"] = pow
	}

	return headers
}

func CreateDeepSeekSession(auth models.DeepSeekAuth, session StoredWebSession, customHeaders map[string]string) (string, error) {
	payloadBytes, _ := json.Marshal(map[string]string{"agent": "chat"})
	req, _ := http.NewRequest("POST", deepSeekBaseURL+"/api/v0/chat_session/create", bytes.NewBuffer(payloadBytes))
	for k, v := range DeepSeekHeaders(auth, session, customHeaders) {
		req.Header.Set(k, v)
	}

	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := readMaybeGzip(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DeepSeek session error: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BizData struct {
				ID string `json:"id"`
			} `json:"biz_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.Code != 0 || result.Data.BizData.ID == "" {
		if result.Msg == "" {
			result.Msg = string(body)
		}
		return "", fmt.Errorf("DeepSeek session business error: %d - %s", result.Code, result.Msg)
	}
	return result.Data.BizData.ID, nil
}

func SendDeepSeekChatRequest(auth models.DeepSeekAuth, session StoredWebSession, sessionID string, prompt string, thinking bool, search bool, modelType string, customHeaders map[string]string) (*http.Response, error) {
	if strings.TrimSpace(modelType) == "" {
		modelType = "default"
	}
	payload := map[string]interface{}{
		"chat_session_id":   sessionID,
		"parent_message_id": nil,
		"model_type":        modelType,
		"preempt":           false,
		"prompt":            prompt,
		"ref_file_ids":      []string{},
		"thinking_enabled":  thinking,
		"search_enabled":    search,
	}
	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", deepSeekBaseURL+"/api/v0/chat/completion", bytes.NewBuffer(payloadBytes))
	headers := DeepSeekHeaders(auth, session, customHeaders)
	if strings.TrimSpace(os.Getenv("DEEPSEEK_POW_RESPONSE")) == "" {
		powResponse, err := GetDeepSeekPoWResponse(auth, session, customHeaders)
		if err != nil {
			return nil, err
		}
		headers["x-ds-pow-response"] = powResponse
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return GlobalHTTPClient.Do(req)
}

func ParseDeepSeekStream(body io.Reader) models.DeepSeekChatResult {
	return ParseDeepSeekStreamMode(body, false)
}

// ParseDeepSeekStreamMode parses both the classic patch stream and the newer
// Expert fragment stream. Expert fragments publish their type (THINK or
// RESPONSE) once and then append content through an otherwise ambiguous
// response/fragments/-1/content path, so the active fragment has to be kept as
// stream state.
func ParseDeepSeekStreamMode(body io.Reader, thinking bool) models.DeepSeekChatResult {
	reader := bufio.NewReaderSize(body, 4*1024*1024)
	var result models.DeepSeekChatResult
	state := deepSeekStreamState{target: deepSeekContentTarget}
	if thinking {
		state.target = deepSeekReasoningTarget
	}

	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			parseDeepSeekDataWithState(strings.TrimSpace(line[5:]), &result, &state)
		}
		if err != nil {
			break
		}
	}

	if result.Usage.TotalTokens == 0 {
		result.Usage.CompletionTokens = len(result.Content+result.ReasoningText) / 4
		result.Usage.TotalTokens = result.Usage.CompletionTokens
	}
	return result
}

type deepSeekStreamTarget uint8

const (
	deepSeekContentTarget deepSeekStreamTarget = iota
	deepSeekReasoningTarget
)

type deepSeekStreamState struct {
	target deepSeekStreamTarget
}

func ReadDeepSeekBody(resp *http.Response) (io.Reader, func()) {
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err == nil {
			return gz, func() {
				_ = gz.Close()
				_ = resp.Body.Close()
			}
		}
	}
	return resp.Body, func() { _ = resp.Body.Close() }
}

// RecoverDeepSeekEmptyResponse retries a genuinely empty Web response in a
// fresh session. Expert mode gets one fresh Expert attempt and, unless
// explicitly disabled, one final default/Flash attempt. This keeps transient
// upstream failures from surfacing as successful OpenAI responses with empty
// content, which agent clients cannot distinguish from a completed turn.
func RecoverDeepSeekEmptyResponse(first models.DeepSeekChatResult, auth models.DeepSeekAuth, session StoredWebSession, prompt string, thinking bool, search bool, modelType string, customHeaders map[string]string) (models.DeepSeekChatResult, string, error) {
	if strings.TrimSpace(first.Content) != "" {
		return first, modelType, nil
	}

	type attempt struct {
		modelType string
		thinking  bool
	}
	attempts := []attempt{{modelType: modelType, thinking: thinking}}
	if modelType == "expert" && deepSeekEmptyFallbackEnabled() {
		attempts = append(attempts, attempt{modelType: "default", thinking: false})
	}

	last := first
	for _, candidate := range attempts {
		sessionID, err := CreateDeepSeekSession(auth, session, customHeaders)
		if err != nil {
			continue
		}
		resp, err := SendDeepSeekChatRequest(auth, session, sessionID, prompt, candidate.thinking, search, candidate.modelType, customHeaders)
		if err != nil || resp == nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_, closeBody := ReadDeepSeekBody(resp)
			closeBody()
			continue
		}

		bodyReader, closeBody := ReadDeepSeekBody(resp)
		last = ParseDeepSeekStreamMode(bodyReader, candidate.thinking)
		closeBody()
		if strings.TrimSpace(last.Content) != "" {
			return last, candidate.modelType, nil
		}
	}
	return last, modelType, ErrDeepSeekEmptyResponse
}

func deepSeekEmptyFallbackEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("DEEPSEEK_PRO_EMPTY_FALLBACK")))
	return value != "0" && value != "false" && value != "off" && value != "no"
}

func parseDeepSeekData(dataStr string, result *models.DeepSeekChatResult) {
	state := deepSeekStreamState{target: deepSeekContentTarget}
	parseDeepSeekDataWithState(dataStr, result, &state)
}

func parseDeepSeekDataWithState(dataStr string, result *models.DeepSeekChatResult, state *deepSeekStreamState) {
	if dataStr == "" || dataStr == "{}" || dataStr == "[DONE]" {
		return
	}

	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return
	}
	if parseDeepSeekOpenAIChunk(chunk, result) {
		return
	}

	if id, ok := chunk["response_message_id"].(string); ok && id != "" {
		result.MessageID = id
	}
	if v, ok := chunk["v"].(map[string]interface{}); ok {
		if response, ok := v["response"].(map[string]interface{}); ok {
			if id, ok := response["message_id"].(string); ok && id != "" {
				result.MessageID = id
			}
		}
	}

	path, _ := chunk["p"].(string)
	v, exists := chunk["v"]
	if !exists {
		return
	}
	lowerPath := strings.ToLower(path)
	if strings.Contains(lowerPath, "thinking") || strings.Contains(lowerPath, "reasoning") || strings.Contains(lowerPath, "analysis") || strings.Contains(lowerPath, "thought") {
		state.target = deepSeekReasoningTarget
	} else if strings.Contains(lowerPath, "response/content") && !strings.Contains(lowerPath, "fragments") {
		state.target = deepSeekContentTarget
	}

	if parseDeepSeekFragments(v, result, state) {
		return
	}

	text := deepSeekTextValue(v)
	if text == "" {
		return
	}
	cleanText := strings.TrimSpace(text)
	if cleanText == "FINISHED" || cleanText == "RESPONSE_FINISHED" || strings.Contains(lowerPath, "status") {
		return
	}
	if state.target == deepSeekReasoningTarget {
		result.ReasoningText += text
		return
	}
	result.Content += text
}

// parseDeepSeekFragments handles the current DeepSeek Web protocol. A fragment
// declaration switches the destination for all following /fragments/-1/content
// appends until the next declaration arrives.
func parseDeepSeekFragments(value interface{}, result *models.DeepSeekChatResult, state *deepSeekStreamState) bool {
	var fragments []interface{}
	switch v := value.(type) {
	case []interface{}:
		fragments = v
	case map[string]interface{}:
		response, _ := v["response"].(map[string]interface{})
		fragments, _ = response["fragments"].([]interface{})
	}
	if len(fragments) == 0 {
		return false
	}

	handled := false
	for _, rawFragment := range fragments {
		fragment, _ := rawFragment.(map[string]interface{})
		fragmentType, _ := fragment["type"].(string)
		switch strings.ToUpper(strings.TrimSpace(fragmentType)) {
		case "THINK", "THINKING", "REASONING":
			state.target = deepSeekReasoningTarget
			handled = true
		case "RESPONSE", "ANSWER":
			state.target = deepSeekContentTarget
			handled = true
		default:
			continue
		}

		text := deepSeekTextValue(fragment["content"])
		if state.target == deepSeekReasoningTarget {
			result.ReasoningText += text
		} else {
			result.Content += text
		}
	}
	return handled
}

// Expert Mode may return OpenAI-shaped SSE deltas instead of the p/v patch
// frames used by the default Web mode. Accept both so reasoning-only chunks do
// not get mistaken for empty output.
func parseDeepSeekOpenAIChunk(chunk map[string]interface{}, result *models.DeepSeekChatResult) bool {
	choices, ok := chunk["choices"].([]interface{})
	if !ok {
		return false
	}
	if id, ok := chunk["id"].(string); ok && id != "" {
		result.MessageID = id
	}
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]interface{})
		for _, field := range []string{"delta", "message"} {
			part, _ := choice[field].(map[string]interface{})
			if part == nil {
				continue
			}
			for _, key := range []string{"reasoning_content", "reasoning", "thinking_content", "analysis"} {
				if text := deepSeekTextValue(part[key]); text != "" {
					result.ReasoningText += text
				}
			}
			if text := deepSeekTextValue(part["content"]); text != "" {
				result.Content += text
			}
		}
	}
	if usage, ok := chunk["usage"].(map[string]interface{}); ok {
		result.Usage.PromptTokens = deepSeekIntValue(usage["prompt_tokens"])
		result.Usage.CompletionTokens = deepSeekIntValue(usage["completion_tokens"])
		result.Usage.TotalTokens = deepSeekIntValue(usage["total_tokens"])
	}
	return true
}

func deepSeekIntValue(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func deepSeekTextValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			text := deepSeekTextValue(item)
			if text == "" {
				continue
			}
			cleanText := strings.TrimSpace(text)
			if cleanText == "FINISHED" || cleanText == "RESPONSE_FINISHED" {
				continue
			}
			sb.WriteString(text)
		}
		return sb.String()
	case map[string]interface{}:
		for _, key := range []string{"content", "text", "delta", "reasoning_content", "reasoning", "thinking_content", "analysis"} {
			if text, ok := v[key].(string); ok {
				return text
			}
		}
		if response, ok := v["response"].(map[string]interface{}); ok {
			return deepSeekTextValue(response)
		}
	}
	return ""
}

func readMaybeGzip(resp *http.Response) ([]byte, error) {
	var body io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err == nil {
			defer gz.Close()
			body = gz
		}
	}
	return io.ReadAll(body)
}

func extractDeepSeekUserToken(raw string) string {
	if token := extractCookieValue(raw, "userToken"); token != "" {
		return token
	}
	return ""
}
