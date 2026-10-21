package detection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileSimpleMatchesNormalizedAndRawFields(t *testing.T) {
	event := map[string]any{
		"event_type": "authentication.failure",
		"message":    "Failed login from 10.0.0.5",
		"severity":   "high",
		"score":      7.5,
		"raw": map[string]any{
			"user":    map[string]any{"name": "Alice"},
			"blocked": false,
		},
	}
	cases := []struct {
		field, op, value string
		want             bool
	}{
		{"event_type", "equals", "AUTHENTICATION.FAILURE", true},
		{"message", "contains", "failed login", true},
		{"message", "starts_with", "failed", true},
		{"message", "ends_with", "10.0.0.5", true},
		{"message", "regex", `failed login from 10\.[0-9.]+`, true},
		{"raw.user.name", "equals", "alice", true},
		{"raw.blocked", "equals", "false", true},
		{"raw.missing", "exists", "false", true},
		{"raw.blocked", "exists", "true", true},
		{"score", "gte", "7", true},
		{"score", "lt", "7", false},
		{"message", "contains", "success", false},
	}
	for _, tc := range cases {
		t.Run(tc.field+"_"+tc.op+"_"+tc.value, func(t *testing.T) {
			rule, err := CompileSimple(tc.field, tc.op, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := rule.Match(event); got != tc.want {
				t.Fatalf("Match() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestCompileJSONBooleanConditionsAndLists(t *testing.T) {
	rule, err := CompileJSON([]byte(`{
		"detection": {
			"login": {"event_type": "auth.failure", "raw.user": ["alice", "bob"]},
			"process": [{"raw.process.name|endswith": "powershell.exe"}, {"raw.process.name|endswith": "cmd.exe"}],
			"test_event": {"raw.test": true},
			"condition": "(login or process) and not test_event"
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		event map[string]any
		want  bool
	}{
		{"login", map[string]any{"event_type": "auth.failure", "raw": map[string]any{"user": "BOB"}}, true},
		{"process", map[string]any{"raw": map[string]any{"process": map[string]any{"name": "CMD.EXE"}}}, true},
		{"filtered", map[string]any{"event_type": "auth.failure", "raw": map[string]any{"user": "alice", "test": true}}, false},
		{"unrelated", map[string]any{"event_type": "auth.success", "raw": map[string]any{"user": "alice"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rule.Match(tc.event); got != tc.want {
				t.Fatalf("Match() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestCompileJSONQuantifiersAndAllModifier(t *testing.T) {
	rule, err := CompileJSON([]byte(`{
		"detection": {
			"selection_a": {"message|contains|all": ["failed", "admin"]},
			"selection_b": {"severity": "critical"},
			"selection_c": {"raw.port|gte": 1024},
			"condition": "2 of selection*"
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !rule.Match(map[string]any{"message": "Admin failed login", "severity": "CRITICAL"}) {
		t.Fatal("expected two matching selections")
	}
	if rule.Match(map[string]any{"message": "Admin failed login", "severity": "low"}) {
		t.Fatal("only one selection matched")
	}
	all, err := CompileJSON([]byte(`{"detection":{"a":{"x":1},"b":{"y":2},"condition":"all of them"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !all.Match(map[string]any{"x": 1, "y": 2}) || all.Match(map[string]any{"x": 1}) {
		t.Fatal("all of them must match every selection")
	}
}

func TestLogsourceIsEnforced(t *testing.T) {
	rule, err := CompileJSON([]byte(`{
		"logsource": {"product": "windows"},
		"detection": {"selection": {"event_type": "process_start"}, "condition": "selection"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rule.Match(map[string]any{"event_type": "process_start"}) {
		t.Fatal("missing logsource must not be silently ignored")
	}
	if !rule.Match(map[string]any{"event_type": "process_start", "logsource": map[string]any{"product": "Windows"}}) {
		t.Fatal("matching logsource should permit event")
	}
}

func TestInvalidRulesFailClosed(t *testing.T) {
	definitions := []string{
		`{}`,
		`{"detection":{"condition":"selection"}}`,
		`{"detection":{"selection":{"message|unsupported":"x"},"condition":"selection"}}`,
		`{"detection":{"selection":{"message|re":"["},"condition":"selection"}}`,
		`{"detection":{"selection":{"message":"x"},"condition":"other"}}`,
		`{"detection":{"selection":{"message":"x"},"condition":"not"}}`,
		`{"detection":{"selection":{"message":"x"},"condition":"1 of missing*"}}`,
		`{"detection":{"selection":{"message":"x"},"condition":"selection and ("}}`,
		`{"detection":{"selection":{"message":"x"},"condition":"selection"},"correlation":{}}`,
		`{"detection":{"selection":{"message":"x"},"timeframe":"5m","condition":"selection"}}`,
		`{"detection":{"selection":{"message|exists":"true"},"condition":"selection"}}`,
		`{"detection":{"selection":{"raw.user|gt":"hello"},"condition":"selection"}}`,
	}
	for _, definition := range definitions {
		if rule, err := CompileJSON([]byte(definition)); err == nil {
			t.Fatalf("unexpected acceptance of %s: %#v", definition, rule)
		}
	}
	if _, err := CompileSimple("message", "command", "x"); err == nil {
		t.Fatal("unknown simple operator was accepted")
	}
	if _, err := CompileSimple("message", "regex", strings.Repeat("a", maxRegexBytes+1)); err == nil {
		t.Fatal("oversized regex was accepted")
	}
	if _, err := CompileJSON([]byte(strings.Repeat(" ", maxRuleBytes+1))); err == nil {
		t.Fatal("oversized definition was accepted")
	}
}

func TestDottedLiteralKeyAndArrayEventValue(t *testing.T) {
	rule, err := CompileSimple("raw.process.name", "equals", "powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	event := map[string]any{"raw.process.name": []any{"cmd.exe", "PowerShell.EXE"}}
	if !rule.Match(event) {
		t.Fatal("dotted literal key and array values should match")
	}
	// Decode as JSON to cover json.Number values used by CompileJSON and event import.
	var numberEvent map[string]any
	decoder := json.NewDecoder(strings.NewReader(`{"raw":{"risk":42}}`))
	decoder.UseNumber()
	if err := decoder.Decode(&numberEvent); err != nil {
		t.Fatal(err)
	}
	risk, err := CompileSimple("raw.risk", "gt", "40")
	if err != nil || !risk.Match(numberEvent) {
		t.Fatalf("json.Number comparison: %v", err)
	}
}
