# OpenBridge Gateway on Android (Termux)

Run OpenBridge Gateway directly on your Android phone with Termux. Single binary, no Docker/Node/Python required, pure-Go SQLite.

What you get: dashboard at `http://127.0.0.1:8787` + unified OpenAI-compatible API at `http://127.0.0.1:8787/v1` with one `obg_...` key for all providers.

## Requirements

- Android 8+ (arm64 recommended, armv7 works)
- Termux from F-Droid (Play Store build is outdated)
- ~200 MB free, internet access
- 1 provider API key to start (e.g. Google Gemini, Groq, OpenRouter — see `docs/PROVIDERS.md`)

## 1. Install Termux

1. Install Termux from F-Droid.
2. Open Termux, allow storage if asked:
```bash
termux-setup-storage
```

## 2. Install dependencies

```bash
pkg update && pkg upgrade -y
pkg install -y golang git curl
go version  # need go 1.22+ (see go.mod)
```

## 3. Clone and build

```bash
git clone https://github.com/deshicart/aigateway
cd aigateway
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o openbridge ./cmd/openbridge
chmod +x openbridge
./openbridge version
```

> Why build from source: `scripts/install.sh` downloads from GitHub Releases. Until a release is published, building is the reliable path on Termux. It also auto-detects Termux (`android-arm64` asset) when releases exist:
> ```bash
> sh scripts/install.sh --dir $PREFIX/bin
> ```

## 4. Configure for phone

Low-resource mode is recommended on phones:

```bash
export OPENBRIDGE_HOST=127.0.0.1
export OPENBRIDGE_PORT=8787
export OPENBRIDGE_DATA_DIR=$HOME/.openbridge
export OPENBRIDGE_LOW_RESOURCE=true
# optional:
# export OPENBRIDGE_LOG_LEVEL=INFO
# export OPENBRIDGE_MASTER_KEY="your-long-passphrase-here"
# export OPENBRIDGE_MAX_ATTEMPTS=5

mkdir -p $HOME/.openbridge
./openbridge config
```

Config defaults (see `internal/config/config.go`, `README.md`):

| Env | Default | Notes |
|---|---|---|
| `OPENBRIDGE_HOST` | `127.0.0.1` | keep loopback on phone |
| `OPENBRIDGE_PORT` | `8787` | change if busy |
| `OPENBRIDGE_DATA_DIR` | `~/.openbridge` | db + master key + logs |
| `OPENBRIDGE_MASTER_KEY` | auto-generated | set to keep keys decryptable across reinstalls |
| `OPENBRIDGE_LOW_RESOURCE` | `false` | set `true` on phone: caps concurrency to 16, attempts to 3 |
| `OPENBRIDGE_LOG_LEVEL` | `INFO` | `ERROR/WARN/INFO/DEBUG` |

To persist, add exports to `~/.bashrc` or `~/.profile`.

## 5. First run

```bash
termux-wake-lock
./openbridge start
```

First-run output prints once — copy it:

```
First run: gateway API key generated:
obg_xxxx...
Copy it — shown in full only once
...
OpenBridge v1.0.0 listening on http://127.0.0.1:8787 (dashboard + /v1 API)
```

If port is busy:

```bash
OPENBRIDGE_PORT=8790 ./openbridge start
# or
./openbridge start --port=8790 --host=127.0.0.1
```

Check status in a second Termux session (swipe + New session):

```bash
./openbridge status
```

## 6. Dashboard setup

1. Open in phone browser (Chrome/Firefox): `http://127.0.0.1:8787`
2. Set admin password when prompted.
3. Go to Providers -> Add provider:
   - `google` + Gemini API key, or `groq` / `openrouter` / `cerebras` / etc.
   - Default base URLs are prefilled (see `docs/PROVIDERS.md`).
   - Click Test — checks credentials + reachability + model list.
4. Go to API Keys:
   - Copy existing `Default` key (`obg_...`, prefix only shown after first run), or Create new: `./openbridge key create Phone` in Termux.
5. Playground tab: try `model=auto` to verify routing/failover.

