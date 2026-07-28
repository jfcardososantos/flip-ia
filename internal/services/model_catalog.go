package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CatalogModel struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	OwnedBy       string `json:"owned_by"`
	Description   string `json:"description,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
	Created       int64  `json:"created,omitempty"`
	Dynamic       bool   `json:"dynamic,omitempty"`
}

type ModelProviderStatus struct {
	UpdatedAt    string `json:"updated_at,omitempty"`
	Count        int    `json:"count"`
	Source       string `json:"source"`
	DefaultModel string `json:"default_model,omitempty"`
	Error        string `json:"error,omitempty"`
}

type ModelCatalogSnapshot struct {
	UpdatedAt string                         `json:"updated_at"`
	Models    []CatalogModel                 `json:"models"`
	Providers map[string]ModelProviderStatus `json:"providers"`
}

type catalogDiscoveryResult struct {
	provider     string
	source       string
	defaultModel string
	models       []CatalogModel
	err          error
	attempted    bool
}

var modelCatalog = struct {
	sync.RWMutex
	snapshot ModelCatalogSnapshot
}{
	snapshot: fallbackModelCatalog(),
}

var modelCatalogRefreshMu sync.Mutex

func InitializeModelCatalog(ctx context.Context) {
	if snapshot, err := loadModelCatalogSnapshot(); err == nil && len(snapshot.Models) > 0 {
		setModelCatalogSnapshot(mergeCatalogWithFallback(snapshot))
	}
	if !envBoolDefault("MODEL_CATALOG_REFRESH_ON_STARTUP", true) {
		return
	}
	timeout := modelCatalogTimeout()
	refreshCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := RefreshModelCatalog(refreshCtx); err != nil {
		log.Printf("model catalog startup refresh: %v", err)
	}
}

func StartModelCatalogAutoRefresh(ctx context.Context) {
	interval := modelCatalogRefreshInterval()
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshCtx, cancel := context.WithTimeout(ctx, modelCatalogTimeout())
				err := RefreshModelCatalog(refreshCtx)
				cancel()
				if err != nil {
					log.Printf("model catalog periodic refresh: %v", err)
				}
			}
		}
	}()
}

func RefreshModelCatalogInBackground() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), modelCatalogTimeout())
		defer cancel()
		if err := RefreshModelCatalog(ctx); err != nil {
			log.Printf("model catalog refresh after configuration change: %v", err)
		}
	}()
}

func RefreshModelCatalog(ctx context.Context) error {
	modelCatalogRefreshMu.Lock()
	defer modelCatalogRefreshMu.Unlock()

	discoverers := []func(context.Context) catalogDiscoveryResult{
		discoverQwenModels,
		discoverOpenRouterModels,
		discoverGeminiModels,
		discoverGroqModels,
		discoverCloudflareModels,
		discoverXiaomiModels,
	}
	results := make(chan catalogDiscoveryResult, len(discoverers))
	var wg sync.WaitGroup
	for _, discover := range discoverers {
		wg.Add(1)
		go func(run func(context.Context) catalogDiscoveryResult) {
			defer wg.Done()
			results <- run(ctx)
		}(discover)
	}
	wg.Wait()
	close(results)

	current := CurrentModelCatalog()
	if len(current.Models) == 0 {
		current = fallbackModelCatalog()
	}
	if current.Providers == nil {
		current.Providers = make(map[string]ModelProviderStatus)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var refreshErrors []string
	for result := range results {
		if !result.attempted {
			continue
		}
		if result.err != nil {
			status := current.Providers[result.provider]
			status.Error = result.err.Error()
			if status.Source == "" {
				status.Source = "fallback"
			}
			current.Providers[result.provider] = status
			refreshErrors = append(refreshErrors, result.provider+": "+result.err.Error())
			continue
		}
		if len(result.models) == 0 {
			refreshErrors = append(refreshErrors, result.provider+": empty model list")
			continue
		}
		current.Models = replaceCatalogProvider(current.Models, result.provider, result.models)
		current.Providers[result.provider] = ModelProviderStatus{
			UpdatedAt:    now,
			Count:        len(result.models),
			Source:       result.source,
			DefaultModel: result.defaultModel,
		}
	}
	current = normalizeCatalog(current)
	current.UpdatedAt = now
	setModelCatalogSnapshot(current)
	GlobalCache.Delete("models_list")
	GlobalCache.Delete("ollama_models_list")
	if err := saveModelCatalogSnapshot(current); err != nil {
		refreshErrors = append(refreshErrors, "snapshot: "+err.Error())
	}
	if len(refreshErrors) > 0 {
		return errors.New(strings.Join(refreshErrors, "; "))
	}
	return nil
}

func CurrentModelCatalog() ModelCatalogSnapshot {
	modelCatalog.RLock()
	defer modelCatalog.RUnlock()
	raw, _ := json.Marshal(modelCatalog.snapshot)
	var copy ModelCatalogSnapshot
	_ = json.Unmarshal(raw, &copy)
	return copy
}

func CatalogModelsForProviders(providers ...string) []map[string]interface{} {
	allowed := make(map[string]bool, len(providers))
	for _, provider := range providers {
		allowed[strings.ToLower(strings.TrimSpace(provider))] = true
	}
	snapshot := CurrentModelCatalog()
	out := make([]map[string]interface{}, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		if len(allowed) > 0 && !allowed[model.Provider] {
			continue
		}
		item := map[string]interface{}{
			"id":          model.ID,
			"object":      "model",
			"created":     model.Created,
			"owned_by":    model.OwnedBy,
			"description": model.Description,
			"provider":    model.Provider,
			"dynamic":     model.Dynamic,
		}
		if model.ContextLength > 0 {
			item["context_length"] = model.ContextLength
		}
		out = append(out, item)
	}
	return out
}

func OfficialProviderModels() []map[string]interface{} {
	return CatalogModelsForProviders("gemini", "groq", "openrouter", "cloudflare")
}

func QwenWebModels() []map[string]interface{} {
	return CatalogModelsForProviders("qwen")
}

func XiaomiCatalogModels() []map[string]interface{} {
	return CatalogModelsForProviders("xiaomi")
}

func setModelCatalogSnapshot(snapshot ModelCatalogSnapshot) {
	modelCatalog.Lock()
	modelCatalog.snapshot = normalizeCatalog(snapshot)
	modelCatalog.Unlock()
}

func normalizeCatalog(snapshot ModelCatalogSnapshot) ModelCatalogSnapshot {
	seen := make(map[string]bool)
	models := make([]CatalogModel, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		model.ID = strings.TrimSpace(model.ID)
		model.Provider = strings.ToLower(strings.TrimSpace(model.Provider))
		if model.ID == "" || model.Provider == "" || seen[model.ID] {
			continue
		}
		if model.OwnedBy == "" {
			model.OwnedBy = model.Provider
		}
		if model.Created == 0 {
			model.Created = 1700000000
		}
		seen[model.ID] = true
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider == models[j].Provider {
			return models[i].ID < models[j].ID
		}
		return models[i].Provider < models[j].Provider
	})
	snapshot.Models = models
	if snapshot.Providers == nil {
		snapshot.Providers = make(map[string]ModelProviderStatus)
	}
	for provider, status := range snapshot.Providers {
		status.Count = countCatalogProvider(models, provider)
		snapshot.Providers[provider] = status
	}
	return snapshot
}

func mergeCatalogWithFallback(snapshot ModelCatalogSnapshot) ModelCatalogSnapshot {
	fallback := fallbackModelCatalog()
	if snapshot.Providers == nil {
		snapshot.Providers = make(map[string]ModelProviderStatus)
	}
	for provider := range fallback.Providers {
		if countCatalogProvider(snapshot.Models, provider) == 0 {
			snapshot.Models = append(snapshot.Models, catalogProviderModels(fallback.Models, provider)...)
			snapshot.Providers[provider] = fallback.Providers[provider]
		}
	}
	return normalizeCatalog(snapshot)
}

func replaceCatalogProvider(existing []CatalogModel, provider string, discovered []CatalogModel) []CatalogModel {
	out := make([]CatalogModel, 0, len(existing)+len(discovered))
	for _, model := range existing {
		if model.Provider != provider {
			out = append(out, model)
		}
	}
	return append(out, discovered...)
}

func catalogProviderModels(models []CatalogModel, provider string) []CatalogModel {
	var out []CatalogModel
	for _, model := range models {
		if model.Provider == provider {
			out = append(out, model)
		}
	}
	return out
}

func countCatalogProvider(models []CatalogModel, provider string) int {
	return len(catalogProviderModels(models, provider))
}

func fallbackModelCatalog() ModelCatalogSnapshot {
	models := []CatalogModel{
		{ID: "mimo-v2.5-pro", Provider: "xiaomi", OwnedBy: "xiaomi", Description: "Xiaomi MiMo model"},
		{ID: "gemini-3.5-flash", Provider: "gemini", OwnedBy: "google", Description: "Google Gemini API model"},
		{ID: "gemini-2.5-flash", Provider: "gemini", OwnedBy: "google", Description: "Google Gemini API flash model"},
		{ID: "groq/llama-3.1-8b-instant", Provider: "groq", OwnedBy: "groq", Description: "Groq chat model"},
		{ID: "groq/llama-3.3-70b-versatile", Provider: "groq", OwnedBy: "groq", Description: "Groq chat model"},
		{ID: "openrouter/meta-llama/llama-3.1-8b-instruct:free", Provider: "openrouter", OwnedBy: "openrouter", Description: "OpenRouter free model"},
		{ID: "openrouter/google/gemma-3-12b-it:free", Provider: "openrouter", OwnedBy: "openrouter", Description: "OpenRouter free model"},
		{ID: "cf/@cf/meta/llama-3.1-8b-instruct", Provider: "cloudflare", OwnedBy: "cloudflare", Description: "Cloudflare Workers AI model"},
		{ID: "cf/@cf/openai/gpt-oss-120b", Provider: "cloudflare", OwnedBy: "cloudflare", Description: "Cloudflare Workers AI model"},
		{ID: "qwen-web", Provider: "qwen", OwnedBy: "qwen", Description: "Alias for the current Qwen Web default model", ContextLength: 1000000},
		{ID: "qwen-web/qwen3.7-plus", Provider: "qwen", OwnedBy: "qwen", Description: "Qwen Web model", ContextLength: 1000000},
		{ID: "qwen-web/qwen3.8-max-preview", Provider: "qwen", OwnedBy: "qwen", Description: "Qwen Web model", ContextLength: 1000000},
		{ID: "qwen-web/qwen3.7-max", Provider: "qwen", OwnedBy: "qwen", Description: "Qwen Web model", ContextLength: 1000000},
		{ID: "qwen-web/qwen3.6-plus", Provider: "qwen", OwnedBy: "qwen", Description: "Qwen Web model", ContextLength: 1000000},
	}
	providers := make(map[string]ModelProviderStatus)
	for _, provider := range []string{"xiaomi", "gemini", "groq", "openrouter", "cloudflare", "qwen"} {
		providers[provider] = ModelProviderStatus{Count: countCatalogProvider(models, provider), Source: "fallback"}
	}
	qwenStatus := providers["qwen"]
	qwenStatus.DefaultModel = "qwen3.7-plus"
	providers["qwen"] = qwenStatus
	return normalizeCatalog(ModelCatalogSnapshot{Models: models, Providers: providers})
}

func discoverQwenModels(ctx context.Context) catalogDiscoveryResult {
	endpoint := qwenEnvOrDefault("QWEN_MODELS_URL", "https://chat.qwen.ai/api/v2/models/")
	var envelope struct {
		Data struct {
			Data []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				OwnedBy string `json:"owned_by"`
				Info    struct {
					IsActive bool `json:"is_active"`
					Meta     struct {
						Description      string   `json:"description"`
						ShortDescription string   `json:"short_description"`
						MaxContextLength int      `json:"max_context_length"`
						ChatType         []string `json:"chat_type"`
					} `json:"meta"`
				} `json:"info"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := catalogGET(ctx, endpoint, nil, &envelope); err != nil {
		return catalogDiscoveryResult{provider: "qwen", source: endpoint, err: err, attempted: true}
	}
	models := []CatalogModel{{
		ID: "qwen-web", Provider: "qwen", OwnedBy: "qwen",
		Description: "Alias for the current Qwen Web default model", ContextLength: 1000000, Dynamic: true,
	}}
	defaultModel := ""
	for _, item := range envelope.Data.Data {
		if item.ID == "" || !item.Info.IsActive || !containsString(item.Info.Meta.ChatType, "t2t") {
			continue
		}
		if defaultModel == "" {
			defaultModel = item.ID
		}
		description := firstNonEmpty(item.Info.Meta.ShortDescription, item.Info.Meta.Description, item.Name)
		models = append(models, CatalogModel{
			ID: "qwen-web/" + item.ID, Provider: "qwen", OwnedBy: firstNonEmpty(item.OwnedBy, "qwen"),
			Description: description, ContextLength: item.Info.Meta.MaxContextLength, Dynamic: true,
		})
	}
	return catalogDiscoveryResult{
		provider: "qwen", source: endpoint, defaultModel: defaultModel, models: models, attempted: true,
	}
}

