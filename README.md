# OpenBridge Gateway

Self-hosted, lightweight, multi-provider LLM API Gateway. One unified OpenAI-compatible endpoint (`/v1`) and one API key for many providers — with auto routing, failover, health tracking and a mobile-friendly dashboard.

```
Application ──OpenAI-compatible──▶ OpenBridge Gateway ──┬── Google Gemini
                                                       ├── Groq · Cerebras · OpenRouter
                                                       ├── Mistral · NVIDIA · GitHub Models
                                                       ├── Cloudflare · Hugging Face
                                                       └── Ollama / llama.cpp / LM Studio / vLLM / any OpenAI-compatible endpoint
```

## Quick start

```bash
# Linux / macOS / Termux / WSL
./openbridge
# Windows
OpenBridge.exe
```

Then open **http://127.0.0.1:8787**, set an admin password, add a provider key, click **Test**, copy the unified `obg_...` key, and point any OpenAI-compatible app at `http://localhost:8787/v1`.

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8787/v1", api_key="obg_your_key")
print(client.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Hello"}]).choices[0].message.content)
```

```bash
curl http://localhost:8787/v1/chat/completions \
  -H "Authorization: Bearer obg_your_key" \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Hello"}]}'
```

## Features (v1.0.0)

- OpenAI-compatible: `GET /v1/models`, `POST /v1/chat/completions` (streaming SSE), `POST /v1/responses`, `POST /v1/embeddings`, `/v1/docs`, `/v1/openapi.json`
- 11 provider adapters: Google Gemini (native translation incl. tools/vision/embeddings), Groq, OpenRouter, Cerebras, Mistral, NVIDIA, GitHub Models, Cloudflare, Hugging Face, Ollama, Custom OpenAI-compatible
- Virtual models: `auto`, `auto:fast`, `auto:balanced`, `auto:priority` + `provider/model` + bare IDs
- Automatic failover on 429/timeout/5xx/connection errors (never on 400/401/404); bounded retries (default 5, max 20); exponential health cooldowns
- Per-key health states, latency EMA, rate-limit header tracking, multi-key rotation, sticky sessions (30 min), embeddings pinned to model family
- AES-256-GCM encrypted provider keys; `obg_` gateway keys; hashed admin password + expiring sessions; SSRF guard for custom URLs; request limits; safe logs
- SQLite (WAL) local storage; zero telemetry (opt-in only); offline-capable with local providers
- Monochrome dashboard: Overview, Providers (+wizard), Models, Routing, API Keys, Playground, Analytics, Settings — dark/light, 360px-friendly, keyboard accessible
- Single static binary, no Docker/Node/Python required; low-resource mode; optional Docker image

## Configuration

| Env | Default | Description |
|---|---|---|
| `OPENBRIDGE_HOST` | `127.0.0.1` | Bind address (`0.0.0.0` = LAN, shows warning) |
| `OPENBRIDGE_PORT` | `8787` | Port |
| `OPENBRIDGE_DATA_DIR` | `~/.openbridge` | Data dir (db, master key, logs) |
| `OPENBRIDGE_MASTER_KEY` | *(generated)* | 32-byte/base64/passphrase for key encryption |
| `OPENBRIDGE_LOG_LEVEL` | `INFO` | ERROR/WARN/INFO/DEBUG |
| `OPENBRIDGE_LOW_RESOURCE` | `false` | Reduce checks, concurrency, retention |
| `OPENBRIDGE_MAX_ATTEMPTS` | `5` | Failover attempts cap (≤20) |

## CLI

```
openbridge [start] | status | providers | models | key [create|list] | config | version
```

## Build

```bash
go build -o openbridge ./cmd/openbridge
# cross (examples)
GOOS=linux GOARCH=arm64 go build -o openbridge-linux-arm64 ./cmd/openbridge
GOOS=linux GOARM=7 GOARCH=arm go build -o openbridge-linux-armv7 ./cmd/openbridge
GOOS=windows GOARCH=amd64 go build -o OpenBridge.exe ./cmd/openbridge
GOOS=darwin GOARCH=arm64 go build -o openbridge-macos-arm64 ./cmd/openbridge
GOOS=android GOARCH=arm64 go build -o openbridge-android-arm64 ./cmd/openbridge
```

Pure-Go SQLite (no cgo) — cross-compilation works out of the box. See `scripts/build-all.sh`, `scripts/install.sh`, `docs/`.

## Docs

- `docs/API.md` — endpoint reference
- `docs/PROVIDERS.md` — provider setup matrix
- `docs/SECURITY.md` — threat model & guarantees
- `docs/BENCHMARKS.md` — resource targets & how to measure
- `Dockerfile` — optional container (never required)

## License

TBD by project owner (see PRD §79). No code copied from other projects.
