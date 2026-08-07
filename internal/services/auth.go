package services

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"flip-ai/internal/config"
	"flip-ai/internal/models"
)

// AuthService handles authentication storage and management.
type AuthService struct {
	config     *config.Config
	logger     *slog.Logger
	mu         sync.RWMutex
	storedAuth *models.StoredAuth
	authPath   string
}

// NewAuthService creates a new auth service.
func NewAuthService(cfg *config.Config, logger *slog.Logger) *AuthService {
	return &AuthService{
		config:   cfg,
		logger:   logger,
		authPath: filepath.Join("data", "auth.json"),
	}
}

// Load loads the stored authentication from disk.
func (s *AuthService) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(s.authPath), 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	data, err := os.ReadFile(s.authPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No auth file yet, that's fine
			return nil
		}
		return fmt.Errorf("failed to read auth file: %w", err)
	}

	var auth models.StoredAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return fmt.Errorf("failed to parse auth file: %w", err)
	}

	s.storedAuth = &auth
	s.logger.Info("loaded stored authentication")
	return nil
}

// Store stores the authentication to disk.
func (s *AuthService) Store(auth *models.StoredAuth) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(s.authPath), 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth: %w", err)
	}

	if err := os.WriteFile(s.authPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}

	s.storedAuth = auth
	s.logger.Info("stored authentication")
	return nil
}

// Get returns the stored authentication.
func (s *AuthService) Get() *models.StoredAuth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storedAuth
}

// HasAuth checks if there is stored authentication.
func (s *AuthService) HasAuth() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storedAuth != nil
}

// HasXiaomiAuth checks if Xiaomi authentication is available.
func (s *AuthService) HasXiaomiAuth() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.storedAuth == nil {
		return false
	}
	return s.storedAuth.XiaomiCookie != "" || s.storedAuth.ServiceToken != ""
}

// HasDeepSeekAuth checks if DeepSeek authentication is available.
func (s *AuthService) HasDeepSeekAuth() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.storedAuth == nil {
		return false
	}
	return s.storedAuth.DeepSeekCookie != "" || s.storedAuth.DeepSeekToken != ""
}

// Update replaces the stored authentication in memory without saving to disk.
func (s *AuthService) Update(auth *models.StoredAuth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storedAuth = auth
	return nil
}

// Save writes the current authentication to disk.
func (s *AuthService) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.storedAuth == nil {
		return fmt.Errorf("no auth data to save")
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(s.authPath), 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	data, err := json.MarshalIndent(s.storedAuth, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth: %w", err)
	}

	if err := os.WriteFile(s.authPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}

	s.logger.Info("saved authentication")
	return nil
}

// Clear clears the stored authentication.
func (s *AuthService) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.authPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove auth file: %w", err)
	}

	s.storedAuth = nil
	s.logger.Info("cleared stored authentication")
	return nil
}

// AuthenticateXiaomi authenticates with Xiaomi Mimo.
func (s *AuthService) AuthenticateXiaomi(username, password string) (*models.StoredAuth, error) {
	// TODO: Implement actual Xiaomi authentication
	// For now, we'll just store the credentials
	auth := &models.StoredAuth{
		XiaomiCookie: fmt.Sprintf("username=%s; session=temp", username),
		UserID:       username,
	}
	s.logger.Info("xiaomi authentication placeholder", "username", username)
	return auth, nil
}

// AuthenticateDeepSeek authenticates with DeepSeek.
func (s *AuthService) AuthenticateDeepSeek(token string) (*models.StoredAuth, error) {
	if token == "" {
		return nil, fmt.Errorf("token required")
	}
	auth := &models.StoredAuth{
		DeepSeekToken: token,
	}
	s.logger.Info("deepseek authentication stored")
	return auth, nil
}

// AuthenticateOpenRouter authenticates with OpenRouter.
func (s *AuthService) AuthenticateOpenRouter(token string) (*models.StoredAuth, error) {
	if token == "" {
		return nil, fmt.Errorf("API key required")
	}
	auth := &models.StoredAuth{
		OpenRouterAPIKey: token,
	}
	s.logger.Info("openrouter authentication stored")
	return auth, nil
}

// AuthenticateGemini authenticates with Google Gemini.
func (s *AuthService) AuthenticateGemini(token string) (*models.StoredAuth, error) {
	if token == "" {
		return nil, fmt.Errorf("API key required")
	}
	auth := &models.StoredAuth{
		GeminiAPIKey: token,
	}
	s.logger.Info("gemini authentication stored")
	return auth, nil
}

// AuthenticateGroq authenticates with Groq.
func (s *AuthService) AuthenticateGroq(token string) (*models.StoredAuth, error) {
	if token == "" {
		return nil, fmt.Errorf("API key required")
	}
	auth := &models.StoredAuth{
		GroqAPIKey: token,
	}
	s.logger.Info("groq authentication stored")
	return auth, nil
}

// GetSelectedAuth returns the currently selected authentication.
// This is the main auth source used by the proxy.
func GetSelectedAuth() (*models.StoredAuth, error) {
	// TODO: Check config for selected auth source
	// For now, just read the default auth file
	authPath := filepath.Join("data", "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		return nil, fmt.Errorf("no authentication found: %w", err)
	}

	var auth models.StoredAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("failed to parse auth: %w", err)
	}

	return &auth, nil
}
