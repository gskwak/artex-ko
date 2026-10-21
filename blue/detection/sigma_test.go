package detection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExistingSigmaRulesCompileAndMatch(t *testing.T) {
	fixtures := []struct {
		name  string
		event map[string]any
	}{
		{
			name: "artex_enrich_user_agent.yml",
			event: map[string]any{
				"cs-user-agent": "artex-enrich/1.0",
				"logsource":     map[string]any{"category": "webserver"},
			},
		},
		{
			name: "destructive_command_hunting.yml",
			event: map[string]any{
				"CommandLine": "rm -rf /tmp/example",
				"logsource":   map[string]any{"category": "process_creation"},
			},
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "detections", "sigma", fixture.name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := ConvertSigmaYAML(data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(definition), `"detection"`) {
				t.Fatalf("conversion omitted detection: %s", definition)
			}
			rule, err := CompileSigmaYAML(data)
			if err != nil {
				t.Fatal(err)
			}
			if !rule.Match(fixture.event) {
				t.Fatalf("expected repository rule %s to match", fixture.name)
			}
		})
	}
}

func TestSigmaUnsupportedConstructsFailClearly(t *testing.T) {
	for _, yaml := range []string{
		"title: one\ncorrelation:\n  type: event_count\ndetection:\n  selection:\n    message: x\n  condition: selection\n",
		"detection:\n  selection:\n    message: x\n  timeframe: 5m\n  condition: selection\n",
		"detection:\n  keywords:\n    - marker\n  condition: keywords\n",
		"detection:\n  selection:\n    message: x\n  condition: selection\n---\ndetection:\n  selection:\n    message: y\n  condition: selection\n",
	} {
		if rule, err := CompileSigmaYAML([]byte(yaml)); err == nil {
			t.Fatalf("unexpected acceptance of unsupported Sigma: %#v", rule)
		}
	}
}
