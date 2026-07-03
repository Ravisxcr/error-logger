// Package console pretty-prints captured events to stdout as they arrive.
package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/ravi/error-logger/internal/store"
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

// Print writes a human-readable summary of c to w. isNew reports whether
// this is the first occurrence of c's underlying error; repeats print a
// single compact "seen again" line carrying the running count instead of a
// full block, so the console doesn't get flooded with duplicate stacktraces.
func Print(w io.Writer, c store.Captured, isNew bool) {
	ts := c.LastSeen.Local().Format("15:04:05")

	if c.Event == nil {
		fmt.Fprintf(w, "%s[%s]%s %s%-10s%s project=%s id=%s\n",
			dim, ts, reset, cyan, c.Kind, reset, c.ProjectID, c.ID)
		return
	}

	e := c.Event
	level := e.Level
	if level == "" {
		if c.Kind == "event" {
			level = "error"
		} else {
			level = "info"
		}
	}
	lc := levelColor(level)

	if !isNew {
		var summary string
		switch {
		case e.Exception != nil && len(e.Exception.Values) > 0:
			exc := e.Exception.Values[len(e.Exception.Values)-1]
			summary = fmt.Sprintf("%s: %s", exc.Type, exc.Value)
		case e.Message != nil && e.Message.Text() != "":
			summary = e.Message.Text()
		case e.Transaction != "":
			summary = e.Transaction
		}
		fmt.Fprintf(w, "%s[%s]%s %s%-7s%s project=%s %s(seen again x%d)%s %s %sid=%s%s\n",
			dim, ts, reset, lc, strings.ToUpper(level), reset, c.ProjectID, gray, c.Count, reset, summary, gray, c.ID, reset)
		return
	}

	header := fmt.Sprintf("%s[%s]%s %s%-7s%s project=%s", dim, ts, reset, lc, strings.ToUpper(level), reset, c.ProjectID)
	if c.Kind != "event" {
		header += fmt.Sprintf(" kind=%s", c.Kind)
	}
	if e.Environment != "" {
		header += fmt.Sprintf(" env=%s", e.Environment)
	}
	if e.Release != "" {
		header += fmt.Sprintf(" release=%s", e.Release)
	}
	fmt.Fprintln(w, header)

	if e.Exception != nil {
		for _, exc := range e.Exception.Values {
			fmt.Fprintf(w, "  %s%s%s: %s\n", bold, exc.Type, reset, exc.Value)
			if exc.Stacktrace != nil {
				frames := exc.Stacktrace.Frames
				start := 0
				if len(frames) > 8 {
					start = len(frames) - 8
					fmt.Fprintf(w, "    %s... %d earlier frames omitted%s\n", gray, start, reset)
				}
				for _, f := range frames[start:] {
					loc := f.Filename
					if loc == "" {
						loc = f.AbsPath
					}
					fmt.Fprintf(w, "    %sat%s %s (%s:%d)\n", gray, reset, frameFunc(f.Function), loc, f.Lineno)
				}
			}
		}
	} else if e.Message != nil {
		fmt.Fprintf(w, "  %s\n", e.Message.Text())
	} else if e.Transaction != "" {
		fmt.Fprintf(w, "  %s\n", e.Transaction)
	}

	if len(e.Tags) > 0 {
		var pairs []string
		for k, v := range e.Tags {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
		}
		fmt.Fprintf(w, "  %stags: %s%s\n", gray, strings.Join(pairs, " "), reset)
	}

	fmt.Fprintf(w, "  %sid=%s%s\n", gray, c.ID, reset)
}

func frameFunc(f string) string {
	if f == "" {
		return "<unknown>"
	}
	return f
}
