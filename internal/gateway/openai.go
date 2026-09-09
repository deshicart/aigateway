package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openbridge/gateway/internal/analytics"
	"github.com/openbridge/gateway/internal/models"
	"github.com/openbridge/gateway/internal/providers"
	registry "github.com/openbridge/gateway/internal/providers/registry"
	"github.com/openbridge/gateway/internal/router"
)

// GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok", "version": Version})
}

// GET /v1/models (OpenAI-compatible, requires gateway key)
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireGatewayKey(r); !ok {
		writeError(w, 401, "unauthorized", "invalid or missing gateway API key")
		return
	}
	// merge static catalog with overrides + custom provider models
	overrides := map[string][2]int{}
	rows, _ := s.DB.Query(`SELECT model_id,enabled,priority FROM model_overrides`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var en, pr int
			if err := rows.Scan(&id, &en, &pr); err == nil {
				overrides[id] = [2]int{en, pr}
			}
		}
	}
	type mOut struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	var data []mOut
	for _, m := range models.Catalog() {
		if ov, ok := overrides[m.ID]; ok && ov[0] == 0 {
			continue
		}
		data = append(data, mOut{ID: m.ID, Object: "model", Created: 1700000000, OwnedBy: m.Provider})
		// bare alias
		if i := strings.LastIndex(m.ID, "/"); i >= 0 {
			data = append(data, mOut{ID: m.ID[i+1:], Object: "model", Created: 1700000000, OwnedBy: m.Provider})
		}
	}
	// custom provider model ids from DB
	prows, _ := s.DB.Query(`SELECT model_ids FROM providers WHERE enabled=1`)
	if prows != nil {
		defer prows.Close()
		for prows.Next() {
			var js string
			if err := prows.Scan(&js); err == nil {
				var ids []string
				if json.Unmarshal([]byte(js), &ids) == nil {
					for _, id := range ids {
						data = append(data, mOut{ID: id, Object: "model", Created: 1700000000, OwnedBy: "custom"})
					}
				}
			}
		}
	}
	// virtual models
	data = append(data, mOut{ID: "auto", Object: "model", Created: 1700000000, OwnedBy: "openbridge"})
	if data == nil {
		data = []mOut{}
	}
	writeJSON(w, 200, map[string]any{"object": "list", "data": data})
}

// GET /v1/models/{id}
func (s *Server) handleModelOne(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireGatewayKey(r); !ok {
		writeError(w, 401, "unauthorized", "invalid or missing gateway API key")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if m := models.ByID(id); m != nil {
		writeJSON(w, 200, map[string]any{"id": m.ID, "object": "model", "created": 1700000000, "owned_by": m.Provider})
		return
	}
	writeError(w, 404, "not_found", "model not found")
}

// POST /v1/chat/completions
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	key, ok := s.requireGatewayKey(r)
	if !ok {
		writeError(w, 401, "unauthorized", "invalid or missing gateway API key")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, 400, "invalid_request", "cannot read body")
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	model, _ := raw["model"].(string)
	if model == "" {
		writeError(w, 400, "invalid_request", "model is required")
		return
	}
	need := router.AnalyzeNeeds(raw)
	if model == "fusion" {
		writeError(w, 400, "unsupported", "fusion model is not enabled; enable it in advanced settings")
		return
	}
	req := toChatRequest(raw)
	sessionID := r.Header.Get("X-OpenBridge-Session")
	if sessionID == "" {
		if v, _ := raw["session_id"].(string); v != "" {
			sessionID = v
		}
	}
	req.SessionID = sessionID

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Config.RequestTimeoutSec)*time.Second)
	defer cancel()

	cands, cfg, err := router.BuildCandidates(ctx, s.DB, s.Tracker, model, need, "", s.decrypt)
	if err != nil || len(cands) == 0 {
		analytics.Record(s.DB, "", model, key.ID, false, 0, 0, 0, 0, "no healthy providers")
		writeError(w, 503, "provider_unavailable", "All configured providers are currently unavailable. Add a provider key or wait for cooldown to expire.")
		return
	}
	// sticky sessions: prefer pinned provider/model
	if cfg.StickySessions && sessionID != "" {
		if pp, pm, ok := router.GetPin(ctx, s.DB, sessionID); ok {
			for i, c := range cands {
				if c.Provider == pp && (pm == "" || c.Model == pm) {
					cands[0], cands[i] = cands[i], cands[0]
					break
				}
			}
		}
	}
	// context handoff (disabled by default)
	if cfg.ContextHandoff && len(cands) > 0 {
		raw = withHandoff(raw)
		req = toChatRequest(raw)
	}

	if req.Stream {
		s.serveStream(w, r, ctx, key.ID, cands, req, model, sessionID, cfg)
		return
	}
	s.serveChatOnce(w, r, ctx, key.ID, cands, req, model, sessionID)
}

