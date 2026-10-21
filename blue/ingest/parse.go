// Package ingest parses untrusted security telemetry into bounded, evidence-
// preserving blue-team events. Parsing is atomic: one malformed record rejects
// the entire batch, so callers never store a partially interpreted file.
package ingest

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultMaxBytes   int64 = 8 << 20
	DefaultMaxRecords       = 500
	maxAllowedBytes   int64 = 16 << 20
	maxAllowedRecords       = 5000
	maxRecordBytes          = 128 << 10
	maxFieldBytes           = 16 << 10
)

// Event is the normalized view used for indexing and correlation. Raw retains
// the complete original JSON object or CSV row (including unrecognized fields).
type Event struct {
	Source     string          `json:"source"`
	ExternalID string          `json:"external_id,omitempty"`
	EventType  string          `json:"event_type"`
	Severity   string          `json:"severity"`
	Asset      string          `json:"asset"`
	Message    string          `json:"message"`
	OccurredAt time.Time       `json:"occurred_at"`
	Raw        json.RawMessage `json:"raw"`
}

type Options struct {
	MaxBytes      int64
	MaxRecords    int
	DefaultSource string
}

func (o Options) normalized() Options {
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.MaxBytes > maxAllowedBytes {
		o.MaxBytes = maxAllowedBytes
	}
	if o.MaxRecords <= 0 {
		o.MaxRecords = DefaultMaxRecords
	}
	if o.MaxRecords > maxAllowedRecords {
		o.MaxRecords = maxAllowedRecords
	}
	o.DefaultSource = strings.TrimSpace(o.DefaultSource)
	return o
}

// Parse accepts format "json", "jsonl", "csv", or "auto". JSON input can be
// one event, an array, or an object containing an events array. JSONL is one
// object per line. CSV needs a header row. Required fields are source (or
// DefaultSource), occurred_at/timestamp, and message. event_type defaults to
// "generic" and severity to "info" when absent.
func Parse(r io.Reader, format string, opts Options) ([]Event, error) {
	if r == nil {
		return nil, errors.New("telemetry input is missing")
	}
	opts = opts.normalized()
	data, err := io.ReadAll(io.LimitReader(r, opts.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read telemetry: %w", err)
	}
	if int64(len(data)) > opts.MaxBytes {
		return nil, fmt.Errorf("telemetry exceeds %d bytes", opts.MaxBytes)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}) // UTF-8 BOM
	if !utf8.Valid(data) {
		return nil, errors.New("telemetry must be UTF-8")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("telemetry is empty")
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || format == "auto" {
		format = detectFormat(data)
	}
	switch format {
	case "json", "application/json":
		return parseJSON(data, opts)
	case "jsonl", "ndjson", "application/x-ndjson":
		return parseJSONL(data, opts)
	case "csv", "text/csv":
		return parseCSV(data, opts)
	default:
		return nil, fmt.Errorf("unsupported telemetry format %q", format)
	}
}

func detectFormat(data []byte) string {
	if data[0] == '[' {
		return "json"
	}
	if data[0] == '{' {
		if json.Valid(data) {
			return "json"
		}
		return "jsonl"
	}
	return "csv"
}

func parseJSON(data []byte, opts Options) ([]Event, error) {
	if !json.Valid(data) {
		return nil, errors.New("invalid JSON telemetry")
	}
	var records []json.RawMessage
	switch data[0] {
	case '[':
		if err := json.Unmarshal(data, &records); err != nil {
			return nil, fmt.Errorf("decode JSON events: %w", err)
		}
	case '{':
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, fmt.Errorf("decode JSON telemetry: %w", err)
		}
		if raw, ok := envelope["events"]; ok {
			if err := json.Unmarshal(raw, &records); err != nil {
				return nil, fmt.Errorf("decode events array: %w", err)
			}
		} else {
			records = []json.RawMessage{data}
		}
	default:
		return nil, errors.New("JSON telemetry must be an object or array")
	}
	return normalizeRecords(records, opts)
}

func parseJSONL(data []byte, opts Options) ([]Event, error) {
	lines := bytes.Split(data, []byte{'\n'})
	records := make([]json.RawMessage, 0, min(len(lines), opts.MaxRecords))
	for lineNo, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if len(records) >= opts.MaxRecords {
			return nil, fmt.Errorf("telemetry exceeds %d records", opts.MaxRecords)
		}
		if !json.Valid(line) {
			return nil, fmt.Errorf("line %d: invalid JSON", lineNo+1)
		}
		records = append(records, append(json.RawMessage(nil), line...))
	}
	return normalizeRecords(records, opts)
}

