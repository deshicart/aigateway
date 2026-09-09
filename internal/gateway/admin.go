package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openbridge/gateway/internal/analytics"
	"github.com/openbridge/gateway/internal/auth"
	"github.com/openbridge/gateway/internal/crypto"
	registry "github.com/openbridge/gateway/internal/providers/registry"
	"github.com/openbridge/gateway/internal/router"
	"github.com/openbridge/gateway/internal/security"
)

// GET /api/admin/status — overview numbers for dashboard
func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	var provTotal, provEnabled, keyTotal, gwKeys int
	_ = s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(enabled),0) FROM providers`).Scan(&provTotal, &provEnabled)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM provider_keys WHERE enabled=1`).Scan(&keyTotal)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM gateway_keys`).Scan(&gwKeys)
	sum := analytics.Summarize(s.DB, 24*30)
	// unhealthy count
	var unhealthy int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM provider_keys WHERE health IN ('rate_limited','invalid','timeout','error')`).Scan(&unhealthy)
	endpoint := "http://" + s.Config.Addr() + "/v1"
	var firstKey string
	_ = s.DB.QueryRow(`SELECT key_prefix FROM gateway_keys ORDER BY created_at LIMIT 1`).Scan(&firstKey)
	masked := ""
	if firstKey != "" {
		masked = firstKey + "••••••••"
	}
	writeJSON(w, 200, map[string]any{
		"status": "running", "version": Version, "endpoint": endpoint,
		"api_key_masked": masked,
		"providers":      map[string]any{"connected": provEnabled, "total": provTotal, "unhealthy": unhealthy, "keys": keyTotal},
		"gateway_keys":   gwKeys,
		"requests":       sum.Total, "success_rate": sum.SuccessRate, "avg_latency_ms": sum.AvgLatencyMs,
		"input_tokens": sum.InputTokens, "output_tokens": sum.OutputTokens, "fallbacks": sum.Fallbacks,
	})
}

// --- providers CRUD ---

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(`SELECT id,type,display_name,base_url,auth_type,account_id,model_ids,enabled,priority FROM providers ORDER BY priority DESC`)
	if err != nil {
		writeError(w, 500, "gateway_error", "db error")
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, typ, name, base, authT, acct, mids string
		var en, pri int
		if err := rows.Scan(&id, &typ, &name, &base, &authT, &acct, &mids, &en, &pri); err != nil {
			continue
		}
		var keyCount int
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM provider_keys WHERE provider_id=?`, id).Scan(&keyCount)
		var health string
		_ = s.DB.QueryRow(`SELECT health FROM provider_keys WHERE provider_id=? ORDER BY CASE health WHEN 'healthy' THEN 0 ELSE 1 END LIMIT 1`, id).Scan(&health)
		if health == "" {
			health = "unknown"
		}
		var ids []string
		_ = json.Unmarshal([]byte(mids), &ids)
		out = append(out, map[string]any{
			"id": id, "type": typ, "display_name": name, "base_url": base,
			"auth_type": authT, "account_id": acct, "model_ids": ids,
			"enabled": en == 1, "priority": pri, "keys": keyCount, "health": health,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"providers": out})
}

type providerPayload struct {
	Type         string            `json:"type"`
	DisplayName  string            `json:"display_name"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"api_key"`
	APIKeys      []string          `json:"api_keys"`
	AuthType     string            `json:"auth_type"`
	ExtraHeaders map[string]string `json:"extra_headers"`
	AccountID    string            `json:"account_id"`
	ModelIDs     []string          `json:"model_ids"`
	Enabled      *bool             `json:"enabled"`
	Priority     *int              `json:"priority"`
}

func (s *Server) handleProvidersCreate(w http.ResponseWriter, r *http.Request) {
	var p providerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	if p.Type == "" {
		writeError(w, 400, "invalid_request", "provider type is required")
		return
	}
	base := strings.TrimSpace(p.BaseURL)
	if base == "" {
		base = registry.DefaultBaseURL(p.Type)
	}
	if p.Type == "custom" || p.Type == "openai" || p.Type == "ollama" || base != "" {
		if base != "" {
			v, err := security.ValidateProviderURL(base, true)
			if err != nil {
				writeError(w, 400, "invalid_request", "invalid base URL: "+err.Error())
				return
			}
			base = v
		}
	}
	if p.AuthType == "" {
		p.AuthType = "bearer"
	}
	keys := p.APIKeys
	if p.APIKey != "" {
		keys = append(keys, p.APIKey)
	}
	id := newID()
	en := 1
	if p.Enabled != nil && !*p.Enabled {
		en = 0
	}
	pri := 50
	if p.Priority != nil {
		pri = *p.Priority
	}
	mids, _ := json.Marshal(p.ModelIDs)
	if string(mids) == "null" {
		mids = []byte("[]")
	}
	eh, _ := json.Marshal(p.ExtraHeaders)
	if string(eh) == "null" {
		eh = []byte("{}")
	}
	now := nowStr()
	if _, err := s.DB.Exec(`INSERT INTO providers(id,type,display_name,base_url,auth_type,extra_headers,account_id,model_ids,enabled,priority,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, p.Type, p.DisplayName, base, p.AuthType, string(eh), p.AccountID, string(mids), en, pri, now, now); err != nil {
		writeError(w, 500, "gateway_error", "cannot save provider")
		return
	}
	for i, k := range keys {
		if strings.TrimSpace(k) == "" {
			continue
		}
		enc, err := crypto.Encrypt(s.Master, []byte(strings.TrimSpace(k)))
		if err != nil {
			continue
		}
		_, _ = s.DB.Exec(`INSERT INTO provider_keys(id,provider_id,label,key_enc,enabled,priority,created_at) VALUES(?,?,?,?,?,?,?)`,
			newID(), id, labelFor(i), enc, 1, 50, now)
	}
	s.Log.Infof("provider added: %s (%s)", p.Type, id)
	writeJSON(w, 201, map[string]any{"id": id})
}

