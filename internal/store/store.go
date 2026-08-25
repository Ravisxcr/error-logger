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
// lighter-weight record for envelope item types we don't fully model.
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
	items    []Captured     // newest last
	byID     map[string]int // id -> slice index
	byGroup  map[string]int // projectID:groupKey -> slice index
	capacity int
	file     *os.File
}

// Open creates (or appends to) dataDir/events.jsonl and returns a Store
// with the given in-memory capacity.
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

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events log: %w", err)
	}
	s.file = file
	return s, nil
}

// replay reads an existing events.jsonl (if any) and feeds each line back
// through the in-memory merge logic without re-appending to the file.
func (s *Store) replay(path string) error {
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
		s.insert(capturedEvent)
	}
	return scanner.Err()
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// Add records a captured event. If it belongs to an existing issue group,
// its in-memory occurrence count is incremented and bumped to the newest slot.
// The raw incoming event is always written to disk to preserve occurrence history.
func (s *Store) Add(capturedEvent Captured) (Captured, bool, error) {
	if capturedEvent.Count == 0 {
		capturedEvent.Count = 1
	}
	if capturedEvent.LastSeen.IsZero() {
		capturedEvent.LastSeen = capturedEvent.ReceivedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Persist the actual incoming occurrence line to the log
	if err := s.appendLine(capturedEvent); err != nil {
		return capturedEvent, false, err
	}

	stored, isNew := s.insert(capturedEvent)
	return stored, isNew, nil
}

// compositeGroupKey guarantees per-project isolation for issue grouping.
func compositeGroupKey(projectID, groupKey string) string {
	if groupKey == "" {
		return ""
	}
	return projectID + "\x00" + groupKey
}

// insert merges capturedEvent into in-memory items. Callers must hold s.mu.
func (s *Store) insert(capturedEvent Captured) (Captured, bool) {
	compKey := compositeGroupKey(capturedEvent.ProjectID, capturedEvent.GroupKey)
	if compKey != "" {
		if index, ok := s.byGroup[compKey]; ok {
			existing := s.items[index]
			existing.Count += capturedEvent.Count
			existing.LastSeen = capturedEvent.LastSeen
			// Keep the latest payload/stack trace snapshot in memory for UI inspection
			if capturedEvent.Event != nil {
				existing.Event = capturedEvent.Event
			}

			// Move to newest (end of slice)
			s.items = append(s.items[:index], s.items[index+1:]...)
			s.items = append(s.items, existing)
			s.reindex()
			return existing, false
		}
	}

	s.items = append(s.items, capturedEvent)
	if len(s.items) > s.capacity {
		evictCount := len(s.items) - s.capacity
		// Zero out evicted elements before slicing so GC can reclaim pointers immediately
		for i := 0; i < evictCount; i++ {
			s.items[i] = Captured{}
		}
		s.items = s.items[evictCount:]
	}
	s.reindex()
	return capturedEvent, true
}

// reindex rebuilds byID and byGroup indexes.
func (s *Store) reindex() {
	s.byID = make(map[string]int, len(s.items))
	s.byGroup = make(map[string]int, len(s.items))
	for index, item := range s.items {
		s.byID[item.ID] = index
		if key := compositeGroupKey(item.ProjectID, item.GroupKey); key != "" {
			s.byGroup[key] = index
		}
	}
}

func (s *Store) appendLine(capturedEvent Captured) error {
	line, err := json.Marshal(capturedEvent)
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
	events := make([]Captured, len(s.items))
	for index, item := range s.items {
		events[len(s.items)-1-index] = item
	}
	return events
}

// Get looks up a single captured event by ID.
func (s *Store) Get(id string) (Captured, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.byID[id]
	if !ok {
		return Captured{}, false
	}
	return s.items[index], true
}

// Count returns the number of events currently held in memory.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// ProjectSummary aggregates the issues captured for a single project.
type ProjectSummary struct {
	ProjectID string
	Issues    int
	Events    int
	LastSeen  time.Time
}

// Projects returns a summary per distinct project ID, most recently seen first.
func (s *Store) Projects() []ProjectSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byProject := make(map[string]*ProjectSummary)
	var order []string
	for _, item := range s.items {
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
func (s *Store) ListByProject(projectID string) []Captured {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var events []Captured
	for index := len(s.items) - 1; index >= 0; index-- {
		if s.items[index].ProjectID == projectID {
			events = append(events, s.items[index])
		}
	}
	return events
}

// DeleteEvent removes a single captured row by ID.
func (s *Store) DeleteEvent(id string) (bool, error) {
	deletedCount, err := s.DeleteEvents([]string{id})
	return deletedCount > 0, err
}

// DeleteEvents removes captured events matching eventIDs or their associated groups.
func (s *Store) DeleteEvents(eventIDs []string) (int, error) {
	idSet := make(map[string]bool, len(eventIDs))
	for _, id := range eventIDs {
		idSet[id] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Find any project+group pairings associated with these IDs to ensure full issue deletion
	groupSet := make(map[string]bool)
	for _, item := range s.items {
		if idSet[item.ID] && item.GroupKey != "" {
			groupSet[compositeGroupKey(item.ProjectID, item.GroupKey)] = true
		}
	}

	keptEvents := s.items[:0]
	removedCount := 0
	for _, item := range s.items {
		isTarget := idSet[item.ID] || (item.GroupKey != "" && groupSet[compositeGroupKey(item.ProjectID, item.GroupKey)])
		if isTarget {
			removedCount++
			continue
		}
		keptEvents = append(keptEvents, item)
	}

	// Zero out vacated slice elements
	for i := len(keptEvents); i < len(s.items); i++ {
		s.items[i] = Captured{}
	}
	s.items = keptEvents
	s.reindex()

	if removedCount == 0 {
		return 0, nil
	}

	err := s.rewriteLogStream(func(c Captured) bool {
		if idSet[c.ID] {
			return false
		}
		if c.GroupKey != "" && groupSet[compositeGroupKey(c.ProjectID, c.GroupKey)] {
			return false
		}
		return true
	})
	if err != nil {
		return removedCount, err
	}
	return removedCount, nil
}

// DeleteProject removes every captured row for projectID.
func (s *Store) DeleteProject(projectID string) (int, error) {
	return s.DeleteProjects([]string{projectID})
}

// DeleteProjects removes every captured row whose project ID is in projectIDs.
func (s *Store) DeleteProjects(projectIDs []string) (int, error) {
	set := make(map[string]bool, len(projectIDs))
	for _, id := range projectIDs {
		set[id] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	keptEvents := s.items[:0]
	removedCount := 0
	for _, item := range s.items {
		if set[item.ProjectID] {
			removedCount++
			continue
		}
		keptEvents = append(keptEvents, item)
	}

	for i := len(keptEvents); i < len(s.items); i++ {
		s.items[i] = Captured{}
	}
	s.items = keptEvents
	s.reindex()

	if removedCount == 0 {
		return 0, nil
	}

	err := s.rewriteLogStream(func(c Captured) bool {
		return !set[c.ProjectID]
	})
	if err != nil {
		return removedCount, err
	}
	return removedCount, nil
}

// rewriteLogStream streams the existing log file through a filter into a temporary
// file line-by-line, avoiding loading the entire file into memory. Callers must hold s.mu.
func (s *Store) rewriteLogStream(keep func(Captured) bool) error {
	path := s.file.Name()

	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close current log: %w", err)
	}

	tmpPath := path + ".tmp"
	rewriteErr := func() error {
		inputFile, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer inputFile.Close()

		outputFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer outputFile.Close()

		writer := bufio.NewWriter(outputFile)
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
				if _, err := writer.Write(line); err != nil {
					return err
				}
				if err := writer.WriteByte('\n'); err != nil {
					return err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return writer.Flush()
	}()

	if rewriteErr != nil {
		// Reopen original file on failure
		s.file, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		_ = os.Remove(tmpPath)
		return fmt.Errorf("stream rewrite failed: %w", rewriteErr)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		s.file, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		return fmt.Errorf("replace events log: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("reopen events log: %w", err)
	}
	s.file = file
	return nil
}