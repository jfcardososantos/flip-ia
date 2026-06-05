package models

type OllamaToolFunction struct {
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	Arguments   interface{} `json:"arguments,omitempty"`
	Index       int         `json:"index,omitempty"`
}

type OllamaToolCall struct {
	Type     string             `json:"type,omitempty"`
	Function OllamaToolFunction `json:"function"`
}

type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	Images    []string         `json:"images,omitempty"`
}

type OllamaChatRequest struct {
	Model     string         `json:"model"`
	Messages  []OllamaMessage `json:"messages"`
	Tools     []Tool         `json:"tools"`
	Format    interface{}    `json:"format"`
	Options   interface{}    `json:"options"`
	Stream    *bool          `json:"stream"`
	Think     interface{}    `json:"think"`
	KeepAlive interface{}    `json:"keep_alive"`
	Logprobs  bool           `json:"logprobs"`
}

type OllamaGenerateRequest struct {
	Model     string      `json:"model"`
	Prompt    string      `json:"prompt"`
	Suffix    string      `json:"suffix"`
	System    string      `json:"system"`
	Format    interface{} `json:"format"`
	Options   interface{} `json:"options"`
	Stream    *bool       `json:"stream"`
	Think     interface{} `json:"think"`
	Raw       bool        `json:"raw"`
	KeepAlive interface{} `json:"keep_alive"`
	Logprobs  bool        `json:"logprobs"`
}

type OllamaChatResponse struct {
	Model              string         `json:"model"`
	CreatedAt          string         `json:"created_at"`
	Message            OllamaMessage  `json:"message"`
	Done               bool           `json:"done"`
	DoneReason         string         `json:"done_reason,omitempty"`
	TotalDuration      int64          `json:"total_duration,omitempty"`
	LoadDuration       int64          `json:"load_duration,omitempty"`
	PromptEvalCount    int            `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64          `json:"prompt_eval_duration,omitempty"`
	EvalCount          int            `json:"eval_count,omitempty"`
	EvalDuration       int64          `json:"eval_duration,omitempty"`
}

type OllamaGenerateResponse struct {
	Model              string `json:"model"`
	CreatedAt          string `json:"created_at"`
	Response           string `json:"response,omitempty"`
	Thinking           string `json:"thinking,omitempty"`
	Done               bool   `json:"done"`
	DoneReason         string `json:"done_reason,omitempty"`
	TotalDuration      int64  `json:"total_duration,omitempty"`
	LoadDuration       int64  `json:"load_duration,omitempty"`
	PromptEvalCount    int    `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64  `json:"prompt_eval_duration,omitempty"`
	EvalCount          int    `json:"eval_count,omitempty"`
	EvalDuration       int64  `json:"eval_duration,omitempty"`
}
