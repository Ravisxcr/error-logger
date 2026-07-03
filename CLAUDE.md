# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A local, Sentry-compatible error ingestion server for development, written in Go. It implements the same HTTP endpoints a real Sentry project exposes (the envelope and legacy store APIs), so any official Sentry SDK — the Python `sentry-sdk` in particular — can send events to it just by pointing its DSN at this server instead of sentry.io. No external account or network access needed for local dev.

Captured events are printed to the console (color-coded), browsable in a small web dashboard, and persisted to an append-only `data/events.jsonl` file.

## Commands

```sh
go build ./...                 # build everything
go vet ./...                   # static checks
gofmt -l .                     # list files needing formatting (gofmt -w . to fix)
go run ./cmd/server             # run the server (dashboard at http://localhost:9000/)
go run ./cmd/server -addr :PORT -data-dir DIR -capacity N
```

There are currently no automated tests in the module.

To exercise the server end-to-end, use the bundled Python demo:

```sh
cd demo && uv sync && uv run main.py   # or: pip install -e . && python main.py
```

The demo's `sentry_sdk.init(dsn=...)` in `demo/main.py` points at `http://demo@127.0.0.1:9000/1`; run the Go server first, then the demo, and confirm the raised `ValueError` shows up in the server console and at `http://localhost:9000/`. Note: `demo/` is gitignored (it's a local sandbox, not part of the module).

## Architecture

Request flow: an SDK gzip-POSTs a newline-delimited "envelope" to `/api/{project_id}/envelope/` (or a single JSON body to the legacy `/api/{project_id}/store/`) → parsed into typed events → stored (memory + JSONL) → rendered to console and dashboard.

- **`internal/envelope`** — parses the Sentry envelope wire format itself: a header JSON line followed by (item header, item payload) pairs. Item payloads are either JSON or length-prefixed binary. Handles gzip decompression. This package knows nothing about event *contents* — it just splits the envelope into typed items.
- **`internal/sentryevent`** — Go structs for the subset of the Sentry event JSON schema this server understands (Event, Exception, Stacktrace, Frame, Breadcrumb, User, Request, SDK, tags). `Message` and `TagSet` have custom `UnmarshalJSON` because Sentry SDKs encode them inconsistently across versions (message as bare string vs `{"formatted": ...}`; tags as an object vs an array of `[key, value]` pairs).
- **`internal/store`** — `Store` holds a bounded in-memory ring buffer (`Captured` structs, newest last, capacity-evicted) for the dashboard, plus an always-appended `events.jsonl` file for full history. All access is mutex-guarded; `Captured.ID` is indexed for O(1) detail lookups.
- **`internal/ingest`** — HTTP handlers that glue `envelope` + `sentryevent` + `store` together. `handleEnvelope` iterates envelope items: `event`/`transaction` items are decoded into `sentryevent.Event`; unrecognized item types (`session`, `profile`, `client_report`, attachments, ...) are still acknowledged and stored as a placeholder (kind only, no `Event`) so the SDK never sees a failed request. Auth (`X-Sentry-Auth`, DSN key) is intentionally **not validated** — this is a dev-only tool. Responses mimic Sentry's ack format (`{"id": "..."}`).
- **`internal/console`** — pretty-prints a `store.Captured` to stdout with ANSI colors keyed by level (error/fatal=red, warning=yellow, info=blue, debug=gray); truncates long stack traces to the last 8 frames.
- **`internal/web`** — dashboard handlers (`GET /`, `GET /events/{id}`) using `html/template` with templates and CSS embedded via `go:embed` (`internal/web/templates/*.html`, `internal/web/static/*`). View models (`eventRow`, `detailView`) are built in `web.go` rather than doing logic in templates. The list page auto-refreshes via `<meta http-equiv="refresh">` (no JS/polling by design).
- **`cmd/server/main.go`** — wires flags (`-addr`, `-data-dir`, `-capacity`) to `store.Open`, `ingest.Handler`, and `web.Handler` on a single `http.ServeMux`, using Go 1.22+ method+pattern routing (`"POST /api/{project_id}/envelope/"`).

Key design constraints to preserve:
- **No external dependencies** — the whole server is Go stdlib only (including gzip, html/template, embed). Keep it that way unless there's a strong reason not to.
- **Never fail an SDK request** for envelope item types we don't fully parse — always ack with 200, or the SDK will retry/back off and the dev loses visibility.
- Event/item IDs: prefer `event.EventID`, fall back to the envelope header's `event_id`, fall back to a random 32-hex-char ID (`ingest.newID`).

## Sentry event types

Sentry captures more than just errors — it's built around a general "event" concept with several types:

- **Error events** — exceptions/crashes, the primary use case. Includes stack trace, exception type/value, mechanism (handled vs unhandled).
- **Message events** — arbitrary log-style captures via `captureMessage()`, with a severity level (`fatal`, `error`, `warning`, `info`, `debug`). `info`-level messages are a first-class citizen, not just errors.
- **Transaction events** — performance monitoring / tracing data (spans, durations), used for APM-style features. Structurally different from error/message events — sent as a `transaction` envelope item type.
- **Breadcrumbs** — not standalone events, but a trail of prior actions/logs (clicks, HTTP requests, console logs) attached to whatever event eventually fires, giving context leading up to it.
- **Session/replay data** — release health (crash-free session %) and, in some SDKs, session replay recordings. These are separate envelope item types too (`session`, `replay_event`, etc.).

Since this server parses Sentry envelopes, the key implication is: an envelope can contain different **item types**, not just `event`. Only handling `type: event` items silently drops `transaction`, `session`, and `client_report` items that some SDKs send by default (e.g. the JS SDK sends performance transactions unless tracing is disabled).

For a minimal error logger, the target behavior is:
- Parse and store `event` items (both error and message subtypes — check the `level` field, and whether `exception` or just `message` is present).
- Either ignore or separately log `transaction`/`session`/`client_report` items so parsing doesn't choke on them.

## Progress

Status as of the last session: **initial implementation complete and verified end-to-end.**

Built:
- Envelope parser (gzip + NDJSON items, length-prefixed and newline-terminated payloads)
- Sentry event schema types covering exceptions/stacktraces/breadcrumbs/user/request/tags/SDK/contexts
- In-memory + JSONL-persisted event store
- `/api/{project_id}/envelope/` and `/api/{project_id}/store/` ingestion endpoints, plus no-op acks for `/security/` and `/minidump/`
- Color-coded console output
- Web dashboard (list + detail views, embedded templates/CSS)
- `cmd/server` binary with flags
- `demo/main.py` updated to point at the local server (`http://demo@127.0.0.1:9000/1`)

Verified: ran the server, ran `demo/main.py`, confirmed the raised `ValueError` was captured correctly through the full pipeline (console output, dashboard list/detail pages, `events.jsonl`) including stack trace, tags, and SDK/runtime context.

Not yet done / possible next steps:
- No automated tests (unit tests for `envelope.Parse`, `sentryevent` custom unmarshalers, `store` eviction would be the highest-value additions)
- No auth/validation of the Sentry DSN key (intentional for now, dev-only)
- No filtering/search on the dashboard, no grouping/deduplication of repeated errors
- Nothing has been committed to git yet — current work is unstaged in the `initial-setup` branch

## References

- https://docs.sentry.io/api/requests/ — request/auth conventions (`X-Sentry-Auth` header, DSN key) real Sentry servers expect; relevant if DSN validation is ever added.
- https://docs.sentry.io/api/ratelimits/ — rate-limit response format (`429`, `Retry-After`, `X-Sentry-Rate-Limits`); relevant if rate limiting is ever added.