func labelFor(i int) string { return map[int]string{0: "key-1", 1: "key-2", 2: "key-3"}[i%3] + "" }

func providerIDFrom(r *http.Request) string {
	p := strings.TrimPrefix(r.URL.Path, "/api/providers/")
	if i := strings.Index(p, "/"); i >= 0 {
		p = p[:i]
	}
	return p
}

func (s *Server) handleProviderOne(w http.ResponseWriter, r *http.Request) {
	id := providerIDFrom(r)
	var typ, name, base, authT, eh, acct, mids, created, updated string
	var en, pri int
	if err := s.DB.QueryRow(`SELECT type,display_name,base_url,auth_type,extra_headers,account_id,model_ids,enabled,priority,created_at,updated_at FROM providers WHERE id=?`, id).
		Scan(&typ, &name, &base, &authT, &eh, &acct, &mids, &en, &pri, &created, &updated); err != nil {
		writeError(w, 404, "not_found", "provider not found")
		return
	}
	krows, _ := s.DB.Query(`SELECT id,label,enabled,priority,health,cooldown_until,last_check_at,last_error,created_at FROM provider_keys WHERE provider_id=?`, id)
	var keys []map[string]any
	if krows != nil {
		defer krows.Close()
		for krows.Next() {
			var kid, label, health, cd, lc, le, cr string
			var ken, kpri int
			if err := krows.Scan(&kid, &label, &ken, &kpri, &health, &cd, &lc, &le, &cr); err == nil {
				keys = append(keys, map[string]any{"id": kid, "label": label, "enabled": ken == 1, "priority": kpri, "health": health, "cooldown_until": cd, "last_check_at": lc, "last_error": le, "created_at": cr})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"id": id, "type": typ, "display_name": name, "base_url": base, "auth_type": authT, "account_id": acct, "model_ids": jsonRaw(mids), "enabled": en == 1, "priority": pri, "keys": keys})
}

func (s *Server) handleProviderUpdate(w http.ResponseWriter, r *http.Request) {
	id := providerIDFrom(r)
	var p providerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	sets := []string{}
	args := []any{}
	if p.DisplayName != "" {
		sets = append(sets, "display_name=?")
		args = append(args, p.DisplayName)
	}
	if p.BaseURL != "" {
		v, err := security.ValidateProviderURL(p.BaseURL, true)
		if err != nil {
			writeError(w, 400, "invalid_request", "invalid base URL: "+err.Error())
			return
		}
		sets = append(sets, "base_url=?")
		args = append(args, v)
	}
	if p.Enabled != nil {
		en := 0
		if *p.Enabled {
			en = 1
		}
		sets = append(sets, "enabled=?")
		args = append(args, en)
	}
	if p.Priority != nil {
		sets = append(sets, "priority=?")
		args = append(args, *p.Priority)
	}
	if p.AccountID != "" {
		sets = append(sets, "account_id=?")
		args = append(args, p.AccountID)
	}
	if p.ModelIDs != nil {
		mids, _ := json.Marshal(p.ModelIDs)
		sets = append(sets, "model_ids=?")
		args = append(args, string(mids))
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at=?")
		args = append(args, nowStr(), id)
		_, _ = s.DB.Exec(`UPDATE providers SET `+strings.Join(sets, ",")+` WHERE id=?`, args...)
	}
	// add new keys if supplied
	keys := p.APIKeys
	if p.APIKey != "" {
		keys = append(keys, p.APIKey)
	}
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			continue
		}
		enc, err := crypto.Encrypt(s.Master, []byte(strings.TrimSpace(k)))
		if err != nil {
			continue
		}
		_, _ = s.DB.Exec(`INSERT INTO provider_keys(id,provider_id,label,key_enc,enabled,priority,created_at) VALUES(?,?,?,?,?,?,?)`, newID(), id, "key", enc, 1, 50, nowStr())
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleProviderDelete(w http.ResponseWriter, r *http.Request) {
	id := providerIDFrom(r)
	_, _ = s.DB.Exec(`DELETE FROM provider_keys WHERE provider_id=?`, id)
	_, _ = s.DB.Exec(`DELETE FROM providers WHERE id=?`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/providers/test {type, base_url, api_key}
func (s *Server) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Type    string `json:"type"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	base := p.BaseURL
	if base == "" {
		base = registry.DefaultBaseURL(p.Type)
	}
	ad := registry.Get(p.Type)
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	st := ad.HealthCheck(ctx, p.APIKey, base)
	writeJSON(w, 200, map[string]any{"state": st.State, "message": st.Message, "ok": st.State == "healthy"})
}

// --- models admin ---

func (s *Server) handleAdminModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"models": catalogWithOverrides(s)})
}

func (s *Server) handleModelOverride(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/models/")
	var p struct {
		Enabled  *bool `json:"enabled"`
		Priority *int  `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	en := 1
	pr := 50
	var curEn, curPr int
	if err := s.DB.QueryRow(`SELECT enabled,priority FROM model_overrides WHERE model_id=?`, id).Scan(&curEn, &curPr); err == nil {
		en, pr = curEn, curPr
	}
	if p.Enabled != nil {
		en = 0
		if *p.Enabled {
			en = 1
		}
	}
	if p.Priority != nil {
		pr = *p.Priority
	}
	_, _ = s.DB.Exec(`INSERT INTO model_overrides(model_id,enabled,priority) VALUES(?,?,?) ON CONFLICT(model_id) DO UPDATE SET enabled=excluded.enabled,priority=excluded.priority`, id, en, pr)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/models/test {model}
func (s *Server) handleModelTest(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Model == "" {
		writeError(w, 400, "invalid_request", "model is required")
		return
	}
	if err := security.ValidateModelID(p.Model); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "note": "use Playground Send for a full end-to-end test"})
}

// --- gateway keys ---

func (s *Server) handleKeysList(w http.ResponseWriter, r *http.Request) {
	keys, _ := auth.ListGatewayKeys(s.DB)
	var out []map[string]any
	for _, k := range keys {
		out = append(out, map[string]any{"id": k.ID, "name": k.Name, "prefix": k.KeyPrefix, "created_at": k.CreatedAt, "last_used_at": k.LastUsedAt})
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"keys": out})
}

