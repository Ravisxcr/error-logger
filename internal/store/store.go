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

	"github.com/ravisxcr/error-logger/internal/sentryevent"
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
	store := &Store{
		byID:     make(map[string]int),
		byGroup:  make(map[string]int),
		capacity: capacity,
	}
	if err := store.replay(path); err != nil {
		return nil, fmt.Errorf("replay events log: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events log: %w", err)
	}
	store.file = file
	return store, nil
}

// replay reads an existing events.jsonl (if any) and feeds each line back
// through the same in-memory merge logic Add uses, without re-appending to
// the file. Malformed lines are skipped rather than failing startup.
func (store *Store) replay(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var capturedEvent Captured
		if err := json.Unmarshal(line, &capturedEvent); err != nil {
			continue
		}
		store.insert(capturedEvent)
	}
	return scanner.Err()
}

func (store *Store) Close() error {
	return store.file.Close()
}

// Add records a captured event. If c.GroupKey is non-empty and matches an
// already-held entry, that entry's Count is incremented and bumped to the
// most-recently-seen position instead of appending a new row -- this is what
// mirrors Sentry's issue grouping, collapsing repeated occurrences of the
// same error into one entry with a running count rather than a new line
// every time. It returns the stored entry and whether it was newly created.
// The durable JSONL log still gets one line per occurrence either way, so
// full history is never lost.
func (store *Store) Add(capturedEvent Captured) (Captured, bool, error) {
	if capturedEvent.Count == 0 {
		capturedEvent.Count = 1
	}
	if capturedEvent.LastSeen.IsZero() {
		capturedEvent.LastSeen = capturedEvent.ReceivedAt
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	stored, isNew := store.insert(capturedEvent)
	if err := store.appendLine(stored); err != nil {
		return stored, isNew, err
	}
	return stored, isNew, nil
}

// insert merges capturedEvent into the in-memory items (mirroring Sentry's issue
// grouping) without touching the durable log; used by both Add and replay.
// Callers must hold store.mu.
func (store *Store) insert(capturedEvent Captured) (Captured, bool) {
	if capturedEvent.GroupKey != "" {
		if index, ok := store.byGroup[capturedEvent.GroupKey]; ok {
			existing := store.items[index]
			existing.Count++
			existing.LastSeen = capturedEvent.LastSeen
			store.items = append(store.items[:index], store.items[index+1:]...)
			store.items = append(store.items, existing)
			store.reindex()
			return existing, false
		}
	}

	store.items = append(store.items, capturedEvent)
	if len(store.items) > store.capacity {
		store.items = store.items[len(store.items)-store.capacity:]
	}
	store.reindex()
	return capturedEvent, true
}

// reindex rebuilds byID/byGroup from scratch after items has been mutated
// (eviction or a group bump reordering entries).
func (store *Store) reindex() {
	store.byID = make(map[string]int, store.capacity)
	store.byGroup = make(map[string]int, store.capacity)
	for index, item := range store.items {
		store.byID[item.ID] = index
		if item.GroupKey != "" {
			store.byGroup[item.GroupKey] = index
		}
	}
}

func (store *Store) appendLine(capturedEvent Captured) error {
	line, err := json.Marshal(capturedEvent)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')
	if _, err := store.file.Write(line); err != nil {
		return fmt.Errorf("write event log: %w", err)
	}
	return nil
}

// List returns captured events, newest first.
func (store *Store) List() []Captured {
	store.mu.RLock()
	defer store.mu.RUnlock()
	events := make([]Captured, len(store.items))
	for index, item := range store.items {
		events[len(store.items)-1-index] = item
	}
	return events
}

// Get looks up a single captured event by ID.
func (store *Store) Get(id string) (Captured, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	index, ok := store.byID[id]
	if !ok {
		return Captured{}, false
	}
	return store.items[index], true
}

// Count returns the number of events currently held in memory.
func (store *Store) Count() int {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.items)
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
func (store *Store) Projects() []ProjectSummary {
	store.mu.RLock()
	defer store.mu.RUnlock()

	byProject := make(map[string]*ProjectSummary)
	var order []string
	for _, item := range store.items {
		summary, ok := byProject[item.ProjectID]
		if !ok {
			summary = &ProjectSummary{ProjectID: item.ProjectID}
			byProject[item.ProjectID] = summary
			order = append(order, item.ProjectID)
		}
		summary.Issues++
		summary.Events += item.Count
		if item.LastSeen.After(summary.LastSeen) {
			summary.LastSeen = item.LastSeen
		}
	}

	summaries := make([]ProjectSummary, len(order))
	for index, id := range order {
		summaries[index] = *byProject[id]
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].LastSeen.After(summaries[j].LastSeen) })
	return summaries
}

