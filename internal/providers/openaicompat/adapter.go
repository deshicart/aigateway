// Package openaicompat implements the shared OpenAI-compatible adapter
// used by Groq, Cerebras, Mistral, NVIDIA, GitHub, OpenRouter,
// Hugging Face, Ollama and Custom providers (all expose /chat/completions).
package openaicompat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openbridge/gateway/internal/providers"
)

type Adapter struct {
	ProviderName string
	DefaultBase  string
	ExtraHeaders map[string]string
	Timeout      time.Duration
}

func (a *Adapter) Name() string { return a.ProviderName }

func (a *Adapter) timeout() time.Duration {
	if a.Timeout != 0 {
		return a.Timeout
	}
	return 90 * time.Second
}

func (a *Adapter) baseURL(base string) string {
	if base != "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(a.DefaultBase, "/")
}

func (a *Adapter) toUpstream(req providers.ChatRequest) map[string]any {
	maxTokens := 0
	if req.MaxCompletions != nil {
		maxTokens = *req.MaxCompletions
	} else if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}
	m := map[string]any{
		"model":    stripProviderPrefix(req.Model),
		"messages": req.Messages,
	}
	if req.Temperature != nil {
		m["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		m["top_p"] = *req.TopP
	}
	if maxTokens > 0 {
		m["max_tokens"] = maxTokens
		m["max_completion_tokens"] = maxTokens
	}
	if req.Stop != nil {
		m["stop"] = req.Stop
	}
	if req.Seed != nil {
		m["seed"] = *req.Seed
	}
	if len(req.Tools) > 0 {
		m["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		m["tool_choice"] = req.ToolChoice
	}
	if req.ResponseFormat != nil {
		m["response_format"] = req.ResponseFormat
	}
	return m
}

func stripProviderPrefix(model string) string {
	if i := strings.Index(model, "/"); i >= 0 {
		// keep everything after first slash (handles org/model too? use last? first is provider)
		return model[i+1:]
	}
	return model
}

type chatWire struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role      string               `json:"role"`
			Content   any                  `json:"content"`
			ToolCalls []providers.ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (a *Adapter) Chat(ctx context.Context, apiKey, baseURL string, req providers.ChatRequest) (*providers.ChatResponse, *providers.UpstreamError, map[string]string) {
	client := providers.HTTPClient(a.timeout())
	payload := a.toUpstream(req)
	payload["stream"] = false
	var wire chatWire
	hdr, status, raw, err := providers.DoJSON(ctx, client, "POST", a.baseURL(baseURL)+"/chat/completions", apiKey, a.ExtraHeaders, payload, &wire)
	if err != nil {
		return nil, &providers.UpstreamError{Code: "connection", Msg: err.Error()}, nil
	}
	if status >= 400 {
		return nil, providers.ClassifyHTTP(status, string(raw)), hdrToMap(hdr)
	}
	if len(wire.Choices) == 0 {
		return nil, &providers.UpstreamError{Status: status, Code: "bad_request", Msg: "upstream returned no choices"}, hdrToMap(hdr)
	}
	c := wire.Choices[0]
	return &providers.ChatResponse{
		ID:    wire.ID,
		Model: req.Model,
		Message: providers.ChatMessage{
			Role:      "assistant",
			Content:   c.Message.Content,
			ToolCalls: c.Message.ToolCalls,
		},
		Usage: providers.Usage{
			PromptTokens:     wire.Usage.PromptTokens,
			CompletionTokens: wire.Usage.CompletionTokens,
			TotalTokens:      wire.Usage.TotalTokens,
		},
	}, nil, hdrToMap(hdr)
}

func (a *Adapter) StreamChat(ctx context.Context, apiKey, baseURL string, req providers.ChatRequest) (<-chan providers.StreamEvent, *providers.UpstreamError, map[string]string) {
	client := providers.HTTPClient(a.timeout())
	payload := a.toUpstream(req)
	payload["stream"] = true
	out := make(chan providers.StreamEvent, 16)
	go func() {
		defer close(out)
		body, _ := json.Marshal(payload)
		hreq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL(baseURL)+"/chat/completions", strings.NewReader(string(body)))
		if err != nil {
			out <- providers.StreamEvent{Done: true}
			return
		}
		hreq.Header.Set("Content-Type", "application/json")
		hreq.Header.Set("Accept", "text/event-stream")
		if apiKey != "" {
			hreq.Header.Set("Authorization", "Bearer "+apiKey)
		}
		for k, v := range a.ExtraHeaders {
			hreq.Header.Set(k, v)
		}
		resp, err := client.Do(hreq)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			_ = raw
			return
		}
		providers.ScanSSE(resp.Body, func(data string) bool {
			if data == "[DONE]" {
				out <- providers.StreamEvent{Done: true}
				return false
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
				return true
			}
			d := chunk.Choices[0].Delta
			var text string
			switch c := d.Content.(type) {
			case string:
				text = c
			case nil:
			default:
				b, _ := json.Marshal(c)
				_ = b
			}
			select {
			case out <- providers.StreamEvent{Delta: text, ToolCalls: d.ToolCalls, FinishReason: chunk.Choices[0].FinishReason}:
			case <-ctx.Done():
				return false
			}
			return true
		})
		out <- providers.StreamEvent{Done: true}
	}()
	return out, nil, map[string]string{}
}

