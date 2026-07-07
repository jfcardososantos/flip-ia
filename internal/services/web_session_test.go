package services

import "testing"

func TestValidateWebSessionInputDeepSeekCopiesUserTokenToStorage(t *testing.T) {
	session, err := ValidateWebSessionInput("deepseek", StoredWebSession{
		Cookie: "foo=bar; userToken=abc123",
	})
	if err != nil {
		t.Fatalf("validate deepseek session: %v", err)
	}
	if session.Token != "abc123" {
		t.Fatalf("expected token abc123, got %q", session.Token)
	}
	if session.Storage["userToken"] != "abc123" {
		t.Fatalf("expected storage userToken to be persisted")
	}
}

func TestStoredWebSessionsIncludesLegacyDeepSeek(t *testing.T) {
	sessions := StoredWebSessions(StoredAuth{
		DeepSeekCookie: "foo=bar",
		DeepSeekToken:  "tok",
	})
	session, ok := sessions["deepseek"]
	if !ok {
		t.Fatalf("expected legacy deepseek session to be exposed")
	}
	if session.Cookie != "foo=bar" || session.Token != "tok" {
		t.Fatalf("unexpected legacy session: %+v", session)
	}
}

func TestValidateGenericWebSessionAllowsCookieOnly(t *testing.T) {
	session, err := ValidateWebSessionInput("chatgpt-web", StoredWebSession{
		Cookie: "a=b",
	})
	if err != nil {
		t.Fatalf("expected generic cookie-only session to be accepted: %v", err)
	}
	if session.Cookie != "a=b" {
		t.Fatalf("unexpected cookie: %q", session.Cookie)
	}
}
