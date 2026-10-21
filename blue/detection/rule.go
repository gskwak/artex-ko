// Package detection evaluates bounded, declarative security-event rules.
// A rule can inspect event fields, but cannot execute code, open files, or make
// network requests. The supported JSON form is a small, documented subset of
// Sigma's detection/condition model; unsupported modifiers fail validation.
package detection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	maxRuleBytes      = 64 << 10
	maxSelections     = 64
	maxClauses        = 64
	maxPredicates     = 64
	maxValues         = 100
	maxConditionBytes = 1024
	maxValueBytes     = 4096
	maxRegexBytes     = 512
)

// Rule is an immutable, compiled detection. A Rule is safe to reuse across
// goroutines because Match only reads its compiled state.
type Rule struct {
	selections map[string]selection
	condition  condition
	logsource  []predicate
}

type selection []clause // Alternative clauses are ORed.
type clause []predicate // Predicates in a clause are ANDed.

type predicate struct {
	field  string
	path   []string
	op     string
	all    bool
	cased  bool
	values []value
}

type value struct {
	raw any
	re  *regexp.Regexp
}

// CompileSimple provides a convenient single-condition rule for the UI. Its
// field can be a normalized event key (message, event_type, source, severity,
// asset, occurred_at) or a nested field such as raw.process.name. The operator
// is one of equals, contains, starts_with, ends_with, exists, gt, gte, lt, lte,
// or regex. Empty exists value means true.
func CompileSimple(field, operator, input string) (*Rule, error) {
	field = strings.TrimSpace(field)
	operator = strings.ToLower(strings.TrimSpace(operator))
	if operator == "" {
		operator = "equals"
	}
	modifier := map[string]string{
		"equals":      "",
		"eq":          "",
		"contains":    "contains",
		"starts_with": "startswith",
		"startswith":  "startswith",
		"ends_with":   "endswith",
		"endswith":    "endswith",
		"exists":      "exists",
		"gt":          "gt",
		"gte":         "gte",
		"lt":          "lt",
		"lte":         "lte",
		"regex":       "re",
		"re":          "re",
	}[operator]
	if modifier == "" && operator != "equals" && operator != "eq" {
		return nil, fmt.Errorf("unsupported operator %q", operator)
	}
	var operand any = input
	if modifier == "exists" {
		if strings.TrimSpace(input) == "" {
			operand = true
		} else {
			b, err := strconv.ParseBool(strings.TrimSpace(input))
			if err != nil {
				return nil, fmt.Errorf("exists value must be true or false: %w", err)
			}
			operand = b
		}
	} else if modifier == "gt" || modifier == "gte" || modifier == "lt" || modifier == "lte" {
		if _, err := strconv.ParseFloat(input, 64); err != nil {
			return nil, fmt.Errorf("numeric comparison requires a number: %w", err)
		}
		operand = json.Number(input)
	}
	key := field
	if modifier != "" {
		key += "|" + modifier
	}
	raw, err := json.Marshal(map[string]any{
		"detection": map[string]any{
			"selection": map[string]any{key: operand},
			"condition": "selection",
		},
	})
	if err != nil {
		return nil, err
	}
	return CompileJSON(raw)
}

