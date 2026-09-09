// Package models defines the unified model catalog types and the
// built-in static catalog. Remote catalog sync (PRD §40) may only
// update these metadata fields after signature verification.
package models

// Capabilities describes what a model can do. Unknown = false with
// Status "unknown" — never assume support (PRD §62, Rule 12).
type Model struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"display_name"`
	Provider           string   `json:"provider"`
	ContextLength      int      `json:"context_length"`
	InputModalities    []string `json:"input_modalities,omitempty"`
	OutputModalities   []string `json:"output_modalities,omitempty"`
	SupportsStreaming  bool     `json:"supports_streaming"`
	SupportsTools      bool     `json:"supports_tools"`
	SupportsVision     bool     `json:"supports_vision"`
	SupportsEmbeddings bool     `json:"supports_embeddings"`
	SupportsReasoning  bool     `json:"supports_reasoning"`
	SupportsJSON       bool     `json:"supports_json"`
	SupportsAudio      bool     `json:"supports_audio"`
	Status             string   `json:"status"`     // active, deprecated, unknown
	Priority           int      `json:"priority"`   // higher = preferred
	PriceTier          int      `json:"price_tier"` // 0=free/local 1=cheap 2=mid 3=premium
	Enabled            bool     `json:"enabled"`
}

// Catalog is the built-in static catalog (v1.0.0 snapshot).
func Catalog() []Model {
	return []Model{
		// Google Gemini
		{ID: "google/gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", Provider: "google", ContextLength: 1000000, InputModalities: []string{"text", "image", "audio", "video"}, OutputModalities: []string{"text"}, SupportsStreaming: true, SupportsTools: true, SupportsVision: true, SupportsJSON: true, SupportsAudio: true, Status: "active", Priority: 90, PriceTier: 1, Enabled: true},
		{ID: "google/gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", Provider: "google", ContextLength: 1000000, InputModalities: []string{"text", "image", "audio", "video"}, OutputModalities: []string{"text"}, SupportsStreaming: true, SupportsTools: true, SupportsVision: true, SupportsReasoning: true, SupportsJSON: true, Status: "active", Priority: 80, PriceTier: 3, Enabled: true},
		{ID: "google/gemini-2.0-flash", DisplayName: "Gemini 2.0 Flash", Provider: "google", ContextLength: 1000000, SupportsStreaming: true, SupportsTools: true, SupportsVision: true, SupportsJSON: true, Status: "active", Priority: 70, PriceTier: 1, Enabled: true},
		{ID: "google/text-embedding-004", DisplayName: "Text Embedding 004", Provider: "google", ContextLength: 8192, SupportsEmbeddings: true, Status: "active", Priority: 70, PriceTier: 1, Enabled: true},
		// Groq
		{ID: "groq/llama-3.3-70b-versatile", DisplayName: "Llama 3.3 70B Versatile", Provider: "groq", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, SupportsJSON: true, Status: "active", Priority: 90, PriceTier: 1, Enabled: true},
		{ID: "groq/llama-3.1-8b-instant", DisplayName: "Llama 3.1 8B Instant", Provider: "groq", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, SupportsJSON: true, Status: "active", Priority: 80, PriceTier: 0, Enabled: true},
		{ID: "groq/mixtral-8x7b-32768", DisplayName: "Mixtral 8x7B", Provider: "groq", ContextLength: 32768, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 60, PriceTier: 0, Enabled: true},
		{ID: "groq/whisper-large-v3-turbo", DisplayName: "Whisper Large v3 Turbo", Provider: "groq", ContextLength: 8192, SupportsAudio: true, Status: "active", Priority: 60, PriceTier: 1, Enabled: true},
		// OpenRouter
		{ID: "openrouter/auto", DisplayName: "OpenRouter Auto", Provider: "openrouter", ContextLength: 200000, SupportsStreaming: true, SupportsTools: true, SupportsVision: true, SupportsJSON: true, Status: "active", Priority: 70, PriceTier: 2, Enabled: true},
		{ID: "openrouter/anthropic/claude-3.5-sonnet", DisplayName: "Claude 3.5 Sonnet (via OpenRouter)", Provider: "openrouter", ContextLength: 200000, SupportsStreaming: true, SupportsTools: true, SupportsVision: true, SupportsJSON: true, Status: "active", Priority: 75, PriceTier: 3, Enabled: true},
		// Cerebras
		{ID: "cerebras/llama-3.3-70b", DisplayName: "Llama 3.3 70B (Cerebras)", Provider: "cerebras", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, SupportsJSON: true, Status: "active", Priority: 85, PriceTier: 1, Enabled: true},
		{ID: "cerebras/llama-3.1-8b", DisplayName: "Llama 3.1 8B (Cerebras)", Provider: "cerebras", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 75, PriceTier: 0, Enabled: true},
		// Mistral
		{ID: "mistral/mistral-large-latest", DisplayName: "Mistral Large", Provider: "mistral", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, SupportsVision: false, SupportsJSON: true, Status: "active", Priority: 80, PriceTier: 3, Enabled: true},
		{ID: "mistral/mistral-small-latest", DisplayName: "Mistral Small", Provider: "mistral", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, SupportsJSON: true, Status: "active", Priority: 70, PriceTier: 1, Enabled: true},
		{ID: "mistral/mistral-embed", DisplayName: "Mistral Embed", Provider: "mistral", ContextLength: 8192, SupportsEmbeddings: true, Status: "active", Priority: 70, PriceTier: 1, Enabled: true},
		// NVIDIA
		{ID: "nvidia/meta/llama-3.3-70b-instruct", DisplayName: "Llama 3.3 70B (NVIDIA)", Provider: "nvidia", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 70, PriceTier: 1, Enabled: true},
		{ID: "nvidia/nvidia/llama-3.1-nemotron-70b-instruct", DisplayName: "Nemotron 70B (NVIDIA)", Provider: "nvidia", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 65, PriceTier: 1, Enabled: true},
		// GitHub Models
		{ID: "github/openai/gpt-4o", DisplayName: "GPT-4o (GitHub Models)", Provider: "github", ContextLength: 128000, SupportsStreaming: true, SupportsTools: true, SupportsVision: true, SupportsJSON: true, Status: "active", Priority: 80, PriceTier: 2, Enabled: true},
		{ID: "github/meta/llama-3.3-70b-instruct", DisplayName: "Llama 3.3 70B (GitHub)", Provider: "github", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 70, PriceTier: 0, Enabled: true},
		{ID: "github/openai/text-embedding-3-small", DisplayName: "Text Embedding 3 Small (GitHub)", Provider: "github", ContextLength: 8192, SupportsEmbeddings: true, Status: "active", Priority: 70, PriceTier: 1, Enabled: true},
		// Cloudflare
		{ID: "cloudflare/@cf/meta/llama-3.3-70b-instruct-fp8-fast", DisplayName: "Llama 3.3 70B (Cloudflare)", Provider: "cloudflare", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 60, PriceTier: 1, Enabled: true},
		// Hugging Face
		{ID: "huggingface/meta-llama/Llama-3.3-70B-Instruct", DisplayName: "Llama 3.3 70B (HF)", Provider: "huggingface", ContextLength: 131072, SupportsStreaming: true, SupportsTools: false, Status: "active", Priority: 55, PriceTier: 1, Enabled: true},
		// Ollama (local)
		{ID: "ollama/llama3.3", DisplayName: "Llama 3.3 (Ollama)", Provider: "ollama", ContextLength: 131072, SupportsStreaming: true, SupportsTools: true, Status: "active", Priority: 50, PriceTier: 0, Enabled: true},
		{ID: "ollama/llama3.2-vision", DisplayName: "Llama 3.2 Vision (Ollama)", Provider: "ollama", ContextLength: 131072, SupportsStreaming: true, SupportsVision: true, SupportsTools: true, Status: "active", Priority: 50, PriceTier: 0, Enabled: true},
		{ID: "ollama/nomic-embed-text", DisplayName: "Nomic Embed (Ollama)", Provider: "ollama", ContextLength: 8192, SupportsEmbeddings: true, Status: "active", Priority: 50, PriceTier: 0, Enabled: true},
	}
}

// ByID returns model by full id or bare suffix.
func ByID(id string) *Model {
	for _, m := range Catalog() {
		if m.ID == id {
			c := m
			return &c
		}
	}
	// bare match when unambiguous
	var found *Model
	n := 0
	for _, m := range Catalog() {
		suffix := m.ID
		for i := 0; i < len(m.ID); i++ {
			if m.ID[i] == '/' {
				suffix = m.ID[i+1:]
			}
		}
		if suffix == id {
			c := m
			found = &c
			n++
		}
	}
	if n == 1 {
		return found
	}
	return nil
}
