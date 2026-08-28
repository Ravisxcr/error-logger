package sentryevent

import (
	"encoding/json"
	"testing"
)

func TestEventParsing_FullSentryPayload(t *testing.T) {
	rawJSON := `{
		"event_id": "fc6405785a214844b34bc9730722d37e",
		"timestamp": "2026-08-28T10:15:30.123456Z",
		"platform": "python",
		"level": "error",
		"logger": "my.module",
		"server_name": "worker-1",
		"release": "v2.1.0",
		"environment": "production",
		"message": "Custom event message",
		"tags": [["env", "production"], ["version", "2.1"]],
		"extra": {"request_id": "req-123", "retry_count": 3},
		"user": {"id": "usr_99", "email": "user@test.com", "username": "tester", "ip_address": "127.0.0.1"},
		"request": {"method": "POST", "url": "https://api.example.com/items", "query_string": "sort=desc"},
		"contexts": {
			"runtime": {"name": "CPython", "version": "3.12.3"},
			"os": {"name": "Linux", "version": "6.6.0"}
		},
		"breadcrumbs": [
			{"timestamp": 1724836530.12, "category": "auth", "message": "User login ok", "level": "info"},
			{"timestamp": "2026-08-28T10:15:29Z", "category": "db", "message": "Query executed", "level": "debug"}
		],
		"exception": [
			{
				"type": "KeyError",
				"value": "'item_id'",
				"module": "collections",
				"mechanism": {"type": "try_catch", "handled": false},
				"stacktrace": {
					"frames": [
						{
							"filename": "app/service.py",
							"function": "get_item",
							"lineno": 45,
							"context_line": "    return data['item_id']",
							"pre_context": ["def get_item(data):", "    # Fetch item"],
							"post_context": ["", "def list_items():"],
							"in_app": true,
							"vars": {"data": {}}
						}
					]
				}
			}
		],
		"modules": {"sentry-sdk": "2.64.0"}
	}`

	var ev Event
	if err := json.Unmarshal([]byte(rawJSON), &ev); err != nil {
		t.Fatalf("failed to parse event JSON: %v", err)
	}

	if ev.EventID != "fc6405785a214844b34bc9730722d37e" {
		t.Errorf("expected EventID fc6405785a214844b34bc9730722d37e, got %s", ev.EventID)
	}
	if ev.Platform != "python" {
		t.Errorf("expected platform python, got %s", ev.Platform)
	}
	if ev.Tags["env"] != "production" || ev.Tags["version"] != "2.1" {
		t.Errorf("unexpected tags: %v", ev.Tags)
	}
	if ev.User == nil || ev.User.Email != "user@test.com" {
		t.Errorf("unexpected user: %v", ev.User)
	}
	if ev.Request == nil || ev.Request.URL != "https://api.example.com/items" {
		t.Errorf("unexpected request: %v", ev.Request)
	}
	if ev.Breadcrumbs == nil || len(ev.Breadcrumbs.Values) != 2 {
		t.Fatalf("expected 2 breadcrumbs, got %v", ev.Breadcrumbs)
	}
	if ev.Exception == nil || len(ev.Exception.Values) != 1 {
		t.Fatalf("expected 1 exception, got %v", ev.Exception)
	}
	exc := ev.Exception.Values[0]
	if exc.Type != "KeyError" || exc.Value != "'item_id'" {
		t.Errorf("unexpected exception type/value: %s: %s", exc.Type, exc.Value)
	}
	if exc.Mechanism == nil || *exc.Mechanism.Handled != false {
		t.Errorf("expected mechanism handled=false, got %v", exc.Mechanism)
	}
	if exc.Stacktrace == nil || len(exc.Stacktrace.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %v", exc.Stacktrace)
	}
	frame := exc.Stacktrace.Frames[0]
	if frame.Filename != "app/service.py" || frame.Lineno != 45 || frame.Function != "get_item" {
		t.Errorf("unexpected frame attributes: %v", frame)
	}
	if len(frame.PreContext) != 2 || len(frame.PostContext) != 2 {
		t.Errorf("unexpected context lines: pre=%v, post=%v", frame.PreContext, frame.PostContext)
	}
}