func discoverGeminiModels(ctx context.Context) catalogDiscoveryResult {
	stored, _ := LoadStoredAuth()
	key := firstNonEmpty(os.Getenv("GEMINI_API_KEY"), stored.GeminiAPIKey)
	if key == "" {
		return catalogDiscoveryResult{provider: "gemini"}
	}
	endpoint := qwenEnvOrDefault("GEMINI_MODELS_URL", "https://generativelanguage.googleapis.com/v1beta/models")
	query := url.Values{"pageSize": []string{"1000"}, "key": []string{key}}
	endpoint += "?" + query.Encode()
	var response struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := catalogGET(ctx, endpoint, nil, &response); err != nil {
		return catalogDiscoveryResult{provider: "gemini", source: "Gemini models.list", err: err, attempted: true}
	}
	var models []CatalogModel
	for _, item := range response.Models {
		id := strings.TrimPrefix(item.Name, "models/")
		if id == "" || !strings.HasPrefix(id, "gemini-") || !containsString(item.SupportedGenerationMethods, "generateContent") {
			continue
		}
		models = append(models, CatalogModel{
			ID: id, Provider: "gemini", OwnedBy: "google", Description: firstNonEmpty(item.Description, item.DisplayName),
			ContextLength: item.InputTokenLimit, Dynamic: true,
		})
	}
	return catalogDiscoveryResult{provider: "gemini", source: "Gemini models.list", models: models, attempted: true}
}

