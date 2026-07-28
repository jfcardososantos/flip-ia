package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverQwenModelsUsesCurrentActiveTextModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("User-Agent"), "Go-http-client/") {
			t.Fatalf("Qwen model discovery used blocked Go User-Agent: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"data": [
					{
						"id": "qwen-future",
						"name": "Qwen Future",
						"owned_by": "qwen",
						"info": {
							"is_active": true,
							"meta": {
								"short_description": "Current recommended model",
								"max_context_length": 1000000,
								"chat_type": ["t2t"]
							}
						}
					},
					{
						"id": "qwen-inactive",
						"info": {
							"is_active": false,
							"meta": {"chat_type": ["t2t"]}
						}
					},
					{
						"id": "qwen-image",
						"info": {
							"is_active": true,
							"meta": {"chat_type": ["t2i"]}
						}
					}
				]
			}
		}`))
	}))
	defer server.Close()
	t.Setenv("QWEN_MODELS_URL", server.URL)

	result := discoverQwenModels(context.Background())
	if result.err != nil {
		t.Fatalf("discoverQwenModels: %v", result.err)
	}
	if result.defaultModel != "qwen-future" {
		t.Fatalf("default model = %q; want qwen-future", result.defaultModel)
	}
	if len(result.models) != 2 {
		t.Fatalf("model count = %d; want alias plus one active text model", len(result.models))
	}
	if result.models[0].ID != "qwen-web" || result.models[1].ID != "qwen-web/qwen-future" {
		t.Fatalf("unexpected models: %+v", result.models)
	}
	if result.models[1].ContextLength != 1000000 {
		t.Fatalf("context length = %d; want 1000000", result.models[1].ContextLength)
	}
}

func TestDiscoverOpenRouterModelsKeepsFreeTextModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"id": "vendor/free-by-name:free",
					"name": "Free by name",
					"context_length": 131072,
					"pricing": {"prompt": "1", "completion": "1", "request": "0"},
					"architecture": {"output_modalities": ["text"]}
				},
				{
					"id": "vendor/free-by-price",
					"name": "Free by price",
					"pricing": {"prompt": "0", "completion": "0", "request": "0"},
					"architecture": {"output_modalities": ["text"]}
				},
				{
					"id": "vendor/paid",
					"name": "Paid",
					"pricing": {"prompt": "0.01", "completion": "0.02", "request": "0"},
					"architecture": {"output_modalities": ["text"]}
				},
				{
					"id": "vendor/image:free",
					"name": "Image only",
					"pricing": {"prompt": "0", "completion": "0", "request": "0"},
					"architecture": {"output_modalities": ["image"]}
				}
			]
		}`))
	}))
	defer server.Close()
	t.Setenv("OPENROUTER_MODELS_URL", server.URL)
	t.Setenv("OPENROUTER_FREE_MODELS_ONLY", "true")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("AUTH_STORE_PATH", t.TempDir()+"/auth.json")

	result := discoverOpenRouterModels(context.Background())
	if result.err != nil {
		t.Fatalf("discoverOpenRouterModels: %v", result.err)
	}
	if len(result.models) != 2 {
		t.Fatalf("model count = %d; want 2 free text models: %+v", len(result.models), result.models)
	}
	if result.models[0].ID != "openrouter/vendor/free-by-name:free" ||
		result.models[1].ID != "openrouter/vendor/free-by-price" {
		t.Fatalf("unexpected models: %+v", result.models)
	}
}

func TestMergeCatalogWithFallbackInitializesProviderStatus(t *testing.T) {
	snapshot := mergeCatalogWithFallback(ModelCatalogSnapshot{
		Models: []CatalogModel{{
			ID: "qwen-web/new-model", Provider: "qwen", OwnedBy: "qwen",
		}},
	})
	if snapshot.Providers == nil {
		t.Fatal("provider status map was not initialized")
	}
	if snapshot.Providers["gemini"].Count == 0 {
		t.Fatal("missing fallback Gemini models")
	}
}
