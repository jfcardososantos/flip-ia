package services

import (
	"sync"
	"time"

	"flip-ai/internal/models"
	"flip-ai/internal/utils"
)

// UsageService tracks API usage statistics.
type UsageService struct {
	mu     sync.Mutex
	stats  *models.UsageStats
}

// NewUsageService creates a new usage service.
func NewUsageService() *UsageService {
	return &UsageService{
		stats: &models.UsageStats{
			StatusCounts: make(map[int]int),
		},
	}
}

// Record records a request to the API.
func (u *UsageService) Record(path string, status int) {
	if !utils.IsAPIRoute(path) {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stats.TotalRequests++
	if utils.IsChatRoute(path) {
		u.stats.ChatRequests++
	}
	u.stats.LastRequestAt = time.Now()
	u.stats.StatusCounts[status]++
}

// Snapshot returns a copy of the current usage statistics.
func (u *UsageService) Snapshot() *models.UsageStats {
	u.mu.Lock()
	defer u.mu.Unlock()
	
	copyStats := &models.UsageStats{
		TotalRequests: u.stats.TotalRequests,
		ChatRequests:  u.stats.ChatRequests,
		LastRequestAt: u.stats.LastRequestAt,
		StatusCounts:  make(map[int]int),
	}
	for k, v := range u.stats.StatusCounts {
		copyStats.StatusCounts[k] = v
	}
	return copyStats
}

// GetSuccessCount returns the total number of successful requests (2xx status).
func (u *UsageService) GetSuccessCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stats.StatusCounts[200] + u.stats.StatusCounts[201] + u.stats.StatusCounts[204]
}

// GetErrorCount returns the total number of error requests (4xx, 5xx status).
func (u *UsageService) GetErrorCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	total := 0
	for status, count := range u.stats.StatusCounts {
		if status >= 400 {
			total += count
		}
	}
	return total
}
