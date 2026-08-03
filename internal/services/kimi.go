package services

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"flip-ai/internal/models"
)

const kimiWebBaseURL = "https://www.kimi.com"
const kimiWebChatURL = kimiWebBaseURL + "/apiv2/kimi.gateway.chat.v1.ChatService/Chat"

type KimiChatResult struct {
	Content       string
	ReasoningText string
	ActualModel   string
}

var ErrKimiEmptyResponse = errors.New("Kimi stream ended without response content")
var ErrKimiOverloaded = errors.New("Kimi completion overloaded")
var ErrKimiResourceExhausted = errors.New("Kimi resource quota exhausted")

type kimiWebModelConfig struct {
	Scenario        string
	KimiPlusID      string
	ReasoningEffort string
	ContextLength   string
}

func resolveKimiWebModel(model string) (kimiWebModelConfig, bool) {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "kimi-k3", "kimi/k3", "k3":
		return kimiWebModelConfig{
			Scenario:        "SCENARIO_AUTOMATION_K3",
			ReasoningEffort: kimiK3ReasoningEffort(), ContextLength: "CONTEXT_LENGTH_L",
		}, true
	case "kimi-k2.6", "kimi/k2.6", "k2d6":
		return kimiWebModelConfig{Scenario: "SCENARIO_K2D5", ReasoningEffort: "REASONING_EFFORT_NONE"}, true
	default:
		return kimiWebModelConfig{}, false
	}
}

// Kimi Web currently defaults K3 to HIGH. MAX consumes a separately limited
// quota and starts returning resource_exhausted once that allowance is spent,
// which made the proxy fail even though the regular K3 tier remained usable.
func kimiK3ReasoningEffort() string {
	value := strings.ToUpper(strings.TrimSpace(os.Getenv("KIMI_K3_REASONING_EFFORT")))
	value = strings.TrimPrefix(value, "REASONING_EFFORT_")
	switch value {
	case "NONE", "MINIMAL", "LOW", "MEDIUM", "HIGH", "XHIGH", "MAX":
		return "REASONING_EFFORT_" + value
	default:
		return "REASONING_EFFORT_HIGH"
	}
}

func IsKimiModel(model string) bool {
	_, ok := resolveKimiWebModel(model)
	return ok
}

func GetSelectedKimiSession() (StoredWebSession, string, error) {
	session, err := GetStoredWebSession("kimi")
	if err != nil {
		return StoredWebSession{}, "", err
	}
	token := strings.TrimSpace(WebSessionToken(session))
	if token == "" {
		token = kimiTokenFromCookie(session.Cookie)
	}
	if token == "" {
		return StoredWebSession{}, "", errors.New("missing Kimi access_token from localStorage or kimi-auth cookie")
	}
	return session, token, nil
}

func kimiTokenFromCookie(rawCookie string) string {
	for _, part := range strings.Split(rawCookie, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), "kimi-auth") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func KimiChat(session StoredWebSession, accessToken, model string, messages []models.Message) (KimiChatResult, error) {
	modelConfig, ok := resolveKimiWebModel(model)
	if !ok {
		return KimiChatResult{}, fmt.Errorf("unsupported Kimi Web model %q", model)
	}
	prompt, systemPrompt, err := foldKimiMessages(messages)
	if err != nil {
		return KimiChatResult{}, err
	}
	if prompt == "" {
		return KimiChatResult{}, errors.New("Kimi requires a non-empty user message")
	}

	var lastErr error
	for _, candidate := range kimiChatCandidates(model, modelConfig) {
		body, err := buildKimiChatBody(candidate.config, prompt, systemPrompt)
		if err != nil {
			return KimiChatResult{}, err
		}
		for attempt := 0; attempt < 2; attempt++ {
			result, requestErr := sendKimiChatRequest(session, accessToken, body)
			if requestErr == nil {
				result.ActualModel = candidate.actualModel
				return result, nil
			}
			lastErr = requestErr
			if !errors.Is(requestErr, ErrKimiEmptyResponse) {
				break
			}
		}
		if !isRecoverableKimiModelError(lastErr) {
			return KimiChatResult{}, lastErr
		}
	}
	return KimiChatResult{}, lastErr
}

type kimiChatCandidate struct {
	config      kimiWebModelConfig
	actualModel string
}

