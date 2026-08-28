package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ravisxcr/error-logger/internal/sentryevent"
)

func TestStoreAddAndDeduplicate(t *testing.T) {
	tempDir := t.TempDir()
	st, err := Open(tempDir, 10)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	ev := &sentryevent.Event{
		EventID: "11111111111111111111111111111111",
		Level:   "error",
		Message: &sentryevent.Message{Message: "Database connection failed"},
	}

	// 1. Add first occurrence
	c1, isNew, err := st.Add(Captured{
		ID:         "11111111111111111111111111111111",
		ProjectID:  "backend",
		Kind:       "event",
		ReceivedAt: time.Now(),
		GroupKey:   "db_error_key",
		Event:      ev,
	})
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew=true for first occurrence")
	}
	if c1.Count != 1 {
		t.Errorf("expected Count=1, got %d", c1.Count)
	}

	// 2. Add second occurrence of same group
	c2, isNew, err := st.Add(Captured{
		ID:         "22222222222222222222222222222222",
		ProjectID:  "backend",
		Kind:       "event",
		ReceivedAt: time.Now(),
		GroupKey:   "db_error_key",
		Event:      ev,
	})
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if isNew {
		t.Errorf("expected isNew=false for duplicate occurrence")
	}
	if c2.Count != 2 {
		t.Errorf("expected Count=2, got %d", c2.Count)
	}
	if st.Count() != 1 {
		t.Errorf("expected 1 unique grouped issue in memory, got %d", st.Count())
	}
}

func TestStorePersistenceAndReplay(t *testing.T) {
	tempDir := t.TempDir()
	st, err := Open(tempDir, 100)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	st.Add(Captured{
		ID:         "event-1",
		ProjectID:  "api",
		Kind:       "event",
		ReceivedAt: time.Now(),
		Event:      &sentryevent.Event{EventID: "event-1", Level: "info"},
	})
	st.Close()

	// Reopen store from same directory to test replay
	reopened, err := Open(tempDir, 100)
	if err != nil {
		t.Fatalf("reopen store failed: %v", err)
	}
	defer reopened.Close()

	if reopened.Count() != 1 {
		t.Errorf("expected 1 event replayed from disk, got %d", reopened.Count())
	}
	item, ok := reopened.Get("event-1")
	if !ok {
		t.Errorf("expected event-1 to be present after replay")
	}
	if item.ProjectID != "api" {
		t.Errorf("expected ProjectID=api, got %s", item.ProjectID)
	}
}

func TestStoreDelete(t *testing.T) {
	tempDir := t.TempDir()
	st, err := Open(filepath.Join(tempDir, "data"), 100)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	st.Add(Captured{ID: "e1", ProjectID: "projA", Kind: "event", ReceivedAt: time.Now()})
	st.Add(Captured{ID: "e2", ProjectID: "projB", Kind: "event", ReceivedAt: time.Now()})

	if st.Count() != 2 {
		t.Fatalf("expected 2 events, got %d", st.Count())
	}

	// Delete project projA
	st.DeleteProject("projA")
	if st.Count() != 1 {
		t.Errorf("expected 1 event after deleting projA, got %d", st.Count())
	}
	if _, ok := st.Get("e1"); ok {
		t.Errorf("expected e1 to be deleted")
	}
	if _, ok := st.Get("e2"); !ok {
		t.Errorf("expected e2 to be retained")
	}
}

