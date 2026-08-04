package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrQwenBrowserRelayUnavailable = errors.New("Qwen browser relay is not connected")

type QwenBrowserRelayJob struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type QwenBrowserRelayResult struct {
	JobID   string            `json:"job_id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Error   string            `json:"error"`
}

type qwenBrowserRelayPending struct {
	result chan QwenBrowserRelayResult
}

var qwenBrowserRelay = struct {
	sync.Mutex
	jobs     chan QwenBrowserRelayJob
	pending  map[string]qwenBrowserRelayPending
	lastSeen time.Time
}{
	jobs:    make(chan QwenBrowserRelayJob, 32),
	pending: make(map[string]qwenBrowserRelayPending),
}

func QwenBrowserRelayAvailable() bool {
	qwenBrowserRelay.Lock()
	available := time.Since(qwenBrowserRelay.lastSeen) < qwenBrowserRelayPresenceWindow()
	qwenBrowserRelay.Unlock()
	return available
}

func QwenBrowserRelayStatus() map[string]interface{} {
	qwenBrowserRelay.Lock()
	defer qwenBrowserRelay.Unlock()
	connected := time.Since(qwenBrowserRelay.lastSeen) < qwenBrowserRelayPresenceWindow()
	lastSeen := ""
	if !qwenBrowserRelay.lastSeen.IsZero() {
		lastSeen = qwenBrowserRelay.lastSeen.UTC().Format(time.RFC3339)
	}
	return map[string]interface{}{
		"connected": connected,
		"last_seen": lastSeen,
		"pending":   len(qwenBrowserRelay.pending),
		"queued":    len(qwenBrowserRelay.jobs),
	}
}

func WaitNextQwenBrowserRelayJob(ctx context.Context, wait time.Duration) (QwenBrowserRelayJob, bool) {
	qwenBrowserRelay.Lock()
	qwenBrowserRelay.lastSeen = time.Now()
	qwenBrowserRelay.Unlock()

	if wait <= 0 || wait > 30*time.Second {
		wait = 25 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case job := <-qwenBrowserRelay.jobs:
		qwenBrowserRelay.Lock()
		qwenBrowserRelay.lastSeen = time.Now()
		qwenBrowserRelay.Unlock()
		return job, true
	case <-timer.C:
		return QwenBrowserRelayJob{}, false
	case <-ctx.Done():
		return QwenBrowserRelayJob{}, false
	}
}

func CompleteQwenBrowserRelayJob(result QwenBrowserRelayResult) error {
	result.JobID = strings.TrimSpace(result.JobID)
	if result.JobID == "" {
		return errors.New("Qwen browser relay result is missing job_id")
	}
	qwenBrowserRelay.Lock()
	qwenBrowserRelay.lastSeen = time.Now()
	pending, ok := qwenBrowserRelay.pending[result.JobID]
	qwenBrowserRelay.Unlock()
	if !ok {
		return errors.New("Qwen browser relay job is no longer pending")
	}
	select {
	case pending.result <- result:
		return nil
	default:
		return errors.New("Qwen browser relay result was already delivered")
	}
}

func ResetQwenBrowserRelay(reason string) int {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "browser extension reconnected"
	}
	qwenBrowserRelay.Lock()
	defer qwenBrowserRelay.Unlock()
	cancelled := 0
	for _, pending := range qwenBrowserRelay.pending {
		select {
		case pending.result <- QwenBrowserRelayResult{Error: "Qwen browser relay reset: " + reason}:
			cancelled++
		default:
		}
	}
	for len(qwenBrowserRelay.jobs) > 0 {
		<-qwenBrowserRelay.jobs
	}
	qwenBrowserRelay.lastSeen = time.Now()
	return cancelled
}

func QwenBrowserRelayRequest(method, path string, payload interface{}, headers map[string]string) (*http.Response, error) {
	if !QwenBrowserRelayAvailable() {
		return nil, ErrQwenBrowserRelayUnavailable
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	jobID := qwenID()
	job := QwenBrowserRelayJob{
		ID: jobID, Method: method, URL: qwenWebBaseURL + path,
		Headers: qwenBrowserSafeHeaders(headers), Body: string(raw),
	}
	pending := qwenBrowserRelayPending{result: make(chan QwenBrowserRelayResult, 1)}
	qwenBrowserRelay.Lock()
	qwenBrowserRelay.pending[jobID] = pending
	qwenBrowserRelay.Unlock()
	defer func() {
		qwenBrowserRelay.Lock()
		delete(qwenBrowserRelay.pending, jobID)
		qwenBrowserRelay.Unlock()
	}()

	enqueueTimer := time.NewTimer(2 * time.Second)
	defer enqueueTimer.Stop()
	select {
	case qwenBrowserRelay.jobs <- job:
	case <-enqueueTimer.C:
		return nil, errors.New("Qwen browser relay queue is unavailable")
	}

	timer := time.NewTimer(qwenBrowserRelayRequestTimeout())
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if strings.TrimSpace(result.Error) != "" {
			return nil, errors.New("Qwen browser relay failed: " + strings.TrimSpace(result.Error))
		}
		if result.Status < 100 || result.Status > 599 {
			return nil, fmt.Errorf("Qwen browser relay returned invalid HTTP status %d", result.Status)
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
		return nil, errors.New("Qwen browser relay timed out waiting for the authenticated tab")
	}
}

func qwenBrowserSafeHeaders(headers map[string]string) map[string]string {
	allowed := map[string]bool{
		"accept": true, "accept-language": true, "content-type": true,
		"timezone": true, "version": true, "source": true, "x-request-id": true,
	}
	out := make(map[string]string)
	for key, value := range headers {
		if allowed[strings.ToLower(strings.TrimSpace(key))] && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func qwenBrowserRelayPresenceWindow() time.Duration {
	return time.Duration(intEnvOrDefault("QWEN_BROWSER_RELAY_PRESENCE_SECONDS", 45)) * time.Second
}

func qwenBrowserRelayRequestTimeout() time.Duration {
	return time.Duration(intEnvOrDefault("QWEN_BROWSER_RELAY_TIMEOUT_SECONDS", 180)) * time.Second
}
