# Diagnose and recover failed deliveries

Use the native Go API with the session's bearer token, or an administrator
credential. Every query and replay checks the session. Responses contain
delivery metadata, not message bodies, QR codes, signing keys or target URLs.

```sh
curl "$SERVER/api/$SESSION/events/deliveries?target=webhook&state=failed&limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

`target` is required: `webhook`, `nats`, `rabbitmq` or `redis-streams`. `state` is `failed`
(default), `pending` or `delivered`. `cursor` is an event ingestion sequence,
initially zero; `limit` is 1..200. Keep the target and state unchanged while
paging. This is a live view, not a frozen snapshot: restart from zero to discover
an older event that changed state. One target per query avoids skipping another
delivery of the same event at a page boundary.

Each item contains `eventId`, `event`, `target`, `state`, `attempt`, `createdAt`,
`nextAttemptAt`, `leasedUntil` and `failureCode`. Timestamps use Unix milliseconds.
Failure codes are bounded categories: `transport_error`, `timeout`,
`permanent_rejection` or `retry_exhausted`. No arbitrary transport error strings
are stored. Empty codes may identify deliveries migrated from an older version.
The current delivery record stores the most recent category, not a full attempt
history. Successful delivery clears it.

After fixing the destination, requeue only its failed delivery:

```sh
curl -X POST "$SERVER/api/$SESSION/events/retry-failed" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"eventId":"RETAINED_EVENT_UUID","target":"webhook"}'
```

The target must be enabled in the current process. HTTP 202 means queued, not
delivered. The transaction checks ownership, failed state and absence of an
active lease, resets that target's attempt budget, and writes an audit record.
Other targets remain unchanged. Concurrent attempts cannot both requeue the same
failed state. A pending, delivered or leased target returns 409. An expired event
or event belonging to another session returns 404. Fix the receiver first: each
manual replay starts another bounded attempt cycle.

Retries and replay preserve the event ID. Receivers must authenticate the signed
raw body, persist before acknowledging and deduplicate by `X-WPP-Event-ID`.
The legacy `/events/replay` endpoint deliberately retains its previous behavior:
it requeues all enabled targets, including successful ones. Use `retry-failed`
for selective recovery; legacy calls are now also visible in the audit.

```sh
curl "$SERVER/api/$SESSION/events/replay-audit?cursor=0&limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

Audit cursors are monotonic audit IDs, independent from event cursors. Entries
include event ID, target, time, mode (`failed-only` or `all-states`) and credential
type (`administrator`, `session-token` or `legacy-api`). This identifies the
credential class, not a named human operator. Audit and state changes commit
together. Entries disappear with their parent event and the audit is capped at
100,000 rows. Export to protected external audit storage if longer retention or
individual operator attribution is required.

## Metrics and alerting

The administrator-authenticated `/metrics` endpoint additionally exports:

| Metric | Meaning |
| --- | --- |
| `wppconnect_event_log_events` / `wppconnect_event_log_max_events` | Event count and configured event limit |
| `wppconnect_event_log_bytes` / `wppconnect_event_log_max_bytes` | Logical event payload bytes and configured byte limit, not total disk/WAL usage |
| `wppconnect_target_deliveries{target,state}` | Current delivery count per fixed target/state |
| `wppconnect_target_oldest_pending_seconds{target}` | Age since ingestion of the oldest pending event, including retry delays and leases |
| `wppconnect_rejected_events_total` | Process-local rejection counter; resets after restart |

No phone numbers, session IDs, event IDs or URLs become metric labels. Queue
series are omitted when empty. Scrape authentication must remain outside public
configuration files. A temporary receiver outage should raise age/count metrics;
after recovery the pending series disappears and delivered count increases.

Example Prometheus rules are in [event-delivery-alerts.yml](event-delivery-alerts.yml).
They are operator configuration examples, not an installed alerting service.
Initial thresholds are 80% logical capacity, pending age above 120 seconds for
five minutes, and any failed deliveries for five minutes. Tune these against
your delivery SLO and route alerts through your existing monitoring system.
Also monitor scrape health and filesystem capacity, because inability to persist
an event occurs before the durable-delivery guarantee begins.

The durable log remains bounded by retention, event count and payload bytes.
It is not an infinite archive. Do not remove session volumes or edit SQLite while
the server is using it. Migration adds failure metadata and audit tables without
resetting existing delivery states or attempt counts.

## Redis Streams (opt-in)

Leave `REDIS_STREAM_URL` empty to disable the target. Only the runtime operator selects the destination. Configure a dedicated Redis database or ACL-protected key for this runtime: one target receives the events of every session served by the process. Do not share a stream with another customer. Use `rediss://` for TLS across hosts. Inject credentials through the deployment's secret store. Do not put connection URLs in tickets or logs. The URL may contain a database number, but no query options.

`REDIS_STREAM_KEY` defaults to `wppconnect.events` (1..128 letters, digits, dots, underscores, hyphens or colons). `REDIS_STREAM_MAXLEN` defaults to 10000 entries (1..1000000). Each [XADD](https://redis.io/docs/latest/commands/xadd/) appends the fields `eventId`, `session`, `event`, `createdAt` (Unix milliseconds) and `payload` (exact JSON), trimming the oldest entries to this count. The limit is in entries, not bytes. Size Redis memory and persistence for your payloads. Trimming may remove entries a slow consumer has not processed.

A successful delivery means Redis acknowledged the append. It does not confirm consumer processing, replica acknowledgement or a disk flush. Consumers must deduplicate by `eventId`, not the Redis stream entry ID: after a lost reply, replay can append a second entry for the same event. Retain deduplication records through your replay window. The existing SQLite outbox controls attempts, failure reporting and selective replay; use `target=redis-streams` when investigating. Moving the configured destination also changes where future retries for this target are delivered.

For an isolated integration test use the Redis service in `deploy/integration-test.yml` and set `INTEGRATION_REDIS_URL=redis://127.0.0.1:16379/0`. The test uses a unique key, forces a wrong-type rejection, recovers, publishes through a second client, and checks exact trimming and preservation of identity and payload.
