// Command server runs a local, Sentry-compatible error ingestion server for
// development use. Point any Sentry SDK's DSN at it (e.g.
// http://<any_key>@localhost:9000/1) and captured events are printed to the
// console and browsable at http://localhost:9000/.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/ravisxcr/error-logger/internal/console"
	"github.com/ravisxcr/error-logger/internal/ingest"
	"github.com/ravisxcr/error-logger/internal/store"
	"github.com/ravisxcr/error-logger/internal/web"
)

func main() {
	listenAddr := flag.String("addr", ":9000", "address to listen on")
	dataDir := flag.String("data-dir", "data", "directory to persist captured events (events.jsonl)")
	capacity := flag.Int("capacity", 1000, "number of events to keep in memory for the dashboard")
	flag.Parse()

	logger := log.New(os.Stdout, "", 0)

	eventStore, err := store.Open(*dataDir, *capacity)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer eventStore.Close()

	mux := http.NewServeMux()

	ingestHandler := &ingest.Handler{
		Store:  eventStore,
		Logger: logger,
		Print: func(c store.Captured, isNew bool) {
			console.Print(os.Stdout, c, isNew)
		},
	}
	ingestHandler.RegisterRoutes(mux)

	dashboardHandler := &web.Handler{Store: eventStore}
	dashboardHandler.RegisterRoutes(mux)

	logger.Printf("error-logger listening on %s", *listenAddr)
	logger.Printf("dashboard:      http://localhost%s/", *listenAddr)
	logger.Printf("example DSN:    http://public@localhost%s/1", *listenAddr)
	logger.Printf("events log:     %s/events.jsonl", *dataDir)

	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		logger.Fatalf("server: %v", err)
	}
}
