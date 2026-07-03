// Package sentryevent defines Go types for the subset of the Sentry event
// JSON schema (https://develop.sentry.dev/sdk/event-payloads/) that this
// server needs to accept, store, and render.
package sentryevent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event is a single Sentry "error" or "message" event, as sent inside an
// envelope item of type "event".
type Event struct {
	EventID     string          `json:"event_id"`
	Timestamp   json.RawMessage `json:"timestamp,omitempty"` // float seconds or RFC3339 string
	Platform    string          `json:"platform,omitempty"`
	Level       string          `json:"level,omitempty"`
	Logger      string          `json:"logger,omitempty"`
	Transaction string          `json:"transaction,omitempty"`
	ServerName  string          `json:"server_name,omitempty"`
	Release     string          `json:"release,omitempty"`
	Dist        string          `json:"dist,omitempty"`
	Environment string          `json:"environment,omitempty"`

	Message     *Message               `json:"message,omitempty"`
	LogEntry    *Message               `json:"logentry,omitempty"`
	Exception   *ExceptionContainer    `json:"exception,omitempty"`
	Breadcrumbs *BreadcrumbContainer   `json:"breadcrumbs,omitempty"`
	Threads     json.RawMessage        `json:"threads,omitempty"`
	Request     *Request               `json:"request,omitempty"`
	User        *User                  `json:"user,omitempty"`
	SDK         *SDK                   `json:"sdk,omitempty"`
	Contexts    map[string]interface{} `json:"contexts,omitempty"`
	Tags        TagSet                 `json:"tags,omitempty"`
	Extra       map[string]interface{} `json:"extra,omitempty"`
	Fingerprint []string               `json:"fingerprint,omitempty"`
	Modules     map[string]string      `json:"modules,omitempty"`
}

// Message is the "message" field, which Sentry SDKs send either as a plain
// string (older format) or a structured object (newer format).
type Message struct {
	Formatted string   `json:"formatted,omitempty"`
	Message   string   `json:"message,omitempty"`
	Params    []string `json:"params,omitempty"`
}

// UnmarshalJSON accepts either a bare JSON string or a {"formatted": ...} object.
func (m *Message) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Formatted = s
		return nil
	}
	type alias Message
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = Message(a)
	return nil
}

// Text returns the best human-readable rendering of the message.
func (m *Message) Text() string {
	if m == nil {
		return ""
	}
	if m.Formatted != "" {
		return m.Formatted
	}
	return m.Message
}

// MessageText returns the event's message text, checking both wire encodings
// Sentry SDKs use for it: capture_message sends "message", while the stdlib
// LoggingIntegration sends "logentry".
func (e *Event) MessageText() string {
	if e == nil {
		return ""
	}
	if e.Message != nil && e.Message.Text() != "" {
		return e.Message.Text()
	}
	return e.LogEntry.Text()
}

// TagSet handles Sentry's two tag encodings: an object map, or an array of
// [key, value] pairs.
type TagSet map[string]string

func (t *TagSet) UnmarshalJSON(data []byte) error {
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err == nil {
		*t = m
		return nil
	}
	var pairs [][2]string
	if err := json.Unmarshal(data, &pairs); err != nil {
		return err
	}
	m = make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p[0]] = p[1]
	}
	*t = m
	return nil
}

type ExceptionContainer struct {
	Values []Exception `json:"values,omitempty"`
}

type Exception struct {
	Type       string      `json:"type,omitempty"`
	Value      string      `json:"value,omitempty"`
	Module     string      `json:"module,omitempty"`
	ThreadID   json.Number `json:"thread_id,omitempty"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
	Mechanism  *Mechanism  `json:"mechanism,omitempty"`
}

type Mechanism struct {
	Type        string      `json:"type,omitempty"`
	Handled     *bool       `json:"handled,omitempty"`
	Source      string      `json:"source,omitempty"`
	ExceptionID json.Number `json:"exception_id,omitempty"`
}

type Stacktrace struct {
	Frames []Frame `json:"frames,omitempty"`
}

type Frame struct {
	Filename    string          `json:"filename,omitempty"`
	AbsPath     string          `json:"abs_path,omitempty"`
	Function    string          `json:"function,omitempty"`
	Module      string          `json:"module,omitempty"`
	Lineno      int             `json:"lineno,omitempty"`
	Colno       int             `json:"colno,omitempty"`
	ContextLine string          `json:"context_line,omitempty"`
	PreContext  []string        `json:"pre_context,omitempty"`
	PostContext []string        `json:"post_context,omitempty"`
	InApp       bool            `json:"in_app,omitempty"`
	Vars        json.RawMessage `json:"vars,omitempty"`
}

type BreadcrumbContainer struct {
	Values []Breadcrumb `json:"values,omitempty"`
}

type Breadcrumb struct {
	Timestamp json.RawMessage `json:"timestamp,omitempty"`
	Type      string          `json:"type,omitempty"`
	Category  string          `json:"category,omitempty"`
	Level     string          `json:"level,omitempty"`
	Message   string          `json:"message,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// Time parses the breadcrumb's timestamp, which Sentry SDKs send as either
// an RFC3339 string or a float of seconds since the epoch. The zero Time is
// returned if it's absent or unparseable.
func (b *Breadcrumb) Time() time.Time {
	if b == nil || len(b.Timestamp) == 0 {
		return time.Time{}
	}
	var s string
	if err := json.Unmarshal(b.Timestamp, &s); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
		return time.Time{}
	}
	if f, err := strconv.ParseFloat(string(b.Timestamp), 64); err == nil {
		return time.Unix(0, int64(f*float64(time.Second)))
	}
	return time.Time{}
}

type Request struct {
	URL         string          `json:"url,omitempty"`
	Method      string          `json:"method,omitempty"`
	QueryString string          `json:"query_string,omitempty"`
	Headers     json.RawMessage `json:"headers,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

type User struct {
	ID        string `json:"id,omitempty"`
	Email     string `json:"email,omitempty"`
	Username  string `json:"username,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
}

type SDK struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// GroupKey returns a stable string identifying the "issue" this event
// belongs to, so repeated occurrences of the same underlying error can be
// collapsed into one entry with a running count instead of appearing as
// separate rows (mirrors Sentry's issue grouping). An empty result means
// there isn't enough information to group the event.
func (e *Event) GroupKey() string {
	if e == nil {
		return ""
	}
	if len(e.Fingerprint) > 0 {
		return "fp:" + strings.Join(e.Fingerprint, "\x00")
	}
	if e.Exception != nil && len(e.Exception.Values) > 0 {
		exc := e.Exception.Values[len(e.Exception.Values)-1]
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			f := exc.Stacktrace.Frames[len(exc.Stacktrace.Frames)-1]
			return fmt.Sprintf("exc:%s\x00%s\x00%s", exc.Type, f.Module, f.Function)
		}
		return fmt.Sprintf("exc:%s\x00%s", exc.Type, exc.Value)
	}
	if msg := e.MessageText(); msg != "" {
		return "msg:" + e.Logger + "\x00" + msg
	}
	if e.Transaction != "" {
		return "txn:" + e.Transaction
	}
	return ""
}
