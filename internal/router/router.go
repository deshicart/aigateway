// Package router implements model resolution, routing strategies,
// automatic failover, health/cooldown and rate-limit tracking (PRD §14-21).
package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openbridge/gateway/internal/models"
	"github.com/openbridge/gateway/internal/providers"
)

func jsonUnmarshalStrs(s string, out *[]string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), out)
}

// catalogHas reports whether id is a known catalog id (full or bare suffix).
func catalogHas(id string) bool {
	for _, m := range models.Catalog() {
		if m.ID == id || suffixAfterFirst(m.ID) == id || suffixAfterFirst(m.ID) == suffixAfterFirst(id) {
			return true
		}
	}
	return false
}

// Candidate is one provider/key/model combination to try.
type Candidate struct {
	Provider   string
	ProviderID string
	KeyID      string
	APIKey     string // decrypted
	BaseURL    string
	Model      string // full catalog id e.g. groq/llama-3.3-70b-versatile
	Priority   int
	LatencyEMA float64
	PriceTier  int
}

// Config mirrors routing_config row.
type Config struct {
	Strategy       string
	MaxAttempts    int
	StickySessions bool
	StickyMinutes  int
	ContextHandoff bool
}

// Tracker holds in-memory health, latency and rate state (low-RAM friendly).
type Tracker struct {
	mu        sync.Mutex
	cooldown  map[string]time.Time // keyID -> resume time
	latency   map[string]float64   // provider/model -> EMA ms
	failCount map[string]int
	rr        map[string]int // provider -> round-robin cursor
	lastUsed  map[string]time.Time
}

func NewTracker() *Tracker {
	return &Tracker{
		cooldown:  map[string]time.Time{},
		latency:   map[string]float64{},
		failCount: map[string]int{},
		rr:        map[string]int{},
		lastUsed:  map[string]time.Time{},
	}
}

func (t *Tracker) Cooldown(keyID string, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cooldown[keyID] = time.Now().Add(d)
	t.failCount[keyID]++
}

func (t *Tracker) InCooldown(keyID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	until, ok := t.cooldown[keyID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(t.cooldown, keyID)
		return false
	}
	return true
}

func (t *Tracker) Success(keyID, pm string, ms float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cooldown, keyID)
	t.failCount[keyID] = 0
	t.lastUsed[keyID] = time.Now()
	ema, ok := t.latency[pm]
	if !ok {
		t.latency[pm] = ms
	} else {
		t.latency[pm] = ema*0.7 + ms*0.3
	}
}

func (t *Tracker) Latency(pm string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.latency[pm]
}

// ResolveRequest analyses an OpenAI-style request body for capability needs.
type Need struct {
	Vision     bool
	Tools      bool
	Embeddings bool
	JSON       bool
	Stream     bool
}

func AnalyzeNeeds(body map[string]any) Need {
	var n Need
	if s, _ := body["stream"].(bool); s {
		n.Stream = true
	}
	if _, ok := body["tools"]; ok {
		n.Tools = true
	}
	if rf, ok := body["response_format"].(map[string]any); ok {
		if typ, _ := rf["type"].(string); typ == "json_object" || typ == "json_schema" {
			n.JSON = true
		}
	}
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		switch c := mm["content"].(type) {
		case []any:
			for _, p := range c {
				pm, _ := p.(map[string]any)
				if pm == nil {
					continue
				}
				if pm["type"] == "image_url" {
					n.Vision = true
				}
			}
		}
	}
	return n
}

