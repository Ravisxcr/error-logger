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

	"github.com/ravi/error-logger/internal/console"
	"github.com/ravi/error-logger/internal/ingest"
	"github.com/ravi/error-logger/internal/store"
	"github.com/ravi/error-logger/internal/web"
)

func main() {
	addr := flag.String("addr", ":9000", "address to listen on")
	dataDir := flag.String("data-dir", "data", "directory to persist captured events (events.jsonl)")
	capacity := flag.Int("capacity", 1000, "number of events to keep in memory for the dashboard")
	flag.Parse()

	logger := log.New(os.Stdout, "", 0)

	st, err := store.Open(*dataDir, *capacity)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()

	ing := &ingest.Handler{
		Store:  st,
		Logger: logger,
		Print: func(c store.Captured, isNew bool) {
			console.Print(os.Stdout, c, isNew)
		},
	}
	ing.RegisterRoutes(mux)

	dash := &web.Handler{Store: st}
	dash.RegisterRoutes(mux)

	logger.Printf("error-logger listening on %s", *addr)
	logger.Printf("dashboard:      http://localhost%s/", *addr)
	logger.Printf("example DSN:    http://public@localhost%s/1", *addr)
	logger.Printf("events log:     %s/events.jsonl", *dataDir)

	if err := http.ListenAndServe(*addr, mux); err != nil {
		logger.Fatalf("server: %v", err)
	}
}
