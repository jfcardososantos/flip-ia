package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"flip-ai/internal/models"
	"flip-ai/internal/utils"
)

// ProviderAdapter defines the interface for different provider adapters.
type ProviderAdapter interface {
	ChatCompletion(req *models.ChatRequest, auth *models.StoredAuth) (*models.ChatResponse, error)
	SupportsModel(model string) bool
	Name() string
}

// ProviderRouter routes requests to the appropriate provider adapter.
type ProviderRouter struct {
	adapters []ProviderAdapter
	logger   *slog.Logger
}

// NewProviderRouter creates a new provider router.
func NewProviderRouter(logger *slog.Logger) *ProviderRouter {
	return &ProviderRouter{
		adapters: []ProviderAdapter{},
		logger:   logger,
	}
}

// Register adds a provider adapter to the router.
func (r *ProviderRouter) Register(adapter ProviderAdapter) {
	r.adapters = append(r.adapters, adapter)
	r.logger.Info("registered provider adapter", "provider", adapter.Name())
}

// Route finds the appropriate adapter for the given model.
func (r *ProviderRouter) Route(model string) (*RouteResult, error) {
	for _, adapter := range r.adapters {
		if adapter.SupportsModel(model) {
			return &RouteResult{
				Provider: adapter.Name(),
				Adapter:  adapter,
			}, nil
		}
	}
	return nil, fmt.Errorf("no provider supports model: %s", model)
}

// RouteResult contains the routing information.
type RouteResult struct {
	Provider string
	Adapter  ProviderAdapter
}

// XiaomiAdapter handles Xiaomi Mimo API calls.
type XiaomiAdapter struct {
	logger *slog.Logger
	client *http.Client
}

// NewXiaomiAdapter creates a new Xiaomi adapter.
func NewXiaomiAdapter(logger *slog.Logger) *XiaomiAdapter {
	return &XiaomiAdapter{
		logger: logger,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the provider name.
func (a *XiaomiAdapter) Name() string { return "xiaomi" }

// SupportsModel checks if the model is supported.
func (a *XiaomiAdapter) SupportsModel(model string) bool {
	return strings.HasPrefix(model, "mimo-")
}

// ChatCompletion handles chat completion requests.
func (a *XiaomiAdapter) ChatCompletion(req *models.ChatRequest, auth *models.StoredAuth) (*models.ChatResponse, error) {
	if auth.XiaomiCookie == "" && auth.ServiceToken == "" {
		return nil, fmt.Errorf("xiaomi authentication required")
	}

	// Extract the system prompt and conversation
	var userPrompt string
	var systemPrompt string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
		} else if msg.Role == "user" {
			if userPrompt != "" {
				userPrompt += "\n\n" + msg.Content
			} else {
				userPrompt = msg.Content
			}
		}
	}

	if userPrompt == "" {
		return nil, fmt.Errorf("no user message found")
	}

	// Build Mimo request
	payload := models.MimoPayload{
		MsgID:          utils.GenerateID("msg"),
		ConversationID: utils.GenerateID("conv"),
		Query:          fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt),
		IsEditedQuery:  false,
		ModelConfig: models.ModelConfig{
			EnableThinking: !strings.Contains(req.Model, "no-thinking"),
			WebSearchStatus: func() string {
				if req.WebSearch {
					return "enabled"
				}
				return "disabled"
			}(),
			Model: req.Model,
		},
	}

	// Call Mimo API
	resp, err := a.callMimoAPI(payload, auth)
	if err != nil {
		return nil, fmt.Errorf("xiaomi API error: %w", err)
	}

	// Convert to OpenAI response
	return &models.ChatResponse{
		ID:      utils.GenerateID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []models.OpenAIChoice{
			{
				Index: 0,
				Message: models.OpenAIMessage{
					Role:    "assistant",
					Content: resp.Message,
				},
				FinishReason: "stop",
			},
		},
		Usage: models.OpenAIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

func (a *XiaomiAdapter) callMimoAPI(payload models.MimoPayload, auth *models.StoredAuth) (*models.MimoResponse, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://aistudio.xiaomimimo.com/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if auth.XiaomiCookie != "" {
		req.Header.Set("Cookie", auth.XiaomiCookie)
	}
	if auth.ServiceToken != "" {
		req.Header.Set("Authorization", "Bearer "+auth.ServiceToken)
	}
	if auth.UserID != "" {
		req.Header.Set("X-User-ID", auth.UserID)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xiaomi API returned %d: %s", resp.StatusCode, string(body))
	}

	var mimoResp models.MimoResponse
	if err := json.Unmarshal(body, &mimoResp); err != nil {
		return nil, err
	}

	return &mimoResp, nil
}