## 7. Test the /v1 API

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer obg_your_key" \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Hello from phone"}]}'
```

Python (install `python` + `pip install openai` in Termux if needed):

```python
from openai import OpenAI
client = OpenAI(base_url="http://127.0.0.1:8787/v1", api_key="obg_your_key")
print(client.chat.completions.create(model="auto", messages=[{"role":"user","content":"Hello"}]).choices[0].message.content)
```

Other endpoints: `GET /v1/models`, `POST /v1/responses`, `POST /v1/embeddings`, `/v1/docs`, `/health`.

## 8. Keep it running

Android kills background apps. Options:

```bash
# A. nohup (simple)
nohup ./openbridge start > $HOME/.openbridge/gateway.log 2>&1 &
tail -f $HOME/.openbridge/gateway.log

# B. tmux (reattachable)
pkg install -y tmux
tmux new -s obg
./openbridge start
# detach: Ctrl-b then d, reattach: tmux attach -t obg

# keep CPU awake while running:
termux-wake-lock
# release when done:
termux-wake-unlock
```

Auto-start on Termux boot (requires `termux-boot` app):

```bash
mkdir -p ~/.termux/boot
cat > ~/.termux/boot/openbridge.sh <<'EOF'
#!/data/data/com.termux/files/usr/bin/sh
export OPENBRIDGE_HOST=127.0.0.1
export OPENBRIDGE_PORT=8787
export OPENBRIDGE_DATA_DIR=$HOME/.openbridge
export OPENBRIDGE_LOW_RESOURCE=true
exec $HOME/aigateway/openbridge start >> $HOME/.openbridge/gateway.log 2>&1
EOF
chmod +x ~/.termux/boot/openbridge.sh
```

## 9. Update

```bash
cd ~/aigateway
git pull
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o openbridge ./cmd/openbridge
./openbridge version
# restart running instance
pkill -f "openbridge start" || true
nohup ./openbridge start > $HOME/.openbridge/gateway.log 2>&1 &
```

## 10. LAN access (optional, risky)

Only on trusted Wi-Fi. Others can spend your quota:

```bash
OPENBRIDGE_HOST=0.0.0.0 OPENBRIDGE_PORT=8787 ./openbridge start
```

Then from laptop on same Wi-Fi: `http://PHONE_IP:8787` (find IP with `ifconfig wlan0`). Server logs a warning when not on loopback. Never expose directly to internet.

## CLI on Termux

```
./openbridge [start]            start server (default)
./openbridge status             running or stopped
./openbridge providers          list configured providers
./openbridge models             list catalog models
./openbridge key [create|list]  e.g. ./openbridge key create Phone
./openbridge config             show effective host/port/data_dir
./openbridge version
```

## Troubleshooting

- `go: command not found` → `pkg install -y golang`, open new session.
- `cannot create data dir` → `mkdir -p $HOME/.openbridge`, check `OPENBRIDGE_DATA_DIR`.
- `address already in use` → `OPENBRIDGE_PORT=8790 ./openbridge start` or `pkill -f openbridge`.
- Dashboard blank → use `http://127.0.0.1:8787` (not `localhost` on some WebViews), clear cache, try Firefox.
- `401 / invalid key` → `./openbridge key list`, recreate key, check `Authorization: Bearer obg_...` header.
- Provider `429/5xx` → normal failover (router retries healthy keys, cools down exhausted ones 30s). Check Providers -> Test, `docs/PROVIDERS.md`.
- Battery kill → disable battery optimization for Termux, use `termux-wake-lock`, `tmux`.
- Backup: copy `$HOME/.openbridge/` + `OPENBRIDGE_MASTER_KEY` — without master key, encrypted provider keys can't be decrypted.

## Docs

- `README.md` — quick start + env + build matrix
- `docs/API.md` — endpoint reference
- `docs/PROVIDERS.md` — provider URLs/auth
- `docs/SECURITY.md` — encryption/SSRF model
- `docs/BENCHMARKS.md` — resource targets
- `scripts/build-all.sh` — all Tier-1 targets incl. `android-arm64`
