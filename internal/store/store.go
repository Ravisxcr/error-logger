// Package store keeps captured events in memory (for the dashboard) and
// persists them to an append-only JSONL file on disk (so a restart doesn't
// lose history during a dev session).
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	LastSeen   time.Time          `json:"last_seen"`
	Count      int                `json:"count"`
	GroupKey   string             `json:"group_key,omitempty"` // empty means "don't dedupe this"
	Event      *sentryevent.Event `json:"event,omitempty"`
}

// Store holds a bounded in-memory history plus a durable JSONL log.
type Store struct {
	mu       sync.RWMutex
	items    []Captured // newest last
	byID     map[string]int
	byGroup  map[string]int
	capacity int
	file     *os.File
}

// Open creates (or appends to) dataDir/events.jsonl and returns a Store
// with the given in-memory capacity (oldest events are evicted from memory
// once exceeded; the file keeps the full history). Any events already in
// the log from a previous run are replayed into memory first, so the
// dashboard shows prior history immediately instead of appearing empty
// until the next event arrives.
func Open(dataDir string, capacity int) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dataDir, "events.jsonl")
	s := &Store{
		byID:     make(map[string]int),
		byGroup:  make(map[string]int),
		capacity: capacity,
	}
	if err := s.replay(path); err != nil {
		return nil, fmt.Errorf("replay events log: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events log: %w", err)
	}
	s.file = f
	return s, nil
}

// replay reads an existing events.jsonl (if any) and feeds each line back
// through the same in-memory merge logic Add uses, without re-appending to
// the file. Malformed lines are skipped rather than failing startup.
func (s *Store) replay(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var c Captured
		if err := json.Unmarshal(line, &c); err != nil {
			continue
		}
		s.insert(c)
	}
	return scanner.Err()
}

func (s *Store) Close() error {
	return s.file.Close()
}

// Add records a captured event. If c.GroupKey is non-empty and matches an
// already-held entry, that entry's Count is incremented and bumped to the
// most-recently-seen position instead of appending a new row -- this is what
// mirrors Sentry's issue grouping, collapsing repeated occurrences of the
// same error into one entry with a running count rather than a new line
// every time. It returns the stored entry and whether it was newly created.
// The durable JSONL log still gets one line per occurrence either way, so
// full history is never lost.
func (s *Store) Add(c Captured) (Captured, bool, error) {
	if c.Count == 0 {
		c.Count = 1
	}
	if c.LastSeen.IsZero() {
		c.LastSeen = c.ReceivedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, isNew := s.insert(c)
	if err := s.appendLine(stored); err != nil {
		return stored, isNew, err
	}
	return stored, isNew, nil
}

// insert merges c into the in-memory items (mirroring Sentry's issue
// grouping) without touching the durable log; used by both Add and replay.
// Callers must hold s.mu.
func (s *Store) insert(c Captured) (Captured, bool) {
	if c.GroupKey != "" {
		if idx, ok := s.byGroup[c.GroupKey]; ok {
			existing := s.items[idx]
			existing.Count++
			existing.LastSeen = c.LastSeen
			s.items = append(s.items[:idx], s.items[idx+1:]...)
			s.items = append(s.items, existing)
			s.reindex()
			return existing, false
		}
	}

	s.items = append(s.items, c)
	if len(s.items) > s.capacity {
		s.items = s.items[len(s.items)-s.capacity:]
	}
	s.reindex()
	return c, true
}

// reindex rebuilds byID/byGroup from scratch after items has been mutated
// (eviction or a group bump reordering entries).
func (s *Store) reindex() {
	s.byID = make(map[string]int, s.capacity)
	s.byGroup = make(map[string]int, s.capacity)
	for i, it := range s.items {
		s.byID[it.ID] = i
		if it.GroupKey != "" {
			s.byGroup[it.GroupKey] = i
		}
	}
}

func (s *Store) appendLine(c Captured) error {
	line, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')
	if _, err := s.file.Write(line); err != nil {
		return fmt.Errorf("write event log: %w", err)
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

// ProjectSummary aggregates the issues captured for a single project, for
// the top-level project overview page.
type ProjectSummary struct {
	ProjectID string
	Issues    int // distinct rows: grouped issues plus ungrouped items
	Events    int // total occurrences, including grouped repeats
	LastSeen  time.Time
}

// Projects returns a summary per distinct project ID, most recently seen
// first.
func (s *Store) Projects() []ProjectSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byProject := make(map[string]*ProjectSummary)
	var order []string
	for _, it := range s.items {
		ps, ok := byProject[it.ProjectID]
		if !ok {
			ps = &ProjectSummary{ProjectID: it.ProjectID}
			byProject[it.ProjectID] = ps
			order = append(order, it.ProjectID)
		}
		ps.Issues++
		ps.Events += it.Count
		if it.LastSeen.After(ps.LastSeen) {
			ps.LastSeen = it.LastSeen
		}
	}

	out := make([]ProjectSummary, len(order))
	for i, id := range order {
		out[i] = *byProject[id]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// ListByProject returns captured events for a single project, newest first.
func (s *Store) ListByProject(projectID string) []Captured {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Captured
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].ProjectID == projectID {
			out = append(out, s.items[i])
		}
	}
	return out
}
