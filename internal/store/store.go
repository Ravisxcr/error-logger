// Package store keeps captured events in memory (for the dashboard) and
// persists them to an append-only JSONL file on disk (so a restart doesn't
// lose history during a dev session).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ravi/error-logger/internal/sentryevent"
)

// Captured is one stored event, either a full error/message event or a
// lighter-weight record for envelope item types we don't fully model
// (transaction, session, profile, ...).
type Captured struct {
	ID         string             `json:"id"`
	ProjectID  string             `json:"project_id"`
	Kind       string             `json:"kind"` // "event", "transaction", "session", etc.
	ReceivedAt time.Time          `json:"received_at"`
	Event      *sentryevent.Event `json:"event,omitempty"`
}

// Store holds a bounded in-memory history plus a durable JSONL log.
type Store struct {
	mu       sync.RWMutex
	items    []Captured // newest last
	byID     map[string]int
	capacity int
	file     *os.File
}

// Open creates (or appends to) dataDir/events.jsonl and returns a Store
// with the given in-memory capacity (oldest events are evicted from memory
// once exceeded; the file keeps the full history).
func Open(dataDir string, capacity int) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events log: %w", err)
	}
	return &Store{
		byID:     make(map[string]int),
		capacity: capacity,
		file:     f,
	}, nil
}

func (s *Store) Close() error {
	return s.file.Close()
}

// Add records a captured event, evicting the oldest in-memory entry if over
// capacity, and appends it to the durable log.
func (s *Store) Add(c Captured) error {
	line, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.file.Write(line); err != nil {
		return fmt.Errorf("write event log: %w", err)
	}

	s.items = append(s.items, c)
	if len(s.items) > s.capacity {
		evicted := len(s.items) - s.capacity
		s.items = s.items[evicted:]
		s.byID = make(map[string]int, s.capacity)
		for i, it := range s.items {
			s.byID[it.ID] = i
		}
	} else {
		s.byID[c.ID] = len(s.items) - 1
	}
	return nil
}

// List returns captured events, newest first.
func (s *Store) List() []Captured {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Captured, len(s.items))
	for i, it := range s.items {
		out[len(s.items)-1-i] = it
	}
	return out
}

// Get looks up a single captured event by ID.
func (s *Store) Get(id string) (Captured, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx, ok := s.byID[id]
	if !ok {
		return Captured{}, false
	}
	return s.items[idx], true
}

// Count returns the number of events currently held in memory.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}
