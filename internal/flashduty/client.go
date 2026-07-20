package flashduty

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/flashcatcloud/itrs-geneos/internal/config"
	"github.com/flashcatcloud/itrs-geneos/internal/event"
)

const maxResponseBytes = 1 << 20

type Client struct {
	endpoint       string
	integrationKey string
	retries        int
	httpClient     *http.Client
	logger         *log.Logger
	sleep          func(context.Context, time.Duration) error
}

type Response struct {
	RequestID string
	Attempts  int
}

type apiResponse struct {
	RequestID string    `json:"request_id"`
	Error     *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(cfg config.FlashDutyConfig, logger *log.Logger) *Client {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Client{
		endpoint:       cfg.Endpoint,
		integrationKey: cfg.IntegrationKey,
		retries:        cfg.Retries,
		httpClient:     &http.Client{Timeout: cfg.Timeout},
		logger:         logger,
		sleep:          sleepContext,
	}
}

func (c *Client) Send(ctx context.Context, payload event.Payload) (Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode FlashDuty payload: %w", err)
	}
	requestURL, err := c.requestURL()
	if err != nil {
		return Response{}, err
	}

	maxAttempts := c.retries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, retryAfter, retryable, sendErr := c.sendAttempt(ctx, requestURL, body)
		if sendErr == nil {
			c.logger.Printf("level=info message=%q event_status=%s alert_key=%s attempt=%d http_status=%d request_id=%s",
				"FlashDuty event delivered", payload.EventStatus, payload.AlertKey, attempt, response.status, response.requestID)
			return Response{RequestID: response.requestID, Attempts: attempt}, nil
		}
		redactedErr := errors.New(c.redact(sendErr.Error()))
		if !retryable || attempt == maxAttempts {
			return Response{Attempts: attempt}, fmt.Errorf("deliver FlashDuty event after %d attempt(s): %w", attempt, redactedErr)
		}
		delay := retryAfter
		if delay <= 0 {
			delay = retryDelay(attempt)
		}
		c.logger.Printf("level=warning message=%q event_status=%s alert_key=%s attempt=%d retry_in=%s error=%q",
			"FlashDuty delivery failed; retrying", payload.EventStatus, payload.AlertKey, attempt, delay, redactedErr.Error())
		if err := c.sleep(ctx, delay); err != nil {
			return Response{Attempts: attempt}, fmt.Errorf("wait before FlashDuty retry: %w", err)
		}
	}
	return Response{}, errors.New("unreachable FlashDuty retry state")
}

type attemptResponse struct {
	status    int
	requestID string
}

func (c *Client) sendAttempt(ctx context.Context, requestURL string, body []byte) (attemptResponse, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return attemptResponse{}, 0, false, fmt.Errorf("create FlashDuty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return attemptResponse{}, 0, true, fmt.Errorf("send FlashDuty request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return attemptResponse{status: resp.StatusCode}, 0, true, fmt.Errorf("read FlashDuty response: %w", readErr)
	}
	parsed, parseErr := parseResponse(responseBody)
	result := attemptResponse{status: resp.StatusCode, requestID: parsed.RequestID}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if parseErr != nil {
			return result, 0, false, parseErr
		}
		if parsed.Error != nil {
			return result, 0, false, fmt.Errorf("FlashDuty API error %s: %s", parsed.Error.Code, parsed.Error.Message)
		}
		return result, 0, false, nil
	}

	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	retryAfter := time.Duration(0)
	if retryable {
		retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	}
	detail := strings.TrimSpace(string(responseBody))
	if parsed.Error != nil {
		detail = strings.TrimSpace(parsed.Error.Code + ": " + parsed.Error.Message)
	} else if parseErr != nil && detail == "" {
		detail = parseErr.Error()
	}
	if detail == "" {
		detail = http.StatusText(resp.StatusCode)
	}
	return result, retryAfter, retryable, fmt.Errorf("FlashDuty returned HTTP %d: %s", resp.StatusCode, detail)
}

func parseResponse(body []byte) (apiResponse, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return apiResponse{}, nil
	}
	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return apiResponse{}, fmt.Errorf("decode FlashDuty response: %w", err)
	}
	return parsed, nil
}

func (c *Client) requestURL() (string, error) {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return "", fmt.Errorf("parse FlashDuty endpoint: %w", err)
	}
	query := u.Query()
	query.Set("integration_key", c.integrationKey)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *Client) redact(value string) string {
	redacted := strings.ReplaceAll(value, c.integrationKey, "[REDACTED]")
	redacted = strings.ReplaceAll(redacted, url.QueryEscape(c.integrationKey), "[REDACTED]")
	return redacted
}

func retryDelay(attempt int) time.Duration {
	base := time.Second << (attempt - 1)
	if base > 10*time.Second {
		base = 10 * time.Second
	}
	var value uint16
	if err := binary.Read(rand.Reader, binary.BigEndian, &value); err != nil {
		return base
	}
	return base + time.Duration(value%251)*time.Millisecond
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return capRetryAfter(time.Duration(seconds) * time.Second)
	}
	if when, err := http.ParseTime(value); err == nil {
		return capRetryAfter(when.Sub(now))
	}
	return 0
}

func capRetryAfter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	if delay > 60*time.Second {
		return 60 * time.Second
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
