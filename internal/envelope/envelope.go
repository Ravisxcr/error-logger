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
	var reader io.Reader = body
	if gzipped {
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return Parse(data)
}

// Parse parses raw (already decompressed) envelope bytes.
func Parse(data []byte) (*Envelope, error) {
	headerLine, remainingBytes, found := cutLine(data)
	if !found {
		return nil, fmt.Errorf("envelope: missing header line")
	}
	parsedEnvelope := &Envelope{}
	headerLine = bytes.TrimSpace(headerLine)
	if len(headerLine) > 0 {
		if err := json.Unmarshal(headerLine, &parsedEnvelope.Header); err != nil {
			return nil, fmt.Errorf("envelope header: %w", err)
		}
	}

	for len(remainingBytes) > 0 {
		var itemHeaderLine []byte
		itemHeaderLine, remainingBytes, found = cutLine(remainingBytes)
		if !found {
			// Final line with no trailing newline.
			itemHeaderLine = remainingBytes
			remainingBytes = nil
		}
		itemHeaderLine = bytes.TrimSpace(itemHeaderLine)
		if len(itemHeaderLine) == 0 {
			continue
		}
		var itemHeader ItemHeader
		if err := json.Unmarshal(itemHeaderLine, &itemHeader); err != nil {
			return nil, fmt.Errorf("envelope item header: %w", err)
		}

		var payload []byte
		if itemHeader.Length != nil {
			payloadLength := *itemHeader.Length
			if payloadLength > len(remainingBytes) {
				return nil, fmt.Errorf("envelope item %q: declared length %d exceeds remaining body", itemHeader.Type, payloadLength)
			}
			payload = remainingBytes[:payloadLength]
			remainingBytes = remainingBytes[payloadLength:]
			// Consume the single trailing newline separating this item from the next.
			if len(remainingBytes) > 0 && remainingBytes[0] == '\n' {
				remainingBytes = remainingBytes[1:]
			}
		} else {
			payload, remainingBytes, found = cutLine(remainingBytes)
			if !found {
				payload = remainingBytes
				remainingBytes = nil
			}
		}

		parsedEnvelope.Items = append(parsedEnvelope.Items, Item{Header: itemHeader, Payload: payload})
	}

	return parsedEnvelope, nil
}

// cutLine splits data at the first '\n', returning the part before it, the
// part after it, and true. If there is no '\n', it returns false.
func cutLine(data []byte) (before, after []byte, found bool) {
	newlineIndex := bytes.IndexByte(data, '\n')
	if newlineIndex < 0 {
		return nil, nil, false
	}
	return data[:newlineIndex], data[newlineIndex+1:], true
}
