package security_test

import (
	"testing"

	"github.com/openbridge/gateway/internal/security"
)

func TestBlocksMetadataIP(t *testing.T) {
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://169.254.169.254:80/",
		"http://metadata.google.internal/",
	} {
		if _, err := security.ValidateProviderURL(u, true); err == nil {
			t.Fatalf("must block %s", u)
		}
	}
}

func TestBlocksBadSchemesAndCreds(t *testing.T) {
	for _, u := range []string{
		"ftp://example.com/v1",
		"file:///etc/passwd",
		"gopher://example.com/",
		"https://user:pass@example.com/v1",
		"",
		"not-a-url",
	} {
		if _, err := security.ValidateProviderURL(u, true); err == nil {
			t.Fatalf("must reject %q", u)
		}
	}
}

func TestAllowsLocalAndLAN(t *testing.T) {
	// Local providers (Ollama etc.) must be reachable by default.
	for _, u := range []string{
		"http://localhost:11434/v1",
		"http://127.0.0.1:11434/v1",
		"http://192.168.1.10:8080/v1",
		"https://api.groq.com/openai/v1",
	} {
		if _, err := security.ValidateProviderURL(u, true); err != nil {
			t.Fatalf("must allow %s: %v", u, err)
		}
	}
	if _, err := security.ValidateProviderURL("http://192.168.1.10:8080/v1", false); err == nil {
		t.Fatal("private IP must be rejected when allowPrivate=false")
	}
}

func TestModelIDValidation(t *testing.T) {
	if err := security.ValidateModelID(""); err == nil {
		t.Fatal("empty model must fail")
	}
	if err := security.ValidateModelID("auto"); err != nil {
		t.Fatal(err)
	}
	if err := security.ValidateModelID("google/gemini-2.5-flash"); err != nil {
		t.Fatal(err)
	}
}
