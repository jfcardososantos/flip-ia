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
	"flip-ai/internal/utils"

	"github.com/gin-gonic/gin"
)

// OpenAIHandler handles OpenAI-compatible API endpoints.
type OpenAIHandler struct {
	config        *config.Config
	authService   *services.AuthService
	usageService  *services.UsageService
	providerRouter *providers.ProviderRouter
	logger        *slog.Logger
}

// NewOpenAIHandler creates a new OpenAI handler.
func NewOpenAIHandler(
	cfg *config.Config,
	auth *services.AuthService,
	usage *services.UsageService,
	router *providers.ProviderRouter,
	logger *slog.Logger,
) *OpenAIHandler {
	return &OpenAIHandler{
		config:         cfg,
		authService:    auth,
		usageService:   usage,
		providerRouter: router,
		logger:         logger,
	}
}

// ListModels returns the list of available models.
// GET /v1/models
func (h *OpenAIHandler) ListModels(c *gin.Context) {
	var modelList []models.ModelInfo

	// Add Xiaomi models
	modelList = append(modelList, models.ModelInfo{
		ID:          "mimo-v2.5-pro",
		Object:      "model",
		Created:     time.Now().Unix(),
		OwnedBy:     "xiaomi",
		Description: "Mimo 2.5 Pro",
	})
	modelList = append(modelList, models.ModelInfo{
		ID:          "mimo-v2.5-pro-no-thinking",
		Object:      "model",
		Created:     time.Now().Unix(),
		OwnedBy:     "xiaomi",
		Description: "Mimo 2.5 Pro (no thinking)",
	})

	// Add provider models from official list
	for _, m := range providers.OfficialProviderModels() {
		modelList = append(modelList, models.ModelInfo{
			ID:          m["id"],
			Object:      "model",
			Created:     time.Now().Unix(),
			OwnedBy:     "provider",
			Description: m["description"],
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   modelList,
	})
}

// ChatCompletion handles chat completion requests.
// POST /v1/chat/completions
func (h *OpenAIHandler) ChatCompletion(c *gin.Context) {
	var req models.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid_request",
			"message": err.Error(),
		})
		return
	}

	// Route to the appropriate provider
	route, err := h.providerRouter.Route(req.Model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid_model",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("chat completion",
		"model", req.Model,
		"route", route.Provider,
		"adapter", route.Adapter,
		"stream", req.Stream,
	)

	// For now, return a placeholder response
	// TODO: Implement actual adapter calls
	response := models.ChatResponse{
		ID:      utils.GenerateID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []models.OpenAIChoice{
			{
				Index: 0,
				Message: models.OpenAIMessage{
					Role:    "assistant",
					Content: "Hello from flip-ai proxy! This is a placeholder response. Actual adapter integration coming soon.",
				},
				FinishReason: "stop",
			},
		},
		Usage: models.OpenAIUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	if req.Stream {
		c.Stream(func(w io.Writer) bool {
			// TODO: Implement streaming
			return false
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// Completion handles legacy completion requests.
// POST /v1/completions
func (h *OpenAIHandler) Completion(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "not_implemented",
		"message": "Legacy completions endpoint is being migrated. Use /v1/chat/completions.",
	})
}

// GetHistory returns chat history for a conversation.
// GET /v1/chat/history/:conversationId
func (h *OpenAIHandler) GetHistory(c *gin.Context) {
	conversationID := c.Param("conversationId")
	sync := c.Query("sync") == "true"

	c.JSON(http.StatusOK, gin.H{
		"conversation_id": conversationID,
		"sync":            sync,
		"messages":        []interface{}{},
		"message":         "History endpoint is being migrated. Use SQLite persistence.",
	})
}
