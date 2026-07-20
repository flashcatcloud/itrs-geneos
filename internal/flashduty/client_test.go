package flashduty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flashcatcloud/itrs-geneos/internal/config"
	"github.com/flashcatcloud/itrs-geneos/internal/event"
)

func testPayload() event.Payload {
	return event.Payload{
		EventStatus: "Critical",
		AlertKey:    "geneos:v1:test",
		TitleRule:   "CPU high",
		Labels:      map[string]string{"source": "geneos"},
	}
}

func clientForServer(server *httptest.Server, retries int, logger *log.Logger) *Client {
	cfg := config.Defaults().FlashDuty
	cfg.Endpoint = server.URL
	cfg.IntegrationKey = "secret-key"
	cfg.Retries = retries
	cfg.Timeout = time.Second
	client := New(cfg, logger)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client
}

func TestSendPostsStandardEventAndReturnsRequestID(t *testing.T) {
	var received event.Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request method/header: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if r.URL.Query().Get("integration_key") != "secret-key" {
			t.Fatalf("integration key query parameter missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"req-123","data":{"alert_key":"geneos:v1:test"}}`))
	}))
	defer server.Close()

	response, err := clientForServer(server, 0, nil).Send(context.Background(), testPayload())
	if err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "req-123" || response.Attempts != 1 {
		t.Fatalf("unexpected response %#v", response)
	}
	if received.AlertKey != testPayload().AlertKey || received.EventStatus != "Critical" {
		t.Fatalf("unexpected payload %#v", received)
	}
}

func TestSendRetriesFiveHundreds(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"request_id":"req-ok"}`))
	}))
	defer server.Close()

	response, err := clientForServer(server, 3, nil).Send(context.Background(), testPayload())
	if err != nil {
		t.Fatal(err)
	}
	if response.Attempts != 3 || attempts.Load() != 3 {
		t.Fatalf("attempts response=%d server=%d", response.Attempts, attempts.Load())
	}
}

func TestSendDoesNotRetryFourHundreds(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, `{"error":{"code":"InvalidParameter","message":"bad"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	response, err := clientForServer(server, 3, nil).Send(context.Background(), testPayload())
	if err == nil {
		t.Fatal("expected error")
	}
	if response.Attempts != 1 || attempts.Load() != 1 {
		t.Fatalf("4xx was retried: %#v attempts=%d", response, attempts.Load())
	}
}

func TestSendTreatsSuccessErrorObjectAsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"request_id":"req-bad","error":{"code":"NoLicense","message":"upgrade"}}`))
	}))
	defer server.Close()

	_, err := clientForServer(server, 0, nil).Send(context.Background(), testPayload())
	if err == nil || !strings.Contains(err.Error(), "NoLicense") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestSendRedactsIntegrationKeyFromErrorsAndLogs(t *testing.T) {
	var logs bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret-key rejected", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := clientForServer(server, 1, log.New(&logs, "", 0))

	_, err := client.Send(context.Background(), testPayload())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-key") || strings.Contains(logs.String(), "secret-key") {
		t.Fatalf("secret leaked: err=%q logs=%q", err, logs.String())
	}
}

func TestRetryAfterIsHonoredAndCapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := clientForServer(server, 1, nil)
	var slept time.Duration
	client.sleep = func(_ context.Context, delay time.Duration) error {
		slept = delay
		return errors.New("stop")
	}

	_, err := client.Send(context.Background(), testPayload())
	if err == nil || slept != 60*time.Second {
		t.Fatalf("retry-after delay=%s err=%v", slept, err)
	}
}