func discoverGroqModels(ctx context.Context) catalogDiscoveryResult {
	provider, _ := SelectOfficialProvider("groq/catalog")
	if !provider.Configured {
		return catalogDiscoveryResult{provider: "groq"}
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/models"
	var response struct {
		Data []struct {
			ID            string `json:"id"`
			Created       int64  `json:"created"`
			OwnedBy       string `json:"owned_by"`
			Active        *bool  `json:"active"`
			ContextWindow int    `json:"context_window"`
		} `json:"data"`
	}
	headers := map[string]string{"Authorization": "Bearer " + provider.APIKey}
	if err := catalogGET(ctx, endpoint, headers, &response); err != nil {
		return catalogDiscoveryResult{provider: "groq", source: endpoint, err: err, attempted: true}
	}
	var models []CatalogModel
	for _, item := range response.Data {
		if item.ID == "" || item.Active != nil && !*item.Active || !isGroqChatModel(item.ID) {
			continue
		}
		models = append(models, CatalogModel{
			ID: "groq/" + item.ID, Provider: "groq", OwnedBy: firstNonEmpty(item.OwnedBy, "groq"),
			Description: "Groq active chat model", ContextLength: item.ContextWindow, Created: item.Created, Dynamic: true,
		})
	}
	return catalogDiscoveryResult{provider: "groq", source: endpoint, models: models, attempted: true}
}

func discoverOpenRouterModels(ctx context.Context) catalogDiscoveryResult {
	stored, _ := LoadStoredAuth()
	key := firstNonEmpty(os.Getenv("OPENROUTER_API_KEY"), stored.OpenRouterAPIKey)
	endpoint := qwenEnvOrDefault("OPENROUTER_MODELS_URL", "https://openrouter.ai/api/v1/models")
	headers := map[string]string{}
	if key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	var response struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			Created       int64  `json:"created"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				Request    string `json:"request"`
			} `json:"pricing"`
			Architecture struct {
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := catalogGET(ctx, endpoint, headers, &response); err != nil {
		return catalogDiscoveryResult{provider: "openrouter", source: endpoint, err: err, attempted: true}
	}
	freeOnly := envBoolDefault("OPENROUTER_FREE_MODELS_ONLY", true)
	var models []CatalogModel
	for _, item := range response.Data {
		if item.ID == "" || len(item.Architecture.OutputModalities) > 0 && !containsString(item.Architecture.OutputModalities, "text") {
			continue
		}
		if freeOnly && !strings.HasSuffix(item.ID, ":free") && !zeroPrice(item.Pricing.Prompt, item.Pricing.Completion, item.Pricing.Request) {
			continue
		}
		models = append(models, CatalogModel{
			ID: "openrouter/" + item.ID, Provider: "openrouter", OwnedBy: "openrouter",
			Description: firstNonEmpty(item.Description, item.Name), ContextLength: item.ContextLength,
			Created: item.Created, Dynamic: true,
		})
	}
	return catalogDiscoveryResult{provider: "openrouter", source: endpoint, models: models, attempted: true}
}

func discoverCloudflareModels(ctx context.Context) catalogDiscoveryResult {
	stored, _ := LoadStoredAuth()
	key := firstNonEmpty(os.Getenv("CLOUDFLARE_API_KEY"), stored.CloudflareAPIKey)
	accountID := firstNonEmpty(os.Getenv("CLOUDFLARE_ACCOUNT_ID"), stored.CloudflareAccountID)
	if key == "" || accountID == "" {
		return catalogDiscoveryResult{provider: "cloudflare"}
	}
	endpoint := qwenEnvOrDefault("CLOUDFLARE_MODELS_URL",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/models/search?task=Text%%20Generation&per_page=1000&hide_experimental=true", accountID))
	var response struct {
		Result []map[string]interface{} `json:"result"`
	}
	headers := map[string]string{"Authorization": "Bearer " + key}
	if err := catalogGET(ctx, endpoint, headers, &response); err != nil {
		return catalogDiscoveryResult{provider: "cloudflare", source: endpoint, err: err, attempted: true}
	}
	var models []CatalogModel
	for _, item := range response.Result {
		id := mapString(item, "name", "id")
		if id == "" || !strings.HasPrefix(id, "@cf/") {
			continue
		}
		models = append(models, CatalogModel{
			ID: "cf/" + id, Provider: "cloudflare", OwnedBy: "cloudflare",
			Description: mapString(item, "description", "short_description"), ContextLength: mapInt(item, "context_length"),
			Dynamic: true,
		})
	}
	return catalogDiscoveryResult{provider: "cloudflare", source: endpoint, models: models, attempted: true}
}

func discoverXiaomiModels(ctx context.Context) catalogDiscoveryResult {
	auth, err := GetSelectedAuth()
	if err != nil {
		return catalogDiscoveryResult{provider: "xiaomi"}
	}
	endpoint := qwenEnvOrDefault("XIAOMI_MODELS_URL", "https://aistudio.xiaomimimo.com/open-apis/bot/config")
	var response struct {
		Code int `json:"code"`
		Data struct {
			ModelConfigList []struct {
				Model   string `json:"model"`
				EnIntro string `json:"enIntro"`
			} `json:"modelConfigList"`
		} `json:"data"`
	}
	if err := catalogGET(ctx, endpoint, GetOfficialHeaders(auth, nil), &response); err != nil {
		return catalogDiscoveryResult{provider: "xiaomi", source: endpoint, err: err, attempted: true}
	}
	if response.Code != 0 {
		return catalogDiscoveryResult{provider: "xiaomi", source: endpoint, err: fmt.Errorf("business code %d", response.Code), attempted: true}
	}
	var models []CatalogModel
	for _, item := range response.Data.ModelConfigList {
		if strings.TrimSpace(item.Model) == "" {
			continue
		}
		models = append(models, CatalogModel{
			ID: item.Model, Provider: "xiaomi", OwnedBy: "xiaomi", Description: item.EnIntro, Dynamic: true,
		})
	}
	return catalogDiscoveryResult{provider: "xiaomi", source: endpoint, models: models, attempted: true}
}

func catalogGET(ctx context.Context, endpoint string, headers map[string]string, target interface{}) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", firstNonEmpty(
		os.Getenv("MODEL_CATALOG_USER_AGENT"),
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
	))
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			request.Header.Set(key, value)
		}
	}
	response, err := GlobalHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		preview := strings.TrimSpace(string(body))
		if len(preview) > 240 {
			preview = preview[:240] + "..."
		}
		return fmt.Errorf("invalid JSON response (%s): %w; body=%q", response.Header.Get("Content-Type"), err, preview)
	}
	return nil
}

func isGroqChatModel(id string) bool {
	lower := strings.ToLower(id)
	for _, blocked := range []string{"whisper", "tts", "guard", "safeguard", "prompt-guard"} {
		if strings.Contains(lower, blocked) {
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

func zeroPrice(values ...string) bool {
	for _, value := range values {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || parsed != 0 {
			return false
		}
	}
	return true
}

func mapString(item map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mapInt(item map[string]interface{}, key string) int {
	switch value := item[key].(type) {
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return 0
}

func envBoolDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func modelCatalogTimeout() time.Duration {
	seconds := intEnvOrDefault("MODEL_CATALOG_TIMEOUT_SECONDS", 15)
	return time.Duration(seconds) * time.Second
}

func modelCatalogRefreshInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("MODEL_CATALOG_REFRESH_INTERVAL"))
	if value == "" {
		return 6 * time.Hour
	}
	if value == "0" || strings.EqualFold(value, "off") {
		return 0
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 6 * time.Hour
	}
	return parsed
}

func modelCatalogPath() string {
	if custom := strings.TrimSpace(os.Getenv("MODEL_CATALOG_PATH")); custom != "" {
		return custom
	}
	return filepath.Join(filepath.Dir(authConfigPath()), "model_catalog.json")
}

func loadModelCatalogSnapshot() (ModelCatalogSnapshot, error) {
	raw, err := os.ReadFile(modelCatalogPath())
	if err != nil {
		return ModelCatalogSnapshot{}, err
	}
	var snapshot ModelCatalogSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return ModelCatalogSnapshot{}, err
	}
	return snapshot, nil
}

func saveModelCatalogSnapshot(snapshot ModelCatalogSnapshot) error {
	path := modelCatalogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
