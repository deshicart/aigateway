# OpenBridge API reference (v1.0.0)

Base URL: `http://127.0.0.1:8787/v1`. All `/v1/*` routes require
`Authorization: Bearer obg_...` (unified gateway key, managed in dashboard → API Keys).

## GET /health (no auth)

```json
{"status": "ok", "version": "1.0.0"}
```

## GET /v1/models

OpenAI-compatible model list: full `provider/model` IDs, bare aliases, custom
provider IDs and the virtual `auto` model.

## POST /v1/chat/completions

OpenAI-compatible. Extra behavior:

- `"model": "auto" | "auto:fast" | "auto:balanced" | "auto:priority"` routes automatically.
- `"model": "provider/model"` pins a provider; bare IDs resolve when unambiguous.
- `stream: true` returns SSE (`data: {...}` … `data: [DONE]`), proxied without buffering.
- `session_id` (top-level) or `X-OpenBridge-Session` header opts into sticky sessions.
- Response headers: `X-OpenBridge-Provider`, `X-OpenBridge-Model`, `X-OpenBridge-Latency`, `X-OpenBridge-Fallback-Attempts`.
- Supported: temperature, top_p, max_tokens/max_completion_tokens, stop, seed (where upstream supports), tools/tool_choice, response_format, multimodal `image_url` content.
- Errors are OpenAI-style: `{"error": {"message", "type": "gateway_error", "code": "provider_unavailable"}}`. On total failure the message lists per-provider attempts (no secrets).

## POST /v1/responses

OpenAI Responses API subset: `{"model", "input": string|array, ...}` → mapped onto chat routing. Returns a `response` object with `output` + `usage`.

## POST /v1/embeddings

`{"model", "input"}`. Routing is restricted to the requested embedding model
family — embeddings never fail over to a different, incompatible model.

## GET /v1/docs, GET /v1/openapi.json

Human docs and OpenAPI schema.

## GET /health/providers (admin) · GET /metrics (localhost or admin)

Per-key health snapshot and Prometheus-style counters.

## Admin REST (dashboard session cookie or session-token bearer)

- `POST /api/admin/login|logout`, `GET /api/admin/status`
- `GET|POST /api/providers`, `GET|PUT|DELETE /api/providers/{id}`, `POST /api/providers/test`
- `GET /api/models`, `PUT /api/models/{id}`, `POST /api/models/test`
- `GET|POST /api/keys`, `DELETE /api/keys/{id}`
- `GET|PUT /api/routing`, `GET /api/analytics?hours=168`, `GET /api/health/providers`
- `GET|PUT /api/settings`, `POST /api/playground`, `GET /api/export`, `POST /api/import`
