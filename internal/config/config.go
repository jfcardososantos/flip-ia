// Package config handles loading and validation of application configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Port            string
	CORSOrigin      string
	APIKey          string
	SettingsPassword string
	DefaultModel    string
	RequestAPIKey   string
	
	// Provider keys
	GeminiAPIKey      string
	GroqAPIKey        string
	OpenRouterAPIKey  string
	OpenRouterHTTPReferer string
	OpenRouterAppTitle    string
	CloudflareAPIKey  string
	CloudflareAccountID string
	
	// Catalog settings
	CatalogRefreshOnStartup bool
	CatalogRefreshInterval  time.Duration
	CatalogTimeoutSeconds   int
	CatalogPath             string
	OpenRouterFreeOnly      bool
	DeepSeekModelsURL       string
	
	// Qwen Web settings
	QwenWebDefaultModel   string
	QwenWebRolloverTokens int
	QwenWebHandoffChars   int
	QwenWebThinking       bool
	QwenWebTimezone       string
	QwenWebVersion        string
	QwenWebUseAuthorization bool
	
	// Agent settings
	AgentEnableThinking     bool
	AgentFastMode           bool
	AgentMaxContextChars    int
	AgentMaxMessages        int
	AgentMaxToolResultChars int
	AgentSequentialTools    bool
	CodingAgentWorkspace    string
	CodingAgentToolTimeout  int
	CodingAgentMaxFileBytes int
	CodingAgentMaxToolOutputChars int
	
	// DeepSeek
	DeepSeekPoWWASMPath string
	
	// Default web search
	DefaultWebSearch bool
	
	// Auth store path
	AuthStorePath string
}

// Load loads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:            getEnv("PORT", "3000"),
		CORSOrigin:      getEnv("CORS_ORIGIN", "*"),
		APIKey:          getEnv("API_KEY", ""),
		SettingsPassword: getEnv("SETTINGS_PASSWORD", ""),
		DefaultModel:    getEnv("DEFAULT_MODEL", "mimo-v2.5-pro"),
		RequestAPIKey:   getEnv("REQUEST_API_KEY", ""),
		
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		OpenRouterAPIKey:   getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterHTTPReferer: getEnv("OPENROUTER_HTTP_REFERER", ""),
		OpenRouterAppTitle: getEnv("OPENROUTER_APP_TITLE", "flip-ai"),
		CloudflareAPIKey:   getEnv("CLOUDFLARE_API_KEY", ""),
		CloudflareAccountID: getEnv("CLOUDFLARE_ACCOUNT_ID", ""),
		
		CatalogRefreshOnStartup: getEnvBool("MODEL_CATALOG_REFRESH_ON_STARTUP", true),
		CatalogRefreshInterval:  getEnvDuration("MODEL_CATALOG_REFRESH_INTERVAL", 6*time.Hour),
		CatalogTimeoutSeconds:   getEnvInt("MODEL_CATALOG_TIMEOUT_SECONDS", 15),
		CatalogPath:             getEnv("MODEL_CATALOG_PATH", ""),
		OpenRouterFreeOnly:      getEnvBool("OPENROUTER_FREE_MODELS_ONLY", true),
		DeepSeekModelsURL:       getEnv("DEEPSEEK_MODELS_URL", "https://api-docs.deepseek.com/quick_start/pricing/"),
		
		QwenWebDefaultModel:     getEnv("QWEN_WEB_DEFAULT_MODEL", ""),
		QwenWebRolloverTokens:   getEnvInt("QWEN_WEB_ROLLOVER_TOKENS", 850000),
		QwenWebHandoffChars:     getEnvInt("QWEN_WEB_HANDOFF_CHARS", 120000),
		QwenWebThinking:         getEnvBool("QWEN_WEB_THINKING", true),
		QwenWebTimezone:         getEnv("QWEN_WEB_TIMEZONE", ""),
		QwenWebVersion:          getEnv("QWEN_WEB_VERSION", "0.2.81"),
		QwenWebUseAuthorization: getEnvBool("QWEN_WEB_USE_AUTHORIZATION", false),
		
		AgentEnableThinking:     getEnvBool("AGENT_ENABLE_THINKING", false),
		AgentFastMode:           getEnvBool("AGENT_FAST_MODE", true),
		AgentMaxContextChars:    getEnvInt("AGENT_MAX_CONTEXT_CHARS", 100000),
		AgentMaxMessages:        getEnvInt("AGENT_MAX_MESSAGES", 20),
		AgentMaxToolResultChars: getEnvInt("AGENT_MAX_TOOL_RESULT_CHARS", 6000),
		AgentSequentialTools:    getEnvBool("AGENT_SEQUENTIAL_TOOLS", true),
		CodingAgentWorkspace:    getEnv("CODING_AGENT_WORKSPACE", "."),
		CodingAgentToolTimeout:  getEnvInt("CODING_AGENT_TOOL_TIMEOUT_SECONDS", 60),
		CodingAgentMaxFileBytes: getEnvInt("CODING_AGENT_MAX_FILE_BYTES", 256000),
		CodingAgentMaxToolOutputChars: getEnvInt("CODING_AGENT_MAX_TOOL_OUTPUT_CHARS", 12000),
		
		DeepSeekPoWWASMPath: getEnv("DEEPSEEK_POW_WASM_PATH", "internal/assets/sha3_wasm_bg.7b9ca65ddd.wasm"),
		
		DefaultWebSearch: getEnvBool("DEFAULT_WEB_SEARCH", false),
		AuthStorePath:   getEnv("AUTH_STORE_PATH", "data/auth.json"),
	}
	
	// Fallback: if REQUEST_API_KEY is not set, check other names
	if cfg.RequestAPIKey == "" {
		cfg.RequestAPIKey = getEnv("INFERENCE_API_KEY", "")
	}
	if cfg.RequestAPIKey == "" {
		cfg.RequestAPIKey = getEnv("PROXY_API_KEY", "")
	}
	
	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	lower := strings.ToLower(value)
	return lower == "true" || lower == "1" || lower == "yes" || lower == "on"
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intVal, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intVal
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	dur, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return dur
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}
	return nil
}
