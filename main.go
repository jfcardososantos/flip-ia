/*
 * File: main.go
 * Project: flip-ai
 * Author: Pedro Farias
 * Created: 2026-04-29
 */

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flip-ai/internal/config"
	"flip-ai/internal/handlers"
	"flip-ai/internal/middleware"
	"flip-ai/internal/services"
	"flip-ai/internal/services/providers"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		slog.Warn("No .env file found, using environment variables")
	}

	// Load configuration
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	logger.Info("Starting flip-ai proxy",
		"version", "v1.0.0",
		"port", cfg.Port,
		"default_model", cfg.DefaultModel,
	)

	// Initialize services
	authService := services.NewAuthService(cfg, logger)
	if err := authService.Load(); err != nil {
		logger.Error("Failed to load auth data", "error", err)
	} else {
		if authService.HasAuth() {
			logger.Info("Auth data loaded successfully")
		} else {
			logger.Info("No auth data found")
		}
	}

	usageService := services.NewUsageService()
	providerRouter := providers.NewProviderRouter(logger)

	// Initialize handlers
	openAIHandler := handlers.NewOpenAIHandler(cfg, authService, usageService, providerRouter, logger)
	ollamaHandler := handlers.NewOllamaHandler(cfg, authService, usageService, providerRouter, logger)
	adminHandler := handlers.NewAdminHandler(cfg, authService, usageService, logger)
	dashboardHandler := handlers.NewDashboardHandler(cfg, authService, usageService, logger)

	// Setup Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Global middleware
	r.Use(middleware.RecoveryMiddleware(logger))
	r.Use(middleware.LoggingMiddleware(logger))
	r.Use(middleware.CORSMiddleware(cfg.CORSOrigin))

	// Health endpoint (public)
	r.GET("/health", adminHandler.Health)

	// Auth endpoints
	authGroup := r.Group("/auth")
	{
		authGroup.GET("/status", adminHandler.AuthStatus)
		authGroup.GET("/debug", middleware.NewAuthMiddleware(cfg).RequireAdmin(), adminHandler.AuthDebug)
		authGroup.POST("/import", middleware.NewAuthMiddleware(cfg).RequireAdmin(), adminHandler.ImportAuth)
		authGroup.POST("/provider/import", middleware.NewAuthMiddleware(cfg).RequireAdmin(), adminHandler.ImportProviderAuth)
		authGroup.POST("/extension/import", middleware.NewAuthMiddleware(cfg).RequireAdmin(), adminHandler.ImportExtensionAuth)
		authGroup.POST("/clear", middleware.NewAuthMiddleware(cfg).RequireAdmin(), adminHandler.ClearAuth)
	}

	// OpenAI-compatible API
	v1 := r.Group("/v1")
	v1.Use(middleware.NewAuthMiddleware(cfg).RequireAPIKey())
	{
		v1.GET("/models", openAIHandler.ListModels)
		v1.POST("/chat/completions", openAIHandler.ChatCompletion)
		v1.POST("/completions", openAIHandler.Completion)
		v1.GET("/chat/history/:conversationId", openAIHandler.GetHistory)
	}

	// Ollama-compatible API
	api := r.Group("/api")
	api.Use(middleware.NewAuthMiddleware(cfg).RequireAPIKey())
	{
		api.GET("/tags", ollamaHandler.ListTags)
		api.POST("/chat", ollamaHandler.Chat)
		api.POST("/generate", ollamaHandler.Generate)
		api.GET("/version", ollamaHandler.Version)
	}

	// Dashboard
	r.GET("/", dashboardHandler.Index)
	r.GET("/dashboard", dashboardHandler.Index)
	r.GET("/settings", dashboardHandler.Settings)
	r.POST("/settings/save", dashboardHandler.SaveSettings)
	r.GET("/downloads/flip-ai-session-extension.zip", dashboardHandler.DownloadExtension)
	r.GET("/api/stats", dashboardHandler.UsageStats)
	r.GET("/api/models/official", dashboardHandler.OfficialModels)

	// Start server
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		logger.Info("Server starting", "address", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown error", "error", err)
	}

	logger.Info("Server stopped")
}