// BuildCandidates queries DB + static catalog and returns ordered candidates.
func BuildCandidates(ctx context.Context, db *sql.DB, tr *Tracker, model string, need Need, strategy string, decrypt func(enc string) (string, error)) ([]Candidate, *Config, error) {
	cfg := loadConfig(ctx, db)
	if strategy == "" {
		strategy = cfg.Strategy
	}
	// normalize "auto:fast" etc.
	baseModel := model
	sub := ""
	if i := strings.Index(model, ":"); strings.HasPrefix(model, "auto") && i >= 0 {
		baseModel = model[:i]
		sub = model[i+1:]
		if sub != "" {
			strategy = sub
		}
	}

	rows, err := db.QueryContext(ctx, `SELECT id,type,base_url,enabled,priority,model_ids FROM providers WHERE enabled=1`)
	if err != nil {
		return nil, cfg, err
	}
	defer rows.Close()
	type prov struct {
		id, typ, base string
		pri           int
		modelIDs      []string
	}
	var provs []prov
	for rows.Next() {
		var p prov
		var en int
		var mids string
		if err := rows.Scan(&p.id, &p.typ, &p.base, &en, &p.pri, &mids); err == nil {
			_ = jsonUnmarshalStrs(mids, &p.modelIDs)
			provs = append(provs, p)
		}
	}

	// model overrides (enabled/priority)
	overrides := map[string][2]int{}
	orows, _ := db.QueryContext(ctx, `SELECT model_id,enabled,priority FROM model_overrides`)
	if orows != nil {
		defer orows.Close()
		for orows.Next() {
			var id string
			var en, pr int
			if err := orows.Scan(&id, &en, &pr); err == nil {
				overrides[id] = [2]int{en, pr}
			}
		}
	}

	var cands []Candidate
	for _, p := range provs {
		krows, err := db.QueryContext(ctx, `SELECT id,key_enc,enabled,priority,health,cooldown_until FROM provider_keys WHERE provider_id=? AND enabled=1`, p.id)
		if err != nil {
			continue
		}
		var keys []struct {
			id, enc, health, cooldown string
			pri                       int
		}
		for krows.Next() {
			var k struct {
				id, enc, health, cooldown string
				pri                       int
			}
			var en int
			if err := krows.Scan(&k.id, &k.enc, &en, &k.pri, &k.health, &k.cooldown); err == nil {
				keys = append(keys, k)
			}
		}
		krows.Close()
		for _, k := range keys {
			if k.health == "disabled" || k.health == "invalid" {
				continue
			}
			if tr.InCooldown(k.id) {
				continue
			}
			if k.cooldown != "" {
				if t, err := time.Parse(time.RFC3339Nano, k.cooldown); err == nil && time.Now().Before(t) {
					continue
				}
			}
			plain, err := decrypt(k.enc)
			if err != nil {
				continue
			}
			// enumerate models for this provider
			for _, m := range models.Catalog() {
				if m.Provider != providerGroup(p.typ) {
					continue
				}
				// capability filter (smart selection, PRD §61)
				if need.Embeddings && !m.SupportsEmbeddings {
					continue
				}
				if !need.Embeddings && m.SupportsEmbeddings && baseModel != m.ID && !strings.HasPrefix(baseModel, "auto") {
					// never fail over chat to an embedding model
					continue
				}
				if need.Vision && !m.SupportsVision {
					continue
				}
				if need.Tools && !m.SupportsTools {
					continue
				}
				// model filter
				if !modelMatches(baseModel, m, p.typ) {
					continue
				}
				if ov, ok := overrides[m.ID]; ok {
					if ov[0] == 0 {
						continue
					}
				}
				pri := m.Priority + p.pri + k.pri
				if ov, ok := overrides[m.ID]; ok {
					pri = ov[1] + p.pri + k.pri
				}
				pm := p.typ + "/" + m.ID
				cands = append(cands, Candidate{
					Provider: p.typ, ProviderID: p.id, KeyID: k.id,
					APIKey: plain, BaseURL: p.base, Model: m.ID,
					Priority: pri, LatencyEMA: tr.Latency(pm), PriceTier: m.PriceTier,
				})
			}
			// user-configured model IDs (custom endpoints, local Ollama names, etc.)
			// become synthetic candidates so explicit requests and `auto` can reach them.
			for _, id := range p.modelIDs {
				id = strings.TrimSpace(id)
				if id == "" || catalogHas(id) {
					continue
				}
				full := id
				if !strings.Contains(id, "/") {
					full = providerGroup(p.typ) + "/" + id
				}
				if ov, ok := overrides[full]; ok && ov[0] == 0 {
					continue
				}
				if ov, ok := overrides[id]; ok && ov[0] == 0 {
					continue
				}
				syn := models.Model{
					ID: full, DisplayName: id, Provider: providerGroup(p.typ),
					SupportsStreaming: true, SupportsTools: true, SupportsVision: true,
					SupportsJSON: true, Status: "active", Priority: 50, PriceTier: 1, Enabled: true,
				}
				if need.Embeddings {
					// Only route embeddings to a custom model the user explicitly asked for.
					if !modelMatches(baseModel, syn, p.typ) {
						continue
					}
				} else if need.Vision && !syn.SupportsVision {
					continue
				} else if need.Tools && !syn.SupportsTools {
					continue
				}
				if !modelMatches(baseModel, syn, p.typ) {
					continue
				}
				pri := syn.Priority + p.pri + k.pri
				pm := p.typ + "/" + full
				cands = append(cands, Candidate{
					Provider: p.typ, ProviderID: p.id, KeyID: k.id,
					APIKey: plain, BaseURL: p.base, Model: full,
					Priority: pri, LatencyEMA: tr.Latency(pm), PriceTier: syn.PriceTier,
				})
			}
		}
	}

	sortCandidates(cands, strategy, tr)
	// cap attempts
	max := cfg.MaxAttempts
	if max < 1 {
		max = 1
	}
	if max > 20 {
		max = 20
	}
	if len(cands) > max {
		cands = cands[:max]
	}
	return cands, cfg, nil
}

