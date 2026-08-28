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
type Event struct {
	EventID     string          `json:"event_id"`
	Type        string          `json:"type,omitempty"`      // "default" (error), "transaction", or "session"
	Timestamp   json.RawMessage `json:"timestamp,omitempty"` // float seconds or RFC3339 string
	Received    json.RawMessage `json:"received,omitempty"`  // ingest time recorded by server/relay
	Platform    string          `json:"platform,omitempty"`
	Level       string          `json:"level,omitempty"`
	Logger      string          `json:"logger,omitempty"`
	Transaction string          `json:"transaction,omitempty"`
	Culprit     string          `json:"culprit,omitempty"` // legacy/fallback function name
	ServerName  string          `json:"server_name,omitempty"`
	Release     string          `json:"release,omitempty"`
	Dist        string          `json:"dist,omitempty"`
	Environment string          `json:"environment,omitempty"`

	Message     *Message             `json:"message,omitempty"`
	LogEntry    *Message             `json:"logentry,omitempty"`
	Exception   *ExceptionContainer  `json:"exception,omitempty"`
	Breadcrumbs *BreadcrumbContainer `json:"breadcrumbs,omitempty"`
	Threads     json.RawMessage      `json:"threads,omitempty"`
	Request     *Request             `json:"request,omitempty"`
	User        *User                `json:"user,omitempty"`
	SDK         *SDK                 `json:"sdk,omitempty"`
	Contexts    map[string]any       `json:"contexts,omitempty"`
	Tags        TagSet               `json:"tags,omitempty"`
	Extra       map[string]any       `json:"extra,omitempty"`
	Fingerprint []string             `json:"fingerprint,omitempty"`
	Modules     map[string]string    `json:"modules,omitempty"`
}

// Message is the "message" field, which Sentry SDKs send either as a plain
// string (older format) or a structured object (newer format).
type Message struct {
	Formatted string `json:"formatted,omitempty"`
	Message   string `json:"message,omitempty"`
	Params    []any  `json:"params,omitempty"`
}

// UnmarshalJSON accepts either a bare JSON string or a {"formatted": ...} object.
func (msg *Message) UnmarshalJSON(data []byte) error {
	var strValue string
	if err := json.Unmarshal(data, &strValue); err == nil {
		msg.Formatted = strValue
		return nil
	}
	type messageAlias Message
	var msgAlias messageAlias
	if err := json.Unmarshal(data, &msgAlias); err != nil {
		return err
	}
	*msg = Message(msgAlias)
	return nil
}

// Text returns the best human-readable rendering of the message.
func (msg *Message) Text() string {
	if msg == nil {
		return ""
	}
	if msg.Formatted != "" {
		return msg.Formatted
	}
	return msg.Message
}

// MessageText returns the event's message text, checking both wire encodings
// Sentry SDKs use for it: capture_message sends "message", while the stdlib
// LoggingIntegration sends "logentry".
func (event *Event) MessageText() string {
	if event == nil {
		return ""
	}
	if event.Message != nil && event.Message.Text() != "" {
		return event.Message.Text()
	}
	return event.LogEntry.Text()
}

// TagSet handles Sentry's two tag encodings: an object map, or an array of
// [key, value] pairs.
type TagSet map[string]string

func (tags *TagSet) UnmarshalJSON(data []byte) error {
	tagMap := map[string]string{}
	if err := json.Unmarshal(data, &tagMap); err == nil {
		*tags = tagMap
		return nil
	}
	var pairs [][2]string
	if err := json.Unmarshal(data, &pairs); err != nil {
		return err
	}
	tagMap = make(map[string]string, len(pairs))
	for _, pair := range pairs {
		tagMap[pair[0]] = pair[1]
	}
	*tags = tagMap
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
func (crumb *Breadcrumb) Time() time.Time {
	if crumb == nil || len(crumb.Timestamp) == 0 {
		return time.Time{}
	}
	var timestampStr string
	if err := json.Unmarshal(crumb.Timestamp, &timestampStr); err == nil {
		if parsedTime, err := time.Parse(time.RFC3339Nano, timestampStr); err == nil {
			return parsedTime
		}
		return time.Time{}
	}
	if floatTimestamp, err := strconv.ParseFloat(string(crumb.Timestamp), 64); err == nil {
		return time.Unix(0, int64(floatTimestamp*float64(time.Second)))
	}
	return time.Time{}
}

func (c *ExceptionContainer) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	// Case 1: Bare array format `[{...}, {...}]`
	var list []Exception
	if err := json.Unmarshal(data, &list); err == nil {
		c.Values = list
		return nil
	}
	// Case 2: Object format `{"values": [...]}`
	type containerAlias ExceptionContainer
	var obj containerAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*c = ExceptionContainer(obj)
	return nil
}

func (c *BreadcrumbContainer) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	// Case 1: Bare array format `[{...}, {...}]`
	var list []Breadcrumb
	if err := json.Unmarshal(data, &list); err == nil {
		c.Values = list
		return nil
	}
	// Case 2: Object format `{"values": [...]}`
	type containerAlias BreadcrumbContainer
	var obj containerAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*c = BreadcrumbContainer(obj)
	return nil
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
func (event *Event) GroupKey() string {
	if event == nil {
		return ""
	}
	if len(event.Fingerprint) > 0 {
		return "fp:" + strings.Join(event.Fingerprint, "\x00")
	}
	if event.Exception != nil && len(event.Exception.Values) > 0 {
		lastException := event.Exception.Values[len(event.Exception.Values)-1]
		if lastException.Stacktrace != nil && len(lastException.Stacktrace.Frames) > 0 {
			frames := lastException.Stacktrace.Frames

			// Look for the innermost in_app frame (from bottom up)
			var targetFrame *Frame
			for i := len(frames) - 1; i >= 0; i-- {
				if frames[i].InApp {
					targetFrame = &frames[i]
					break
				}
			}
			// Fallback to the absolute last frame if no in_app frame exists
			if targetFrame == nil {
				targetFrame = &frames[len(frames)-1]
			}

			return fmt.Sprintf("exc:%s\x00%s\x00%s", lastException.Type, targetFrame.Module, targetFrame.Function)
		}
		return fmt.Sprintf("exc:%s\x00%s", lastException.Type, lastException.Value)
	}
	if msgText := event.MessageText(); msgText != "" {
		return "msg:" + event.Logger + "\x00" + msgText
	}
	if event.Transaction != "" {
		return "txn:" + event.Transaction
	}
	if event.Culprit != "" {
		return "culprit:" + event.Culprit
	}
	return ""
}

func (event *Event) Time() time.Time {
	if event == nil || len(event.Timestamp) == 0 {
		return time.Time{}
	}
	var timestampStr string
	if err := json.Unmarshal(event.Timestamp, &timestampStr); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, timestampStr); err == nil {
			return t
		}
		return time.Time{}
	}
	if floatTimestamp, err := strconv.ParseFloat(string(event.Timestamp), 64); err == nil {
		return time.Unix(0, int64(floatTimestamp*float64(time.Second)))
	}
	return time.Time{}
}
