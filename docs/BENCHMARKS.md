# Benchmarks — targets & method (PRD §46, §74)

Targets (to be measured, never faked):

| Metric | Target |
|---|---|
| Idle RAM | < 50 MB preferred |
| Idle CPU | near-zero |
| Startup | < 2 s preferred |
| Request overhead vs direct | minimal (headers + routing only; streaming unbuffered) |
| Storage | binary + small SQLite |

## How to measure

```bash
# build + run
go build -o openbridge ./cmd/openbridge
OPENBRIDGE_DATA_DIR=/tmp/obg-bench ./openbridge &
# startup (cold)
time (OPENBRIDGE_DATA_DIR=/tmp/obg-bench2 ./openbridge & sleep 0.2; curl -s http://127.0.0.1:8787/health)
# idle RSS after 60s
ps -o rss=,pcpu= -p $(pgrep -f 'openbridge$' | head -1)
# overhead: compare direct upstream vs gateway for same model/prompt
scripts/bench.sh
```

`scripts/bench.sh` hits `/health` and (with a configured provider) a fixed
chat prompt through the gateway vs directly, reporting p50 deltas. Record
results per device class (512MB / 1GB / 2GB / 4GB / 8GB+) in this file —
do not claim numbers without a run.
