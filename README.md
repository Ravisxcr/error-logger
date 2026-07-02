# error-logger

A local, Sentry-compatible error ingestion server for development. It
implements the same HTTP endpoints a real Sentry project would (the
envelope and store APIs), so any official Sentry SDK — the Python
`sentry-sdk` in particular — can send events to it just by changing the
DSN. No sentry.io account needed for local dev.

Captured events are:
- printed to the console as they arrive, color-coded with exception type,
  message, and stack trace
- browsable in a small web dashboard
- persisted to an append-only `data/events.jsonl` file

## Run the server

```sh
go run ./cmd/server
```

Flags:

| flag         | default | description                                  |
|--------------|---------|-----------------------------------------------|
| `-addr`      | `:9000` | address to listen on                          |
| `-data-dir`  | `data`  | directory for `events.jsonl`                  |
| `-capacity`  | `1000`  | events kept in memory for the dashboard        |

Dashboard: http://localhost:9000/

## Point a Python app at it

```python
import sentry_sdk

sentry_sdk.init(dsn="http://any_key@127.0.0.1:9000/1")
```

The key and project ID (`1` above) can be anything — this server doesn't
enforce auth, it's meant for local dev only.

## Try it with the bundled demo

```sh
cd demo
uv sync            # or: pip install -e .
uv run main.py      # or: python main.py
```

(in another terminal, run `go run ./cmd/server` first). The demo raises a
`ValueError`, which shows up immediately in the server console and at
http://localhost:9000/.

## Project layout

```
cmd/server/            entry point (flags, wiring)
internal/sentryevent/  Go types for the Sentry event JSON schema
internal/envelope/     envelope wire-format parser (gzip + NDJSON items)
internal/store/        in-memory ring buffer + JSONL persistence
internal/ingest/       HTTP handlers for /api/{project_id}/envelope/ and /store/
internal/console/      colored console output
internal/web/          dashboard (html/template + embedded CSS)
demo/                  minimal Python project using sentry_sdk against this server
```

## Scope

This is a dev-time inbox, not a Sentry replacement: no auth, no rate
limiting, no multi-project isolation beyond tagging by project ID, no
grouping/deduplication of similar errors. `event` and `transaction`
envelope items are fully parsed; other item types (`session`, `profile`,
`client_report`, attachments, …) are acknowledged but stored only as a
placeholder so the SDK doesn't see failed requests.
