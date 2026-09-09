// Package gateway implements the HTTP server: OpenAI-compatible public API,
// admin REST API and embedded dashboard.
package gateway

import (
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/openbridge/gateway/internal/config"
	"github.com/openbridge/gateway/internal/router"
)

//go:embed webdist
var webDist embed.FS

// Server is the gateway HTTP server.
type Server struct {
	DB      *sql.DB
	Config  *config.Config
	Master  []byte
	Tracker *router.Tracker
	Log     *Logger
	sem     chan struct{}
	mux     *http.ServeMux
}

// New creates a Server.
func New(db *sql.DB, cfg *config.Config, master []byte, log *Logger) *Server {
	s := &Server{
		DB: db, Config: cfg, Master: master,
		Tracker: router.NewTracker(), Log: log,
		sem: make(chan struct{}, cfg.MaxConcurrent),
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.withRecovery(s.withLimits(s.mux))
}

func (s *Server) routes() {
	m := s.mux
	// public
	m.HandleFunc("GET /health", s.handleHealth)
	m.HandleFunc("GET /v1/models", s.handleModels)
	m.HandleFunc("GET /v1/models/", s.handleModelOne)
	m.HandleFunc("POST /v1/chat/completions", s.handleChat)
	m.HandleFunc("POST /v1/responses", s.handleResponses)
	m.HandleFunc("POST /v1/embeddings", s.handleEmbeddings)
	m.HandleFunc("GET /v1/docs", s.handleDocs)
	m.HandleFunc("GET /v1/openapi.json", s.handleOpenAPI)
	m.HandleFunc("GET /metrics", s.handleMetrics)
	// admin (dashboard + cli use session or gateway key? admin needs session)
	m.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	m.HandleFunc("POST /api/admin/logout", s.handleAdminLogout)
	m.HandleFunc("GET /api/admin/status", s.requireAdmin(s.handleAdminStatus))
	m.HandleFunc("GET /api/providers", s.requireAdmin(s.handleProvidersList))
	m.HandleFunc("POST /api/providers", s.requireAdmin(s.handleProvidersCreate))
	m.HandleFunc("GET /api/providers/", s.requireAdmin(s.handleProviderOne))
	m.HandleFunc("PUT /api/providers/", s.requireAdmin(s.handleProviderUpdate))
	m.HandleFunc("DELETE /api/providers/", s.requireAdmin(s.handleProviderDelete))
	m.HandleFunc("POST /api/providers/test", s.requireAdmin(s.handleProviderTest))
	m.HandleFunc("GET /api/models", s.requireAdmin(s.handleAdminModels))
	m.HandleFunc("PUT /api/models/", s.requireAdmin(s.handleModelOverride))
	m.HandleFunc("POST /api/models/test", s.requireAdmin(s.handleModelTest))
	m.HandleFunc("GET /api/keys", s.requireAdmin(s.handleKeysList))
	m.HandleFunc("POST /api/keys", s.requireAdmin(s.handleKeysCreate))
	m.HandleFunc("DELETE /api/keys/", s.requireAdmin(s.handleKeysDelete))
	m.HandleFunc("GET /api/routing", s.requireAdmin(s.handleRoutingGet))
	m.HandleFunc("PUT /api/routing", s.requireAdmin(s.handleRoutingPut))
	m.HandleFunc("GET /api/analytics", s.requireAdmin(s.handleAnalytics))
	m.HandleFunc("GET /api/health/providers", s.requireAdmin(s.handleProvidersHealth))
	m.HandleFunc("GET /api/settings", s.requireAdmin(s.handleSettingsGet))
	m.HandleFunc("PUT /api/settings", s.requireAdmin(s.handleSettingsPut))
	m.HandleFunc("POST /api/playground", s.requireAdmin(s.handlePlayground))
	m.HandleFunc("GET /api/export", s.requireAdmin(s.handleExport))
	m.HandleFunc("POST /api/import", s.requireAdmin(s.handleImport))
	// dashboard static
	sub, _ := fs.Sub(webDist, "webdist")
	fileServer := http.FileServer(http.FS(sub))
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback to index.html
		b, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}

// concurrency + body-limit middleware
func (s *Server) withLimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		default:
			writeError(w, 429, "rate_limited", "too many concurrent requests")
			return
		}
		if r.ContentLength > s.Config.MaxBodyBytes {
			writeError(w, 413, "invalid_request", "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, s.Config.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.Log.Errorf("panic: %v", rec)
				writeError(w, 500, "gateway_error", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
