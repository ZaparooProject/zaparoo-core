# API timeout diagnostics

`apidiag` records content-free timing and pressure snapshots for HTTP JSON-RPC
and dispatched WebSocket requests. It does not change request deadlines,
responses, database admission, or cancellation behavior. Collection is disabled
when telemetry is disabled, except for explicitly injected offline test sinks.

## Lifecycle and timings

- WebSocket request queue wait is recorded separately. The request budget still
  starts at execution, not enqueue time. Unbounded backup methods stay unbounded.
- A deadline callback reports even when a handler or database admission lock is
  still blocked. Stage completion, error handling, and response cleanup share an
  exactly-once recorder.
- A returned nested deadline or network timeout is labeled `operation`; expiry
  of the request context is labeled `request`. Disconnect/shutdown cancellation
  and ordinary successful completion produce no diagnostic event.
- The recorder remains attached through response queueing, serialization,
  writing, and `AfterWrite`. HTTP request-body reading, pre-dispatch WebSocket
  failures, heartbeat frames, REST routes, and SSE are outside this envelope.
- Timings are inclusive and may overlap. Do not add stage totals to estimate
  elapsed time. `active_stages` includes unfinished work; `timeout_stage` is the
  most recently started active stage, not proof of a particular root cause.
- Shared hooks cover handler execution, WebSocket database admission and image
  slots, response building, response queueing, response writing, and callbacks.
  Search/browse also measure concurrency admission and database work. Browse's
  explicit connection acquisition has a separate `database_pool` phase. Other
  methods initially have shared envelope timings, not exhaustive internal spans.
- WebSocket `response_write` includes encryption/session-lock work and Melody
  frame enqueueing. It does **not** establish socket delivery or client receipt.

`Begin` returns an idempotent completion function. Concurrent spans are bounded
at 16. No request payload or arbitrary label can be attached to a span. When
adding a method, update the explicit method-name allowlist only after privacy
review; unknown names become `unknown`. A registration coverage test guards drift.

## Pressure snapshots

The start and timeout snapshots use only SQL pool counters, atomics, and
nonblocking reads of in-memory activity flags. No SQL query, filesystem probe,
network call, profile, or `runtime.ReadMemStats` is performed to build them.

Both database pools expose open/in-use/idle/maximum counts. Pool wait deltas are
**shared process counters**, not that request's own waits; decreasing counters
make the delta unavailable. MediaDB additionally exposes indexing pool-boost,
optimization, recovery, and transaction state. Scraping, playback, and service
recovery are flags only. Contended state locks produce `unknown`, not `false`.

Runtime heap size, goroutine count, and process age use coarse power-of-two
buckets. Durations and counters have upper bounds. Snapshot values are taken at
slightly different instants and are clues, not an atomic view of the process.

## Sentry boundary

`telemetry.CaptureAPITimeout` uses a fresh scope. The SDK's `BeforeSend` hook
reconstructs the entire event from a typed report carried in an `EventHint`,
retaining only fixed fields, validated enums, bounded numbers, and SDK/build
metadata. The message is always `API request timed out`.

No request body, parameters, SQL, query, path, filename, URL, header, token,
client/request ID, input hash, raw error, stack, breadcrumb, attachment, or
inherited context/user is included. These events do not carry the existing
installation user ID, so Sentry affected-user counts are not available for them.
Offline fake-transport tests inject private canaries into error text and scope
fields and check the serialized event.

Each recorder emits at most one structured timeout event. Central API error
handling avoids a second unstructured timeout/cancellation event; known duplicate
history/activity query logs follow the same rule. Other legacy/background
telemetry keeps its existing policy: this boundary is not a global scrubber or a
full tracing/deduplication system.

Request shape, search terms, SQL tracing, library sizes, device profiling, and
performance fixes are deliberately excluded. These diagnostics identify the next
place to investigate; they do not by themselves establish why a request is slow.
