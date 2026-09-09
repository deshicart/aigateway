// Package registry wires all provider adapters behind the Provider interface.
package registry

import (
	"github.com/openbridge/gateway/internal/providers"
	"github.com/openbridge/gateway/internal/providers/google"
	"github.com/openbridge/gateway/internal/providers/openaicompat"
)

// Get returns the adapter for a provider type.
func Get(providerType string) providers.Provider {
	switch providerType {
	case "google":
		return google.Adapter{}
	case "groq":
		return &openaicompat.Adapter{ProviderName: "groq", DefaultBase: "https://api.groq.com/openai/v1"}
	case "openrouter":
		return &openaicompat.Adapter{ProviderName: "openrouter", DefaultBase: "https://openrouter.ai/api/v1",
			ExtraHeaders: map[string]string{"HTTP-Referer": "https://github.com/openbridge/gateway", "X-Title": "OpenBridge Gateway"}}
	case "cerebras":
		return &openaicompat.Adapter{ProviderName: "cerebras", DefaultBase: "https://api.cerebras.ai/v1"}
	case "mistral":
		return &openaicompat.Adapter{ProviderName: "mistral", DefaultBase: "https://api.mistral.ai/v1"}
	case "nvidia":
		return &openaicompat.Adapter{ProviderName: "nvidia", DefaultBase: "https://integrate.api.nvidia.com/v1"}
	case "github":
		return &openaicompat.Adapter{ProviderName: "github", DefaultBase: "https://models.github.ai/inference"}
	case "cloudflare":
		// Base URL must be fully configured by user:
		// https://api.cloudflare.com/client/v4/accounts/{account}/ai/v1
		return &openaicompat.Adapter{ProviderName: "cloudflare", DefaultBase: ""}
	case "huggingface":
		return &openaicompat.Adapter{ProviderName: "huggingface", DefaultBase: "https://router.huggingface.co/v1"}
	case "ollama":
		return &openaicompat.Adapter{ProviderName: "ollama", DefaultBase: "http://localhost:11434/v1"}
	case "custom", "openai", "openai-compatible":
		return &openaicompat.Adapter{ProviderName: "custom", DefaultBase: ""}
	default:
		// Unknown types fall back to OpenAI-compatible custom behavior.
		return &openaicompat.Adapter{ProviderName: providerType, DefaultBase: ""}
	}
}

// DefaultBaseURL returns the default base URL for a provider type.
func DefaultBaseURL(providerType string) string {
	switch providerType {
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "cerebras":
		return "https://api.cerebras.ai/v1"
	case "mistral":
		return "https://api.mistral.ai/v1"
	case "nvidia":
		return "https://integrate.api.nvidia.com/v1"
	case "github":
		return "https://models.github.ai/inference"
	case "huggingface":
		return "https://router.huggingface.co/v1"
	case "ollama":
		return "http://localhost:11434/v1"
	default:
		return ""
	}
}

// KnownTypes lists supported provider types.
func KnownTypes() []string {
	return []string{"google", "groq", "openrouter", "cerebras", "mistral", "nvidia", "github", "cloudflare", "huggingface", "ollama", "custom"}
}
