// Package handlers provides HTTP handlers for the flip-ai proxy.
package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"flip-ai/internal/config"
	"flip-ai/internal/models"
	"flip-ai/internal/services"
	"flip-ai/internal/utils"

	"github.com/gin-gonic/gin"
)

// AdminHandler handles administrative endpoints.
type AdminHandler struct {
	config       *config.Config
	authService  *services.AuthService
	usageService *services.UsageService
	logger       *slog.Logger
	startTime    time.Time
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(
	cfg *config.Config,
	auth *services.AuthService,
	usage *services.UsageService,
	logger *slog.Logger,
) *AdminHandler {
	return &AdminHandler{
		config:       cfg,
		authService:  auth,
		usageService: usage,
		logger:       logger,
		startTime:    time.Now(),
	}
}

// Health returns the health status of the proxy.
// GET /health
func (h *AdminHandler) Health(c *gin.Context) {
	uptime := time.Since(h.startTime)
	auth := h.authService.Get()
	hasAuth := h.authService.HasAuth()

	_ = auth // placeholder for future use
	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"uptime":       uptime.String(),
		"version":      "flip-ai/v1.0.0",
		"auth_loaded":  hasAuth,
		"auth_source":  h.getAuthSource(),
		"default_model": h.config.DefaultModel,
	})
}

// AuthStatus returns the authentication status.
// GET /auth/status
func (h *AdminHandler) AuthStatus(c *gin.Context) {
	hasAuth := h.authService.HasAuth()
	hasXiaomi := h.authService.HasXiaomiAuth()
	hasDeepSeek := h.authService.HasDeepSeekAuth()

	c.JSON(http.StatusOK, gin.H{
		"authenticated": hasAuth,
		"providers": gin.H{
			"xiaomi":   hasXiaomi,
			"deepseek": hasDeepSeek,
		},
	})
}

// AuthDebug returns debug information about the stored auth (admin only).
// GET /auth/debug
func (h *AdminHandler) AuthDebug(c *gin.Context) {
	auth := h.authService.Get()
	if auth == nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "No auth data loaded",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"xiaomi_cookie":       utils.MaskValue(auth.XiaomiCookie),
		"service_token":       utils.MaskValue(auth.ServiceToken),
		"user_id":             utils.MaskValue(auth.UserID),
		"xiaomi_chatbot_ph":   utils.MaskValue(auth.XiaomiChatbot),
		"deepseek_cookie":     utils.MaskValue(auth.DeepSeekCookie),
		"deepseek_token":      utils.MaskValue(auth.DeepSeekToken),
		"gemini_api_key":      utils.MaskValue(auth.GeminiAPIKey),
		"groq_api_key":        utils.MaskValue(auth.GroqAPIKey),
		"openrouter_api_key":  utils.MaskValue(auth.OpenRouterAPIKey),
		"cloudflare_api_key":  utils.MaskValue(auth.CloudflareAPIKey),
		"cloudflare_account_id": utils.MaskValue(auth.CloudflareAccountID),
		"default_model":       auth.DefaultModel,
		"request_api_key":     utils.MaskValue(auth.RequestAPIKey),
		"web_sessions_count":  len(auth.WebSessions),
	})
}

// ImportAuth imports authentication data.
// POST /auth/import
func (h *AdminHandler) ImportAuth(c *gin.Context) {
	var req struct {
		XiaomiCookie  string `json:"xiaomiCookie"`
		ServiceToken  string `json:"serviceToken"`
		UserID        string `json:"userId"`
		XiaomiChatbot string `json:"xiaomichatbotPh"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}

	// Get existing auth
	auth := h.authService.Get()
	if auth == nil {
		auth = &models.StoredAuth{}
	}

	// Update Xiaomi auth fields
	if req.XiaomiCookie != "" {
		auth.XiaomiCookie = strings.TrimSpace(req.XiaomiCookie)
	}
	if req.ServiceToken != "" {
		auth.ServiceToken = strings.TrimSpace(req.ServiceToken)
	}
	if req.UserID != "" {
		auth.UserID = strings.TrimSpace(req.UserID)
	}
	if req.XiaomiChatbot != "" {
		auth.XiaomiChatbot = strings.TrimSpace(req.XiaomiChatbot)
	}

	if err := h.authService.Update(auth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "save_failed",
			"message": err.Error(),
		})
		return
	}

	if err := h.authService.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "save_failed",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("auth imported", "source", "api")
	c.JSON(http.StatusOK, gin.H{
		"message": "Auth data imported successfully",
		"has_auth": h.authService.HasAuth(),
	})
}

// ImportProviderAuth imports provider API keys.
// POST /auth/provider/import
func (h *AdminHandler) ImportProviderAuth(c *gin.Context) {
	var req struct {
		Provider        string `json:"provider"`
		APIKey          string `json:"apiKey"`
		AccountID       string `json:"accountId,omitempty"`
		HTTPReferer     string `json:"httpReferer,omitempty"`
		AppTitle        string `json:"appTitle,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}

	auth := h.authService.Get()
	if auth == nil {
		auth = &models.StoredAuth{}
	}

	// Update provider key
	switch strings.ToLower(req.Provider) {
	case "gemini":
		auth.GeminiAPIKey = strings.TrimSpace(req.APIKey)
	case "groq":
		auth.GroqAPIKey = strings.TrimSpace(req.APIKey)
	case "openrouter":
		auth.OpenRouterAPIKey = strings.TrimSpace(req.APIKey)
		if req.HTTPReferer != "" {
			auth.OpenRouterHTTPReferer = strings.TrimSpace(req.HTTPReferer)
		}
		if req.AppTitle != "" {
			auth.OpenRouterAppTitle = strings.TrimSpace(req.AppTitle)
		}
	case "cloudflare":
		auth.CloudflareAPIKey = strings.TrimSpace(req.APIKey)
		if req.AccountID != "" {
			auth.CloudflareAccountID = strings.TrimSpace(req.AccountID)
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_provider",
			"message": fmt.Sprintf("Unsupported provider: %s", req.Provider),
		})
		return
	}

	if err := h.authService.Update(auth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "save_failed",
			"message": err.Error(),
		})
		return
	}

	if err := h.authService.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "save_failed",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("provider auth imported", "provider", req.Provider)
	c.JSON(http.StatusOK, gin.H{
		"message": "Provider auth imported successfully",
	})
}

