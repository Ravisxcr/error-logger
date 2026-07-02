// Package envelope parses the Sentry "envelope" wire format: a header JSON
// line followed by any number of (item header, item payload) line pairs.
// See https://develop.sentry.dev/sdk/envelopes/.
package envelope

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

// Header is the top-level envelope header line.
type Header struct {
	EventID string `json:"event_id,omitempty"`
	DSN     string `json:"dsn,omitempty"`
	SentAt  string `json:"sent_at,omitempty"`
}

// ItemHeader precedes each item's payload.
type ItemHeader struct {
	Type        string `json:"type,omitempty"`
	Length      *int   `json:"length,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Filename    string `json:"filename,omitempty"`
}

// Item is one envelope item: a header plus its raw payload bytes.
type Item struct {
	Header  ItemHeader
	Payload []byte
}

// Envelope is a parsed envelope: one header plus zero or more items.
type Envelope struct {
	Header Header
	Items  []Item
}

// Decode reads and decompresses (if needed) the request body, then parses
// it as a Sentry envelope.
func Decode(body io.Reader, gzipped bool) (*Envelope, error) {
	var r io.Reader = body
	if gzipped {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		r = gz
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return Parse(data)
}

// Parse parses raw (already decompressed) envelope bytes.
func Parse(data []byte) (*Envelope, error) {
	line, rest, ok := cutLine(data)
	if !ok {
		return nil, fmt.Errorf("envelope: missing header line")
	}
	env := &Envelope{}
	line = bytes.TrimSpace(line)
	if len(line) > 0 {
		if err := json.Unmarshal(line, &env.Header); err != nil {
			return nil, fmt.Errorf("envelope header: %w", err)
		}
	}

	for len(rest) > 0 {
		var itemHeaderLine []byte
		itemHeaderLine, rest, ok = cutLine(rest)
		if !ok {
			// Final line with no trailing newline.
			itemHeaderLine = rest
			rest = nil
		}
		itemHeaderLine = bytes.TrimSpace(itemHeaderLine)
		if len(itemHeaderLine) == 0 {
			continue
		}
		var ih ItemHeader
		if err := json.Unmarshal(itemHeaderLine, &ih); err != nil {
			return nil, fmt.Errorf("envelope item header: %w", err)
		}

		var payload []byte
		if ih.Length != nil {
			n := *ih.Length
			if n > len(rest) {
				return nil, fmt.Errorf("envelope item %q: declared length %d exceeds remaining body", ih.Type, n)
			}
			payload = rest[:n]
			rest = rest[n:]
			// Consume the single trailing newline separating this item from the next.
			if len(rest) > 0 && rest[0] == '\n' {
				rest = rest[1:]
			}
		} else {
			payload, rest, ok = cutLine(rest)
			if !ok {
				payload = rest
				rest = nil
			}
		}

		env.Items = append(env.Items, Item{Header: ih, Payload: payload})
	}

	return env, nil
}

// cutLine splits data at the first '\n', returning the part before it, the
// part after it, and true. If there is no '\n', it returns false.
func cutLine(data []byte) (before, after []byte, found bool) {
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		return nil, nil, false
	}
	return data[:i], data[i+1:], true
}