func parseCSV(data []byte, opts Options) ([]Event, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.ReuseRecord = false
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	if len(header) == 0 || len(header) > 128 {
		return nil, errors.New("CSV must have 1 to 128 columns")
	}
	seen := make(map[string]bool, len(header))
	for i, h := range header {
		h = canonicalKey(h)
		if h == "" || seen[h] {
			return nil, fmt.Errorf("CSV header %d is empty or duplicated", i+1)
		}
		header[i] = h
		seen[h] = true
	}
	events := make([]Event, 0)
	for rowNo := 2; ; rowNo++ {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV row %d: %w", rowNo, err)
		}
		if len(row) != len(header) {
			return nil, fmt.Errorf("CSV row %d has %d columns, expected %d", rowNo, len(row), len(header))
		}
		if len(events) >= opts.MaxRecords {
			return nil, fmt.Errorf("telemetry exceeds %d records", opts.MaxRecords)
		}
		object := make(map[string]string, len(header))
		for i, value := range row {
			object[header[i]] = value
		}
		raw, err := json.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("CSV row %d: encode evidence: %w", rowNo, err)
		}
		event, err := normalizeRecord(raw, opts)
		if err != nil {
			return nil, fmt.Errorf("CSV row %d: %w", rowNo, err)
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil, errors.New("telemetry has no events")
	}
	return events, nil
}

func normalizeRecords(records []json.RawMessage, opts Options) ([]Event, error) {
	if len(records) == 0 {
		return nil, errors.New("telemetry has no events")
	}
	if len(records) > opts.MaxRecords {
		return nil, fmt.Errorf("telemetry exceeds %d records", opts.MaxRecords)
	}
	events := make([]Event, 0, len(records))
	for i, raw := range records {
		event, err := normalizeRecord(raw, opts)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func normalizeRecord(raw json.RawMessage, opts Options) (Event, error) {
	if len(raw) > maxRecordBytes {
		return Event{}, fmt.Errorf("event exceeds %d bytes", maxRecordBytes)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Event{}, errors.New("event must be a JSON object")
	}
	canonical := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		canonical[canonicalKey(key)] = value
	}
	get := func(names ...string) (string, error) {
		for _, name := range names {
			if value, ok := canonical[name]; ok && string(value) != "null" {
				var s string
				if err := json.Unmarshal(value, &s); err != nil {
					// Timestamps and external IDs may legitimately be numeric.
					var n json.Number
					if err := json.Unmarshal(value, &n); err != nil {
						return "", fmt.Errorf("%s must be a string or number", name)
					}
					s = n.String()
				}
				if len(s) > maxFieldBytes {
					return "", fmt.Errorf("%s exceeds %d bytes", name, maxFieldBytes)
				}
				return strings.TrimSpace(s), nil
			}
		}
		return "", nil
	}
	source, err := get("source", "logsource")
	if err != nil {
		return Event{}, err
	}
	if source == "" {
		source = opts.DefaultSource
	}
	if source == "" {
		return Event{}, errors.New("source is required")
	}
	if len(source) > maxFieldBytes {
		return Event{}, fmt.Errorf("source exceeds %d bytes", maxFieldBytes)
	}
	when, err := get("occurred_at", "timestamp", "event_time", "time")
	if err != nil {
		return Event{}, err
	}
	if when == "" {
		return Event{}, errors.New("occurred_at or timestamp is required")
	}
	occurredAt, err := parseTime(when)
	if err != nil {
		return Event{}, err
	}
	message, err := get("message", "description", "summary")
	if err != nil {
		return Event{}, err
	}
	if message == "" {
		return Event{}, errors.New("message is required")
	}
	eventType, err := get("event_type", "type", "category")
	if err != nil {
		return Event{}, err
	}
	if eventType == "" {
		eventType = "generic"
	}
	severity, err := get("severity", "level")
	if err != nil {
		return Event{}, err
	}
	if severity == "" {
		severity = "info"
	}
	asset, err := get("asset", "host", "hostname", "device")
	if err != nil {
		return Event{}, err
	}
	externalID, err := get("external_id", "event_id", "record_id")
	if err != nil {
		return Event{}, err
	}
	return Event{
		Source: source, ExternalID: externalID, EventType: eventType,
		Severity: strings.ToLower(severity), Asset: asset, Message: message,
		OccurredAt: occurredAt, Raw: append(json.RawMessage(nil), raw...),
	}, nil
}

func canonicalKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", "_", " ", "_").Replace(s)
	return s
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && n > 0 && n < 1e14 {
		if n > 1e11 { // common millisecond timestamps
			return time.UnixMilli(int64(n)).UTC(), nil
		}
		return time.Unix(int64(n), 0).UTC(), nil
	}
	return time.Time{}, errors.New("occurred_at must be RFC3339, UTC date/time, or Unix seconds/milliseconds")
}
