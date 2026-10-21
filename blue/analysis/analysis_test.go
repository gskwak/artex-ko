package analysis

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

const validReply = `{"summary":"One login sequence needs investigation.","verdict":"inconclusive","confidence":"medium","severity":"unknown","facts":[{"text":"A failed login was recorded.","evidence":[{"kind":"event","id":11}]}],"hypotheses":[{"text":"The alert may indicate credential abuse.","evidence":[{"kind":"alert","id":22}]}],"unknowns":["No endpoint telemetry was supplied."],"detection_ideas":[{"text":"Review repeated failures per account.","evidence":[{"kind":"event","id":11}]}],"response_recommendations":[{"text":"Ask the owner to review the account.","evidence":[{"kind":"alert","id":22}]}]}`

const taskReply = `{"summary":"A login failure was recorded.","verdict":"inconclusive","confidence":"medium","severity":"unknown","facts":[{"text":"A failed login was recorded.","evidence":[{"kind":"event","id":11}]}],"hypotheses":[],"unknowns":["The sign-in origin was not supplied."],"detection_ideas":[],"response_recommendations":[],"task_outcome":{"status":"answered","summary":"The requested authentication review found one failed login; the origin is unknown.","evidence":[{"kind":"event","id":11}]}}`

func fixture() Input {
	return Input{
		CaseID: 1, Title: "Suspicious sign-in", Status: "open", Severity: "medium",
		Events: []Event{{ID: 11, Source: "auth", Message: "login failed", OccurredAt: time.Now(), Raw: json.RawMessage(`{"message":"login failed"}`)}},
		Alerts: []Alert{{ID: 22, EventID: 11, Title: "Failed login", Severity: "medium"}},
	}
}

func TestParseRequiresVerifiedEvidence(t *testing.T) {
	a, err := Parse(validReply, fixture())
	if err != nil {
		t.Fatal(err)
	}
	if a.Verdict != "inconclusive" || !strings.Contains(a.Markdown(), "[event #11]") {
		t.Fatalf("unexpected assessment: %+v", a)
	}
	events, alerts := a.EvidenceIDs()
	if len(events) != 1 || events[0] != 11 || len(alerts) != 1 || alerts[0] != 22 {
		t.Fatalf("wrong evidence refs: events=%v alerts=%v", events, alerts)
	}
	if _, err := Parse(strings.Replace(validReply, `"id":11`, `"id":999`, 1), fixture()); err == nil {
		t.Fatal("accepted a citation outside this case")
	}
	if _, err := Parse(strings.Replace(validReply, `"evidence":[{"kind":"event","id":11}]`, `"evidence":[]`, 1), fixture()); err == nil {
		t.Fatal("accepted an uncited fact")
	}
}

func TestParseRejectsExtraContent(t *testing.T) {
	if _, err := Parse(validReply+` {}`, fixture()); err == nil {
		t.Fatal("accepted multiple JSON documents")
	}
	if _, err := Parse("```json\n"+validReply+"\n```", fixture()); err != nil {
		t.Fatalf("rejected JSON fence: %v", err)
	}
}

func TestParseRejectsUnsupportedVerdict(t *testing.T) {
	for _, verdict := range []string{"malicious", "benign"} {
		severity := "unknown"
		if verdict == "benign" {
			severity = "none"
		}
		text := `{"summary":"No support","verdict":"` + verdict + `","confidence":"high","severity":"` + severity + `","facts":[],"hypotheses":[],"unknowns":["Need more logs"],"detection_ideas":[],"response_recommendations":[]}`
		if _, err := Parse(text, fixture()); err == nil {
			t.Fatalf("accepted %s verdict without cited facts", verdict)
		}
	}
	text := `{"summary":"Need more logs","verdict":"inconclusive","confidence":"low","severity":"unknown","facts":[],"hypotheses":[{"text":"Could be benign","evidence":[{"kind":"event","id":11}]}],"unknowns":["Need more logs"],"detection_ideas":[],"response_recommendations":[]}`
	if _, err := Parse(text, fixture()); err != nil {
		t.Fatalf("rejected uncertain but cited analysis: %v", err)
	}
}

func TestParseCalibratesAssessedSeverity(t *testing.T) {
	for _, fields := range []struct {
		verdict, confidence, severity string
		valid                         bool
	}{
		{"inconclusive", "high", "high", false},
		{"benign", "high", "high", false},
		{"malicious", "medium", "critical", false},
		{"malicious", "high", "critical", true},
	} {
		text := `{"summary":"An event was recorded","verdict":"` + fields.verdict + `","confidence":"` + fields.confidence + `","severity":"` + fields.severity + `","facts":[{"text":"An event was recorded","evidence":[{"kind":"event","id":11}]}],"hypotheses":[],"unknowns":[],"detection_ideas":[],"response_recommendations":[]}`
		_, err := Parse(text, fixture())
		if (err == nil) != fields.valid {
			t.Errorf("verdict=%s confidence=%s severity=%s: error=%v", fields.verdict, fields.confidence, fields.severity, err)
		}
	}
}

type captureProvider struct {
	request llm.CompletionRequest
	output  string
}

