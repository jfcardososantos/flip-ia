package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrChatGPTBrowserRelayUnavailable = errors.New("ChatGPT browser relay is not connected")

type ChatGPTBrowserRelayJob struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type ChatGPTBrowserRelayResult struct {
	JobID   string            `json:"job_id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Error   string            `json:"error"`
}

type chatGPTPendingRelay struct {
	result chan ChatGPTBrowserRelayResult
}

var chatGPTBrowserRelay = struct {
	sync.Mutex
	jobs     chan ChatGPTBrowserRelayJob
	pending  map[string]chatGPTPendingRelay
	lastSeen time.Time
}{
	jobs:    make(chan ChatGPTBrowserRelayJob, 32),
	pending: make(map[string]chatGPTPendingRelay),
}

func ChatGPTBrowserRelayAvailable() bool {
	chatGPTBrowserRelay.Lock()
	available := time.Since(chatGPTBrowserRelay.lastSeen) < chatGPTBrowserRelayPresenceWindow()
	chatGPTBrowserRelay.Unlock()
	return available
}

func ChatGPTBrowserRelayStatus() map[string]interface{} {
	chatGPTBrowserRelay.Lock()
	defer chatGPTBrowserRelay.Unlock()
	connected := time.Since(chatGPTBrowserRelay.lastSeen) < chatGPTBrowserRelayPresenceWindow()
	lastSeen := ""
	if !chatGPTBrowserRelay.lastSeen.IsZero() {
		lastSeen = chatGPTBrowserRelay.lastSeen.UTC().Format(time.RFC3339)
	}
	return map[string]interface{}{
		"connected": connected,
		"last_seen": lastSeen,
		"pending":   len(chatGPTBrowserRelay.pending),
		"queued":    len(chatGPTBrowserRelay.jobs),
	}
}

func WaitNextChatGPTBrowserRelayJob(ctx context.Context, wait time.Duration) (ChatGPTBrowserRelayJob, bool) {
	chatGPTBrowserRelay.Lock()
	chatGPTBrowserRelay.lastSeen = time.Now()
	chatGPTBrowserRelay.Unlock()

	if wait <= 0 || wait > 30*time.Second {
		wait = 25 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case job := <-chatGPTBrowserRelay.jobs:
		chatGPTBrowserRelay.Lock()
		chatGPTBrowserRelay.lastSeen = time.Now()
		chatGPTBrowserRelay.Unlock()
		return job, true
	case <-timer.C:
		return ChatGPTBrowserRelayJob{}, false
	case <-ctx.Done():
		return ChatGPTBrowserRelayJob{}, false
	}
}

func CompleteChatGPTBrowserRelayJob(result ChatGPTBrowserRelayResult) error {
	result.JobID = strings.TrimSpace(result.JobID)
	if result.JobID == "" {
		return errors.New("ChatGPT browser relay result is missing job_id")
	}
	chatGPTBrowserRelay.Lock()
	chatGPTBrowserRelay.lastSeen = time.Now()
	pending, ok := chatGPTBrowserRelay.pending[result.JobID]
	chatGPTBrowserRelay.Unlock()
	if !ok {
		return errors.New("ChatGPT browser relay job is no longer pending")
	}
	select {
	case pending.result <- result:
		return nil
	default:
		return errors.New("ChatGPT browser relay result was already delivered")
	}
}

func ResetChatGPTBrowserRelay(reason string) int {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "browser extension reconnected"
	}
	chatGPTBrowserRelay.Lock()
	defer chatGPTBrowserRelay.Unlock()
	cancelled := 0
	for _, pending := range chatGPTBrowserRelay.pending {
		select {
		case pending.result <- ChatGPTBrowserRelayResult{Error: "ChatGPT browser relay reset: " + reason}:
			cancelled++
		default:
		}
	}
	for len(chatGPTBrowserRelay.jobs) > 0 {
		<-chatGPTBrowserRelay.jobs
	}
	chatGPTBrowserRelay.lastSeen = time.Now()
	return cancelled
}

func ChatGPTBrowserRelayRequest(session StoredWebSession, method, url string, body []byte, headers map[string]string) (*http.Response, error) {
	if !ChatGPTBrowserRelayAvailable() {
		return nil, ErrChatGPTBrowserRelayUnavailable
	}
	jobID := qwenID()
	job := ChatGPTBrowserRelayJob{
		ID: jobID, Method: method, URL: url,
		Headers: chatGPTBrowserSafeHeaders(session, headers), Body: string(body),
	}
	pending := chatGPTPendingRelay{result: make(chan ChatGPTBrowserRelayResult, 1)}
	chatGPTBrowserRelay.Lock()
	chatGPTBrowserRelay.pending[jobID] = pending
	chatGPTBrowserRelay.Unlock()
	defer func() {
		chatGPTBrowserRelay.Lock()
		delete(chatGPTBrowserRelay.pending, jobID)
		chatGPTBrowserRelay.Unlock()
	}()

	enqueueTimer := time.NewTimer(2 * time.Second)
	defer enqueueTimer.Stop()
	select {
	case chatGPTBrowserRelay.jobs <- job:
	case <-enqueueTimer.C:
		return nil, errors.New("ChatGPT browser relay queue is unavailable")
	}

	timer := time.NewTimer(chatGPTBrowserRelayRequestTimeout())
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if strings.TrimSpace(result.Error) != "" {
			return nil, errors.New("ChatGPT browser relay failed: " + strings.TrimSpace(result.Error))
		}
		if result.Status < 100 || result.Status > 599 {
			return nil, fmt.Errorf("ChatGPT browser relay returned invalid HTTP status %d", result.Status)
		}
		responseHeaders := make(http.Header)
		for key, value := range result.Headers {
			if strings.TrimSpace(key) != "" {
				responseHeaders.Set(key, value)
			}
		}
		return &http.Response{
			StatusCode: result.Status,
			Status:     strconv.Itoa(result.Status) + " relayed",
			Header:     responseHeaders,
			Body:       io.NopCloser(strings.NewReader(result.Body)),
		}, nil
	case <-timer.C:
		return nil, errors.New("ChatGPT browser relay timed out waiting for the authenticated tab")
	}
}

func chatGPTBrowserSafeHeaders(session StoredWebSession, headers map[string]string) map[string]string {
	allowed := map[string]bool{
		"accept": true, "accept-language": true, "content-type": true,
		"authorization": true, "oai-language": true, "openai-organization": true, "openai-intent": true,
	}
	out := make(map[string]string)
	for key, value := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if allowed[lower] && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func chatGPTBrowserRelayPresenceWindow() time.Duration {
	return time.Duration(intEnvOrDefault("CHATGPT_BROWSER_RELAY_PRESENCE_SECONDS", 45)) * time.Second
}

func chatGPTBrowserRelayRequestTimeout() time.Duration {
	return time.Duration(intEnvOrDefault("CHATGPT_BROWSER_RELAY_TIMEOUT_SECONDS", 300)) * time.Second
}
