package middleware

import (
	"net/http"
	"strings"

	"flip-ai/internal/config"
	"flip-ai/internal/services"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates API keys for protected endpoints.
func AuthMiddleware(cfg *config.Config, authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip auth for public endpoints
		if isPublicEndpoint(c.Request.URL.Path) {
			c.Next()
			return
		}

		// Check API key
		apiKey := c.GetHeader("Authorization")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}

		if apiKey != "" {
			// Strip "Bearer " prefix if present
			apiKey = strings.TrimPrefix(apiKey, "Bearer ")
			apiKey = strings.TrimSpace(apiKey)

			if cfg.RequestAPIKey != "" && apiKey == cfg.RequestAPIKey {
				c.Set("authenticated", true)
				c.Next()
				return
			}
		}

		// Check if we have any auth stored
		if authService.HasAuth() {
			// Allow requests that use stored auth
			c.Set("authenticated", true)
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "authentication_required",
			"message": "Please provide a valid API key via Authorization header or authenticate via the dashboard",
		})
		c.Abort()
	}
}

func isPublicEndpoint(path string) bool {
	publicPaths := []string{
		"/health",
		"/",
		"/dashboard",
		"/api/login",
		"/api/logout",
		"/api/auth/status",
		"/favicon.ico",
		"/static/",
		"/v1/models", // Models endpoint is public for discovery
	}

	for _, p := range publicPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}

	return false
}
