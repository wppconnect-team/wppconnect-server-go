# Persistent send idempotency

The native Go API accepts an optional `Idempotency-Key` header on the send
operations listed in OpenAPI. Without it, the existing behavior is unchanged.
This requires a build containing the send ledger; old images do not implement
it. Cloud proxy support and upgrading existing allocations are separate rollout
steps. Do not assume an older server honors an unknown header.

Use a new random key for each intended operation, such as a UUID. Keys contain
1..128 ASCII letters, digits, `.`, `_`, `:` or `-`. Keep the same key and exact
request bytes when retrying that operation. The fingerprint includes the route,
query, Content-Type and complete body. JSON formatting changes or a new multipart
boundary therefore conflict, as do aliases with different native route names.

```sh
curl -X POST "$SERVER/api/$SESSION/send-message" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 03263df0-7611-451e-9404-941f5930bafd' \
  -d '{"phone":"5511999999999","message":"Hello"}'
```

The server commits a reservation to `.sends.db` before executing the handler.
It commits the HTTP result before returning it. Repeating a completed request
returns the saved status and body with `Idempotency-Replayed: true`, without
uploading media or sending again. A changed request with the same session/key
returns `409 IDEMPOTENCY_CONFLICT`. The same key in another session is independent.
Authentication still runs before every lookup, including cached responses.

After losing the HTTP response, retrieve it without another send:

```sh
curl "$SERVER/api/$SESSION/send-result" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Idempotency-Key: 03263df0-7611-451e-9404-941f5930bafd'
```

This returns `state`, `httpStatus`, `response` and Unix-seconds `createdAt`:

| State | Meaning and action |
| --- | --- |
| `complete` | The handler result was persisted. Inspect its HTTP status and body, including errors and accepted message IDs. This is not a recipient delivery/read receipt. |
| `ambiguous` | A 408 or 5xx result was persisted. Some or all protocol work may have happened. Same-key retries return that error without repeating work. Reconcile observed messages/ACKs before deciding what to do. |
| `pending_or_unknown` | The handler is running or the process stopped before recording its result. Repeating the POST returns 409. Do not invent a new key to bypass it. |

A missing key returns `404 SEND_RESULT_NOT_FOUND` in this session. A successful
lookup always returns HTTP 200; `httpStatus` describes the original operation.
Validation errors are also retained. To intentionally correct a failed request,
use a new key only after confirming no uncertain protocol work remains.

This provides durable duplicate suppression, not exactly-once WhatsApp delivery.
It cannot resolve a process crash between protocol acceptance and result commit.
The API does not automatically resend or erase unresolved reservations. Batch
sends retain the original completed IDs and failure information together.

## Storage and operation

The SQLite ledger uses WAL and FULL synchronous commits, with one logical owner
per reservation. Concurrent requests and separate connections share the same
unique session/key constraint. Include `.sends.db` and its live WAL consistently
in backup/restore alongside session state. Never restore one to an older point
without reconciling the other. Protect the data directory: results may contain
message identifiers, recipient metadata and handler error details. Raw keys,
authentication headers and request bodies are not stored in the ledger.

Complete records expire after at least seven days, pruned on a subsequent claim.
Never reuse keys for new work, and never retry an old operation beyond the
retention window. Ambiguous and pending records are not automatically expired.
Capacity is bounded to 100,000 records and 128 MiB of response/reservation data.
Each pending operation reserves 64 KiB for its result. If storage is unavailable
or capacity is exhausted, new keyed sends fail with 503 before protocol work.
An oversized/unpersistable result stays unresolved and returns
`SEND_RESULT_UNKNOWN`; it is never silently retried.

There are four concurrent keyed-send slots. Request bodies are bounded by the
existing media request limit and spooled to private temporary files, removed on
normal completion. Use a private, size-limited temporary filesystem and account
for files left by an abrupt process termination. No background sender retries
these commands. Unsupported operations reject the header explicitly; see the
generated OpenAPI rather than assuming every mutation has this guarantee.
