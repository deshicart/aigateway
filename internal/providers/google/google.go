// Package google implements the Gemini API adapter with OpenAI translation
// (chat, streaming via SSE, tools as functionDeclarations, vision as inlineData, embeddings).
package google

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openbridge/gateway/internal/providers"
)

type Adapter struct{}

func (Adapter) Name() string { return "google" }

const genBase = "https://generativelanguage.googleapis.com/v1beta"

func modelName(m string) string {
	m = strings.TrimPrefix(m, "google/")
	m = strings.TrimPrefix(m, "models/")
	return m
}

// --- translation helpers ---

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}
type geminiPart struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *geminiInlineData `json:"inlineData,omitempty"`
	FunctionCall     any               `json:"functionCall,omitempty"`
	FunctionResponse any               `json:"functionResponse,omitempty"`
}
type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

func toGeminiContents(msgs []providers.ChatMessage) []geminiContent {
	var out []geminiContent
	for _, m := range msgs {
		role := "user"
		switch m.Role {
		case "assistant":
			role = "model"
		case "system":
			role = "user" // system folded as leading user content
		case "tool":
			// tool result -> functionResponse part
			text := contentToText(m.Content)
			out = append(out, geminiContent{Role: "user", Parts: []geminiPart{{FunctionResponse: map[string]any{"name": m.Name, "response": map[string]any{"result": text}}}}})
			continue
		}
		parts := contentToParts(m.Role, m.Content)
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				if args == nil {
					args = map[string]any{}
				}
				parts = append(parts, geminiPart{FunctionCall: map[string]any{"name": tc.Function.Name, "args": args}})
			}
		}
		if len(parts) == 0 {
			parts = []geminiPart{{Text: ""}}
		}
		out = append(out, geminiContent{Role: role, Parts: parts})
	}
	return out
}

func contentToText(c any) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, _ := json.Marshal(v)
		// try to extract text parts
		var parts []map[string]any
		if err := json.Unmarshal(b, &parts); err == nil {
			var sb strings.Builder
			for _, p := range parts {
				if t, ok := p["text"].(string); ok {
					sb.WriteString(t)
				}
			}
			if sb.Len() > 0 {
				return sb.String()
			}
		}
		var single map[string]any
		if err := json.Unmarshal(b, &single); err == nil {
			if t, ok := single["text"].(string); ok {
				return t
			}
		}
		return string(b)
	}
}

func contentToParts(role string, c any) []geminiPart {
	if s, ok := c.(string); ok {
		return []geminiPart{{Text: s}}
	}
	b, _ := json.Marshal(c)
	var parts []map[string]any
	if err := json.Unmarshal(b, &parts); err != nil {
		// single part object?
		var one map[string]any
		if err2 := json.Unmarshal(b, &one); err2 == nil {
			return partFromMap(one)
		}
		return []geminiPart{{Text: string(b)}}
	}
	var out []geminiPart
	for _, p := range parts {
		out = append(out, partFromMap(p)...)
	}
	return out
}

func partFromMap(p map[string]any) []geminiPart {
	t, _ := p["type"].(string)
	switch t {
	case "text":
		s, _ := p["text"].(string)
		return []geminiPart{{Text: s}}
	case "image_url":
		var urlStr string
		switch iu := p["image_url"].(type) {
		case string:
			urlStr = iu
		case map[string]any:
			urlStr, _ = iu["url"].(string)
		}
		if strings.HasPrefix(urlStr, "data:") {
			// data:mime;base64,xxx
			rest := strings.TrimPrefix(urlStr, "data:")
			parts := strings.SplitN(rest, ";base64,", 2)
			if len(parts) == 2 {
				return []geminiPart{{InlineData: &geminiInlineData{MimeType: parts[0], Data: parts[1]}}}
			}
		}
		// Remote URLs: Gemini REST inline requires bytes; pass as text reference
		// and let model fetch fail gracefully rather than pretending vision works.
		return []geminiPart{{Text: "[image: " + urlStr + "]"}}
	default:
		if s, ok := p["text"].(string); ok {
			return []geminiPart{{Text: s}}
		}
		return nil
	}
}

func toGeminiTools(tools []providers.Tool) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	var decls []map[string]any
	for _, t := range tools {
		params := t.Function.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		decls = append(decls, map[string]any{
			"name": t.Function.Name, "description": t.Function.Description, "parameters": params,
		})
	}
	return []map[string]any{{"functionDeclarations": decls}}
}

// --- API calls ---