func (s *Server) serveChatOnce(w http.ResponseWriter, r *http.Request, ctx context.Context, keyID string, cands []router.Candidate, req providers.ChatRequest, wantModel, sessionID string) {
	var attempts []string
	start := time.Now()
	for i, c := range cands {
		ad := registry.Get(c.Provider)
		creq := req
		creq.Model = c.Model
		cstart := time.Now()
		uctx, cancel := context.WithTimeout(ctx, time.Duration(s.Config.UpstreamTimeoutSec)*time.Second)
		resp, uerr, _ := ad.Chat(uctx, c.APIKey, c.BaseURL, creq)
		lat := time.Since(cstart)
		cancel()
		if uerr == nil {
			router.ReportResult(ctx, s.DB, s.Tracker, c, nil, float64(lat.Milliseconds()))
			if sessionID != "" {
				var cfgSticky = true
				_ = cfgSticky
				router.SetPin(ctx, s.DB, sessionID, c.Provider, c.Model, 30)
			}
			ms := int(time.Since(start).Milliseconds())
			analytics.Record(s.DB, c.Provider, c.Model, keyID, true, ms, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, i, "")
			s.Log.Infof("chat %s -> %s/%s %dms", wantModel, c.Provider, shortModel(c.Model), ms)
			w.Header().Set("X-OpenBridge-Provider", c.Provider)
			w.Header().Set("X-OpenBridge-Model", c.Model)
			w.Header().Set("X-OpenBridge-Latency", lat.String())
			w.Header().Set("X-OpenBridge-Fallback-Attempts", fmt.Sprint(i))
			writeJSON(w, 200, openAIChatResponse(resp, c.Model))
			return
		}
		router.ReportResult(ctx, s.DB, s.Tracker, c, uerr, 0)
		attempts = append(attempts, fmt.Sprintf("%s — %s", c.Provider, shortErr(uerr)))
		s.Log.Warnf("chat attempt %d %s failed: %s", i+1, c.Provider, shortErr(uerr))
		if !uerr.Retryable() {
			// permanent error on this candidate; try next provider only if error is
			// key-specific auth failure and others exist — otherwise surface it.
			if uerr.Code == "auth_error" || uerr.Code == "not_found" || uerr.Code == "bad_request" {
				// If only one candidate or all remaining same provider, return detail.
				continue
			}
			continue
		}
	}
	ms := int(time.Since(start).Milliseconds())
	analytics.Record(s.DB, "", wantModel, keyID, false, ms, 0, 0, len(cands), strings.Join(attempts, "; "))
	msg := "All configured providers are currently unavailable."
	if len(attempts) > 0 {
		msg += "\nAttempts:\n" + strings.Join(attempts, "\n")
	}
	writeError(w, 503, "provider_unavailable", msg)
}

func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, ctx context.Context, keyID string, cands []router.Candidate, req providers.ChatRequest, wantModel, sessionID string, cfg *router.Config) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "gateway_error", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	var attempts []string
	start := time.Now()
	chatID := "chatcmpl-" + shortID()
	for i, c := range cands {
		ad := registry.Get(c.Provider)
		creq := req
		creq.Model = c.Model
		uctx, cancel := context.WithTimeout(ctx, time.Duration(s.Config.MaxStreamSec)*time.Second)
		ch, uerr, _ := ad.StreamChat(uctx, c.APIKey, c.BaseURL, creq)
		if uerr != nil {
			cancel()
			router.ReportResult(ctx, s.DB, s.Tracker, c, uerr, 0)
			attempts = append(attempts, fmt.Sprintf("%s — %s", c.Provider, shortErr(uerr)))
			if !uerr.Retryable() {
				continue
			}
			continue
		}
		// stream first candidate that yields; pass-through deltas immediately (no buffering)
		w.Header().Set("X-OpenBridge-Provider", c.Provider)
		w.Header().Set("X-OpenBridge-Model", c.Model)
		sent := false
		failed := false
		for ev := range ch {
			if ev.Done {
				break
			}
			sent = true
			chunk := map[string]any{
				"id": chatID, "object": "chat.completion.chunk", "created": time.Now().Unix(),
				"model":   c.Model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": ev.Delta}, "finish_reason": nil}},
			}
			if ev.FinishReason != "" {
				chunk["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": ev.FinishReason}}
			}
			b, _ := json.Marshal(chunk)
			_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
			flusher.Flush()
			_ = failed
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
		cancel()
		ms := int(time.Since(start).Milliseconds())
		router.ReportResult(ctx, s.DB, s.Tracker, c, nil, float64(ms))
		analytics.Record(s.DB, c.Provider, c.Model, keyID, true, ms, 0, 0, i, "")
		if sessionID != "" {
			router.SetPin(ctx, s.DB, sessionID, c.Provider, c.Model, 30)
		}
		_ = sent
		return
	}
	// all failed: SSE error then close
	eb, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "All configured providers failed: " + strings.Join(attempts, "; "), "type": "gateway_error", "code": "provider_unavailable"}})
	_, _ = io.WriteString(w, "data: "+string(eb)+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
	ms := int(time.Since(start).Milliseconds())
	analytics.Record(s.DB, "", wantModel, keyID, false, ms, 0, 0, len(cands), strings.Join(attempts, "; "))
}

