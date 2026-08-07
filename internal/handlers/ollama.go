// Package handlers provides HTTP handlers for the flip-ai proxy.
package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"flip-ai/internal/config"
	"flip-ai/internal/models"
	"flip-ai/internal/services"
	"flip-ai/internal/services/providers"

	"github.com/gin-gonic/gin"
)

// OllamaHandler handles Ollama-compatible API endpoints.
type OllamaHandler struct {
	config         *config.Config
	authService    *services.AuthService
	usageService   *services.UsageService
	providerRouter *providers.ProviderRouter
	logger         *slog.Logger
}

// NewOllamaHandler creates a new Ollama handler.
func NewOllamaHandler(
	cfg *config.Config,
	auth *services.AuthService,
	usage *services.UsageService,
	router *providers.ProviderRouter,
	logger *slog.Logger,
) *OllamaHandler {
	return &OllamaHandler{
		config:         cfg,
		authService:    auth,
		usageService:   usage,
		providerRouter: router,
		logger:         logger,
	}
}

// ListTags returns the list of available models in Ollama format.
// GET /api/tags
func (h *OllamaHandler) ListTags(c *gin.Context) {
	models := []gin.H{}

	// Add Xiaomi models
	models = append(models, gin.H{
		"name":       "mimo-v2.5-pro",
		"model":      "mimo-v2.5-pro",
		"modified_at": time.Now().Format(time.RFC3339),
		"size":       0,
		"digest":     "",
		"details": gin.H{
			"format":          "xiaomi",
			"family":          "mimo",
			"families":        []string{"mimo"},
			"parameter_size":  "",
			"quantization_level": "",
		},
	})
	models = append(models, gin.H{
		"name":       "mimo-v2.5-pro-no-thinking",
		"model":      "mimo-v2.5-pro-no-thinking",
		"modified_at": time.Now().Format(time.RFC3339),
		"size":       0,
		"digest":     "",
		"details": gin.H{
			"format":          "xiaomi",
			"family":          "mimo",
			"families":        []string{"mimo"},
			"parameter_size":  "",
			"quantization_level": "",
		},
	})

	c.JSON(http.StatusOK, gin.H{
		"models": models,
	})
}

// Chat handles Ollama chat requests.
// POST /api/chat
func (h *OllamaHandler) Chat(c *gin.Context) {
	var req struct {
		Model    string           `json:"model"`
		Messages []models.Message `json:"messages"`
		Stream   *bool            `json:"stream,omitempty"`
		Options  interface{}      `json:"options,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}

	// Default stream to false if not provided
	stream := false
	if req.Stream != nil {
		stream = *req.Stream
	}

	// Route to the appropriate provider
	route, err := h.providerRouter.Route(req.Model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_model",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("ollama chat",
		"model", req.Model,
		"route", route.Provider,
		"stream", stream,
	)

	// For now, return a placeholder response
	// TODO: Implement actual adapter calls
	if stream {
		c.Stream(func(w io.Writer) bool {
			// TODO: Implement streaming
			return false
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"model":      req.Model,
		"created_at": time.Now().Format(time.RFC3339),
		"message": gin.H{
			"role":    "assistant",
			"content": "Hello from flip-ai proxy! This is a placeholder Ollama response. Actual adapter integration coming soon.",
		},
		"done": true,
	})
}

// Generate handles Ollama generate requests.
// POST /api/generate
func (h *OllamaHandler) Generate(c *gin.Context) {
	var req struct {
		Model    string `json:"model"`
		Prompt   string `json:"prompt"`
		Stream   *bool  `json:"stream,omitempty"`
		Options  interface{} `json:"options,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}

	// Default stream to false if not provided
	stream := false
	if req.Stream != nil {
		stream = *req.Stream
	}

	// Route to the appropriate provider
	route, err := h.providerRouter.Route(req.Model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_model",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("ollama generate",
		"model", req.Model,
		"route", route.Provider,
		"stream", stream,
	)

	// For now, return a placeholder response
	// TODO: Implement actual adapter calls
	c.JSON(http.StatusOK, gin.H{
		"model":      req.Model,
		"created_at": time.Now().Format(time.RFC3339),
		"response":   "Hello from flip-ai proxy! This is a placeholder Ollama generate response. Actual adapter integration coming soon.",
		"done":       true,
	})
}

// Version returns the Ollama API version.
// GET /api/version
func (h *OllamaHandler) Version(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": "0.1.0",
	})
}
