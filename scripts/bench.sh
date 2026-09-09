#!/bin/sh
# Minimal overhead benchmark: gateway /health latency + optional chat comparison.
# Usage: OPENBRIDGE_URL=http://127.0.0.1:8787 OBG_KEY=obg_... sh scripts/bench.sh
set -eu
URL="${OPENBRIDGE_URL:-http://127.0.0.1:8787}"
echo "== /health p50 over 20 requests"
i=0; : > /tmp/obg-bench.txt
while [ $i -lt 20 ]; do
  start=$(date +%s%N)
  curl -s -o /dev/null "$URL/health"
  end=$(date +%s%N)
  echo $(( (end - start) / 1000000 )) >> /tmp/obg-bench.txt
  i=$((i + 1))
done
sort -n /tmp/obg-bench.txt | awk '{a[NR]=$1} END {print "p50="a[int(NR/2)]"ms min="a[1]"ms max="a[NR]"ms"}'
if [ -n "${OBG_KEY:-}" ]; then
  echo "== chat via gateway (model=auto)"
  time curl -s -o /dev/null -H "Authorization: Bearer $OBG_KEY" -H 'Content-Type: application/json' \
    -d '{"model":"auto","messages":[{"role":"user","content":"Reply with: ok"}]}' "$URL/v1/chat/completions"
fi
echo "Record RSS/CPU with: ps -o rss=,pcpu= -p <pid>"