func (a *Adapter) Embed(ctx context.Context, apiKey, baseURL string, req providers.EmbedRequest) (*providers.EmbedResponse, *providers.UpstreamError) {
	client := providers.HTTPClient(a.timeout())
	payload := map[string]any{"model": stripProviderPrefix(req.Model), "input": req.Input}
	var wire struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Model string          `json:"model"`
		Usage providers.Usage `json:"usage"`
	}
	_, status, raw, err := providers.DoJSON(ctx, client, "POST", a.baseURL(baseURL)+"/embeddings", apiKey, a.ExtraHeaders, payload, &wire)
	if err != nil {
		return nil, &providers.UpstreamError{Code: "connection", Msg: err.Error()}
	}
	if status >= 400 {
		return nil, providers.ClassifyHTTP(status, string(raw))
	}
	out := &providers.EmbedResponse{Model: req.Model, Usage: wire.Usage}
	for _, d := range wire.Data {
		out.Data = append(out.Data, providers.EmbedData{Index: d.Index, Embedding: d.Embedding})
	}
	return out, nil
}

func (a *Adapter) HealthCheck(ctx context.Context, apiKey, baseURL string) providers.HealthStatus {
	if apiKey == "" && a.ProviderName != "ollama" {
		return providers.HealthStatus{State: "invalid", Message: "missing API key"}
	}
	client := providers.HTTPClient(15 * time.Second)
	hdr, status, raw, err := providers.DoJSON(ctx, client, "GET", a.baseURL(baseURL)+"/models", apiKey, a.ExtraHeaders, nil, nil)
	_ = hdr
	_ = raw
	if err != nil {
		if ctx.Err() != nil {
			return providers.HealthStatus{State: "timeout", Message: err.Error()}
		}
		return providers.HealthStatus{State: "error", Message: err.Error()}
	}
	switch {
	case status == 200:
		return providers.HealthStatus{State: "healthy", Message: "ok"}
	case status == 401 || status == 403:
		return providers.HealthStatus{State: "invalid", Message: "authentication failed"}
	case status == 429:
		return providers.HealthStatus{State: "rate_limited", Message: "rate limited"}
	case status >= 500:
		return providers.HealthStatus{State: "error", Message: "upstream error"}
	default:
		return providers.HealthStatus{State: "error", Message: "unexpected status"}
	}
}

func hdrToMap(h http.Header) map[string]string {
	m := map[string]string{}
	for _, k := range []string{"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "retry-after"} {
		if v := h.Get(k); v != "" {
			m[k] = v
		}
	}
	return m
}