func providerGroup(typ string) string {
	if typ == "custom" || typ == "openai" || typ == "openai-compatible" {
		return "custom"
	}
	return typ
}

func modelMatches(want string, m models.Model, provType string) bool {
	if want == "" || want == "auto" {
		return true
	}
	if want == m.ID {
		return true
	}
	// provider/model form
	if strings.Contains(want, "/") {
		return want == m.ID || strings.HasSuffix(m.ID, "/"+suffixAfterFirst(want))
	}
	// bare id: match suffix or any model of the requested provider
	if strings.EqualFold(want, suffixAfterFirst(m.ID)) {
		return true
	}
	// provider name alone -> all models of that provider
	if strings.EqualFold(want, m.Provider) || strings.EqualFold(want, provType) {
		return true
	}
	return false
}

func suffixAfterFirst(s string) string {
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func sortCandidates(c []Candidate, strategy string, tr *Tracker) {
	tr.mu.Lock()
	rr := map[string]int{}
	for k, v := range tr.rr {
		rr[k] = v
	}
	tr.mu.Unlock()
	switch strategy {
	case "fastest", "fast":
		sort.SliceStable(c, func(i, j int) bool {
			li, lj := c[i].LatencyEMA, c[j].LatencyEMA
			if li == 0 {
				li = 1e9
			}
			if lj == 0 {
				lj = 1e9
			}
			if li != lj {
				return li < lj
			}
			return c[i].Priority > c[j].Priority
		})
	case "cheapest", "cheap":
		sort.SliceStable(c, func(i, j int) bool {
			if c[i].PriceTier != c[j].PriceTier {
				return c[i].PriceTier < c[j].PriceTier
			}
			return c[i].Priority > c[j].Priority
		})
	case "balanced":
		sort.SliceStable(c, func(i, j int) bool {
			si := float64(c[i].Priority) - float64(c[i].PriceTier)*10 - c[i].LatencyEMA/1000
			sj := float64(c[j].Priority) - float64(c[j].PriceTier)*10 - c[j].LatencyEMA/1000
			return si > sj
		})
	case "roundrobin":
		// rotate per provider group
		sort.SliceStable(c, func(i, j int) bool { return c[i].Priority > c[j].Priority })
	default: // auto, priority
		sort.SliceStable(c, func(i, j int) bool { return c[i].Priority > c[j].Priority })
	}
	_ = rr
}

func loadConfig(ctx context.Context, db *sql.DB) *Config {
	cfg := &Config{Strategy: "auto", MaxAttempts: 5, StickySessions: true, StickyMinutes: 30}
	row := db.QueryRowContext(ctx, `SELECT strategy,max_attempts,sticky_sessions,sticky_minutes,context_handoff FROM routing_config WHERE id=1`)
	var sticky, handoff int
	_ = row.Scan(&cfg.Strategy, &cfg.MaxAttempts, &sticky, &cfg.StickyMinutes, &handoff)
	cfg.StickySessions = sticky == 1
	cfg.ContextHandoff = handoff == 1
	return cfg
}

// Sticky pin helpers.
func GetPin(ctx context.Context, db *sql.DB, sessionID string) (provider, model string, ok bool) {
	if sessionID == "" {
		return "", "", false
	}
	var p, m, exp string
	err := db.QueryRowContext(ctx, `SELECT provider,model,expires_at FROM sticky_pins WHERE session_id=?`, sessionID).Scan(&p, &m, &exp)
	if err != nil {
		return "", "", false
	}
	if t, err := time.Parse(time.RFC3339Nano, exp); err != nil || time.Now().After(t) {
		return "", "", false
	}
	return p, m, true
}

func SetPin(ctx context.Context, db *sql.DB, sessionID, provider, model string, minutes int) {
	if sessionID == "" || minutes <= 0 {
		return
	}
	exp := time.Now().Add(time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339Nano)
	_, _ = db.ExecContext(ctx, `INSERT INTO sticky_pins(session_id,provider,model,expires_at) VALUES(?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET provider=excluded.provider,model=excluded.model,expires_at=excluded.expires_at`, sessionID, provider, model, exp)
}

// ReportResult updates tracker + DB health after an attempt.
func ReportResult(ctx context.Context, db *sql.DB, tr *Tracker, c Candidate, uerr *providers.UpstreamError, latencyMs float64) {
	pm := c.Provider + "/" + c.Model
	if uerr == nil {
		tr.Success(c.KeyID, pm, latencyMs)
		_, _ = db.ExecContext(ctx, `UPDATE provider_keys SET health='healthy',consecutive_failures=0,last_check_at=?,last_error='' WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), c.KeyID)
		return
	}
	health := "error"
	cooldown := 30 * time.Second
	switch uerr.Code {
	case "rate_limited":
		health = "rate_limited"
		cooldown = 30 * time.Second
	case "timeout":
		health = "timeout"
		cooldown = 20 * time.Second
	case "auth_error":
		health = "invalid"
		cooldown = 0
	case "unavailable", "connection":
		health = "error"
		cooldown = 20 * time.Second
	default:
		cooldown = 0 // permanent: don't cooldown, just record
	}
	if cooldown > 0 {
		tr.Cooldown(c.KeyID, cooldown)
		until := time.Now().Add(cooldown).UTC().Format(time.RFC3339Nano)
		_, _ = db.ExecContext(ctx, `UPDATE provider_keys SET health=?,cooldown_until=?,consecutive_failures=consecutive_failures+1,last_check_at=?,last_error=? WHERE id=?`, health, until, time.Now().UTC().Format(time.RFC3339Nano), uerr.Msg, c.KeyID)
	} else {
		_, _ = db.ExecContext(ctx, `UPDATE provider_keys SET health=?,consecutive_failures=consecutive_failures+1,last_check_at=?,last_error=? WHERE id=?`, health, time.Now().UTC().Format(time.RFC3339Nano), uerr.Msg, c.KeyID)
	}
}