// ListByProject returns captured events for a single project, newest first.
func (store *Store) ListByProject(projectID string) []Captured {
	store.mu.RLock()
	defer store.mu.RUnlock()
	var events []Captured
	for index := len(store.items) - 1; index >= 0; index-- {
		if store.items[index].ProjectID == projectID {
			events = append(events, store.items[index])
		}
	}
	return events
}

// DeleteEvent removes a single captured row by ID. See DeleteEvents.
func (store *Store) DeleteEvent(id string) (bool, error) {
	deletedCount, err := store.DeleteEvents([]string{id})
	return deletedCount > 0, err
}

// DeleteEvents removes every captured row whose ID is in eventIDs from memory
// and from the durable log. Since grouped issues share one ID across every
// occurrence line (see insert), this drops each issue's whole history, not
// just its latest occurrence. Reports how many rows were deleted.
func (store *Store) DeleteEvents(eventIDs []string) (int, error) {
	set := make(map[string]bool, len(eventIDs))
	for _, id := range eventIDs {
		set[id] = true
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	keptEvents := store.items[:0]
	removedCount := 0
	for _, item := range store.items {
		if set[item.ID] {
			removedCount++
			continue
		}
		keptEvents = append(keptEvents, item)
	}
	store.items = keptEvents
	store.reindex()

	if removedCount == 0 {
		return 0, nil
	}
	if err := store.rewriteLog(func(capturedEvent Captured) bool { return !set[capturedEvent.ID] }); err != nil {
		return removedCount, err
	}
	return removedCount, nil
}

// DeleteProject removes every captured row for projectID. See DeleteProjects.
func (store *Store) DeleteProject(projectID string) (int, error) {
	return store.DeleteProjects([]string{projectID})
}

// DeleteProjects removes every captured row whose project ID is in
// projectIDs from memory and from the durable log. Reports how many rows
// were deleted.
func (store *Store) DeleteProjects(projectIDs []string) (int, error) {
	set := make(map[string]bool, len(projectIDs))
	for _, id := range projectIDs {
		set[id] = true
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	keptEvents := store.items[:0]
	removedCount := 0
	for _, item := range store.items {
		if set[item.ProjectID] {
			removedCount++
			continue
		}
		keptEvents = append(keptEvents, item)
	}
	store.items = keptEvents
	store.reindex()

	if removedCount == 0 {
		return 0, nil
	}
	if err := store.rewriteLog(func(capturedEvent Captured) bool { return !set[capturedEvent.ProjectID] }); err != nil {
		return removedCount, err
	}
	return removedCount, nil
}

// rewriteLog regenerates events.jsonl keeping only the lines for which keep
// returns true. The log is otherwise append-only, so deletion is the one
// case that needs a full rewrite; callers must hold store.mu.
func (store *Store) rewriteLog(keep func(Captured) bool) error {
	path := store.file.Name()

	var keptLines [][]byte
	if err := func() error {
		inputFile, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer inputFile.Close()

		scanner := bufio.NewScanner(inputFile)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var capturedEvent Captured
			if err := json.Unmarshal(line, &capturedEvent); err != nil {
				continue
			}
			if keep(capturedEvent) {
				keptLines = append(keptLines, append([]byte(nil), line...))
			}
		}
		return scanner.Err()
	}(); err != nil {
		return fmt.Errorf("read events log: %w", err)
	}

	if err := store.file.Close(); err != nil {
		return fmt.Errorf("close events log: %w", err)
	}

	tmpPath := path + ".tmp"
	outputFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create temp log: %w", err)
	}
	for _, line := range keptLines {
		if _, err := outputFile.Write(line); err == nil {
			_, err = outputFile.Write([]byte("\n"))
		} else {
			outputFile.Close()
			return fmt.Errorf("write temp log: %w", err)
		}
	}
	if err := outputFile.Close(); err != nil {
		return fmt.Errorf("write temp log: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace events log: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("reopen events log: %w", err)
	}
	store.file = file
	return nil
}
