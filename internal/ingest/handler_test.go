package ingest

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ravisxcr/error-logger/internal/store"
)

func setupTestServer(t *testing.T) (*store.Store, *http.ServeMux) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "events.jsonl"), 100)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	handler := &Handler{
		Store:  st,
		Logger: log.New(io.Discard, "", 0),
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return st, mux
}

func TestHandleEnvelope_PythonFormat(t *testing.T) {
	st, mux := setupTestServer(t)

	envelopePayload := `{"event_id":"409600d884be4db9824da5db530db721","sent_at":"2026-08-28T10:00:00Z"}
{"type":"event","content_type":"application/json"}
{"event_id":"409600d884be4db9824da5db530db721","platform":"python","level":"error","exception":{"values":[{"type":"ZeroDivisionError","value":"division by zero","stacktrace":{"frames":[{"filename":"div.py","lineno":10,"in_app":true}]}}]},"tags":{"runtime":"cpython"}}
`

	req := httptest.NewRequest("POST", "/api/python-proj/envelope/", bytes.NewBufferString(envelopePayload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	events := st.ListByProject("python-proj")
	if len(events) != 1 {
		t.Fatalf("expected 1 event in store, got %d", len(events))
	}
	if events[0].Event == nil || events[0].Event.Exception == nil || len(events[0].Event.Exception.Values) == 0 {
		t.Fatalf("expected exception in event, got nil")
	}
	if events[0].Event.Exception.Values[0].Type != "ZeroDivisionError" {
		t.Errorf("expected ZeroDivisionError, got %s", events[0].Event.Exception.Values[0].Type)
	}
	if events[0].Event.Tags["runtime"] != "cpython" {
		t.Errorf("expected tag runtime=cpython, got %v", events[0].Event.Tags)
	}
}

func TestHandleEnvelope_Gzipped(t *testing.T) {
	st, mux := setupTestServer(t)

	envelopePayload := `{"event_id":"aabbccddeeff00112233445566778899"}
{"type":"event"}
{"event_id":"aabbccddeeff00112233445566778899","platform":"node","level":"warning","logentry":{"message":"High memory usage"}}
`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte(envelopePayload))
	gw.Close()

	req := httptest.NewRequest("POST", "/api/node-proj/envelope", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	events := st.ListByProject("node-proj")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event.MessageText() != "High memory usage" {
		t.Errorf("expected message 'High memory usage', got %q", events[0].Event.MessageText())
	}
}

func TestHandleEnvelope_MultipleItems(t *testing.T) {
	st, mux := setupTestServer(t)

	// An envelope containing a session and an error event
	payload := `{"event_id":"11223344556677889900aabbccddeeff"}
{"type":"session","length":18}
{"started":"now"}
{"type":"event"}
{"event_id":"11223344556677889900aabbccddeeff","platform":"go","level":"error","message":"Worker panic"}
`
	req := httptest.NewRequest("POST", "/api/go-proj/envelope/", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	events := st.ListByProject("go-proj")
	if len(events) != 2 {
		t.Fatalf("expected 2 items captured (session + event), got %d", len(events))
	}
}

func TestHandleOptions_CORS(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/browser-proj/envelope/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected Access-Control-Allow-Origin=*, got %q", origin)
	}
}