func (s *Server) handleKeysCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)
	if strings.TrimSpace(p.Name) == "" {
		p.Name = "My Application"
	}
	plain, rec, err := auth.CreateGatewayKey(s.DB, p.Name)
	if err != nil {
		writeError(w, 500, "gateway_error", "cannot create key")
		return
	}
	writeJSON(w, 201, map[string]any{"id": rec.ID, "name": rec.Name, "key": plain, "prefix": rec.KeyPrefix, "created_at": rec.CreatedAt})
}

func (s *Server) handleKeysDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/keys/")
	_, _ = s.DB.Exec(`DELETE FROM gateway_keys WHERE id=?`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- routing ---

func (s *Server) handleRoutingGet(w http.ResponseWriter, r *http.Request) {
	var strategy string
	var max, sticky, minutes, handoff int
	_ = s.DB.QueryRow(`SELECT strategy,max_attempts,sticky_sessions,sticky_minutes,context_handoff FROM routing_config WHERE id=1`).Scan(&strategy, &max, &sticky, &minutes, &handoff)
	writeJSON(w, 200, map[string]any{"strategy": strategy, "max_attempts": max, "sticky_sessions": sticky == 1, "sticky_minutes": minutes, "context_handoff": handoff == 1})
}

func (s *Server) handleRoutingPut(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Strategy       string `json:"strategy"`
		MaxAttempts    *int   `json:"max_attempts"`
		StickySessions *bool  `json:"sticky_sessions"`
		StickyMinutes  *int   `json:"sticky_minutes"`
		ContextHandoff *bool  `json:"context_handoff"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	var strategy string
	var max, sticky, minutes, handoff int
	_ = s.DB.QueryRow(`SELECT strategy,max_attempts,sticky_sessions,sticky_minutes,context_handoff FROM routing_config WHERE id=1`).Scan(&strategy, &max, &sticky, &minutes, &handoff)
	if p.Strategy != "" {
		switch p.Strategy {
		case "auto", "priority", "fastest", "cheapest", "balanced":
			strategy = p.Strategy
		default:
			writeError(w, 400, "invalid_request", "unknown strategy")
			return
		}
	}
	if p.MaxAttempts != nil {
		max = *p.MaxAttempts
		if max < 1 {
			max = 1
		}
		if max > 20 {
			max = 20
		}
	}
	if p.StickySessions != nil {
		sticky = 0
		if *p.StickySessions {
			sticky = 1
		}
	}
	if p.StickyMinutes != nil {
		minutes = *p.StickyMinutes
	}
	if p.ContextHandoff != nil {
		handoff = 0
		if *p.ContextHandoff {
			handoff = 1
		}
	}
	_, _ = s.DB.Exec(`UPDATE routing_config SET strategy=?,max_attempts=?,sticky_sessions=?,sticky_minutes=?,context_handoff=? WHERE id=1`, strategy, max, sticky, minutes, handoff)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- analytics / health / settings ---

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	hours := 24 * 7
	if v := r.URL.Query().Get("hours"); v != "" {
		var n int
		if _, err := parseInt(v, &n); err == nil && n > 0 && n <= 24*90 {
			hours = n
		}
	}
	sum := analytics.Summarize(s.DB, hours)
	writeJSON(w, 200, sum)
}

func (s *Server) handleProvidersHealth(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.DB.Query(`SELECT pk.id,p.type,pk.label,pk.health,pk.cooldown_until,pk.last_check_at,pk.last_error FROM provider_keys pk JOIN providers p ON p.id=pk.provider_id`)
	var out []map[string]any
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id, typ, label, health, cd, lc, le string
			if err := rows.Scan(&id, &typ, &label, &health, &cd, &lc, &le); err == nil {
				out = append(out, map[string]any{"key_id": id, "provider": typ, "label": label, "health": health, "cooldown_until": cd, "last_check_at": lc, "last_error": le})
			}
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"keys": out})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.DB.Query(`SELECT key,value FROM settings`)
	m := map[string]string{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err == nil {
				// never expose secrets/hashes to the dashboard payload
				if k == "admin_pass_hash" || k == "admin_user" {
					continue
				}
				m[k] = v
			}
		}
	}
	m["low_resource"] = boolStr(s.Config.LowResource)
	m["host"] = s.Config.Host
	m["port"] = intStr(s.Config.Port)
	writeJSON(w, 200, map[string]any{"settings": m})
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var m map[string]string
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	allow := map[string]bool{"theme": true, "dashboard_user": true}
	for k, v := range m {
		if allow[k] {
			auth.SetSetting(s.DB, k, v)
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/playground {model, system, user, temperature}
func (s *Server) handlePlayground(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Model       string   `json:"model"`
		System      string   `json:"system"`
		User        string   `json:"user"`
		Temperature *float64 `json:"temperature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || strings.TrimSpace(p.User) == "" {
		writeError(w, 400, "invalid_request", "user message is required")
		return
	}
	if p.Model == "" {
		p.Model = "auto"
	}
	msgs := []any{}
	if p.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": p.System})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": p.User})
	chatBody := map[string]any{"model": p.Model, "messages": msgs}
	if p.Temperature != nil {
		chatBody["temperature"] = *p.Temperature
	}
	req := toChatRequest(chatBody)
	need := router.AnalyzeNeeds(chatBody)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Config.RequestTimeoutSec)*time.Second)
	defer cancel()
	cands, _, err := router.BuildCandidates(ctx, s.DB, s.Tracker, p.Model, need, "", s.decrypt)
	if err != nil || len(cands) == 0 {
		writeError(w, 503, "provider_unavailable", "No healthy providers available.")
		return
	}
	start := time.Now()
	var attempts []string
	for i, c := range cands {
		ad := registry.Get(c.Provider)
		creq := req
		creq.Model = c.Model
		uctx, cc := context.WithTimeout(ctx, time.Duration(s.Config.UpstreamTimeoutSec)*time.Second)
		resp, uerr, _ := ad.Chat(uctx, c.APIKey, c.BaseURL, creq)
		lat := time.Since(start)
		cc()
		if uerr == nil {
			router.ReportResult(ctx, s.DB, s.Tracker, c, nil, float64(lat.Milliseconds()))
			writeJSON(w, 200, map[string]any{
				"response": contentText(resp.Message.Content), "provider": c.Provider, "model": c.Model,
				"latency": lat.String(), "latency_ms": int(lat.Milliseconds()),
				"tokens":    map[string]any{"prompt": resp.Usage.PromptTokens, "completion": resp.Usage.CompletionTokens, "total": resp.Usage.TotalTokens},
				"fallbacks": i,
			})
			return
		}
		router.ReportResult(ctx, s.DB, s.Tracker, c, uerr, 0)
		attempts = append(attempts, c.Provider+" — "+shortErr(uerr))
		if !uerr.Retryable() {
			continue
		}
	}
	writeJSON(w, 503, map[string]any{"ok": false, "attempts": attempts})
}

