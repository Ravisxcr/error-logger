package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravisxcr/error-logger/internal/sentryevent"
	"github.com/ravisxcr/error-logger/internal/store"
)

func TestWebRendering(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "events.jsonl"), 100)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	// Insert sample event
	sampleEvent := &sentryevent.Event{
		EventID:     "1234567890abcdef1234567890abcdef",
		Platform:    "python",
		Level:       "error",
		Logger:      "main",
		ServerName:  "srv-1",
		Environment: "prod",
		Release:     "v1.0.0",
		LogEntry: &sentryevent.Message{
			Message: "Test error message",
		},
		Exception: &sentryevent.ExceptionContainer{
			Values: []sentryevent.Exception{
				{
					Type:  "ValueError",
					Value: "Invalid value provided",
					Stacktrace: &sentryevent.Stacktrace{
						Frames: []sentryevent.Frame{
							{
								Filename: "main.py",
								Function: "run",
								Lineno:   42,
								InApp:    true,
							},
						},
					},
				},
			},
		},
	}
	st.Add(store.Captured{
		ID:         "1234567890abcdef1234567890abcdef",
		ProjectID:  "proj-1",
		Kind:       "event",
		ReceivedAt: time.Now(),
		LastSeen:   time.Now(),
		Count:      1,
		Event:      sampleEvent,
	})

	handler := &Handler{Store: st, DisableDelete: false}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Test GET /
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); len(body) == 0 {
		t.Fatalf("GET / returned empty body")
	}

	// Test GET /projects/proj-1
	req = httptest.NewRequest("GET", "/projects/proj-1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /projects/proj-1 returned status %d: %s", rec.Code, rec.Body.String())
	}

	// Test GET /events/{id}
	events := st.ListByProject("proj-1")
	if len(events) == 0 {
		t.Fatalf("expected at least 1 event in store")
	}
	eventID := events[0].ID

	req = httptest.NewRequest("GET", "/events/"+eventID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/%s returned status %d: %s", eventID, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if len(body) == 0 {
		t.Fatalf("GET /events/%s returned empty body", eventID)
	}
}

