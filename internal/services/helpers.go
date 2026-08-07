package services

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"flip-ai/internal/models"
)

// GlobalHTTPClient is a shared HTTP client for services.
var GlobalHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

// CurrentModelCatalog holds the current model catalog.
var CurrentModelCatalog = &ModelCatalog{
	models: make(map[string]models.ModelInfo),
}

// ModelCatalog manages available models.
type ModelCatalog struct {
	mu     sync.RWMutex
	models map[string]models.ModelInfo
}

// Get retrieves a model from the catalog.
func (c *ModelCatalog) Get(id string) (models.ModelInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.models[id]
	return m, ok
}

// List returns all models in the catalog.
func (c *ModelCatalog) List() []models.ModelInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]models.ModelInfo, 0, len(c.models))
	for _, m := range c.models {
		result = append(result, m)
	}
	return result
}

// Register adds a model to the catalog.
func (c *ModelCatalog) Register(model models.ModelInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.models[model.ID] = model
}

// cleanEnvValue removes quotes and whitespace from environment values.
func cleanEnvValue(val string) string {
	if len(val) > 0 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
		val = val[1 : len(val)-1]
	}
	return val
}

// extractCookieValue extracts a cookie value from a cookie string.
func extractCookieValue(cookieStr, name string) string {
	// Simple extraction
	for _, part := range strings.Split(cookieStr, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+"=") {
			return strings.TrimPrefix(part, name+"=")
		}
	}
	return ""
}

// ExtractText extracts text from a response.
func ExtractText(resp interface{}) string {
	if s, ok := resp.(string); ok {
		return s
	}
	return ""
}

// ValidateDeepSeekAuthInput validates DeepSeek auth input.
func ValidateDeepSeekAuthInput(cookie, token string) error {
	if cookie == "" && token == "" {
		return fmt.Errorf("either cookie or token is required")
	}
	return nil
}

// kimiTokenFromCookie extracts Kimi token from cookie.
func kimiTokenFromCookie(cookie string) string {
	return extractCookieValue(cookie, "token")
}

// envBoolDefault returns true if the env var is set to a truthy value.
func envBoolDefault(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	val = strings.ToLower(strings.TrimSpace(val))
	return val == "true" || val == "1" || val == "yes" || val == "on"
}
