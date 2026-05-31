/*
 * File: fingerprint_test.go
 * Project: services
 * Created: 2026-05-06
 *
 * Last Modified: Wed May 06 2026
 * Modified By: Pedro Farias
 */

package services

import (
	"mimoproxy/internal/models"
	"testing"
)

func TestGenerateFingerprint(t *testing.T) {
	tests := []struct {
		name     string
		messages []models.Message
		wantNonEmpty bool
	}{
		{
			name: "single user message",
			messages: []models.Message{
				{Role: "user", Content: "hello"},
			},
			wantNonEmpty: true,
		},
		{
			name: "system and user message",
			messages: []models.Message{
				{Role: "system", Content: "you are a helpful assistant"},
				{Role: "user", Content: "hello"},
			},
			wantNonEmpty: true,
		},
		{
			name: "long message truncation",
			messages: []models.Message{
				{Role: "user", Content: "this is a very long message that should be truncated because it exceeds the limit of two hundred characters in the fingerprint generation logic to ensure stability and efficiency in the session management"},
			},
			wantNonEmpty: true,
		},
		{
			name: "no user message fallback",
			messages: []models.Message{
				{Role: "system", Content: "instructions"},
			},
			wantNonEmpty: true,
		},
		{
			name: "empty messages",
			messages: []models.Message{},
			wantNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateFingerprint(tt.messages)
			if tt.wantNonEmpty && got == "" {
				t.Errorf("GenerateFingerprint() returned empty fingerprint")
			}
			if !tt.wantNonEmpty && got != "" {
				t.Errorf("GenerateFingerprint() = %v, want empty fingerprint", got)
			}
		})
	}
}

func TestBuildAuthAcceptsCookieStyleValues(t *testing.T) {
	auth, err := buildAuth(
		`serviceToken="token-123"`,
		`userId=456`,
		`xiaomichatbot_ph="ph-789"`,
		"",
	)
	if err != nil {
		t.Fatalf("buildAuth() returned error: %v", err)
	}

	if auth.Token != "token-123" {
		t.Fatalf("unexpected token: %q", auth.Token)
	}
	if auth.UserID != "456" {
		t.Fatalf("unexpected user id: %q", auth.UserID)
	}
	if auth.Ph != "ph-789" {
		t.Fatalf("unexpected ph: %q", auth.Ph)
	}
}

func TestBuildAuthAcceptsRawCookie(t *testing.T) {
	auth, err := buildAuth(
		"",
		"",
		"",
		`serviceToken="token-abc"; userId=999; xiaomichatbot_ph="ph-xyz"; other=value`,
	)
	if err != nil {
		t.Fatalf("buildAuth() returned error: %v", err)
	}

	if auth.Token != "token-abc" {
		t.Fatalf("unexpected token: %q", auth.Token)
	}
	if auth.UserID != "999" {
		t.Fatalf("unexpected user id: %q", auth.UserID)
	}
	if auth.Ph != "ph-xyz" {
		t.Fatalf("unexpected ph: %q", auth.Ph)
	}
	if auth.Cookie == "" {
		t.Fatal("expected raw cookie to be preserved")
	}
}
