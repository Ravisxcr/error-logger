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
	"strconv"

	"github.com/ravisxcr/error-logger/internal/console"
	"github.com/ravisxcr/error-logger/internal/ingest"
	"github.com/ravisxcr/error-logger/internal/store"
	"github.com/ravisxcr/error-logger/internal/web"
)

func envString(keys []string, fallback string) string {
	for _, k := range keys {
		if val := os.Getenv(k); val != "" {
			return val
		}
	}
	return fallback
}

func envInt(keys []string, fallback int) int {
	for _, k := range keys {
		if val := os.Getenv(k); val != "" {
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return fallback
}

func envBool(keys []string, fallback bool) bool {
	for _, k := range keys {
		if val := os.Getenv(k); val != "" {
			if b, err := strconv.ParseBool(val); err == nil {
				return b
			}
		}
	}
	return fallback
}

func getListenAddr() string {
	if addr := envString([]string{"ERROR_LOGGER_ADDR", "ADDR"}, ""); addr != "" {
		return addr
	}
	if port := envString([]string{"ERROR_LOGGER_PORT", "PORT"}, ""); port != "" {
		if len(port) > 0 && port[0] == ':' {
			return port
		}
		return ":" + port
	}
	return ":9000"
}

func main() {
	defaultAddr := getListenAddr()
	defaultDataDir := envString([]string{"ERROR_LOGGER_DATA_DIR", "DATA_DIR"}, "data")
	defaultCapacity := envInt([]string{"ERROR_LOGGER_CAPACITY", "CAPACITY"}, 1000)
	defaultDisableDelete := envBool([]string{"ERROR_LOGGER_DISABLE_DELETE", "DISABLE_DELETE"}, false)
	defaultDisableConsoleLog := envBool([]string{"ERROR_LOGGER_DISABLE_CONSOLE_LOG", "DISABLE_CONSOLE_LOG"}, false)

	listenAddr := flag.String("addr", defaultAddr, "address to listen on (env: ADDR, PORT)")
	dataDir := flag.String("data-dir", defaultDataDir, "directory to persist captured events (events.jsonl) (env: DATA_DIR)")
	capacity := flag.Int("capacity", defaultCapacity, "number of events to keep in memory for the dashboard (env: CAPACITY)")
	disableDelete := flag.Bool("disable-delete", defaultDisableDelete, "disable deleting events/issues/projects from the dashboard (env: DISABLE_DELETE)")
	disableConsoleLog := flag.Bool("disable-console-log", defaultDisableConsoleLog, "disable printing captured events to the console (env: DISABLE_CONSOLE_LOG)")
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
	}
	if !*disableConsoleLog {
		ingestHandler.Print = func(c store.Captured, isNew bool) {
			console.Print(os.Stdout, c, isNew)
		}
	}
	ingestHandler.RegisterRoutes(mux)

	dashboardHandler := &web.Handler{Store: eventStore, DisableDelete: *disableDelete}
	dashboardHandler.RegisterRoutes(mux)

	logger.Printf("error-logger listening on %s", *listenAddr)
	logger.Printf("dashboard:      http://localhost%s/", *listenAddr)
	logger.Printf("example DSN:    http://public@localhost%s/1", *listenAddr)
	logger.Printf("events log:     %s/events.jsonl", *dataDir)
	if *disableDelete {
		logger.Printf("delete:         disabled")
	}
	if *disableConsoleLog {
		logger.Printf("console log:    disabled")
	}

	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		logger.Fatalf("server: %v", err)
	}
}
