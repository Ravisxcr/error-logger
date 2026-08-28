# 🐞 Error Logger (Local Sentry-Compatible Server)

A local, lightweight, and Sentry-compatible error ingestion server designed for development. 

It implements the same HTTP endpoints a real Sentry project would (the envelope and store APIs). This means any official Sentry SDK — such as the Python `sentry-sdk` — can send events to it simply by changing the DSN. **No sentry.io account or internet connection is needed for local development.**

---

## ✨ Features

- **Sentry Protocol Compatible**: Emulates Sentry's `envelope` and `store` APIs, working seamlessly with existing Sentry SDKs.
- **Real-time Console Output**: Captured events are printed to the console immediately, featuring color-coded exception types, messages, and stack traces.
- **Web Dashboard**: A beautiful, built-in dashboard (served at `http://localhost:9000/`) to browse, inspect, and manage caught exceptions.
- **Event Persistence**: Errors are persisted locally to an append-only `data/events.jsonl` file.
- **Event & Project Management**: Supports deletion of individual captured Sentry events or entire projects directly from the dashboard.
- **Zero Configuration**: Fast and simple to start without complex authentication, rate limiting, or database setups.

## 🚀 Getting Started

### 1. Run the Server

You can run the server directly using Go:

```sh
go run ./cmd/server
```

**Configuration Options (Flags & Environment Variables):**

| Flag                   | Environment Variable | Default | Description                                                |
|------------------------|----------------------|---------|------------------------------------------------------------|
| `-addr`                | `ADDR` or `PORT`     | `:9000` | Address to listen on (e.g. `:8080`, `8080`)                |
| `-data-dir`            | `DATA_DIR`           | `data` (`/app/data` in Docker) | Directory for the `events.jsonl` database |
| `-capacity`            | `CAPACITY`           | `1000`  | Max events kept in memory for the dashboard                |
| `-disable-delete`      | `DISABLE_DELETE`     | `false` | Disable deleting events/issues/projects from the dashboard |
| `-disable-console-log` | `DISABLE_CONSOLE_LOG`| `false` | Disable printing captured events to the console            |

*Note: CLI flags take precedence over environment variables, which take precedence over default values.*

Once running, you can access the dashboard at: **http://localhost:9000/**

### 2. Point Your App to the Server

Initialize your Sentry SDK as usual, but point the DSN to your local server. The key and project ID (e.g., `1`) can be anything!

**Python Example:**
```python
import sentry_sdk

# The authentication key and project ID are not strictly enforced.
sentry_sdk.init(dsn="http://any_key@127.0.0.1:9000/1")

# Test it out!
raise ValueError("Oops, something went wrong!")
```

## 🎮 Bundled Demo

The repository includes a minimal Python project to test the integration.

1. Start the go server in one terminal: `go run ./cmd/server`
2. Run the Python demo in another terminal:

```sh
cd demo
uv sync            # or: pip install -e .
uv run main.py     # or: python main.py
```

The demo raises a `ValueError`, which will immediately show up in the server console and on the local dashboard.

## 📁 Project Layout

```text
├── cmd/server/            # Application entry point (flags, wiring)
├── internal/
│   ├── sentryevent/       # Go types for the Sentry event JSON schema
│   ├── envelope/          # Envelope wire-format parser (gzip + NDJSON items)
│   ├── store/             # In-memory ring buffer, JSONL persistence & deletion
│   ├── ingest/            # HTTP handlers for /api/{project_id}/envelope/ and /store/
│   ├── console/           # Colored console output formatting
│   └── web/               # Web dashboard (html/template + embedded CSS/JS)
└── demo/                  # Minimal Python project using sentry_sdk
```

## ⚠️ Scope & Limitations

This tool is explicitly designed as a **dev-time inbox**, not a production Sentry replacement.
- No authentication or authorization.
- No rate limiting.
- No multi-project isolation (beyond tagging events by project ID).
- No grouping or deduplication of similar errors.

**Supported Event Types:**
- `event` and `transaction` envelope items are fully parsed and displayed.
- Other item types (e.g., `session`, `profile`, `client_report`, attachments) are acknowledged and saved as placeholders so the SDK operates normally, but they are not fully processed.

---
*Built for developers who want a fast, local feedback loop for application errors.*
