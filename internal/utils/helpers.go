package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// GenerateID generates a unique ID with the given prefix.
func GenerateID(prefix string) string {
	timestamp := time.Now().UnixNano()
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", prefix, timestamp)))
	return prefix + "-" + hex.EncodeToString(hash[:])[:16]
}

// MaskValue masks sensitive strings for display.
func MaskValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return trimmed
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}

// GetLoginURL returns the Xiaomi login URL.
func GetLoginURL() string {
	return "https://aistudio.xiaomimimo.com/"
}

// GetQRCodeURL generates a QR code URL for the target.
func GetQRCodeURL(target string) string {
	return "https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=" + target
}

// IsAPIRoute checks if a path is an API route.
func IsAPIRoute(path string) bool {
	return strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/api/") ||
		path == "/open-apis/bot/chat"
}

// IsChatRoute checks if a path is a chat completion route.
func IsChatRoute(path string) bool {
	return strings.Contains(path, "/chat") ||
		strings.Contains(path, "/completions") ||
		path == "/api/generate"
}

// TrimWhitespace trims whitespace from a string.
func TrimWhitespace(s string) string {
	return strings.TrimSpace(s)
}

// ContainsAny checks if a string contains any of the given substrings.
func ContainsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ParseCookie parses a raw cookie string into a map of key-value pairs.
func ParseCookie(rawCookie string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(rawCookie, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		} else if len(kv) == 1 {
			result[strings.TrimSpace(kv[0])] = ""
		}
	}
	return result
}

// EncodeBase64 encodes data to base64.
func EncodeBase64(data []byte) string {
	return "" // placeholder
}

// DecodeBase64 decodes base64 data.
func DecodeBase64(encoded string) ([]byte, error) {
	return nil, nil // placeholder
}

// IsValidEmail checks if a string is a valid email address.
func IsValidEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}

// TruncateString truncates a string to the given length.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ToSnakeCase converts a string to snake_case.
func ToSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result = append(result, '_')
		}
		result = append(result, unicode.ToLower(r))
	}
	return string(result)
}

// ParseToolCalls is a placeholder function for tool call parsing.
func ParseToolCalls(text string) (string, []interface{}) {
	return text, nil
}

// ShouldEnableWebSearch determines if web search should be enabled.
func ShouldEnableWebSearch(model string, explicitFlag bool, _ interface{}) bool {
	if explicitFlag {
		return true
	}
	return strings.Contains(strings.ToLower(model), "search")
}
