package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openbridge/gateway/internal/auth"
	"github.com/openbridge/gateway/internal/crypto"
	"github.com/openbridge/gateway/internal/models"
)

// Version is the gateway version (PRD §70).
const Version = "1.0.0"

// POST /api/admin/login {username, password}
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid_request", "malformed JSON")
		return
	}
	// first-run: no password set -> set it
	stored := auth.GetSetting(s.DB, "admin_pass_hash")
	if stored == "" {
		if p.Password == "" {
			writeError(w, 400, "invalid_request", "password is required to initialize admin account")
			return
		}
		h, err := crypto.HashPassword(p.Password)
		if err != nil {
			writeError(w, 500, "gateway_error", "cannot set password")
			return
		}
		auth.SetSetting(s.DB, "admin_pass_hash", h)
		if p.Username != "" {
			auth.SetSetting(s.DB, "admin_user", p.Username)
		}
		stored = h
	}
	wantUser := auth.GetSetting(s.DB, "admin_user")
	if wantUser == "" {
		wantUser = s.Config.AdminUser
	}
	if subtleNE(p.Username, wantUser) || !crypto.VerifyPassword(p.Password, stored) {
		// constant-ish delay against brute force
		time.Sleep(400 * time.Millisecond)
		writeError(w, 401, "unauthorized", "invalid credentials")
		return
	}
	tok, err := auth.CreateSession(s.DB, 24*time.Hour)
	if err != nil {
		writeError(w, 500, "gateway_error", "cannot create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "obg_session", Value: tok, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(24 * time.Hour),
	})
	writeJSON(w, 200, map[string]any{"ok": true, "token": tok, "first_run": false})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "obg_session", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func subtleNE(a, b string) bool {
	if len(a) != len(b) {
		return true
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v != 0
}

// GET /metrics (lightweight; requires admin or localhost)
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	allowed := false
	if c, err := r.Cookie("obg_session"); err == nil && auth.ValidateSession(s.DB, c.Value) {
		allowed = true
	}
	if host := strings.Split(r.RemoteAddr, ":")[0]; host == "127.0.0.1" || host == "::1" {
		allowed = true
	}
	if !allowed {
		writeError(w, 401, "unauthorized", "metrics require local access or admin session")
		return
	}
	var total, succ int
	var avg sql_NullFloat
	_ = total
	_ = succ
	_ = avg
	row := s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success),0), COALESCE(AVG(latency_ms),0) FROM usage_stats`)
	var t, sc int
	var a float64
	_ = row.Scan(&t, &sc, &a)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "# HELP openbridge_requests_total Total gateway requests\n# TYPE openbridge_requests_total counter\nopenbridge_requests_total %d\n", t)
	fmt.Fprintf(w, "# HELP openbridge_requests_success Successful gateway requests\n# TYPE openbridge_requests_success counter\nopenbridge_requests_success %d\n", sc)
	fmt.Fprintf(w, "# HELP openbridge_latency_avg_ms Average latency ms\n# TYPE openbridge_latency_avg_ms gauge\nopenbridge_latency_avg_ms %.2f\n", a)
	fmt.Fprintf(w, "openbridge_version_info{version=\"%s\"} 1\n", Version)
}

type sql_NullFloat struct{}

// GET /v1/docs — minimal HTML docs
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OpenBridge API Docs</title>
<style>body{font-family:system-ui,sans-serif;max-width:760px;margin:2rem auto;padding:0 1rem;color:#111}pre{background:#f4f4f4;padding:1rem;overflow:auto}code{background:#f4f4f4}</style></head><body>
<h1>OpenBridge Gateway API</h1>
<p>Base URL: <code>/v1</code>. Authenticate with <code>Authorization: Bearer obg_...</code>.</p>
<h2>Endpoints</h2>
<ul><li><code>GET /v1/models</code></li><li><code>POST /v1/chat/completions</code></li><li><code>POST /v1/responses</code></li><li><code>POST /v1/embeddings</code></li><li><code>GET /health</code></li></ul>
<h2>Example</h2>
<pre>curl http://localhost:8787/v1/chat/completions -H "Authorization: Bearer obg_your_key" -H "Content-Type: application/json" -d '{"model":"auto","messages":[{"role":"user","content":"Hello"}]}'</pre>
<p>Full schema: <a href="/v1/openapi.json">/v1/openapi.json</a></p>
</body></html>`))
}

// GET /v1/openapi.json
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "OpenBridge Gateway", "version": Version},
		"servers": []any{map[string]any{"url": "/v1"}},
		"paths": map[string]any{
			"/chat/completions": map[string]any{"post": map[string]any{"summary": "Chat completions (OpenAI-compatible + auto routing)"}},
			"/responses":        map[string]any{"post": map[string]any{"summary": "Responses API"}},
			"/embeddings":       map[string]any{"post": map[string]any{"summary": "Embeddings"}},
			"/models":           map[string]any{"get": map[string]any{"summary": "List models"}},
		},
	})
}

func catalogWithOverrides(s *Server) []map[string]any {
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
	var out []map[string]any
	for _, m := range models.Catalog() {
		en, pr := m.Enabled, m.Priority
		if ov, ok := overrides[m.ID]; ok {
			en = ov[0] == 1
			pr = ov[1]
		}
		b, _ := json.Marshal(m)
		var mm map[string]any
		_ = json.Unmarshal(b, &mm)
		mm["enabled"] = en
		mm["priority"] = pr
		out = append(out, mm)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func shortID() string {
	return strconv.FormatInt(time.Now().UnixNano()%1294967295, 36)
}

func parseSScanf(s string, n *int) (int, error) {
	return fmt.Sscanf(strings.TrimSpace(s), "%d", n)
}

func itoa(n int) string { return strconv.Itoa(n) }