// OpenRouterAdapter handles OpenRouter API calls.
type OpenRouterAdapter struct {
	logger *slog.Logger
	client *http.Client
}

// NewOpenRouterAdapter creates a new OpenRouter adapter.
func NewOpenRouterAdapter(logger *slog.Logger) *OpenRouterAdapter {
	return &OpenRouterAdapter{
		logger: logger,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the provider name.
func (a *OpenRouterAdapter) Name() string { return "openrouter" }

// SupportsModel checks if the model is supported.
func (a *OpenRouterAdapter) SupportsModel(model string) bool {
	return !strings.HasPrefix(model, "mimo-")
}

// ChatCompletion handles chat completion requests.
func (a *OpenRouterAdapter) ChatCompletion(req *models.ChatRequest, auth *models.StoredAuth) (*models.ChatResponse, error) {
	if auth.OpenRouterAPIKey == "" {
		return nil, fmt.Errorf("OpenRouter API key required")
	}

	// Build OpenRouter request
	orReq := map[string]interface{}{
		"model":      req.Model,
		"messages":   req.Messages,
		"stream":     req.Stream,
		"temperature": req.Temperature,
		"max_tokens": req.MaxTokens,
		"top_p":      req.TopP,
	}

	if len(req.Tools) > 0 {
		orReq["tools"] = req.Tools
		orReq["tool_choice"] = req.ToolChoice
		if req.ParallelToolCalls != nil {
			orReq["parallel_tool_calls"] = *req.ParallelToolCalls
		}
	}

	jsonData, err := json.Marshal(orReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+auth.OpenRouterAPIKey)
	if auth.OpenRouterHTTPReferer != "" {
		httpReq.Header.Set("HTTP-Referer", auth.OpenRouterHTTPReferer)
	}
	if auth.OpenRouterAppTitle != "" {
		httpReq.Header.Set("X-Title", auth.OpenRouterAppTitle)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter returned %d: %s", resp.StatusCode, string(body))
	}

	var orResp models.ChatResponse
	if err := json.Unmarshal(body, &orResp); err != nil {
		return nil, err
	}

	return &orResp, nil
}

// OfficialProviderModels returns the list of official provider models.
func OfficialProviderModels() []map[string]string {
	return []map[string]string{
		{"id": "mimo-v2.5-pro", "description": "Xiaomi Mimo 2.5 Pro"},
		{"id": "mimo-v2.5-pro-no-thinking", "description": "Xiaomi Mimo 2.5 Pro (no thinking)"},
		{"id": "gpt-4o", "description": "OpenAI GPT-4o"},
		{"id": "gpt-4o-mini", "description": "OpenAI GPT-4o Mini"},
		{"id": "claude-3.5-sonnet", "description": "Anthropic Claude 3.5 Sonnet"},
		{"id": "claude-3.5-haiku", "description": "Anthropic Claude 3.5 Haiku"},
		{"id": "gemini-2.0-flash", "description": "Google Gemini 2.0 Flash"},
		{"id": "gemini-2.0-pro", "description": "Google Gemini 2.0 Pro"},
		{"id": "llama-3.3-70b", "description": "Meta Llama 3.3 70B"},
		{"id": "llama-3.3-8b", "description": "Meta Llama 3.3 8B"},
		{"id": "deepseek-v3", "description": "DeepSeek V3"},
		{"id": "deepseek-r1", "description": "DeepSeek R1"},
		{"id": "mistral-large", "description": "Mistral Large"},
		{"id": "mistral-medium", "description": "Mistral Medium"},
	}
}
