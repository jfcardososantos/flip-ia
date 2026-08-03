package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestParseKimiConnectStream(t *testing.T) {
	frame := func(flags byte, event map[string]interface{}) []byte {
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		result := kimiConnectFrame(body)
		result[0] = flags
		return result
	}
	raw := append(frame(0, map[string]interface{}{"op": "set", "mask": "block.think", "block": map[string]interface{}{"think": map[string]string{"content": "raciocinio"}}}), frame(0, map[string]interface{}{"op": "append", "mask": "block.text.content", "block": map[string]interface{}{"text": map[string]string{"content": "resposta"}}})...)
	raw = append(raw, frame(2, map[string]interface{}{})...)

	result, err := parseKimiConnectStream(raw)
	if err != nil {
		t.Fatalf("parse stream: %v", err)
	}
	if result.Content != "resposta" || result.ReasoningText != "raciocinio" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestParseKimiConnectStreamTreatsEmptyErrorTrailerAsSuccess(t *testing.T) {
	frame := func(flags byte, event map[string]interface{}) []byte {
		body, _ := json.Marshal(event)
		result := kimiConnectFrame(body)
		result[0] = flags
		return result
	}
	raw := append(frame(0, map[string]interface{}{"op": "append", "mask": "block.text.content", "block": map[string]interface{}{"text": map[string]string{"content": "bom dia"}}}), frame(2, map[string]interface{}{"error": map[string]interface{}{}})...)

	result, err := parseKimiConnectStream(raw)
	if err != nil {
		t.Fatalf("empty Connect error trailer must be successful: %v", err)
	}
	if result.Content != "bom dia" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestParseKimiConnectStreamPreservesRealTrailerError(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"error": map[string]interface{}{"code": "unavailable", "message": "overloaded"}})
	raw := kimiConnectFrame(body)
	raw[0] = 2

	_, err := parseKimiConnectStream(raw)
	if err == nil || err.Error() != "Kimi stream error: unavailable: overloaded" {
		t.Fatalf("expected meaningful upstream error, got %v", err)
	}
}

func TestParseKimiConnectStreamRejectsEmptySuccessfulStream(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"error": map[string]interface{}{}})
	raw := kimiConnectFrame(body)
	raw[0] = 2

	_, err := parseKimiConnectStream(raw)
	if !errors.Is(err, ErrKimiEmptyResponse) {
		t.Fatalf("expected empty-response error, got %v", err)
	}
}

func TestKimiModelAliases(t *testing.T) {
	for _, model := range []string{"kimi-k3", "kimi/k3", "k3", "kimi-k2.6", "kimi/k2.6", "k2d6"} {
		if !IsKimiModel(model) {
			t.Fatalf("expected %q to route to Kimi", model)
		}
	}
}

func TestKimiK3DefaultsToRegularHighQuota(t *testing.T) {
	t.Setenv("KIMI_K3_REASONING_EFFORT", "")
	config, ok := resolveKimiWebModel("kimi-k3")
	if !ok || config.ReasoningEffort != "REASONING_EFFORT_HIGH" {
		t.Fatalf("unexpected K3 config: %+v", config)
	}
	if config.Scenario != "SCENARIO_AUTOMATION_K3" || config.KimiPlusID != "" {
		t.Fatalf("K3 API must use the regular automation model, got %+v", config)
	}

	t.Setenv("KIMI_K3_REASONING_EFFORT", "max")
	config, _ = resolveKimiWebModel("kimi-k3")
	if config.ReasoningEffort != "REASONING_EFFORT_MAX" {
		t.Fatalf("expected explicit MAX override, got %+v", config)
	}
}

func TestKimiK3CandidatesDegradeEffortAndFallbackTransparently(t *testing.T) {
	t.Setenv("KIMI_K3_REASONING_EFFORT", "MAX")
	t.Setenv("KIMI_K3_FALLBACK", "true")
	config, _ := resolveKimiWebModel("kimi-k3")
	candidates := kimiChatCandidates("kimi-k3", config)
	wantEfforts := []string{"REASONING_EFFORT_MAX", "REASONING_EFFORT_HIGH", "REASONING_EFFORT_MEDIUM", "REASONING_EFFORT_LOW", "REASONING_EFFORT_NONE"}
	if len(candidates) != len(wantEfforts) {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
	for i, want := range wantEfforts {
		if candidates[i].config.ReasoningEffort != want {
			t.Fatalf("candidate %d effort = %q; want %q", i, candidates[i].config.ReasoningEffort, want)
		}
	}
	if candidates[len(candidates)-1].actualModel != "kimi-k2.6" {
		t.Fatalf("expected transparent K2.6 fallback, got %+v", candidates[len(candidates)-1])
	}
}

func TestKimiOverloadExceptionIsRecoverable(t *testing.T) {
	err := kimiBlockExceptionError(map[string]interface{}{
		"exception": map[string]interface{}{"error": map[string]interface{}{"reason": "REASON_COMPLETION_OVERLOADED"}},
	})
	if !errors.Is(err, ErrKimiOverloaded) {
		t.Fatalf("expected overload sentinel, got %v", err)
	}
}

func TestKimiTimeoutIsRecoverable(t *testing.T) {
	if !isRecoverableKimiModelError(context.DeadlineExceeded) {
		t.Fatal("Kimi request timeout must allow the next adaptive candidate")
	}
}

func TestKimiWebRequestTimeoutConfiguration(t *testing.T) {
	t.Setenv("KIMI_WEB_REQUEST_TIMEOUT", "30s")
	if got := kimiWebRequestTimeout(); got.String() != "30s" {
		t.Fatalf("unexpected configured timeout: %s", got)
	}
	t.Setenv("KIMI_WEB_REQUEST_TIMEOUT", "invalid")
	if got := kimiWebRequestTimeout(); got.String() != "45s" {
		t.Fatalf("unexpected default timeout: %s", got)
	}
}
