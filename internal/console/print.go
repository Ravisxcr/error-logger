// Package console pretty-prints captured events to stdout as they arrive.
package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/ravisxcr/error-logger/internal/store"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
	cyan   = "\x1b[36m"
	gray   = "\x1b[90m"
)

func levelColor(level string) string {
	switch strings.ToLower(level) {
	case "fatal", "error":
		return red
	case "warning":
		return yellow
	case "info":
		return blue
	case "debug":
		return gray
	default:
		return cyan
	}
}

// Print writes a human-readable summary of capturedEvent to writer. isNew reports whether
// this is the first occurrence of capturedEvent's underlying error; repeats print a
// single compact "seen again" line carrying the running count instead of a
// full block, so the console doesn't get flooded with duplicate stacktraces.
func Print(writer io.Writer, capturedEvent store.Captured, isNew bool) {
	timestamp := capturedEvent.LastSeen.Local().Format("15:04:05")

	if capturedEvent.Event == nil {
		fmt.Fprintf(writer, "%s[%s]%s %s%-10s%s project=%s id=%s\n",
			dim, timestamp, reset, cyan, capturedEvent.Kind, reset, capturedEvent.ProjectID, capturedEvent.ID)
		return
	}

	event := capturedEvent.Event
	level := event.Level
	if level == "" {
		if capturedEvent.Kind == "event" {
			level = "error"
		} else {
			level = "info"
		}
	}
	colorCode := levelColor(level)

	if !isNew {
		var summary string
		switch {
		case event.Exception != nil && len(event.Exception.Values) > 0:
			exception := event.Exception.Values[len(event.Exception.Values)-1]
			summary = fmt.Sprintf("%s: %s", exception.Type, exception.Value)
		case event.MessageText() != "":
			summary = event.MessageText()
		case event.Transaction != "":
			summary = event.Transaction
		}
		fmt.Fprintf(writer, "%s[%s]%s %s%-7s%s project=%s %s(seen again x%d)%s %s %sid=%s%s\n",
			dim, timestamp, reset, colorCode, strings.ToUpper(level), reset, capturedEvent.ProjectID, gray, capturedEvent.Count, reset, summary, gray, capturedEvent.ID, reset)
		return
	}

	header := fmt.Sprintf("%s[%s]%s %s%-7s%s project=%s", dim, timestamp, reset, colorCode, strings.ToUpper(level), reset, capturedEvent.ProjectID)
	if capturedEvent.Kind != "event" {
		header += fmt.Sprintf(" kind=%s", capturedEvent.Kind)
	}
	if event.Environment != "" {
		header += fmt.Sprintf(" env=%s", event.Environment)
	}
	if event.Release != "" {
		header += fmt.Sprintf(" release=%s", event.Release)
	}
	fmt.Fprintln(writer, header)

	if event.Exception != nil {
		for _, exception := range event.Exception.Values {
			fmt.Fprintf(writer, "  %s%s%s: %s\n", bold, exception.Type, reset, exception.Value)
			if exception.Stacktrace != nil {
				frames := exception.Stacktrace.Frames
				startIndex := 0
				if len(frames) > 8 {
					startIndex = len(frames) - 8
					fmt.Fprintf(writer, "    %s... %d earlier frames omitted%s\n", gray, startIndex, reset)
				}
				for _, stackFrame := range frames[startIndex:] {
					location := stackFrame.Filename
					if location == "" {
						location = stackFrame.AbsPath
					}
					fmt.Fprintf(writer, "    %sat%s %s (%s:%d)\n", gray, reset, formatFunctionName(stackFrame.Function), location, stackFrame.Lineno)
				}
			}
		}
	} else if messageText := event.MessageText(); messageText != "" {
		fmt.Fprintf(writer, "  %s\n", messageText)
	} else if event.Transaction != "" {
		fmt.Fprintf(writer, "  %s\n", event.Transaction)
	}

	if event.Breadcrumbs != nil && len(event.Breadcrumbs.Values) > 0 {
		crumbs := event.Breadcrumbs.Values
		startIndex := 0
		if len(crumbs) > 8 {
			startIndex = len(crumbs) - 8
			fmt.Fprintf(writer, "  %sBreadcrumbs (%d earlier omitted):%s\n", gray, startIndex, reset)
		} else {
			fmt.Fprintf(writer, "  %sBreadcrumbs:%s\n", gray, reset)
		}
		for _, breadcrumb := range crumbs[startIndex:] {
			breadcrumbTimestamp := breadcrumb.Time().Local().Format("15:04:05.000")
			breadcrumbLevelColor := levelColor(breadcrumb.Level)
			category := breadcrumb.Category
			if category == "" {
				category = breadcrumb.Type
			}
			fmt.Fprintf(writer, "    %s%s%s %s%-7s%s %s%s%s: %s\n",
				dim, breadcrumbTimestamp, reset, breadcrumbLevelColor, strings.ToUpper(breadcrumb.Level), reset, gray, category, reset, breadcrumb.Message)
		}
	}

	if len(event.Tags) > 0 {
		var tagPairs []string
		for tagKey, tagValue := range event.Tags {
			tagPairs = append(tagPairs, fmt.Sprintf("%s=%s", tagKey, tagValue))
		}
		fmt.Fprintf(writer, "  %stags: %s%s\n", gray, strings.Join(tagPairs, " "), reset)
	}

	fmt.Fprintf(writer, "  %sid=%s%s\n", gray, capturedEvent.ID, reset)
}

func formatFunctionName(functionName string) string {
	if functionName == "" {
		return "<unknown>"
	}
	return functionName
}