// --- export/import ---

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	prows, _ := s.DB.Query(`SELECT id,type,display_name,base_url,auth_type,account_id,model_ids,enabled,priority FROM providers`)
	var provs []map[string]any
	if prows != nil {
		defer prows.Close()
		for prows.Next() {
			var id, typ, name, base, authT, acct, mids string
			var en, pri int
			if err := prows.Scan(&id, &typ, &name, &base, &authT, &acct, &mids, &en, &pri); err == nil {
				provs = append(provs, map[string]any{"type": typ, "display_name": name, "base_url": base, "auth_type": authT, "account_id": acct, "model_ids": jsonRaw(mids), "enabled": en == 1, "priority": pri})
			}
		}
	}
	var strategy string
	var max, sticky, minutes, handoff int
	_ = s.DB.QueryRow(`SELECT strategy,max_attempts,sticky_sessions,sticky_minutes,context_handoff FROM routing_config WHERE id=1`).Scan(&strategy, &max, &sticky, &minutes, &handoff)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=openbridge-config.json")
	writeJSON(w, 200, map[string]any{"version": 1, "exported_at": nowStr(), "providers": provs,
		"routing": map[string]any{"strategy": strategy, "max_attempts": max, "sticky_sessions": sticky == 1, "sticky_minutes": minutes},
		"note":    "provider API keys are NOT included in plaintext export"})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var doc struct {
		Providers []providerPayload `json:"providers"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	count := 0
	for _, p := range doc.Providers {
		if p.Type == "" {
			continue
		}
		mids, _ := json.Marshal(p.ModelIDs)
		now := nowStr()
		_, err := s.DB.Exec(`INSERT INTO providers(id,type,display_name,base_url,auth_type,model_ids,enabled,priority,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			newID(), p.Type, p.DisplayName, p.BaseURL, "bearer", string(mids), 1, 50, now, now)
		if err == nil {
			count++
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "imported": count})
}

// --- small utils ---

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nowStr() string { return time.Now().UTC().Format("2006-01-02T15:04:05.999Z07:00") }

func jsonRaw(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return []string{}
	}
	return v
}

func parseInt(s string, out *int) (int, error) {
	var n int
	_, err := parseSScanf(s, &n)
	*out = n
	return n, err
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intStr(n int) string { return itoa(n) }

var _ = crypto.Redact
