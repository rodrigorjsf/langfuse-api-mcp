#!/usr/bin/env bash
# PROTOTYPE — boot one Langfuse version (upstream compose at that tag, deps pinned), scan every operation of
# that tag's spec at runtime, tear down. Usage: probe-version.sh <tag> [clickhouse_tag]
set -euo pipefail
T=$1; CH=${2:-25.12}; V=${T#v}; D=$(cd "$(dirname "$0")" && pwd)/$T; S=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$D" && cd "$D"
git -C "$S/lfrepo.git" show "$T:docker-compose.yml" > docker-compose.yml
sed -i -E "s#(langfuse/langfuse-worker):[0-9]+\$#\1:$V#; s#(langfuse/langfuse):[0-9]+\$#\1:$V#; s#(clickhouse/clickhouse-server)(:[0-9.]+)?\$#\1:$CH#" docker-compose.yml
sed 's/lfproto-wipeme/lfrt-wipeme/' "$S/lf/.env" > .env; echo POSTGRES_VERSION=17 >> .env
grep -E 'image:' docker-compose.yml | head -3
git -C "$S/lfrepo.git" show "$T:web/public/generated/api/openapi.yml" | python3 -c "import sys,yaml,json;json.dump(yaml.load(sys.stdin,Loader=yaml.CSafeLoader),open('spec.json','w'))"
docker compose down -v >/dev/null 2>&1 || true
docker compose pull -q >/dev/null 2>&1; t0=$(date +%s); docker compose up -d >/dev/null 2>&1
until curl -sf localhost:3000/api/public/ready >/dev/null; do sleep 3; [ $(( $(date +%s)-t0 )) -gt 400 ] && { echo "NOT READY"; docker compose logs --tail 20 langfuse-web; exit 1; }; done
echo "ready $(( $(date +%s)-t0 ))s $(curl -s localhost:3000/api/public/health)"
python3 "$S/lf3/scan.py" spec.json http://localhost:3000 scan.json | head -40
docker compose down -v >/dev/null 2>&1
