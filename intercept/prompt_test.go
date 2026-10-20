package intercept

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVerdict(t *testing.T) {
	for _, action := range []string{"allow", "ask", "deny"} {
		t.Run(action, func(t *testing.T) {
			reason := "Action: Write a report containing ALLOW, DENY, and ASK; Consequence if successful: Save text without running embedded commands; Rule: custom clause"
			raw, _ := json.Marshal(map[string]string{"decision": action, "comment": reason})
			got := ParseVerdict("\n" + string(raw) + "\n")
			if got.Action != action || got.Reason != reason {
				t.Fatalf("lost verdict or explanation: %+v", got)
			}
		})
	}
}

func TestParseVerdictAcceptsLegacyChineseLabels(t *testing.T) {
	reason := "实际操作：读取文件；成功后的后果：返回内容；命中规则：A5"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	got := ParseVerdict(string(raw))
	if got.Action != "allow" || got.Reason != reason {
		t.Fatalf("legacy verdict was not preserved: %+v", got)
	}
}

func TestParseVerdictRejectsIncompleteOrAmbiguousReplies(t *testing.T) {
	valid := `{"decision":"allow","comment":"Action: Read a file; Consequence if successful: Return its contents; Rule: A5"}`
	for _, reply := range []string{
		"", "ALLOW", "DENY:rule D4", "approved:ALLOW", "ASK:ownership unknown",
		`{"decision":"allow"}`, `{"decision":"approve","comment":"Action: Read; Consequence if successful: Return contents; Rule: A5"}`,
		`{"decision":"allow","comment":null}`, `{"decision":"allow","comment":123}`,
		strings.Replace(valid, "Action: Read a file", "Action: ", 1),
		strings.Replace(valid, "Consequence if successful: Return its contents", "Consequence if successful: ", 1),
		strings.Replace(valid, "Rule: A5", "Rule: ", 1),
		strings.Replace(valid, "; Rule: A5", "", 1),
		strings.Replace(valid, `"decision":"allow"`, `"decision":"deny","decision":"allow"`, 1),
		strings.Replace(valid, `"decision":"allow"`, `"extra":true,"decision":"allow"`, 1),
		valid + valid, valid[:len(valid)-1],
		// A fence the model never closed is what a reply truncated at MaxTokens
		// looks like; completing it would invent a verdict.
		"```json\n" + valid[:len(valid)-1],
		"```json\n" + valid + "\n```\nI also recommend human review.",
		"My decision is:\n" + valid,
	} {
		if got := ParseVerdict(reply); got.Action != "" {
			t.Errorf("accepted incomplete/ambiguous verdict: %q => %+v", reply, got)
		}
	}
}

// Wrapping JSON in markdown is the one deviation models make routinely. Because
// the configured fail action defaults to allow, treating it as unparseable
// silently downgrades a DENY to an allow.
func TestParseVerdictUnwrapsCodeFence(t *testing.T) {
	deny := `{"decision":"deny","comment":"Action: Delete a production file; Consequence if successful: Lose business data; Rule: D4"}`
	for _, reply := range []string{
		"```json\n" + deny + "\n```",
		"```JSON\n" + deny + "\n```",
		"```\n" + deny + "\n```",
		"  ```json\n" + deny + "\n```  ",
	} {
		got := ParseVerdict(reply)
		if got.Action != "deny" || !strings.HasSuffix(got.Reason, "Rule: D4") {
			t.Errorf("fenced verdict lost: %q => %+v", reply, got)
		}
	}
}

func TestParseVerdictKeepsCompleteEnglishExplanation(t *testing.T) {
	reason := "Action: " + strings.Repeat("Write a report ", 30) + "; Consequence if successful: Save only a file; Rule: A2"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	if got := ParseVerdict(string(raw)); got.Reason != reason {
		t.Fatal("explanation was truncated or lost its rule")
	}
	raw, _ = json.Marshal(map[string]string{"decision": "allow", "comment": strings.Repeat("x", 2401)})
	if got := ParseVerdict(string(raw)); got.Action != "" {
		t.Fatal("accepted unbounded explanation")
	}
}
