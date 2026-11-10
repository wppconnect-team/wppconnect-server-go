#!/usr/bin/env bash
set -euo pipefail
workspace="$(mktemp -d "$RUNNER_TEMP/chatwoot-inbox.XXXXXXXX")"
app="chatwoot-inbox-fixture-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"
trap 'docker rm -f "$app" >/dev/null 2>&1 || true' EXIT
export SECRET_KEY_BASE="$(openssl rand -hex 64)"
export RAILS_ENV=production POSTGRES_HOST=127.0.0.1 POSTGRES_DATABASE=chatwoot POSTGRES_USERNAME=postgres
export REDIS_URL=redis://127.0.0.1:6379 FRONTEND_URL=http://127.0.0.1:3000 ENABLE_TELEMETRY=false
common=(--network host --env SECRET_KEY_BASE --env RAILS_ENV --env POSTGRES_HOST --env POSTGRES_DATABASE --env POSTGRES_USERNAME --env POSTGRES_PASSWORD --env REDIS_URL --env FRONTEND_URL --env ENABLE_TELEMETRY)
docker run --rm "${common[@]}" --entrypoint bundle "$CHATWOOT_TEST_IMAGE" exec rails db:chatwoot_prepare
docker run -d --name "$app" "${common[@]}" -v "$PWD/scripts/chatwoot-inbox-fixture.rb:/tmp/seed-relay.rb:ro" -v "$workspace:/fixture" --entrypoint bundle "$CHATWOOT_TEST_IMAGE" exec rails server -b 127.0.0.1 -p 3000 >/dev/null
for i in {1..60}; do
  if curl --silent --output /dev/null http://127.0.0.1:3000/; then break; fi
  sleep 2
done
docker exec "$app" bundle exec rails runner /tmp/seed-relay.rb
export CHATWOOT_INBOX_FIXTURE="$workspace/state.json"
go test ./cmd/chatwoot-text-relay -count=1 -run '^TestChatwootAPIInbox$' -v
