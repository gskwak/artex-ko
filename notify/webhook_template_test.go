package notify

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// These tests lock down the capabilities of generic Webhook templates.
//
// This is the only place in the package where a user supplied string is evaluated
// as code. The tests make its allowed capabilities explicit, so a future method
// on the template context or a readFile function in FuncMap cannot silently
// expand them under the guise of a small, harmless change.

// TestTemplateContextHasNoMethods is the most important boundary check.
//
// text/template can call exported methods ({{.Foo}} can access either a field
// or a method). Any type with exported methods reachable from the template
// context therefore exposes those methods to the template author. The context
// deliberately contains only data: exported fields and no methods.
//
// If this fails, someone added a method to webhookTemplateData or webhookItem.
// Before allowing it, check whether a template could use it to read private data.
func TestTemplateContextHasNoMethods(t *testing.T) {
	for _, v := range []any{webhookTemplateData{}, webhookItem{}} {
		typ := reflect.TypeOf(v)
		if n := typ.NumMethod(); n != 0 {
			var names []string
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Fatalf("%s exposes %d methods (%s): text/template can call them, "+
				"which gives template authors access to those capabilities", typ.Name(), n, strings.Join(names, ", "))
		}
	}
}

// TestTemplateFuncsAreMinimal locks down the functions exposed to templates.
//
// Each additional FuncMap entry adds a capability. Currently only json and
// jsons are allowed; they serialize values as JSON fragments and cannot read
// files, make requests, or execute commands.
func TestTemplateFuncsAreMinimal(t *testing.T) {
	var got []string
	for name := range webhookTemplateFuncs {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"json", "jsons"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("template function set changed: got %v, want %v. Before adding a function, "+
			"confirm it does not expand the capabilities (no file access, network requests, or command execution)", got, want)
	}
}

// TestTemplateCannotReachUnknownData checks out of bounds template access:
// missing data must cause an error, and that error must not expose message data.
func TestTemplateCannotReachUnknownData(t *testing.T) {
	msg := singleMsg()
	_, err := renderWebhookBody(`{"x": {{.Environment}}, "y": {{.Env}}}`, msg)
	if err == nil {
		t.Fatal("accessing a missing field should return an error")
	}
	// Errors must not contain real message content (the finding class or summary).
	for _, leak := range []string{msg.Items[0].VulnClass, strings.SplitN(msg.Items[0].Summary, "\n", 2)[0]} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("template error leaked message content %q: %v", leak, err)
		}
	}
}

// TestTemplateRenderFailsPermanently treats a malformed template as a
// configuration error. Retries cannot fix it and would waste three backoff
// attempts on each delivery.
func TestTemplateRenderFailsPermanently(t *testing.T) {
	cfg := map[string]any{
		"url":           "https://example.com/hook",
		"body_template": `{{.Items.`,
	}
	if err := (webhookChannel{}).Validate(cfg); err == nil {
		t.Fatal("template syntax errors should be rejected when the configuration is saved")
	}
	// Even if validation is bypassed, delivery must fail permanently.
	_, err := (webhookChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("invalid template should fail permanently, got %v", err)
	}
}

// TestTemplateCanOnlyProduceJSON requires rendered templates to be valid JSON.
// This also prevents templates from producing plain text for another protocol.
func TestTemplateCanOnlyProduceJSON(t *testing.T) {
	// A valid template passes validation.
	ok := map[string]any{"url": "https://example.com/hook", "body_template": `{"t":{{json .Title}}}`}
	if err := (webhookChannel{}).Validate(ok); err != nil {
		t.Fatalf("valid template should pass validation: %v", err)
	}
	// Rendered non JSON content must be rejected instead of sent unchanged.
	bad := map[string]any{"url": "http://127.0.0.1:1/hook", "body_template": `not json {{.Count}}`}
	_, err := (webhookChannel{}).Send(context.Background(), bad, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("non JSON output should fail permanently, got %v", err)
	}
	if !strings.Contains(err.Error(), "올바른 JSON") {
		t.Errorf("error should identify the JSON issue, got %v", err)
	}
}