func kimiChatCandidates(model string, primary kimiWebModelConfig) []kimiChatCandidate {
	if !isKimiK3Alias(model) {
		return []kimiChatCandidate{{config: primary, actualModel: "kimi-k2.6"}}
	}

	efforts := []string{primary.ReasoningEffort, "REASONING_EFFORT_HIGH", "REASONING_EFFORT_MEDIUM", "REASONING_EFFORT_LOW"}
	seen := map[string]bool{}
	candidates := make([]kimiChatCandidate, 0, len(efforts)+1)
	for _, effort := range efforts {
		if seen[effort] {
			continue
		}
		seen[effort] = true
		config := primary
		config.ReasoningEffort = effort
		candidates = append(candidates, kimiChatCandidate{config: config, actualModel: "kimi-k3"})
	}
	if kimiK3FallbackEnabled() {
		fallback, _ := resolveKimiWebModel("kimi-k2.6")
		candidates = append(candidates, kimiChatCandidate{config: fallback, actualModel: "kimi-k2.6"})
	}
	return candidates
}

func isKimiK3Alias(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "kimi-k3", "kimi/k3", "k3":
		return true
	default:
		return false
	}
}

func kimiK3FallbackEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("KIMI_K3_FALLBACK")))
	return value != "0" && value != "false" && value != "off" && value != "no"
}

func isRecoverableKimiModelError(err error) bool {
	return errors.Is(err, ErrKimiEmptyResponse) || errors.Is(err, ErrKimiOverloaded) || errors.Is(err, ErrKimiResourceExhausted)
}

func buildKimiChatBody(modelConfig kimiWebModelConfig, prompt, systemPrompt string) ([]byte, error) {
	options := map[string]interface{}{
		"thinking":         true,
		"enable_plugin":    false,
		"reasoning_effort": modelConfig.ReasoningEffort,
	}
	if modelConfig.ContextLength != "" {
		options["context_length"] = modelConfig.ContextLength
	}
	if systemPrompt != "" {
		options["system_prompt"] = systemPrompt
	}
	payload := map[string]interface{}{
		"chat_id":  "",
		"scenario": modelConfig.Scenario,
		"tools":    []interface{}{},
		"message": map[string]interface{}{
			"id": "", "parent_id": "", "children_message_ids": []interface{}{}, "role": "user",
			"blocks":   []interface{}{map[string]interface{}{"id": "", "message_id": "", "text": map[string]string{"content": prompt}}},
			"scenario": modelConfig.Scenario, "labels": []interface{}{}, "references": []interface{}{}, "is_goal": false,
		},
		"options": options, "project_id": "",
	}
	if modelConfig.KimiPlusID != "" {
		payload["kimiplus_id"] = modelConfig.KimiPlusID
	}
	return json.Marshal(payload)
}

