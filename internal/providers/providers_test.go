package providers_test

import (
	"testing"

	"github.com/openbridge/gateway/internal/providers"
	registry "github.com/openbridge/gateway/internal/providers/registry"
)

func TestClassifyRetryable(t *testing.T) {
	cases := []struct {
		status int
		body   string
		retry  bool
	}{
		{429, "rate limit", true},
		{503, "overloaded", true},
		{500, "boom", true},
		{408, "timeout", true},
		{400, "bad request", false},
		{401, "invalid api key", false},
		{404, "not found", false},
	}
	for _, c := range cases {
		u := providers.ClassifyHTTP(c.status, c.body)
		if u.Retryable() != c.retry {
			t.Fatalf("status %d retry=%v want %v", c.status, u.Retryable(), c.retry)
		}
	}
}

func TestRegistryCoversAllPRDProviders(t *testing.T) {
	for _, typ := range []string{"google", "groq", "openrouter", "cerebras", "mistral", "nvidia", "github", "cloudflare", "huggingface", "ollama", "custom"} {
		ad := registry.Get(typ)
		if ad == nil || ad.Name() == "" {
			t.Fatalf("missing adapter for %s", typ)
		}
	}
	if len(registry.KnownTypes()) < 11 {
		t.Fatal("registry must list all 11 provider types")
	}
}