// ClearAuth clears all authentication data.
// POST /auth/clear
func (h *AdminHandler) ClearAuth(c *gin.Context) {
	auth := &models.StoredAuth{}
	if err := h.authService.Update(auth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "clear_failed",
			"message": err.Error(),
		})
		return
	}
	if err := h.authService.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "clear_failed",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("auth cleared")
	c.JSON(http.StatusOK, gin.H{
		"message": "Auth data cleared successfully",
	})
}

// ImportExtensionAuth imports auth from browser extension.
// POST /auth/extension/import
func (h *AdminHandler) ImportExtensionAuth(c *gin.Context) {
	var req struct {
		XiaomiCookie  string            `json:"xiaomiCookie"`
		ServiceToken  string            `json:"serviceToken"`
		UserID        string            `json:"userId"`
		XiaomiChatbot string            `json:"xiaomichatbotPh"`
		WebSessions   map[string]models.StoredWebSession `json:"webSessions,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}

	auth := h.authService.Get()
	if auth == nil {
		auth = &models.StoredAuth{}
	}

	// Update auth fields
	if req.XiaomiCookie != "" {
		auth.XiaomiCookie = strings.TrimSpace(req.XiaomiCookie)
	}
	if req.ServiceToken != "" {
		auth.ServiceToken = strings.TrimSpace(req.ServiceToken)
	}
	if req.UserID != "" {
		auth.UserID = strings.TrimSpace(req.UserID)
	}
	if req.XiaomiChatbot != "" {
		auth.XiaomiChatbot = strings.TrimSpace(req.XiaomiChatbot)
	}

	// Merge web sessions
	if req.WebSessions != nil {
		if auth.WebSessions == nil {
			auth.WebSessions = make(map[string]models.StoredWebSession)
		}
		for k, v := range req.WebSessions {
			auth.WebSessions[k] = v
		}
	}

	if err := h.authService.Update(auth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "save_failed",
			"message": err.Error(),
		})
		return
	}

	if err := h.authService.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "save_failed",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("extension auth imported")
	c.JSON(http.StatusOK, gin.H{
		"message": "Extension auth imported successfully",
	})
}

// getAuthSource returns the source of authentication data.
func (h *AdminHandler) getAuthSource() string {
	if h.authService.HasAuth() {
		return "data/auth.json"
	}
	return "none"
}

// settingsSessionValue generates a settings session cookie value.
func settingsSessionValue(password string) string {
	sum := sha256.Sum256([]byte("flip-ai-settings:" + password))
	return hex.EncodeToString(sum[:])
}

// settingsPassword returns the settings password from config.
func (h *AdminHandler) settingsPassword() string {
	if h.config.SettingsPassword != "" {
		return h.config.SettingsPassword
	}
	return h.config.APIKey
}

// settingsAuthenticated checks if the settings session is authenticated.
func (h *AdminHandler) settingsAuthenticated(c *gin.Context) bool {
	password := h.settingsPassword()
	if password == "" {
		return false
	}
	cookieValue, err := c.Cookie("flip_ai_settings")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(
		[]byte(cookieValue),
		[]byte(settingsSessionValue(password)),
	) == 1
}

// setSettingsCookie sets the settings session cookie.
func (h *AdminHandler) setSettingsCookie(c *gin.Context) {
	password := h.settingsPassword()
	if password == "" {
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("flip_ai_settings", settingsSessionValue(password), 8*60*60, "/", "", false, true)
}

// clearSettingsCookie clears the settings session cookie.
func (h *AdminHandler) clearSettingsCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("flip_ai_settings", "", -1, "/", "", false, true)
}
