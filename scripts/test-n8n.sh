#!/usr/bin/env bash
# Isolated n8n service, synthetic events, no production destinations.
set -euo pipefail
image=docker.n8n.io/n8nio/n8n:2.47.3
fixture_id="$(python3 -c 'import uuid;print(uuid.uuid4().hex)')"
volume="wpp-n8n-${fixture_id}"
container="wpp-n8n-${fixture_id}"
temp_dir="$(mktemp -d)"
cleanup() {
  if [ "$(docker inspect --format '{{index .Config.Labels "wpp.fixture"}}' "$container" 2>/dev/null || true)" = "$fixture_id" ]; then docker rm -f "$container" >/dev/null; fi
  if [ "$(docker volume inspect --format '{{index .Labels "wpp.fixture"}}' "$volume" 2>/dev/null || true)" = "$fixture_id" ]; then docker volume rm "$volume" >/dev/null; fi
  rm -f -- "$temp_dir/credential.json" "$temp_dir/workflow.json"
  rmdir "$temp_dir"
}
trap cleanup EXIT
export INTEGRATION_N8N_TOKEN="$(openssl rand -hex 32)"
cp examples/n8n/receive-events.json "$temp_dir/workflow.json"
python3 - "$temp_dir/credential.json" <<'PY'
import json,os,sys
with open(sys.argv[1],"w") as f:
 json.dump([{"id":"WppHeaderFixture1","name":"WPPConnect workflow token","type":"httpHeaderAuth","data":{"name":"X-WPP-Workflow-Token","value":os.environ["INTEGRATION_N8N_TOKEN"]}}],f)
PY
# Disposable fixture; allow container uid 1000 to read the import.
chmod 755 "$temp_dir"
chmod 644 "$temp_dir/"*.json
docker volume create --label "wpp.fixture=$fixture_id" "$volume" >/dev/null
args=(--label "wpp.fixture=$fixture_id" -v "$volume:/home/node/.n8n" -v "$temp_dir:/fixture:ro" -e N8N_DIAGNOSTICS_ENABLED=false -e N8N_VERSION_NOTIFICATIONS_ENABLED=false -e N8N_PERSONALIZATION_ENABLED=false)
docker run --rm "${args[@]}" "$image" import:credentials --input=/fixture/credential.json >/dev/null
docker run --rm "${args[@]}" "$image" import:workflow --input=/fixture/workflow.json >/dev/null
docker run --rm "${args[@]}" "$image" publish:workflow --id=WppEventsFixture1 >/dev/null
docker run -d --name "$container" "${args[@]}" -p 127.0.0.1::5678 "$image" >/dev/null
port="$(docker port "$container" 5678/tcp | sed -n 's/^127.0.0.1://p')"
for i in $(seq 1 90); do if curl -fsS "http://127.0.0.1:$port/healthz/readiness" >/dev/null 2>&1; then break;fi; sleep 1; done
export INTEGRATION_N8N_URL="http://127.0.0.1:$port/webhook/wppconnect-events"
go test ./internal/eventbus -run '^TestIntegrationN8NReceiver$' -count=1 -timeout 60s
