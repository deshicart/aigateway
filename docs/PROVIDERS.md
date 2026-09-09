# Provider setup matrix

| Provider | Type | Base URL (default) | Auth | Notes |
|---|---|---|---|---|
| Google Gemini | `google` | `https://generativelanguage.googleapis.com/v1beta` (built-in) | API key | Native adapter: chat, SSE streaming, tools→functionDeclarations, vision→inlineData, `text-embedding-004` embeddings |
| Groq | `groq` | `https://api.groq.com/openai/v1` | Bearer | OpenAI-compatible |
| OpenRouter | `openrouter` | `https://openrouter.ai/api/v1` | Bearer | Adds `HTTP-Referer`/`X-Title` |
| Cerebras | `cerebras` | `https://api.cerebras.ai/v1` | Bearer | OpenAI-compatible |
| Mistral | `mistral` | `https://api.mistral.ai/v1` | Bearer | incl. `mistral-embed` |
| NVIDIA | `nvidia` | `https://integrate.api.nvidia.com/v1` | Bearer | OpenAI-compatible |
| GitHub Models | `github` | `https://models.github.ai/inference` | PAT (`github_pat_...`) | OpenAI-compatible |
| Cloudflare | `cloudflare` | *(you provide)* `https://api.cloudflare.com/client/v4/accounts/{account}/ai/v1` | API token | Account ID goes in the URL path |
| Hugging Face | `huggingface` | `https://router.huggingface.co/v1` | `hf_...` token | Serverless inference providers |
| Ollama | `ollama` | `http://localhost:11434/v1` | none required | Local; add your pulled tags under Model IDs too |
| Custom | `custom` | *(you provide)* any `http(s)` OpenAI-compatible base | bearer/none | llama.cpp, LM Studio, vLLM, LocalAI, other gateways; LAN URLs allowed; metadata IPs/odd schemes/embedded creds blocked |

Multiple API keys per provider are supported — the router rotates among healthy
keys and cools down exhausted ones (30s default). Dashboard → Providers → Test
checks credentials, endpoint reachability and model listing.
