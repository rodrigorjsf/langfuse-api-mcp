#!/usr/bin/env bash
# PROTOTYPE — seed via OTLP/protobuf, then check whether key read APIs return data. Run while a stack is up.
S=$(cd "$(dirname "$0")/.." && pwd); cd "$S/lf3"
docker run --rm --network host -v "$PWD":/w -w /w python:3.13-slim sh -c "pip install -q opentelemetry-proto==1.* 2>/dev/null; python seed_pb.py" 2>&1 | tail -1 | cut -c1-40
K=pk-lf-proto-local:sk-lf-proto-local; B=localhost:3000/api/public
n(){ python3 -c "import sys,json
try:
  d=json.load(sys.stdin); x=d.get('data',d)
  print(len(x) if isinstance(x,list) else ('obj' if x else 0))
except Exception as e: print('non-json')"; }
for i in $(seq 1 24); do c=$(curl -s -u $K "$B/observations?limit=100" | n); [ "$c" != "0" ] && [ "$c" != "non-json" ] && break; sleep 5; done
for p in "observations?limit=100" "v2/observations?limit=1000&fields=core" "traces?limit=100" "v2/metrics?query=%7B%22view%22%3A%22observations%22%2C%22metrics%22%3A%5B%7B%22measure%22%3A%22count%22%2C%22aggregation%22%3A%22count%22%7D%5D%2C%22fromTimestamp%22%3A%222026-01-01T00%3A00%3A00Z%22%2C%22toTimestamp%22%3A%222026-12-31T00%3A00%3A00Z%22%7D" "sessions?limit=10" "v3/scores?limit=10"; do
  printf '%-28s %s rows=%s\n' "${p%%\?*}" "$(curl -s -o /dev/null -w '%{http_code}' -u $K "$B/$p")" "$(curl -s -u $K "$B/$p" | n)"; done
