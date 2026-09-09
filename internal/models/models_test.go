package models_test

import (
	"testing"

	"github.com/openbridge/gateway/internal/models"
)

func TestCatalogSanity(t *testing.T) {
	c := models.Catalog()
	if len(c) < 20 {
		t.Fatalf("catalog too small: %d", len(c))
	}
	seen := map[string]bool{}
	for _, m := range c {
		if m.ID == "" || m.Provider == "" {
			t.Fatalf("model missing id/provider: %+v", m)
		}
		if seen[m.ID] {
			t.Fatalf("duplicate model id %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestByIDBareMatch(t *testing.T) {
	m := models.ByID("gemini-2.5-flash")
	if m == nil {
		t.Fatal("bare id should resolve when unambiguous")
	}
	if models.ByID("google/gemini-2.5-flash") == nil {
		t.Fatal("full id must resolve")
	}
	if models.ByID("no-such-model-xyz") != nil {
		t.Fatal("unknown model must return nil")
	}
}