type geminiResp struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string `json:"text"`
				FunctionCall *struct {
					Name string         `json:"name"`
					Args map[string]any `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (Adapter) Chat(ctx context.Context, apiKey, _ string, req providers.ChatRequest) (*providers.ChatResponse, *providers.UpstreamError, map[string]string) {
	payload := map[string]any{"contents": toGeminiContents(req.Messages)}
	if t := toGeminiTools(req.Tools); t != nil {
		payload["tools"] = t
	}
	if req.Temperature != nil {
		payload["generationConfig"] = map[string]any{"temperature": *req.Temperature}
	}
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", genBase, modelName(req.Model), apiKey)
	client := providers.HTTPClient(90 * time.Second)
	var gr geminiResp
	_, status, raw, err := providers.DoJSON(ctx, client, "POST", url, "", nil, payload, &gr)
	if err != nil {
		return nil, &providers.UpstreamError{Code: "connection", Msg: err.Error()}, nil
	}
	if status >= 400 || gr.Error != nil {
		msg := string(raw)
		if gr.Error != nil {
			msg = gr.Error.Message
		}
		return nil, providers.ClassifyHTTP(status, msg), nil
	}
	if len(gr.Candidates) == 0 {
		return nil, &providers.UpstreamError{Status: status, Code: "bad_request", Msg: "no candidates"}, nil
	}
	c := gr.Candidates[0]
	var text strings.Builder
	var calls []providers.ToolCall
	for _, p := range c.Content.Parts {
		text.WriteString(p.Text)
		if p.FunctionCall != nil {
			args, _ := json.Marshal(p.FunctionCall.Args)
			tc := providers.ToolCall{ID: "call_" + p.FunctionCall.Name, Type: "function"}
			tc.Function.Name = p.FunctionCall.Name
			tc.Function.Arguments = string(args)
			calls = append(calls, tc)
		}
	}
	return &providers.ChatResponse{
		ID:    "chatcmpl-gemini",
		Model: req.Model,
		Message: providers.ChatMessage{
			Role: "assistant", Content: text.String(), ToolCalls: calls,
		},
		Usage: providers.Usage{
			PromptTokens: gr.UsageMetadata.PromptTokenCount, CompletionTokens: gr.UsageMetadata.CandidatesTokenCount, TotalTokens: gr.UsageMetadata.TotalTokenCount,
		},
	}, nil, nil
}

func (Adapter) StreamChat(ctx context.Context, apiKey, _ string, req providers.ChatRequest) (<-chan providers.StreamEvent, *providers.UpstreamError, map[string]string) {
	out := make(chan providers.StreamEvent, 16)
	go func() {
		defer close(out)
		payload := map[string]any{"contents": toGeminiContents(req.Messages)}
		if t := toGeminiTools(req.Tools); t != nil {
			payload["tools"] = t
		}
		b, _ := json.Marshal(payload)
		url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", genBase, modelName(req.Model), apiKey)
		hreq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
		if err != nil {
			return
		}
		hreq.Header.Set("Content-Type", "application/json")
		resp, err := providers.HTTPClient(90 * time.Second).Do(hreq)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return
		}
		providers.ScanSSE(resp.Body, func(data string) bool {
			var gr geminiResp
			if err := json.Unmarshal([]byte(data), &gr); err != nil || len(gr.Candidates) == 0 {
				return true
			}
			var sb strings.Builder
			for _, p := range gr.Candidates[0].Content.Parts {
				sb.WriteString(p.Text)
			}
			select {
			case out <- providers.StreamEvent{Delta: sb.String()}:
			case <-ctx.Done():
				return false
			}
			return true
		})
		out <- providers.StreamEvent{Done: true}
	}()
	return out, nil, nil
}

func (Adapter) Embed(ctx context.Context, apiKey, _ string, req providers.EmbedRequest) (*providers.EmbedResponse, *providers.UpstreamError) {
	texts := toTexts(req.Input)
	type item struct {
		Content struct {
			Parts []map[string]string `json:"parts"`
		} `json:"content"`
	}
	var out providers.EmbedResponse
	out.Model = req.Model
	for i, t := range texts {
		payload := map[string]any{"content": map[string]any{"parts": []map[string]string{{"text": t}}}}
		url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", genBase, modelName(req.Model), apiKey)
		var wire struct {
			Embedding struct {
				Values []float32 `json:"values"`
			} `json:"embedding"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_, status, raw, err := providers.DoJSON(ctx, providers.HTTPClient(60*time.Second), "POST", url, "", nil, payload, &wire)
		if err != nil {
			return nil, &providers.UpstreamError{Code: "connection", Msg: err.Error()}
		}
		if status >= 400 {
			msg := string(raw)
			if wire.Error != nil {
				msg = wire.Error.Message
			}
			return nil, providers.ClassifyHTTP(status, msg)
		}
		out.Data = append(out.Data, providers.EmbedData{Index: i, Embedding: wire.Embedding.Values})
		_ = item{}
	}
	return &out, nil
}

func toTexts(input any) []string {
	switch v := input.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else {
				b, _ := json.Marshal(e)
				out = append(out, string(b))
			}
		}
		return out
	default:
		b, _ := json.Marshal(v)
		var arr []string
		if err := json.Unmarshal(b, &arr); err == nil {
			return arr
		}
		return []string{string(b)}
	}
}

func (Adapter) HealthCheck(ctx context.Context, apiKey, _ string) providers.HealthStatus {
	if apiKey == "" {
		return providers.HealthStatus{State: "invalid", Message: "missing API key"}
	}
	url := fmt.Sprintf("%s/models?key=%s&pageSize=1", genBase, apiKey)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := providers.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return providers.HealthStatus{State: "error", Message: err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == 200:
		return providers.HealthStatus{State: "healthy", Message: "ok"}
	case resp.StatusCode == 400:
		// key valid but bad request shape still means auth ok
		return providers.HealthStatus{State: "healthy", Message: "ok"}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return providers.HealthStatus{State: "invalid", Message: "authentication failed"}
	case resp.StatusCode == 429:
		return providers.HealthStatus{State: "rate_limited", Message: "rate limited"}
	default:
		return providers.HealthStatus{State: "error", Message: "unexpected status"}
	}
}

var _ = base64.StdEncoding