// POST /v1/responses (OpenAI Responses API -> mapped onto chat)
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	key, ok := s.requireGatewayKey(r)
	if !ok {
		writeError(w, 401, "unauthorized", "invalid or missing gateway API key")
		return
	}
	body, _ := io.ReadAll(r.Body)
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	// map input -> messages
	var msgs []any
	if inp, ok := raw["input"]; ok {
		switch v := inp.(type) {
		case string:
			msgs = []any{map[string]any{"role": "user", "content": v}}
		case []any:
			for _, item := range v {
				im, _ := item.(map[string]any)
				if im == nil {
					continue
				}
				role, _ := im["role"].(string)
				if role == "" {
					role = "user"
				}
				content := im["content"]
				msgs = append(msgs, map[string]any{"role": role, "content": content})
			}
		}
	}
	model, _ := raw["model"].(string)
	if model == "" {
		model = "auto"
	}
	chatBody := map[string]any{"model": model, "messages": msgs}
	for _, k := range []string{"temperature", "top_p", "max_output_tokens", "stream", "tools", "tool_choice"} {
		if v, ok := raw[k]; ok {
			if k == "max_output_tokens" {
				chatBody["max_tokens"] = v
			} else {
				chatBody[k] = v
			}
		}
	}
	need := router.AnalyzeNeeds(chatBody)
	req := toChatRequest(chatBody)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Config.RequestTimeoutSec)*time.Second)
	defer cancel()
	cands, _, err := router.BuildCandidates(ctx, s.DB, s.Tracker, model, need, "", s.decrypt)
	if err != nil || len(cands) == 0 {
		analytics.Record(s.DB, "", model, key.ID, false, 0, 0, 0, 0, "no healthy providers")
		writeError(w, 503, "provider_unavailable", "All configured providers are currently unavailable.")
		return
	}
	start := time.Now()
	for i, c := range cands {
		ad := registry.Get(c.Provider)
		creq := req
		creq.Model = c.Model
		uctx, cc := context.WithTimeout(ctx, time.Duration(s.Config.UpstreamTimeoutSec)*time.Second)
		resp, uerr, _ := ad.Chat(uctx, c.APIKey, c.BaseURL, creq)
		cc()
		if uerr == nil {
			router.ReportResult(ctx, s.DB, s.Tracker, c, nil, float64(time.Since(start).Milliseconds()))
			text := contentText(resp.Message.Content)
			analytics.Record(s.DB, c.Provider, c.Model, key.ID, true, int(time.Since(start).Milliseconds()), resp.Usage.PromptTokens, resp.Usage.CompletionTokens, i, "")
			writeJSON(w, 200, map[string]any{
				"id": "resp_" + shortID(), "object": "response", "created_at": time.Now().Unix(),
				"model": c.Model, "status": "completed",
				"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}},
				"usage":  map[string]any{"input_tokens": resp.Usage.PromptTokens, "output_tokens": resp.Usage.CompletionTokens, "total_tokens": resp.Usage.TotalTokens},
			})
			return
		}
		router.ReportResult(ctx, s.DB, s.Tracker, c, uerr, 0)
		if !uerr.Retryable() {
			continue
		}
	}
	writeError(w, 503, "provider_unavailable", "All configured providers are currently unavailable.")
}

