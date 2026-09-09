// Package providers defines the Provider adapter interface.
// Provider-specific HTTP logic MUST live inside adapters; the router
// must not contain provider-specific code (PRD §10).
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatMessage is an OpenAI-style message.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is an OpenAI-style tool call.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool is an OpenAI-style tool definition.
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters,omitempty"`
	} `json:"function"`
}

// ChatRequest is the normalized gateway request.
type ChatRequest struct {
	Model          string        `json:"model"`
	Messages       []ChatMessage `json:"messages"`
	Temperature    *float64      `json:"temperature,omitempty"`
	TopP           *float64      `json:"top_p,omitempty"`
	MaxTokens      *int          `json:"max_tokens,omitempty"`
	MaxCompletions *int          `json:"max_completion_tokens,omitempty"`
	Stop           any           `json:"stop,omitempty"`
	Seed           *int64        `json:"seed,omitempty"`
	Stream         bool          `json:"stream,omitempty"`
	Tools          []Tool        `json:"tools,omitempty"`
	ToolChoice     any           `json:"tool_choice,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
	SessionID      string        `json:"-"`
}

// Usage tracks token counts.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is the normalized gateway response.
type ChatResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Message ChatMessage `json:"message"`
	Usage   Usage       `json:"usage"`
}

// StreamEvent is one SSE delta.
type StreamEvent struct {
	Delta        string
	ToolCalls    []ToolCall
	FinishReason string
	Done         bool
}

// EmbedRequest normalizes embedding calls.
type EmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

// EmbedResponse normalizes embedding results.
type EmbedResponse struct {
	Model string      `json:"model"`
	Data  []EmbedData `json:"data"`
	Usage Usage       `json:"usage"`
}

// EmbedData is one embedding vector.
type EmbedData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// HealthStatus is per-key health (PRD §18).
type HealthStatus struct {
	State   string // healthy, rate_limited, invalid, timeout, error, disabled, unknown
	Message string
}

// Provider is the adapter interface.
type Provider interface {
	Name() string
	Chat(ctx context.Context, apiKey, baseURL string, req ChatRequest) (*ChatResponse, *UpstreamError, map[string]string)
	StreamChat(ctx context.Context, apiKey, baseURL string, req ChatRequest) (<-chan StreamEvent, *UpstreamError, map[string]string)
	Embed(ctx context.Context, apiKey, baseURL string, req EmbedRequest) (*EmbedResponse, *UpstreamError)
	HealthCheck(ctx context.Context, apiKey, baseURL string) HealthStatus
}

// UpstreamError classifies failures for failover decisions (PRD §16).
type UpstreamError struct {
	Status int    // HTTP status (0 = transport)
	Code   string // rate_limited, timeout, unavailable, bad_request, auth_error, not_found, ...
	Msg    string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream %d/%s: %s", e.Status, e.Code, e.Msg)
}

// Retryable reports whether the router may fail over to another provider/key.
func (e *UpstreamError) Retryable() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case "rate_limited", "timeout", "unavailable", "connection":
		return true
	}
	if e.Status == 429 || e.Status == 408 || e.Status == 502 || e.Status == 503 || e.Status == 504 {
		return true
	}
	return false
}

func ClassifyHTTP(status int, body string) *UpstreamError {
	lb := strings.ToLower(body)
	switch {
	case status == 429:
		return &UpstreamError{Status: status, Code: "rate_limited", Msg: truncate(body, 500)}
	case status == 408 || status == 504:
		return &UpstreamError{Status: status, Code: "timeout", Msg: truncate(body, 500)}
	case status >= 500:
		return &UpstreamError{Status: status, Code: "unavailable", Msg: truncate(body, 500)}
	case status == 401 || status == 403 || strings.Contains(lb, "api key") && (strings.Contains(lb, "invalid") || strings.Contains(lb, "incorrect")):
		return &UpstreamError{Status: status, Code: "auth_error", Msg: truncate(body, 500)}
	case status == 404:
		return &UpstreamError{Status: status, Code: "not_found", Msg: truncate(body, 500)}
	default:
		return &UpstreamError{Status: status, Code: "bad_request", Msg: truncate(body, 500)}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Shared HTTP client (tuned for low RAM, no connection leaks).
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        32,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     60 * time.Second,
			DisableCompression:  false,
		},
	}
}

// DoJSON posts JSON and decodes JSON response.
func DoJSON(ctx context.Context, client *http.Client, method, url, apiKey string, extraHeaders map[string]string, payload any, out any) (http.Header, int, []byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return resp.Header, resp.StatusCode, nil, err
	}
	if out != nil && resp.StatusCode < 400 && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	return resp.Header, resp.StatusCode, raw, nil
}

// ScanSSE consumes an SSE stream, calling fn per data: line.
func ScanSSE(r io.Reader, fn func(data string) bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			d := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if !fn(d) {
				return
			}
		}
	}
}
