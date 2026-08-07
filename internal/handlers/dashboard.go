// Package handlers provides HTTP handlers for the flip-ai proxy.
package handlers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flip-ai/internal/config"
	"flip-ai/internal/models"
	"flip-ai/internal/services"
	"flip-ai/internal/services/providers"

	"github.com/gin-gonic/gin"
)

// DashboardHandler handles dashboard and UI endpoints.
type DashboardHandler struct {
	config       *config.Config
	authService  *services.AuthService
	usageService *services.UsageService
	logger       *slog.Logger
	startTime    time.Time
}

// NewDashboardHandler creates a new dashboard handler.
func NewDashboardHandler(
	cfg *config.Config,
	auth *services.AuthService,
	usage *services.UsageService,
	logger *slog.Logger,
) *DashboardHandler {
	return &DashboardHandler{
		config:       cfg,
		authService:  auth,
		usageService: usage,
		logger:       logger,
		startTime:    time.Now(),
	}
}

// Index renders the main dashboard page.
// GET / or /dashboard
func (h *DashboardHandler) Index(c *gin.Context) {
	usage := h.usageService.Snapshot()
	auth := h.authService.Get()
	_ = auth // placeholder for future use
	hasAuth := h.authService.HasAuth()

	lastRequest := "Never"
	if !usage.LastRequestAt.IsZero() {
		lastRequest = usage.LastRequestAt.Format(time.RFC3339)
	}

	success := usage.StatusCounts[200] + usage.StatusCounts[201] + usage.StatusCounts[204]
	errors := 0
	for status, count := range usage.StatusCounts {
		if status >= 400 {
			errors += count
		}
	}

	c.HTML(http.StatusOK, "index.html", gin.H{
		"title":          "flip-ai Dashboard",
		"total_requests": usage.TotalRequests,
		"chat_requests":  usage.ChatRequests,
		"success_count":  success,
		"error_count":    errors,
		"last_request":   lastRequest,
		"uptime":         time.Since(h.startTime).String(),
		"has_auth":       hasAuth,
		"default_model":  h.config.DefaultModel,
		"has_api_key":    h.config.RequestAPIKey != "" || h.config.APIKey != "",
		"cors_origin":    h.config.CORSOrigin,
		"version":        "flip-ai/v1.0.0",
	})
}

// Settings renders the settings page.
// GET /settings
func (h *DashboardHandler) Settings(c *gin.Context) {
	auth := h.authService.Get()
	if auth == nil {
		auth = &models.StoredAuth{}
	}

	c.HTML(http.StatusOK, "settings.html", gin.H{
		"title":                 "flip-ai Settings",
		"default_model":         auth.DefaultModel,
		"has_request_api_key":   auth.RequestAPIKey != "",
		"has_xiaomi_auth":       h.authService.HasXiaomiAuth(),
		"has_deepseek_auth":     h.authService.HasDeepSeekAuth(),
		"has_gemini":            auth.GeminiAPIKey != "",
		"has_groq":              auth.GroqAPIKey != "",
		"has_openrouter":        auth.OpenRouterAPIKey != "",
		"has_cloudflare":        auth.CloudflareAPIKey != "",
		"web_sessions_count":    len(auth.WebSessions),
	})
}

// SaveSettings saves settings from the settings page.
// POST /settings/save
func (h *DashboardHandler) SaveSettings(c *gin.Context) {
	err := c.Request.ParseForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "parse_error",
			"message": err.Error(),
		})
		return
	}

	auth := h.authService.Get()
	if auth == nil {
		auth = &models.StoredAuth{}
	}

	// Update fields from form
	if val := c.PostForm("default_model"); val != "" {
		auth.DefaultModel = strings.TrimSpace(val)
	}
	if val := c.PostForm("request_api_key"); val != "" {
		auth.RequestAPIKey = strings.TrimSpace(val)
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

	h.logger.Info("settings saved")
	c.Redirect(http.StatusFound, "/settings?saved=true")
}

// DownloadExtension downloads the browser extension as a zip file.
// GET /downloads/flip-ai-session-extension.zip
func (h *DashboardHandler) DownloadExtension(c *gin.Context) {
	extensionPath := filepath.Join(".", "extension")

	// Check if extension directory exists
	if _, err := os.Stat(extensionPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "extension_not_found",
			"message": "Extension directory not found. Please build the extension first.",
		})
		return
	}

	// Create zip archive
	zipData, err := h.zipDirectory(extensionPath)
	if err != nil {
		h.logger.Error("failed to zip extension", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "zip_failed",
			"message": err.Error(),
		})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=flip-ai-session-extension.zip")
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Length", fmt.Sprintf("%d", len(zipData)))
	c.Data(http.StatusOK, "application/zip", zipData)
}

// zipDirectory creates a zip archive of a directory.
func (h *DashboardHandler) zipDirectory(root string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		writer, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, file)
		return err
	})
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UsageStats returns usage statistics as JSON.
// GET /api/stats
func (h *DashboardHandler) UsageStats(c *gin.Context) {
	usage := h.usageService.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"total_requests": usage.TotalRequests,
		"chat_requests":  usage.ChatRequests,
		"last_request_at": usage.LastRequestAt,
		"status_counts":  usage.StatusCounts,
	})
}

// OfficialModels returns the list of official provider models as JSON.
// GET /api/models/official
func (h *DashboardHandler) OfficialModels(c *gin.Context) {
	models := []gin.H{}
	for _, m := range providers.OfficialProviderModels() {
		models = append(models, gin.H{
			"id":          m["id"],
			"description": m["description"],
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"models": models,
	})
}
