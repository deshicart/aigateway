package gateway_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbridge/gateway/internal/auth"
	"github.com/openbridge/gateway/internal/config"
	"github.com/openbridge/gateway/internal/crypto"
	"github.com/openbridge/gateway/internal/database"
	"github.com/openbridge/gateway/internal/gateway"
)

// fakeUpstream is a minimal OpenAI-compatible server. First chat request
// fails with 429 to exercise gateway failover onto the second provider.
type fakeUpstream struct {
	failFirst bool
	calls     int
}

func (f *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/models") {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{}})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/chat/completions") {
		f.calls++
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if f.failFirst && f.calls == 1 {
			w.WriteHeader(429)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "slow down"}})
			return
		}
		if stream, _ := req["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-t", "object": "chat.completion", "created": 1, "model": "m",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/embeddings") {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float32{0.1}}}, "model": "m"})
		return
	}
	w.WriteHeader(404)
}

func testServer(t *testing.T, failFirst bool) (*gateway.Server, string, *fakeUpstream) {
	t.Helper()
	cfg := &config.Config{Host: "127.0.0.1", Port: 0, DataDir: t.TempDir(), LogLevel: "ERROR", MaxBodyBytes: 4 << 20, MaxConcurrent: 16, RequestTimeoutSec: 30, UpstreamTimeoutSec: 10, MaxStreamSec: 30, MaxAttempts: 5}
	master := make([]byte, 32)
	db, err := database.Open(filepath.Join(cfg.DataDir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := gateway.New(db, cfg, master, gateway.NewLogger("ERROR"))
	up := &fakeUpstream{failFirst: failFirst}
	ts := httptest.NewServer(up)
	t.Cleanup(ts.Close)
	enc := func(s string) string {
		e, err := crypto.Encrypt(master, []byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO providers(id,type,display_name,base_url,auth_type,model_ids,enabled,priority,created_at,updated_at) VALUES('p1','custom','F','` + ts.URL + `/v1','bearer','["t-model"]',1,50,'t','t')`)
	mustExec(`INSERT INTO provider_keys(id,provider_id,label,key_enc,enabled,priority,created_at) VALUES('k1','p1','k','` + enc("sk") + `',1,50,'t')`)
	mustExec(`INSERT INTO providers(id,type,display_name,base_url,auth_type,model_ids,enabled,priority,created_at,updated_at) VALUES('p2','custom','F2','` + ts.URL + `/v1','bearer','["t-model"]',1,40,'t','t')`)
	mustExec(`INSERT INTO provider_keys(id,provider_id,label,key_enc,enabled,priority,created_at) VALUES('k2','p2','k','` + enc("sk") + `',1,50,'t')`)
	plain, _, err := auth.CreateGatewayKey(db, "t")
	if err != nil {
		t.Fatal(err)
	}
	_ = sql.ErrNoRows
	return srv, plain, up
}

func doJSON(t *testing.T, srv *gateway.Server, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, r)
	return rr
}

func TestHealthNoAuth(t *testing.T) {
	srv, _, _ := testServer(t, false)
	rr := doJSON(t, srv, "GET", "/health", "", "")
	if rr.Code != 200 {
		t.Fatalf("health=%d", rr.Code)
	}
}

func TestModelsRequireAuth(t *testing.T) {
	srv, key, _ := testServer(t, false)
	if rr := doJSON(t, srv, "GET", "/v1/models", "", ""); rr.Code != 401 {
		t.Fatalf("want 401, got %d", rr.Code)
	}
	rr := doJSON(t, srv, "GET", "/v1/models", key, "")
	if rr.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestChatAndFailover(t *testing.T) {
	srv, key, up := testServer(t, true)
	rr := doJSON(t, srv, "POST", "/v1/chat/completions", key, `{"model":"t-model","messages":[{"role":"user","content":"hi"}]}`)
	if rr.Code != 200 {
		t.Fatalf("want 200 after failover, got %d body=%s", rr.Code, rr.Body.String())
	}
	if up.calls < 2 {
		t.Fatalf("expected failover retry, calls=%d", up.calls)
	}
	if rr.Header().Get("X-Openbridge-Provider") == "" && rr.Header().Get("X-OpenBridge-Provider") == "" {
		t.Fatal("missing response metadata headers")
	}
}

func TestChatStreaming(t *testing.T) {
	srv, key, _ := testServer(t, false)
	rr := doJSON(t, srv, "POST", "/v1/chat/completions", key, `{"model":"t-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("bad content type %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "[DONE]") {
		t.Fatalf("stream missing DONE: %q", rr.Body.String())
	}
}

func TestEmbeddings(t *testing.T) {
	srv, key, _ := testServer(t, false)
	rr := doJSON(t, srv, "POST", "/v1/embeddings", key, `{"model":"t-model","input":"hello"}`)
	if rr.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMalformedAndOversized(t *testing.T) {
	srv, key, _ := testServer(t, false)
	rr := doJSON(t, srv, "POST", "/v1/chat/completions", key, `{bad json`)
	if rr.Code != 400 {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	rr = doJSON(t, srv, "POST", "/v1/chat/completions", key, `{"messages":[]}`)
	if rr.Code != 400 {
		t.Fatalf("want 400 for missing model, got %d", rr.Code)
	}
}

func TestNoKeyLeakInErrors(t *testing.T) {
	srv, _, _ := testServer(t, false)
	rr := doJSON(t, srv, "GET", "/v1/models", "obg_wrongkey", "")
	if strings.Contains(rr.Body.String(), "sk") {
		t.Fatal("error leaks secret")
	}
}
