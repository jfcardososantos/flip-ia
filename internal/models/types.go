package models

import "time"

// StoredAuth represents the authentication data persisted in data/auth.json.
type StoredAuth struct {
	XiaomiCookie          string                       `json:"xiaomiCookie,omitempty"`
	ServiceToken          string                       `json:"serviceToken,omitempty"`
	UserID                string                       `json:"userId,omitempty"`
	XiaomiChatbot         string                       `json:"xiaomichatbotPh,omitempty"`
	DeepSeekCookie        string                       `json:"deepseekCookie,omitempty"`
	DeepSeekToken         string                       `json:"deepseekToken,omitempty"`
	GeminiAPIKey          string                       `json:"geminiApiKey,omitempty"`
	GroqAPIKey            string                       `json:"groqApiKey,omitempty"`
	OpenRouterAPIKey      string                       `json:"openrouterApiKey,omitempty"`
	OpenRouterHTTPReferer string                       `json:"openrouterHttpReferer,omitempty"`
	OpenRouterAppTitle    string                       `json:"openrouterAppTitle,omitempty"`
	CloudflareAPIKey      string                       `json:"cloudflareApiKey,omitempty"`
	CloudflareAccountID   string                       `json:"cloudflareAccountId,omitempty"`
	DefaultModel          string                       `json:"defaultModel,omitempty"`
	RequestAPIKey         string                       `json:"requestApiKey,omitempty"`
	WebSessions           map[string]StoredWebSession `json:"webSessions,omitempty"`
}

// StoredWebSession represents a web session for a specific provider.
type StoredWebSession struct {
	Provider string            `json:"provider"`
	Cookie   string            `json:"cookie,omitempty"`
	Token    string            `json:"token,omitempty"`
	Storage  map[string]string `json:"storage,omitempty"`
	Source   string            `json:"source,omitempty"`
}

// UsageStats tracks API usage metrics.
type UsageStats struct {
	TotalRequests int
	ChatRequests  int
	LastRequestAt time.Time
	StatusCounts  map[int]int
}

// ModelInfo represents a model in the catalog.
type ModelInfo struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Description string `json:"description,omitempty"`
}

// ChatRequest represents the OpenAI-compatible chat completion request.
type ChatRequest struct {
	Model             string          `json:"model"`
	Messages          []OpenAIMessage `json:"messages"`
	Stream            bool            `json:"stream,omitempty"`
	User              string          `json:"user,omitempty"`
	Tools             []OpenAITool    `json:"tools,omitempty"`
	ToolChoice        interface{}     `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	WebSearch         bool            `json:"web_search,omitempty"`
	Temperature       float64         `json:"temperature,omitempty"`
	MaxTokens         int             `json:"max_tokens,omitempty"`
	TopP              float64         `json:"top_p,omitempty"`
	N                 int             `json:"n,omitempty"`
	Stop              []string        `json:"stop,omitempty"`
	PresencePenalty   float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty  float64         `json:"frequency_penalty,omitempty"`
	LogitBias         map[string]int  `json:"logit_bias,omitempty"`
}

// OpenAIMessage represents a chat message.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// OpenAITool represents a function tool definition.
type OpenAITool struct {
	Type     string          `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction represents a function definition for tool calling.
type OpenAIFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ChatResponse represents the OpenAI-compatible chat completion response.
type ChatResponse struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage     `json:"usage"`
}

// OpenAIChoice represents a response choice.
type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

// OpenAIUsage represents token usage statistics.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status     string `json:"status"`
	Uptime     string `json:"uptime"`
	Version    string `json:"version"`
	AuthLoaded bool   `json:"auth_loaded"`
}
