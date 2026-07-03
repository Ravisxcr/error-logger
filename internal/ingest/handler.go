// Package ingest implements the Sentry-compatible ingestion endpoints:
// the modern envelope endpoint and the legacy store endpoint. Both are
// enough for the Python `sentry_sdk` client to talk to this server as if
// it were sentry.io.
package ingest

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ravi/error-logger/internal/envelope"
	"github.com/ravi/error-logger/internal/sentryevent"
	"github.com/ravi/error-logger/internal/store"
)

// Printer is anything that can render a captured event as it arrives
// (satisfied by console.Print). isNew reports whether this is the first
// occurrence of the underlying error or a repeat that bumped an existing
// entry's count.
type Printer func(c store.Captured, isNew bool)

type Handler struct {
	Store  *store.Store
	Print  Printer
	Logger *log.Logger
}

// RegisterRoutes wires the ingestion endpoints onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/{project_id}/envelope/", h.handleEnvelope)
	mux.HandleFunc("POST /api/{project_id}/store/", h.handleStore)
	mux.HandleFunc("POST /api/{project_id}/security/", h.handleIgnore)
	mux.HandleFunc("POST /api/{project_id}/minidump/", h.handleIgnore)
}

func (h *Handler) handleEnvelope(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")
	gzipped := strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip")

	env, err := envelope.Decode(r.Body, gzipped)
	if err != nil {
		h.Logger.Printf("envelope decode error (project=%s): %v", projectID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	eventID := env.Header.EventID
	for _, item := range env.Items {
		kind := item.Header.Type
		if kind == "" {
			kind = "event"
		}

		switch kind {
		case "event", "transaction":
			var e sentryevent.Event
			if err := json.Unmarshal(item.Payload, &e); err != nil {
				h.Logger.Printf("skip malformed %s item (project=%s): %v", kind, projectID, err)
				continue
			}
			id := e.EventID
			if id == "" {
				id = eventID
			}
			if id == "" {
				id = newID()
			}
			h.capture(store.Captured{
				ID:         id,
				ProjectID:  projectID,
				Kind:       kind,
				ReceivedAt: time.Now(),
				GroupKey:   groupKey(projectID, &e),
				Event:      &e,
			})
		default:
			// session, profile, attachment, client_report, etc: acknowledge
			// but don't attempt to fully parse. Each gets its own fresh ID
			// (rather than reusing the envelope's event_id) since an envelope
			// can carry several of these items and they'd otherwise collide
			// in the store's ID index.
			h.capture(store.Captured{
				ID:         newID(),
				ProjectID:  projectID,
				Kind:       kind,
				ReceivedAt: time.Now(),
			})
		}
	}

	writeAck(w, eventID)
}

func (h *Handler) handleStore(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")
	gzipped := strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip")

	body, err := readBody(r.Body, gzipped)
	if err != nil {
		h.Logger.Printf("store decode error (project=%s): %v", projectID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var e sentryevent.Event
	if err := json.Unmarshal(body, &e); err != nil {
		h.Logger.Printf("malformed store payload (project=%s): %v", projectID, err)
		http.Error(w, "invalid event payload", http.StatusBadRequest)
		return
	}
	if e.EventID == "" {
		e.EventID = newID()
	}

	h.capture(store.Captured{
		ID:         e.EventID,
		ProjectID:  projectID,
		Kind:       "event",
		ReceivedAt: time.Now(),
		GroupKey:   groupKey(projectID, &e),
		Event:      &e,
	})

	writeAck(w, e.EventID)
}

// handleIgnore acknowledges endpoints we intentionally don't process
// (security reports, minidumps) so the SDK doesn't treat them as failures.
func (h *Handler) handleIgnore(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	writeAck(w, "")
}

func (h *Handler) capture(c store.Captured) {
	stored, isNew, err := h.Store.Add(c)
	if err != nil {
		h.Logger.Printf("store event: %v", err)
	}
	if h.Print != nil {
		h.Print(stored, isNew)
	}
}

// groupKey combines the project ID with the event's own grouping identity so
// identical errors from different projects are never merged together.
func groupKey(projectID string, e *sentryevent.Event) string {
	k := e.GroupKey()
	if k == "" {
		return ""
	}
	return projectID + "\x00" + k
}

func readBody(r io.Reader, gzipped bool) ([]byte, error) {
	if !gzipped {
		return io.ReadAll(r)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	return io.ReadAll(gz)
}

func writeAck(w http.ResponseWriter, eventID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if eventID == "" {
		w.Write([]byte(`{}`))
		return
	}
	fmt.Fprintf(w, `{"id":%q}`, eventID)
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}