func sendKimiChatRequest(session StoredWebSession, accessToken string, body []byte) (KimiChatResult, error) {
	req, err := http.NewRequest(http.MethodPost, kimiWebChatURL, bytes.NewReader(kimiConnectFrame(body)))
	if err != nil {
		return KimiChatResult{}, err
	}
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	if strings.TrimSpace(session.UserAgent) != "" {
		userAgent = session.UserAgent
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", kimiWebBaseURL)
	req.Header.Set("Referer", kimiWebBaseURL+"/")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Connect-Protocol-Version", "1")

	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return KimiChatResult{}, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return KimiChatResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KimiChatResult{}, fmt.Errorf("Kimi returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return parseKimiConnectStream(responseBody)
}

func kimiConnectFrame(payload []byte) []byte {
	framed := make([]byte, len(payload)+5)
	binary.BigEndian.PutUint32(framed[1:5], uint32(len(payload)))
	copy(framed[5:], payload)
	return framed
}

func parseKimiConnectStream(raw []byte) (KimiChatResult, error) {
	var result KimiChatResult
	frameShapes := make([]string, 0, 12)
	for len(raw) > 0 {
		if len(raw) < 5 {
			return KimiChatResult{}, errors.New("truncated Kimi Connect frame")
		}
		flags := raw[0]
		length := int(binary.BigEndian.Uint32(raw[1:5]))
		if length > 8*1024*1024 || len(raw) < 5+length {
			return KimiChatResult{}, errors.New("invalid Kimi Connect frame length")
		}
		payload := raw[5 : 5+length]
		raw = raw[5+length:]
		var event map[string]interface{}
		if len(payload) > 0 && json.Unmarshal(payload, &event) != nil {
			return KimiChatResult{}, errors.New("invalid Kimi Connect payload")
		}
		if flags&2 != 0 {
			if streamErr := kimiConnectEndError(event); streamErr != nil {
				return KimiChatResult{}, streamErr
			}
			if strings.TrimSpace(result.Content) == "" {
				return result, kimiEmptyResponseWithShapes(frameShapes)
			}
			return result, nil
		}
		op, _ := event["op"].(string)
		mask, _ := event["mask"].(string)
		block, _ := event["block"].(map[string]interface{})
		if len(frameShapes) < cap(frameShapes) {
			frameShapes = append(frameShapes, kimiFrameShape(op, mask, event, block))
		}
		if block == nil {
			continue
		}
		if mask == "block.exception" {
			return result, kimiBlockExceptionError(block)
		}
		if op == "set" && mask == "block.text" {
			result.Content += kimiBlockContent(block, "text")
		}
		if op == "append" && mask == "block.text.content" {
			result.Content += kimiBlockContent(block, "text")
		}
		if op == "set" && mask == "block.think" {
			result.ReasoningText += kimiBlockContent(block, "think")
		}
		if op == "append" && mask == "block.think.content" {
			result.ReasoningText += kimiBlockContent(block, "think")
		}
	}
	// Some edge/CDN paths close the HTTP body after the final content frame
	// without forwarding the optional Connect end envelope. A complete answer is
	// still usable; only a content-less close is an upstream failure.
	if strings.TrimSpace(result.Content) != "" {
		return result, nil
	}
	return result, kimiEmptyResponseWithShapes(frameShapes)
}

func kimiBlockExceptionError(block map[string]interface{}) error {
	detail := map[string]interface{}{}
	for _, key := range []string{"exception", "error"} {
		if value, exists := block[key]; exists && value != nil {
			detail[key] = value
		}
	}
	if len(detail) == 0 {
		return errors.New("Kimi K3 returned an exception block")
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return errors.New("Kimi K3 returned an unreadable exception block")
	}
	if len(encoded) > 1200 {
		encoded = encoded[:1200]
	}
	if strings.Contains(string(encoded), "REASON_COMPLETION_OVERLOADED") {
		return fmt.Errorf("%w: %s", ErrKimiOverloaded, encoded)
	}
	return fmt.Errorf("Kimi K3 exception: %s", encoded)
}

func kimiEmptyResponseWithShapes(shapes []string) error {
	if len(shapes) == 0 {
		return ErrKimiEmptyResponse
	}
	return fmt.Errorf("%w (frame shapes: %s)", ErrKimiEmptyResponse, strings.Join(shapes, "; "))
}

func kimiFrameShape(op, mask string, event, block map[string]interface{}) string {
	eventKeys := sortedKimiKeys(event)
	blockKeys := sortedKimiKeys(block)
	return fmt.Sprintf("op=%q mask=%q keys=%s block=%s", op, mask, strings.Join(eventKeys, ","), strings.Join(blockKeys, ","))
}

func sortedKimiKeys(value map[string]interface{}) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Connect end-stream envelopes may legally contain no error, null, or an empty
// error object. The current Kimi K3 endpoint uses the latter. Treat it as an
// error only when it carries an actual code/message; formatting a missing
// message produced the misleading "Kimi stream error: <nil>" failure.
func kimiConnectEndError(event map[string]interface{}) error {
	rawError, exists := event["error"]
	if !exists || rawError == nil {
		return nil
	}
	errorValue, ok := rawError.(map[string]interface{})
	if !ok {
		text := strings.TrimSpace(fmt.Sprint(rawError))
		if text == "" || text == "<nil>" {
			return nil
		}
		return fmt.Errorf("Kimi stream error: %s", text)
	}

	message := strings.TrimSpace(fmt.Sprint(errorValue["message"]))
	if message == "<nil>" {
		message = ""
	}
	code := strings.TrimSpace(fmt.Sprint(errorValue["code"]))
	if code == "<nil>" || code == "0" || strings.EqualFold(code, "ok") {
		code = ""
	}
	if message == "" && code == "" {
		return nil
	}
	if message == "" {
		message = code
	} else if code != "" {
		message = code + ": " + message
	}
	if strings.EqualFold(code, "resource_exhausted") {
		return fmt.Errorf("%w: %s", ErrKimiResourceExhausted, message)
	}
	return fmt.Errorf("Kimi stream error: %s", message)
}

func kimiBlockContent(block map[string]interface{}, kind string) string {
	item, _ := block[kind].(map[string]interface{})
	content, _ := item["content"].(string)
	return content
}

func foldKimiMessages(messages []models.Message) (string, string, error) {
	var system, conversation []string
	for _, message := range messages {
		text := ExtractText(message.Content, false)
		switch message.Role {
		case "system", "developer":
			if text != "" {
				system = append(system, text)
			}
		case "user":
			if text != "" {
				if len(conversation) == 0 {
					conversation = append(conversation, text)
				} else {
					conversation = append(conversation, "User: "+text)
				}
			}
		case "assistant":
			if text != "" {
				conversation = append(conversation, "Assistant: "+text)
			}
		case "tool", "function":
			if text != "" {
				conversation = append(conversation, "Tool result: "+text)
			}
		default:
			return "", "", fmt.Errorf("Kimi Web does not support message role %s", message.Role)
		}
	}
	return strings.Join(conversation, "\n\n"), strings.Join(system, "\n\n"), nil
}
