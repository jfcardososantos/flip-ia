package models

// MimoPayload represents the request payload for the Xiaomi Mimo API.
type MimoPayload struct {
	MsgID          string      `json:"msg_id"`
	ConversationID string      `json:"conversation_id"`
	Query          string      `json:"query"`
	IsEditedQuery  bool        `json:"is_edited_query"`
	ModelConfig    ModelConfig `json:"model_config"`
}

// ModelConfig represents the model configuration for Mimo requests.
type ModelConfig struct {
	EnableThinking  bool   `json:"enable_thinking"`
	WebSearchStatus string `json:"web_search_status"`
	Model           string `json:"model"`
}

// MimoResponse represents the response from the Xiaomi Mimo API.
type MimoResponse struct {
	Message string      `json:"message"`
	Usage   MimoUsage   `json:"usage"`
	Error   *string     `json:"error,omitempty"`
}

// MimoUsage represents token usage from Xiaomi Mimo.
type MimoUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