func (p *captureProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (p *captureProvider) Complete(_ context.Context, request llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.request = request
	acc := llm.NewAccumulator()
	acc.Add(llm.StreamEvent{Type: llm.SETextDelta, Text: p.output})
	return acc.Message(), "end_turn", llm.Usage{}, nil
}

func TestAnalyzeHasNoToolsAndBoundsUntrustedEvidence(t *testing.T) {
	in := fixture()
	in.Events[0].Raw = json.RawMessage(`{"payload":"` + strings.Repeat("untrusted", 1000) + `"}`)
	p := &captureProvider{output: validReply}
	if _, err := Analyze(t.Context(), p, in, 8192); err != nil {
		t.Fatal(err)
	}
	if len(p.request.Tools) != 0 {
		t.Fatalf("analyst received tools: %+v", p.request.Tools)
	}
	if p.request.MaxTokens != 4096 || len(p.request.System) != 2 || len(p.request.Messages) != 1 {
		t.Fatalf("wrong bounded model request: %+v", p.request)
	}
	user := p.request.Messages[0].Content[0].Text
	if strings.Contains(user, strings.Repeat("untrusted", 300)) || !strings.Contains(user, "[truncated]") {
		t.Fatal("raw telemetry was not bounded")
	}
}

func TestAnalyzeBoundsTotalEvidenceAndMarksPartialReview(t *testing.T) {
	in := fixture()
	for id := int64(12); id < 51; id++ {
		in.Events = append(in.Events, Event{ID: id, Message: strings.Repeat("x", 3000), Raw: json.RawMessage(`{"data":"` + strings.Repeat("y", 3000) + `"}`)})
	}
	p := &captureProvider{output: validReply}
	a, err := Analyze(t.Context(), p, in, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !a.EvidenceTruncated || !strings.Contains(a.Markdown(), "only a subset") {
		t.Fatal("partial evidence review was not disclosed")
	}
	if n := len(p.request.Messages[0].Content[0].Text); n > maxInputBytes {
		t.Fatalf("evidence payload exceeded bound: %d bytes", n)
	}
}

func TestAnalyzeNeedsEvidence(t *testing.T) {
	p := &captureProvider{output: validReply}
	if _, err := Analyze(t.Context(), p, Input{CaseID: 1}, 0); err == nil {
		t.Fatal("empty case should not call model")
	}
}

func TestTaskAnalysisCarriesBoundedFocusAndRequiresOutcome(t *testing.T) {
	in := fixture()
	in.Task = &Task{ID: 7, Title: "Review authentication evidence", Description: strings.Repeat("context ", 1000), Priority: "critical"}
	p := &captureProvider{output: taskReply}
	a, err := Analyze(t.Context(), p, in, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.TaskOutcome == nil || a.TaskOutcome.Status != "answered" || !strings.Contains(a.Markdown(), "requested authentication review") {
		t.Fatalf("task-specific outcome was not returned: %+v", a.TaskOutcome)
	}
	if len(p.request.System) != 3 || p.request.System[2] != TaskRuntimeContract || len(p.request.Tools) != 0 {
		t.Fatalf("task analysis missed the read-only contract: %+v", p.request)
	}
	var supplied Input
	if err := json.Unmarshal([]byte(p.request.Messages[0].Content[0].Text), &supplied); err != nil {
		t.Fatal(err)
	}
	if supplied.Task == nil || supplied.Task.ID != 7 || supplied.Task.Title != in.Task.Title || supplied.Task.Priority != "critical" ||
		len([]rune(supplied.Task.Description)) > maxFieldRunes+20 {
		t.Fatalf("task focus or bounds were lost: %+v", supplied.Task)
	}
	p.output = validReply
	if _, err := Analyze(t.Context(), p, in, 0); err == nil || !strings.Contains(err.Error(), "task outcome") {
		t.Fatalf("accepted a generic case response for a task: %v", err)
	}
}

func TestTaskOutcomeMustBeExplicitAndCited(t *testing.T) {
	in := fixture()
	in.Task = &Task{ID: 7, Title: "Review authentication evidence"}
	for _, reply := range []string{
		strings.Replace(taskReply, `"status":"answered"`, `"status":"done"`, 1),
		strings.Replace(taskReply, `"evidence":[{"kind":"event","id":11}]}}`, `"evidence":[]}}`, 1),
		strings.Replace(taskReply, `"evidence":[{"kind":"event","id":11}]}}`, `"evidence":[{"kind":"event","id":999}]}}`, 1),
	} {
		if _, err := Parse(reply, in); err == nil {
			t.Fatalf("accepted invalid task outcome: %s", reply)
		}
	}
	insufficient := strings.Replace(taskReply,
		`"status":"answered","summary":"The requested authentication review found one failed login; the origin is unknown."`,
		`"status":"insufficient_evidence","summary":"The source IP was not included in the supplied records."`, 1)
	if _, err := Parse(insufficient, in); err != nil {
		t.Fatalf("rejected explicit inability to answer: %v", err)
	}
	if _, err := Parse(taskReply, fixture()); err == nil {
		t.Fatal("accepted a task outcome in a generic case analysis")
	}
}
