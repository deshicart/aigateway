// Shared helpers: logging, JSON, errors, auth extraction.
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openbridge/gateway/internal/auth"
)

// Logger is a safe logger (never logs secrets).
type Logger struct {
	mu    sync.Mutex
	level string
	out   *os.File
}

func NewLogger(level string) *Logger {
	return &Logger{level: strings.ToUpper(level), out: os.Stderr}
}

var lvlOrder = map[string]int{"ERROR": 0, "WARN": 1, "INFO": 2, "DEBUG": 3}

func (l *Logger) log(lvl, msg string, args ...any) {
	if lvlOrder[lvl] > lvlOrder[l.level] {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "[%s] %s %s\n", lvl, time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(msg, args...))
}

func (l *Logger) Infof(m string, a ...any)  { l.log("INFO", m, a...) }
func (l *Logger) Warnf(m string, a ...any)  { l.log("WARN", m, a...) }
func (l *Logger) Errorf(m string, a ...any) { l.log("ERROR", m, a...) }
func (l *Logger) Debugf(m string, a ...any) { l.log("DEBUG", m, a...) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"message": msg, "type": "gateway_error", "code": code},
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// requireGatewayKey authenticates /v1/* requests with a unified gateway key.
func (s *Server) requireGatewayKey(r *http.Request) (*auth.GatewayKey, bool) {
	tok := bearerToken(r)
	if tok == "" || !strings.HasPrefix(tok, "obg_") {
		return nil, false
	}
	k, ok := auth.ValidateGatewayKey(s.DB, tok)
	return k, ok
}

// requireAdmin authenticates /api/* dashboard requests via session cookie
// or (for CLI) the admin session token as bearer.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// cookie session
		if c, err := r.Cookie("obg_session"); err == nil && auth.ValidateSession(s.DB, c.Value) {
			next(w, r)
			return
		}
		// bearer session token (CLI)
		if tok := bearerToken(r); tok != "" && !strings.HasPrefix(tok, "obg_") && auth.ValidateSession(s.DB, tok) {
			next(w, r)
			return
		}
		writeError(w, 401, "unauthorized", "admin authentication required")
	}
}

func (s *Server) decrypt(enc string) (string, error) {
	return decryptWith(s.Master, enc)
}
