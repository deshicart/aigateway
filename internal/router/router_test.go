package router_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/openbridge/gateway/internal/crypto"
	"github.com/openbridge/gateway/internal/database"
	"github.com/openbridge/gateway/internal/providers"
	"github.com/openbridge/gateway/internal/router"
)

func testDB(t *testing.T) (*sql.DB, func(string) (string, error)) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	master := make([]byte, 32)
	dec := func(enc string) (string, error) {
		b, err := crypto.Decrypt(master, enc)
		return string(b), err
	}
	addProvider := func(id, typ, base string, pri int, models string) {
		_, err := db.Exec(`INSERT INTO providers(id,type,display_name,base_url,auth_type,model_ids,enabled,priority,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			id, typ, id, base, "bearer", models, 1, pri, "t", "t")
		if err != nil {
			t.Fatal(err)
		}
	}
	addKey := func(id, prov, secret string) {
		enc, _ := crypto.Encrypt(master, []byte(secret))
		_, err := db.Exec(`INSERT INTO provider_keys(id,provider_id,label,key_enc,enabled,priority,created_at) VALUES(?,?,?,?,?,?,?)`, id, prov, "k", enc, 1, 50, "t")
		if err != nil {
			t.Fatal(err)
		}
	}
	addProvider("p-groq", "groq", "", 60, "[]")
	addKey("k1", "p-groq", "sk-1")
	addProvider("p-custom", "custom", "http://localhost:11434/v1", 10, `["my-model"]`)
	addKey("k2", "p-custom", "sk-2")
	return db, dec
}

func TestAutoRoutingPrefersPriority(t *testing.T) {
	db, dec := testDB(t)
	defer db.Close()
	tr := router.NewTracker()
	cands, _, err := router.BuildCandidates(context.Background(), db, tr, "auto", router.Need{}, "", dec)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) == 0 {
		t.Fatal("expected candidates")
	}
	// groq catalog models (pri 90/80/60 + provider 60) beat custom synthetic (50+10)
	if cands[0].Provider != "groq" {
		t.Fatalf("expected groq first, got %s %s", cands[0].Provider, cands[0].Model)
	}
}

func TestBareAndSlashedModelResolution(t *testing.T) {
	db, dec := testDB(t)
	defer db.Close()
	tr := router.NewTracker()
	for _, want := range []string{"groq/llama-3.3-70b-versatile", "llama-3.3-70b-versatile", "my-model", "custom/my-model"} {
		cands, _, err := router.BuildCandidates(context.Background(), db, tr, want, router.Need{}, "", dec)
		if err != nil {
			t.Fatal(err)
		}
		if len(cands) == 0 {
			t.Fatalf("no candidate for %q", want)
		}
	}
}

func TestVisionSmartSelection(t *testing.T) {
	db, dec := testDB(t)
	defer db.Close()
	tr := router.NewTracker()
	// groq catalog models have no vision; only custom synthetic (vision passthrough) qualifies
	cands, _, err := router.BuildCandidates(context.Background(), db, tr, "auto", router.Need{Vision: true}, "", dec)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.Provider == "groq" {
			t.Fatalf("non-vision groq model selected for vision request: %s", c.Model)
		}
	}
}

func TestEmbeddingsNeverChat(t *testing.T) {
	db, dec := testDB(t)
	defer db.Close()
	tr := router.NewTracker()
	// chat request must not route to embedding models
	cands, _, err := router.BuildCandidates(context.Background(), db, tr, "auto", router.Need{}, "", dec)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.Model == "google/text-embedding-004" || c.Model == "mistral/mistral-embed" {
			t.Fatalf("embedding model routed for chat: %s", c.Model)
		}
	}
}

func TestCooldownExcludesKey(t *testing.T) {
	db, dec := testDB(t)
	defer db.Close()
	tr := router.NewTracker()
	ctx := context.Background()
	before, _, _ := router.BuildCandidates(ctx, db, tr, "auto", router.Need{}, "", dec)
	if len(before) == 0 {
		t.Fatal("expected candidates")
	}
	victim := before[0]
	router.ReportResult(ctx, db, tr, victim, &providers.UpstreamError{Status: 429, Code: "rate_limited", Msg: "slow down"}, 0)
	after, _, _ := router.BuildCandidates(ctx, db, tr, "auto", router.Need{}, "", dec)
	for _, c := range after {
		if c.KeyID == victim.KeyID {
			t.Fatal("rate-limited key must be in cooldown")
		}
	}
}

func TestPermanentErrorsDoNotRetry(t *testing.T) {
	u := &providers.UpstreamError{Status: 400, Code: "bad_request", Msg: "bad"}
	if u.Retryable() {
		t.Fatal("400 must not be retryable")
	}
	u = &providers.UpstreamError{Status: 401, Code: "auth_error", Msg: "bad key"}
	if u.Retryable() {
		t.Fatal("401 must not be retryable")
	}
	for _, u := range []*providers.UpstreamError{
		{Status: 429, Code: "rate_limited"},
		{Status: 503, Code: "unavailable"},
		{Status: 0, Code: "timeout"},
		{Status: 0, Code: "connection"},
	} {
		if !u.Retryable() {
			t.Fatalf("%v must be retryable", u)
		}
	}
}

func TestStickyPins(t *testing.T) {
	db, _ := testDB(t)
	defer db.Close()
	ctx := context.Background()
	router.SetPin(ctx, db, "sess-1", "groq", "groq/llama-3.3-70b-versatile", 30)
	p, m, ok := router.GetPin(ctx, db, "sess-1")
	if !ok || p != "groq" || m == "" {
		t.Fatalf("pin missing: %v %v %v", p, m, ok)
	}
	if _, _, ok := router.GetPin(ctx, db, "nope"); ok {
		t.Fatal("unknown session must miss")
	}
}
