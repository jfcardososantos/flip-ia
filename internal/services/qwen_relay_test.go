package services

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestQwenBrowserRelayRoundTrip(t *testing.T) {
	qwenBrowserRelay.Lock()
	qwenBrowserRelay.lastSeen = time.Time{}
	qwenBrowserRelay.pending = make(map[string]qwenBrowserRelayPending)
	for len(qwenBrowserRelay.jobs) > 0 {
		<-qwenBrowserRelay.jobs
	}
	qwenBrowserRelay.Unlock()
	defer func() {
		qwenBrowserRelay.Lock()
		qwenBrowserRelay.lastSeen = time.Time{}
		qwenBrowserRelay.pending = make(map[string]qwenBrowserRelayPending)
		for len(qwenBrowserRelay.jobs) > 0 {
			<-qwenBrowserRelay.jobs
		}
		qwenBrowserRelay.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobCh := make(chan QwenBrowserRelayJob, 1)
	go func() {
		job, ok := WaitNextQwenBrowserRelayJob(ctx, 2*time.Second)
		if ok {
			jobCh <- job
		}
	}()
	deadline := time.Now().Add(time.Second)
	for !QwenBrowserRelayAvailable() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !QwenBrowserRelayAvailable() {
		t.Fatal("relay poll did not mark the browser as connected")
	}

	type requestResult struct {
		response *http.Response
		err      error
	}
	responseCh := make(chan requestResult, 1)
	go func() {
		response, err := QwenBrowserRelayRequest(http.MethodPost, "/api/v2/chat/completions?chat_id=1", map[string]string{"hello": "world"}, map[string]string{
			"Content-Type": "application/json", "Cookie": "must-not-leak", "Version": "0.2.81",
		})
		responseCh <- requestResult{response: response, err: err}
	}()

	job := <-jobCh
	if job.ID == "" || !strings.Contains(job.Body, `"hello":"world"`) {
		t.Fatalf("unexpected relay job: %+v", job)
	}
	if job.Headers["Cookie"] != "" || job.Headers["Content-Type"] != "application/json" {
		t.Fatalf("unsafe or missing browser relay headers: %+v", job.Headers)
	}
	if err := CompleteQwenBrowserRelayJob(QwenBrowserRelayResult{
		JobID: job.ID, Status: http.StatusOK,
		Headers: map[string]string{"Content-Type": "text/event-stream"}, Body: "data: [DONE]\n\n",
	}); err != nil {
		t.Fatalf("complete relay job: %v", err)
	}
	result := <-responseCh
	if result.err != nil {
		t.Fatalf("relay request: %v", result.err)
	}
	defer result.response.Body.Close()
	body, _ := io.ReadAll(result.response.Body)
	if result.response.StatusCode != http.StatusOK || string(body) != "data: [DONE]\n\n" {
		t.Fatalf("unexpected relayed response: status=%d body=%q", result.response.StatusCode, body)
	}
}
