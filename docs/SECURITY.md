# Security model

- Provider keys encrypted at rest with AES-256-GCM. Master key from
  `OPENBRIDGE_MASTER_KEY` or generated into `$DATA_DIR/master.key` (0600). Never hard-coded.
- Gateway keys stored as SHA-256 hashes; only the `obg_` prefix is shown after creation.
- Admin password hashed with PBKDF2-HMAC-SHA256 (120k iterations, stdlib only); sessions are random 256-bit tokens, HttpOnly cookie, 24h expiry.
- Custom provider URLs validated: only `http(s)`, no embedded credentials, cloud-metadata hosts/IPs (`169.254.169.254`, `metadata.google.internal`) and link-local blocked.
- Request body capped (default 4 MiB), concurrent requests bounded, upstream + stream timeouts enforced.
- Logs never include keys, auth headers, prompts or completions — only routing decisions and latencies.
- No telemetry. No outbound calls except to configured providers and (optional, future) signed catalog sync.
- Error responses are normalized; upstream bodies truncated and never include secrets.
- Bind default is loopback. LAN mode requires explicit `OPENBRIDGE_HOST=0.0.0.0` and logs a warning.
