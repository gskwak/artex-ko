package detection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ConvertSigmaYAML converts one supported Sigma YAML document to the JSON
// definition accepted by CompileJSON. It preserves detection and logsource,
// while metadata (title, id, description, tags, etc.) stays the caller's
// responsibility. Unsupported Sigma constructs cause an error rather than a
// misleading partial match.
func ConvertSigmaYAML(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxRuleBytes {
		return nil, fmt.Errorf("Sigma document must contain 1..%d bytes", maxRuleBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var source map[string]any
	if err := decoder.Decode(&source); err != nil {
		return nil, fmt.Errorf("decode Sigma YAML: %w", err)
	}
	if source == nil {
		return nil, errors.New("Sigma document must be a mapping")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("only one Sigma YAML document is supported")
	}
	detection, ok := source["detection"].(map[string]any)
	if !ok {
		return nil, errors.New("Sigma detection must be a mapping")
	}
	if _, ok := source["correlation"]; ok {
		return nil, errors.New("Sigma correlation rules are not supported")
	}
	if _, ok := detection["timeframe"]; ok {
		return nil, errors.New("Sigma timeframe rules are not supported")
	}
	definition := map[string]any{"detection": detection}
	if logsource, ok := source["logsource"]; ok {
		definition["logsource"] = logsource
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("convert Sigma values: %w", err)
	}
	if _, err := CompileJSON(encoded); err != nil {
		return nil, fmt.Errorf("unsupported Sigma rule: %w", err)
	}
	return encoded, nil
}

// CompileSigmaYAML compiles one supported Sigma YAML document directly. It
// applies the same matching semantics and limits as CompileJSON.
func CompileSigmaYAML(raw []byte) (*Rule, error) {
	definition, err := ConvertSigmaYAML(raw)
	if err != nil {
		return nil, err
	}
	return CompileJSON(definition)
}
