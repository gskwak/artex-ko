package detection

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type condition interface {
	match(map[string]selection, map[string]any) bool
}

type selectionRef string

func (r selectionRef) match(selections map[string]selection, event map[string]any) bool {
	return selections[string(r)].match(event)
}

type andCondition struct{ left, right condition }

func (c andCondition) match(selections map[string]selection, event map[string]any) bool {
	return c.left.match(selections, event) && c.right.match(selections, event)
}

type orCondition struct{ left, right condition }

func (c orCondition) match(selections map[string]selection, event map[string]any) bool {
	return c.left.match(selections, event) || c.right.match(selections, event)
}

type notCondition struct{ inner condition }

func (c notCondition) match(selections map[string]selection, event map[string]any) bool {
	return !c.inner.match(selections, event)
}

type ofCondition struct {
	names []string
	min   int
}

func (c ofCondition) match(selections map[string]selection, event map[string]any) bool {
	count := 0
	for _, name := range c.names {
		if selections[name].match(event) {
			count++
			if count >= c.min {
				return true
			}
		}
	}
	return false
}

type conditionToken struct {
	text string
	pos  int
}

func tokenizeCondition(input string) ([]conditionToken, error) {
	tokens := make([]conditionToken, 0, 16)
	for i := 0; i < len(input); {
		r := rune(input[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}
		if input[i] == '(' || input[i] == ')' {
			tokens = append(tokens, conditionToken{text: input[i : i+1], pos: i})
			i++
		} else if isIdentifierChar(input[i]) || input[i] == '*' {
			start := i
			for i < len(input) && (isIdentifierChar(input[i]) || input[i] == '*') {
				i++
			}
			tokens = append(tokens, conditionToken{text: input[start:i], pos: start})
		} else {
			return nil, fmt.Errorf("invalid character at byte %d", i)
		}
		if len(tokens) > 128 {
			return nil, fmt.Errorf("condition has more than 128 tokens")
		}
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty condition")
	}
	return tokens, nil
}

func isIdentifierChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

type conditionParser struct {
	tokens     []conditionToken
	position   int
	depth      int
	selections map[string]selection
}

func parseCondition(input string, selections map[string]selection) (condition, error) {
	tokens, err := tokenizeCondition(input)
	if err != nil {
		return nil, err
	}
	p := &conditionParser{tokens: tokens, selections: selections}
	condition, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.position != len(tokens) {
		return nil, fmt.Errorf("unexpected token %q at byte %d", tokens[p.position].text, tokens[p.position].pos)
	}
	return condition, nil
}

func (p *conditionParser) peek() string {
	if p.position >= len(p.tokens) {
		return ""
	}
	return strings.ToLower(p.tokens[p.position].text)
}

func (p *conditionParser) consume(text string) bool {
	if p.peek() == text {
		p.position++
		return true
	}
	return false
}

func (p *conditionParser) parseOr() (condition, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.consume("or") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orCondition{left: left, right: right}
	}
	return left, nil
}

func (p *conditionParser) parseAnd() (condition, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.consume("and") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = andCondition{left: left, right: right}
	}
	return left, nil
}

func (p *conditionParser) parseUnary() (condition, error) {
	if p.consume("not") {
		p.depth++
		if p.depth > 32 {
			return nil, fmt.Errorf("condition nesting exceeds 32")
		}
		inner, err := p.parseUnary()
		p.depth--
		if err != nil {
			return nil, err
		}
		return notCondition{inner: inner}, nil
	}
	return p.parsePrimary()
}

func (p *conditionParser) parsePrimary() (condition, error) {
	if p.consume("(") {
		p.depth++
		if p.depth > 32 {
			return nil, fmt.Errorf("condition nesting exceeds 32")
		}
		inside, err := p.parseOr()
		p.depth--
		if err != nil {
			return nil, err
		}
		if !p.consume(")") {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return inside, nil
	}
	if p.position >= len(p.tokens) {
		return nil, fmt.Errorf("condition ends unexpectedly")
	}
	word := p.tokens[p.position].text
	if word == ")" {
		return nil, fmt.Errorf("unexpected closing parenthesis")
	}
	if strings.EqualFold(word, "all") || strings.EqualFold(word, "any") || isPositiveInteger(word) {
		return p.parseOf()
	}
	if strings.Contains(word, "*") {
		return nil, fmt.Errorf("wildcard %q requires N/all of syntax", word)
	}
	if _, exists := p.selections[word]; !exists {
		return nil, fmt.Errorf("unknown selection %q", word)
	}
	p.position++
	return selectionRef(word), nil
}

func isPositiveInteger(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (p *conditionParser) parseOf() (condition, error) {
	quantifier := strings.ToLower(p.tokens[p.position].text)
	p.position++
	if !p.consume("of") {
		return nil, fmt.Errorf("expected 'of' after %q", quantifier)
	}
	if p.position >= len(p.tokens) {
		return nil, fmt.Errorf("expected selection pattern after 'of'")
	}
	pattern := p.tokens[p.position].text
	p.position++
	if pattern == "(" || pattern == ")" || strings.Contains(pattern[:len(pattern)-1], "*") {
		return nil, fmt.Errorf("invalid selection pattern %q", pattern)
	}
	var names []string
	if strings.EqualFold(pattern, "them") || pattern == "*" {
		for name := range p.selections {
			names = append(names, name)
		}
	} else if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		for name := range p.selections {
			if strings.HasPrefix(name, prefix) {
				names = append(names, name)
			}
		}
	} else if _, exists := p.selections[pattern]; exists {
		names = []string{pattern}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("selection pattern %q matches no selections", pattern)
	}
	sort.Strings(names)
	minimum := 1
	if quantifier == "all" {
		minimum = len(names)
	} else if quantifier != "any" {
		n, err := strconv.Atoi(quantifier)
		if err != nil || n < 1 || n > len(names) {
			return nil, fmt.Errorf("selection count %q must be between 1 and %d", quantifier, len(names))
		}
		minimum = n
	}
	return ofCondition{names: names, min: minimum}, nil
}
