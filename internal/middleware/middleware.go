package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"flip-ai/internal/config"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware returns CORS middleware.
func CORSMiddleware(origin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
		c.Header("Access-Control-Expose-Headers", "Content-Length")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// RecoveryMiddleware returns a recovery middleware.
func RecoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		logger.Error("panic recovered",
			"error", recovered,
			"path", c.Request.URL.Path,
		)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "internal_server_error",
		})
	})
}

// LoggingMiddleware returns request logging middleware.
func LoggingMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		logger.Info("request",
			"method", method,
			"path", path,
			"status", status,
			"latency", latency,
			"ip", c.ClientIP(),
		)
	}
}

// AuthMiddlewareBuilder creates auth middleware.
type AuthMiddlewareBuilder struct {
	cfg *config.Config
}

// NewAuthMiddleware creates a new auth middleware builder.
func NewAuthMiddleware(cfg *config.Config) *AuthMiddlewareBuilder {
	return &AuthMiddlewareBuilder{cfg: cfg}
}

// RequireAPIKey returns middleware that requires API key authentication.
func (b *AuthMiddlewareBuilder) RequireAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		// For now, same as admin auth (no auth service required)
		c.Next()
	}
}

// RequireAdmin returns middleware that requires admin authentication.
func (b *AuthMiddlewareBuilder) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		// For now, same as API key auth
		c.Next()
	}
}
