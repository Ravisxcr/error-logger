package envelope

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestParseEnvelope(t *testing.T) {
	raw := `{"event_id":"9ec79c33ec9942ab8353589fcb2e04dc","dsn":"http://public@localhost:9000/1"}
{"type":"session","length":15}
{"started":"1"}
{"type":"event"}
{"message":"Test message from SDK"}
`
	env, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if env.Header.EventID != "9ec79c33ec9942ab8353589fcb2e04dc" {
		t.Errorf("expected event_id 9ec79c33ec9942ab8353589fcb2e04dc, got %s", env.Header.EventID)
	}
	if len(env.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(env.Items))
	}
	if env.Items[0].Header.Type != "session" {
		t.Errorf("item 0: expected type session, got %s", env.Items[0].Header.Type)
	}
	if string(env.Items[0].Payload) != `{"started":"1"}` {
		t.Errorf("item 0 payload: got %q", string(env.Items[0].Payload))
	}
	if env.Items[1].Header.Type != "event" {
		t.Errorf("item 1: expected type event, got %s", env.Items[1].Header.Type)
	}
	if string(env.Items[1].Payload) != `{"message":"Test message from SDK"}` {
		t.Errorf("item 1 payload: got %q", string(env.Items[1].Payload))
	}
}

func TestDecodeGzip(t *testing.T) {
	raw := `{"event_id":"123"}
{"type":"event"}
{"message":"Hello Gzip"}
`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte(raw))
	gw.Close()

	env, err := Decode(&buf, true)
	if err != nil {
		t.Fatalf("Decode gzip error: %v", err)
	}
	if env.Header.EventID != "123" {
		t.Errorf("expected event_id 123, got %s", env.Header.EventID)
	}
	if len(env.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(env.Items))
	}
	if !strings.Contains(string(env.Items[0].Payload), "Hello Gzip") {
		t.Errorf("expected payload to contain 'Hello Gzip', got %s", string(env.Items[0].Payload))
	}
}
