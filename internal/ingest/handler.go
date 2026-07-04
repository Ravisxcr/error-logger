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

	"github.com/ravisxcr/error-logger/internal/envelope"
	"github.com/ravisxcr/error-logger/internal/sentryevent"
	"github.com/ravisxcr/error-logger/internal/store"
)

// Printer is anything that can render a captured event as it arrives
// (satisfied by console.Print). isNew reports whether this is the first
// occurrence of the underlying error or a repeat that bumped an existing
// entry's count.
type Printer func(capturedEvent store.Captured, isNew bool)

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

func (h *Handler) handleEnvelope(responseWriter http.ResponseWriter, request *http.Request) {
	projectID := request.PathValue("project_id")
	gzipped := strings.EqualFold(request.Header.Get("Content-Encoding"), "gzip")

	parsedEnvelope, err := envelope.Decode(request.Body, gzipped)
	if err != nil {
		h.Logger.Printf("envelope decode error (project=%s): %v", projectID, err)
		http.Error(responseWriter, err.Error(), http.StatusBadRequest)
		return
	}

	eventID := parsedEnvelope.Header.EventID
	for _, item := range parsedEnvelope.Items {
		kind := item.Header.Type
		if kind == "" {
			kind = "event"
		}

		switch kind {
		case "event", "transaction":
			var sentryEvent sentryevent.Event
			if err := json.Unmarshal(item.Payload, &sentryEvent); err != nil {
				h.Logger.Printf("skip malformed %s item (project=%s): %v", kind, projectID, err)
				continue
			}
			id := sentryEvent.EventID
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
				GroupKey:   groupKey(projectID, &sentryEvent),
				Event:      &sentryEvent,
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

	writeAck(responseWriter, eventID)
}

func (h *Handler) handleStore(responseWriter http.ResponseWriter, request *http.Request) {
	projectID := request.PathValue("project_id")
	gzipped := strings.EqualFold(request.Header.Get("Content-Encoding"), "gzip")

	body, err := readBody(request.Body, gzipped)
	if err != nil {
		h.Logger.Printf("store decode error (project=%s): %v", projectID, err)
		http.Error(responseWriter, err.Error(), http.StatusBadRequest)
		return
	}

	var sentryEvent sentryevent.Event
	if err := json.Unmarshal(body, &sentryEvent); err != nil {
		h.Logger.Printf("malformed store payload (project=%s): %v", projectID, err)
		http.Error(responseWriter, "invalid event payload", http.StatusBadRequest)
		return
	}
	if sentryEvent.EventID == "" {
		sentryEvent.EventID = newID()
	}

	h.capture(store.Captured{
		ID:         sentryEvent.EventID,
		ProjectID:  projectID,
		Kind:       "event",
		ReceivedAt: time.Now(),
		GroupKey:   groupKey(projectID, &sentryEvent),
		Event:      &sentryEvent,
	})

	writeAck(responseWriter, sentryEvent.EventID)
}

// handleIgnore acknowledges endpoints we intentionally don't process
// (security reports, minidumps) so the SDK doesn't treat them as failures.
func (h *Handler) handleIgnore(responseWriter http.ResponseWriter, request *http.Request) {
	io.Copy(io.Discard, request.Body)
	writeAck(responseWriter, "")
}

func (h *Handler) capture(capturedEvent store.Captured) {
	stored, isNew, err := h.Store.Add(capturedEvent)
	if err != nil {
		h.Logger.Printf("store event: %v", err)
	}
	if h.Print != nil {
		h.Print(stored, isNew)
	}
}

// groupKey combines the project ID with the event's own grouping identity so
// identical errors from different projects are never merged together.
func groupKey(projectID string, sentryEvent *sentryevent.Event) string {
	key := sentryEvent.GroupKey()
	if key == "" {
		return ""
	}
	return projectID + "\x00" + key
}

func readBody(reader io.Reader, gzipped bool) ([]byte, error) {
	if !gzipped {
		return io.ReadAll(reader)
	}
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gzipReader.Close()
	return io.ReadAll(gzipReader)
}

func writeAck(responseWriter http.ResponseWriter, eventID string) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(http.StatusOK)
	if eventID == "" {
		responseWriter.Write([]byte(`{}`))
		return
	}
	fmt.Fprintf(responseWriter, `{"id":%q}`, eventID)
}

func newID() string {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(randomBytes)
}
