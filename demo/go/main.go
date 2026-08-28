package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

func queryDatabase(query string) error {
	if query == "" {
		return errors.New("query cannot be empty")
	}
	return fmt.Errorf("connection timeout while executing: %s", query)
}

func performTask() {
	defer sentry.Recover()

	if err := queryDatabase("SELECT * FROM users WHERE active = true"); err != nil {
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("service", "billing")
			scope.SetTag("component", "db_pool")
			scope.SetLevel(sentry.LevelError)
			scope.SetExtra("retries", 3)
			sentry.CaptureException(err)
		})
		log.Printf("Captured error to Sentry: %v", err)
	}
}

func triggerPanic() {
	defer sentry.Recover()

	var data map[string]int
	// This causes a runtime panic: assignment to entry in nil map
	data["count"] = 42
}

func main() {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		dsn = "http://public@127.0.0.1:9000/3"
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      "production",
		Release:          "go-demo@1.0.0",
		ServerName:       "api-worker-01",
		TracesSampleRate: 1.0,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	defer sentry.Flush(2 * time.Second)

	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{
			ID:       "go_user_101",
			Email:    "gopher@golang.org",
			Username: "gopher",
		})
		scope.AddBreadcrumb(&sentry.Breadcrumb{
			Category: "lifecycle",
			Message:  "Worker process initialized",
			Level:    sentry.LevelInfo,
		}, 10)
	})

	log.Println("Running Go Sentry SDK demo...")

	// 1. Capture error with stack trace
	performTask()

	// 2. Capture info message
	sentry.CaptureMessage("Worker scheduled job completed successfully")

	// 3. Capture panic with stack trace
	triggerPanic()

	log.Println("Go demo finished successfully.")
}