// CompileJSON parses a Sigma-like JSON definition. Supported examples:
//
//	{"detection":{"selection":{"event_type":"login.failure",
//	  "message|contains":"failed"},"condition":"selection"}}
//
// Selections are maps of field predicates (AND), or arrays of maps (OR).
// Scalar and array values mean one-of matching, unless the key has |all.
// Conditions support and/or/not, parentheses, and N/all of them or a prefix*.
// Sigma correlation/timeframe/pipeline features are intentionally rejected.
func CompileJSON(raw []byte) (*Rule, error) {
	if len(raw) == 0 || len(raw) > maxRuleBytes {
		return nil, fmt.Errorf("rule definition must contain 1..%d bytes", maxRuleBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode rule: %w", err)
	}
	if root == nil {
		return nil, errors.New("rule definition must be an object")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("rule definition has trailing data")
	}
	for key := range root {
		switch key {
		case "detection", "logsource", "title", "description", "tags":
		default:
			return nil, fmt.Errorf("unsupported rule property %q", key)
		}
	}
	det, ok := root["detection"].(map[string]any)
	if !ok {
		return nil, errors.New("detection must be an object")
	}
	if len(det) < 2 || len(det) > maxSelections+1 {
		return nil, errors.New("detection must contain a condition and 1..64 selections")
	}
	conditionText, ok := det["condition"].(string)
	if !ok || strings.TrimSpace(conditionText) == "" || len(conditionText) > maxConditionBytes {
		return nil, errors.New("detection.condition must be a nonempty string of at most 1024 bytes")
	}
	rule := &Rule{selections: make(map[string]selection, len(det)-1)}
	for name, item := range det {
		if name == "condition" {
			continue
		}
		if name == "timeframe" || name == "condition" || !validSelectorName(name) {
			return nil, fmt.Errorf("unsupported selection name %q", name)
		}
		sel, err := compileSelection(item)
		if err != nil {
			return nil, fmt.Errorf("selection %q: %w", name, err)
		}
		rule.selections[name] = sel
	}
	if len(rule.selections) == 0 {
		return nil, errors.New("detection needs at least one selection")
	}
	parsed, err := parseCondition(conditionText, rule.selections)
	if err != nil {
		return nil, fmt.Errorf("detection.condition: %w", err)
	}
	rule.condition = parsed
	if source, exists := root["logsource"]; exists {
		mapping, ok := source.(map[string]any)
		if !ok || len(mapping) == 0 {
			return nil, errors.New("logsource must be a nonempty object")
		}
		// Logsource metadata constrains the event's nested logsource object.
		// Callers importing Sigma definitions must normalize this explicitly.
		prefixed := make(map[string]any, len(mapping))
		for key, v := range mapping {
			prefixed["logsource."+key] = v
		}
		predicates, err := compileClause(prefixed)
		if err != nil {
			return nil, fmt.Errorf("logsource: %w", err)
		}
		rule.logsource = predicates
	}
	return rule, nil
}

func validSelectorName(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	switch strings.ToLower(name) {
	case "and", "or", "not", "all", "any", "of", "them":
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func compileSelection(item any) (selection, error) {
	switch v := item.(type) {
	case map[string]any:
		predicates, err := compileClause(v)
		if err != nil {
			return nil, err
		}
		return selection{predicates}, nil
	case []any:
		if len(v) == 0 || len(v) > maxClauses {
			return nil, fmt.Errorf("selection array needs 1..%d clauses", maxClauses)
		}
		out := make(selection, 0, len(v))
		for i, raw := range v {
			mapping, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("clause %d must be an object", i)
			}
			predicates, err := compileClause(mapping)
			if err != nil {
				return nil, fmt.Errorf("clause %d: %w", i, err)
			}
			out = append(out, predicates)
		}
		return out, nil
	default:
		return nil, errors.New("selection must be an object or an array of objects")
	}
}

func compileClause(mapping map[string]any) (clause, error) {
	if len(mapping) == 0 || len(mapping) > maxPredicates {
		return nil, fmt.Errorf("clause needs 1..%d predicates", maxPredicates)
	}
	keys := make([]string, 0, len(mapping))
	for k := range mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(clause, 0, len(mapping))
	for _, k := range keys {
		p, err := compilePredicate(k, mapping[k])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func compilePredicate(key string, input any) (predicate, error) {
	segments := strings.Split(key, "|")
	field := segments[0]
	path := strings.Split(field, ".")
	if field == "" || len(field) > 200 || len(path) > 16 {
		return predicate{}, fmt.Errorf("invalid field %q", field)
	}
	for _, part := range path {
		if part == "" || len(part) > 80 {
			return predicate{}, fmt.Errorf("invalid field %q", field)
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '@') {
				return predicate{}, fmt.Errorf("invalid field %q", field)
			}
		}
	}
	p := predicate{field: field, path: path, op: "equals"}
	seenOp := false
	for _, modifier := range segments[1:] {
		switch modifier {
		case "all":
			if p.all {
				return predicate{}, fmt.Errorf("duplicate all modifier in %q", key)
			}
			p.all = true
		case "cased":
			if p.cased {
				return predicate{}, fmt.Errorf("duplicate cased modifier in %q", key)
			}
			p.cased = true
		case "contains", "startswith", "endswith", "exists", "gt", "gte", "lt", "lte", "re":
			if seenOp {
				return predicate{}, fmt.Errorf("multiple operators in %q", key)
			}
			p.op = modifier
			seenOp = true
		default:
			return predicate{}, fmt.Errorf("unsupported modifier %q in %q", modifier, key)
		}
	}
	if p.op == "exists" && p.all {
		return predicate{}, errors.New("exists cannot use all")
	}
	items, isList := input.([]any)
	if !isList {
		items = []any{input}
	}
	if len(items) == 0 || len(items) > maxValues {
		return predicate{}, fmt.Errorf("field %q needs 1..%d values", key, maxValues)
	}
	if p.op == "exists" && len(items) != 1 {
		return predicate{}, errors.New("exists requires one boolean value")
	}
	for _, item := range items {
		switch item.(type) {
		case string, json.Number, float64, bool:
		default:
			return predicate{}, fmt.Errorf("field %q values must be strings, numbers, or booleans", key)
		}
		if s, ok := item.(string); ok && len(s) > maxValueBytes {
			return predicate{}, fmt.Errorf("field %q value too long", key)
		}
		if p.op == "exists" {
			if _, ok := item.(bool); !ok {
				return predicate{}, errors.New("exists requires a boolean value")
			}
		}
		if p.op == "gt" || p.op == "gte" || p.op == "lt" || p.op == "lte" {
			if _, ok := numericValue(item); !ok {
				return predicate{}, fmt.Errorf("field %q requires numeric values", key)
			}
		}
		entry := value{raw: item}
		if p.op == "re" {
			pattern, ok := item.(string)
			if !ok || pattern == "" || len(pattern) > maxRegexBytes {
				return predicate{}, fmt.Errorf("field %q requires a regex of 1..%d bytes", key, maxRegexBytes)
			}
			if !p.cased {
				pattern = "(?i)" + pattern
			}
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return predicate{}, fmt.Errorf("field %q invalid regex: %w", key, err)
			}
			entry.re = compiled
		}
		p.values = append(p.values, entry)
	}
	return p, nil
}

// Match reports whether an event satisfies the compiled detection. Event maps
// may contain normalized fields and a nested "raw" map. Missing fields fail all
// comparisons except `|exists:false`.
func (r *Rule) Match(event map[string]any) bool {
	if r == nil || event == nil {
		return false
	}
	for _, p := range r.logsource {
		if !p.match(event) {
			return false
		}
	}
	return r.condition.match(r.selections, event)
}

func (p predicate) match(event map[string]any) bool {
	got, exists := lookup(event, p.field, p.path)
	if p.op == "exists" {
		want := p.values[0].raw.(bool)
		return (exists && got != nil) == want
	}
	if !exists || got == nil {
		return false
	}
	if p.all {
		for _, want := range p.values {
			if !matchEventValue(got, p, want) {
				return false
			}
		}
		return true
	}
	for _, want := range p.values {
		if matchEventValue(got, p, want) {
			return true
		}
	}
	return false
}

func lookup(event map[string]any, field string, path []string) (any, bool) {
	// Some log formats use dotted literal keys. Check the literal name first.
	if direct, ok := event[field]; ok {
		return direct, true
	}
	var current any = event
	for _, part := range path {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func matchEventValue(got any, p predicate, want value) bool {
	switch list := got.(type) {
	case []any:
		for _, item := range list {
			if matchScalar(item, p, want) {
				return true
			}
		}
		return false
	case []string:
		for _, item := range list {
			if matchScalar(item, p, want) {
				return true
			}
		}
		return false
	default:
		return matchScalar(got, p, want)
	}
}

func matchScalar(got any, p predicate, want value) bool {
	if p.op == "gt" || p.op == "gte" || p.op == "lt" || p.op == "lte" {
		left, okLeft := numericValue(got)
		right, okRight := numericValue(want.raw)
		if !okLeft || !okRight {
			return false
		}
		switch p.op {
		case "gt":
			return left > right
		case "gte":
			return left >= right
		case "lt":
			return left < right
		default:
			return left <= right
		}
	}
	actual, ok := scalarString(got)
	if !ok {
		return false
	}
	expected, ok := scalarString(want.raw)
	if !ok {
		return false
	}
	if p.op == "re" {
		return want.re.MatchString(actual)
	}
	if !p.cased {
		actual = strings.ToLower(actual)
		expected = strings.ToLower(expected)
	}
	switch p.op {
	case "equals":
		return actual == expected
	case "contains":
		return strings.Contains(actual, expected)
	case "startswith":
		return strings.HasPrefix(actual, expected)
	case "endswith":
		return strings.HasSuffix(actual, expected)
	default:
		return false
	}
}

func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case bool:
		return strconv.FormatBool(x), true
	default:
		return "", false
	}
}

func numericValue(v any) (float64, bool) {
	text, ok := scalarString(v)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || n != n || n > 1.7976931348623157e+308 || n < -1.7976931348623157e+308 {
		return 0, false
	}
	return n, true
}

func (s selection) match(event map[string]any) bool {
	for _, group := range s {
		matched := true
		for _, predicate := range group {
			if !predicate.match(event) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
