package ingest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseJSONPreservesOriginalRecord(t *testing.T) {
	input := `{"events":[{"source":"edr","timestamp":"2026-10-21T01:02:03+09:00","type":"process.start","level":"HIGH","host":"ws-01","message":"Suspicious process","event_id":42,"extra":{"parent":"shell"}}]}`
	events, err := Parse(strings.NewReader(input), "json", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	e := events[0]
	if e.Source != "edr" || e.ExternalID != "42" || e.EventType != "process.start" || e.Severity != "high" || e.Asset != "ws-01" {
		t.Fatalf("wrong normalization: %+v", e)
	}
	if want := time.Date(2026, 10, 20, 16, 2, 3, 0, time.UTC); !e.OccurredAt.Equal(want) {
		t.Fatalf("time: got %s want %s", e.OccurredAt, want)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(e.Raw, &raw); err != nil || raw["extra"] == nil {
		t.Fatalf("original evidence not preserved: %q, %v", e.Raw, err)
	}
}

func TestParseJSONLAndCSV(t *testing.T) {
	jsonl := "{\"occurred_at\":\"1761000000\",\"message\":\"a\",\"event_type\":\"auth\"}\n" +
		"{\"occurred_at\":1761000000000,\"message\":\"b\"}\n"
	events, err := Parse(strings.NewReader(jsonl), "auto", Options{DefaultSource: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Source != "gateway" || events[1].EventType != "generic" || events[1].Severity != "info" {
		t.Fatalf("wrong JSONL normalization: %+v", events)
	}
	if !events[0].OccurredAt.Equal(events[1].OccurredAt) {
		t.Fatalf("seconds/milliseconds parsed differently: %s / %s", events[0].OccurredAt, events[1].OccurredAt)
	}
	csv := "source,occurred at,event-type,message,asset,custom-field\nsiem,2026-10-21T00:00:00Z,auth.failure,Invalid password,db-1,kept\n"
	events, err = Parse(strings.NewReader(csv), "csv", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "auth.failure" || !bytes.Contains(events[0].Raw, []byte(`"custom_field":"kept"`)) {
		t.Fatalf("CSV evidence or normalization missing: %+v", events)
	}
}

func TestParseRejectsMalformedOrOversizedBatchesAtomically(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		format string
		opts   Options
	}{
		{"missing time", `[{"source":"x","message":"a"}]`, "json", Options{}},
		{"wrong type", `[{"source":"x","timestamp":"2026-10-21T00:00:00Z","message":{}}]`, "json", Options{}},
		{"second record invalid", `[{"source":"x","timestamp":"2026-10-21T00:00:00Z","message":"good"},{"source":"x","timestamp":"bad","message":"bad"}]`, "json", Options{}},
		{"too many", `[{"source":"x","timestamp":"2026-10-21T00:00:00Z","message":"a"},{"source":"x","timestamp":"2026-10-21T00:00:00Z","message":"b"}]`, "json", Options{MaxRecords: 1}},
		{"too large", `[{"source":"x","timestamp":"2026-10-21T00:00:00Z","message":"a"}]`, "json", Options{MaxBytes: 10}},
		{"malformed CSV", "source,occurred_at,message\nx,2026-10-21T00:00:00Z\n", "csv", Options{}},
		{"duplicate CSV header", "source,source,occurred_at,message\nx,y,2026-10-21T00:00:00Z,z\n", "csv", Options{}},
		{"unsupported", "x", "xml", Options{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := Parse(strings.NewReader(tt.input), tt.format, tt.opts)
			if err == nil || events != nil {
				t.Fatalf("expected atomic failure, events=%+v err=%v", events, err)
			}
		})
	}
}

func TestParseRejectsUnsupportedEncoding(t *testing.T) {
	_, err := Parse(bytes.NewReader([]byte{0xff, 0xfe}), "auto", Options{})
	if err == nil {
		t.Fatal("expected UTF-8 error")
	}
}