// POST /v1/embeddings — never fail over across embedding families (PRD §25).
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	key, ok := s.requireGatewayKey(r)
	if !ok {
		writeError(w, 401, "unauthorized", "invalid or missing gateway API key")
		return
	}
	body, _ := io.ReadAll(r.Body)
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	model, _ := raw["model"].(string)
	if model == "" {
		writeError(w, 400, "invalid_request", "model is required")
		return
	}
	m := models.ByID(model)
	if m != nil && !m.SupportsEmbeddings {
		writeError(w, 400, "invalid_request", "model does not support embeddings")
		return
	}
	need := router.Need{Embeddings: true}
	req := providers.EmbedRequest{Model: model, Input: raw["input"]}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Config.RequestTimeoutSec)*time.Second)
	defer cancel()
	cands, _, err := router.BuildCandidates(ctx, s.DB, s.Tracker, model, need, "", s.decrypt)
	if err != nil || len(cands) == 0 {
		analytics.Record(s.DB, "", model, key.ID, false, 0, 0, 0, 0, "no embedding providers")
		writeError(w, 503, "provider_unavailable", "No embedding-capable provider is available for this model.")
		return
	}
	// restrict failover to same provider family for embeddings
	family := cands[0].Provider
	var same []router.Candidate
	for _, c := range cands {
		if c.Provider == family {
			same = append(same, c)
		}
	}
	start := time.Now()
	for i, c := range same {
		ad := registry.Get(c.Provider)
		er := providers.EmbedRequest{Model: c.Model, Input: req.Input}
		uctx, cc := context.WithTimeout(ctx, time.Duration(s.Config.UpstreamTimeoutSec)*time.Second)
		resp, uerr := ad.Embed(uctx, c.APIKey, c.BaseURL, er)
		cc()
		if uerr == nil {
			router.ReportResult(ctx, s.DB, s.Tracker, c, nil, float64(time.Since(start).Milliseconds()))
			analytics.Record(s.DB, c.Provider, c.Model, key.ID, true, int(time.Since(start).Milliseconds()), 0, 0, i, "")
			var data []any
			for _, d := range resp.Data {
				data = append(data, map[string]any{"object": "embedding", "index": d.Index, "embedding": d.Embedding})
			}
			writeJSON(w, 200, map[string]any{"object": "list", "data": data, "model": c.Model,
				"usage": map[string]any{"prompt_tokens": resp.Usage.PromptTokens, "total_tokens": resp.Usage.TotalTokens}})
			return
		}
		router.ReportResult(ctx, s.DB, s.Tracker, c, uerr, 0)
		if !uerr.Retryable() {
			continue
		}
	}
	writeError(w, 503, "provider_unavailable", "All embedding providers for this model family failed.")
}

// --- helpers ---

func toChatRequest(raw map[string]any) providers.ChatRequest {
	var req providers.ChatRequest
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &req)
	// max_completion_tokens alias
	if req.MaxCompletions == nil {
		if v, ok := raw["max_completion_tokens"].(float64); ok {
			n := int(v)
			req.MaxCompletions = &n
		}
	}
	if req.MaxTokens == nil {
		if v, ok := raw["max_tokens"].(float64); ok {
			n := int(v)
			req.MaxTokens = &n
		}
	}
	return req
}

func openAIChatResponse(resp *providers.ChatResponse, model string) map[string]any {
	if resp.ID == "" {
		resp.ID = "chatcmpl-" + shortID()
	}
	msg := map[string]any{"role": "assistant", "content": contentText(resp.Message.Content)}
	if len(resp.Message.ToolCalls) > 0 {
		msg["tool_calls"] = resp.Message.ToolCalls
	}
	return map[string]any{
		"id": resp.ID, "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": resp.Usage.PromptTokens, "completion_tokens": resp.Usage.CompletionTokens, "total_tokens": resp.Usage.TotalTokens},
	}
}

func contentText(c any) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, _ := json.Marshal(v)
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
		return string(b)
	}
}

func withHandoff(raw map[string]any) map[string]any {
	msgs, _ := raw["messages"].([]any)
	handoff := map[string]any{"role": "system", "content": "This conversation has been transferred from another model. Continue the task using the conversation context provided. Do not mention the transfer unless relevant."}
	raw["messages"] = append([]any{handoff}, msgs...)
	return raw
}

func shortModel(m string) string {
	if i := strings.LastIndex(m, "/"); i >= 0 {
		return m[i+1:]
	}
	return m
}

func shortErr(u *providers.UpstreamError) string {
	if u == nil {
		return "ok"
	}
	switch u.Code {
	case "rate_limited":
		return "rate limited"
	case "timeout":
		return "timeout"
	case "auth_error":
		return "authentication error"
	case "not_found":
		return "not found"
	case "connection":
		return "connection failed"
	default:
		return u.Code
	}
}
