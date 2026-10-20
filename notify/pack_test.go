package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// These tests cover packing whole items: when a summary exceeds a channel's
// length limit, report the number of omitted items so callers mark only those
// actually delivered.
//
// Previously, rendering and then truncating the full message marked the
// whole batch delivered, silently dropping findings from the end.

func TestMarkdownBodyPacksWholeItemsWithinByteLimit(t *testing.T) {
	// Two hundred findings exceed WeCom's 4096-byte limit.
	m := batchMsg(200)
	body, kept := markdownBody(m, weComMarkdownLimit)

	if len(body) > weComMarkdownLimit {
		t.Fatalf("body of %d bytes exceeds limit %d", len(body), weComMarkdownLimit)
	}
	if !utf8.ValidString(body) {
		t.Fatal("body is not valid UTF-8")
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("expected a partial batch (0 < kept < %d), got %d", len(m.Items), kept)
	}
	// The header must state how many items this message includes and omits.
	if !strings.Contains(body, "나머지") || !strings.Contains(body, "다음 메시지에서") {
		t.Fatalf("header should state how many items were omitted:\n%s", body[:minInt(400, len(body))])
	}
	// Only the first kept items should appear.
	for i := 0; i < kept; i++ {
		if !strings.Contains(body, "Finding"+itoa(i+1)) {
			t.Fatalf("item %d should appear in this message:\n%s", i+1, body)
		}
	}
	if strings.Contains(body, "Finding"+itoa(kept+1)) {
		t.Fatalf("item %d should not appear (it belongs in the next batch)", kept+1)
	}
}

func TestMarkdownBodyKeepsEverythingWhenUnderLimit(t *testing.T) {
	m := batchMsg(3)
	body, kept := markdownBody(m, 0) // 0 means unlimited.
	if kept != len(m.Items) {
		t.Fatalf("unlimited length should keep all items, got kept=%d", kept)
	}
	if strings.Contains(body, "나머지") {
		t.Fatalf("untruncated body should not include an omitted-items notice:\n%s", body)
	}
}

func TestMarkdownBodyAlwaysKeepsAtLeastOneItem(t *testing.T) {
	// Even when the budget cannot hold one item, emit one and let final
	// truncation handle it; otherwise a long finding could block the batch forever.
	m := batchMsg(5)
	_, kept := markdownBody(m, 50)
	if kept != 1 {
		t.Fatalf("expected at least one item, got %d", kept)
	}
}

func TestMarkdownBodySingleReturnsOne(t *testing.T) {
	_, kept := markdownBody(singleMsg(), 4096)
	if kept != 1 {
		t.Fatalf("single-item message should report one delivered item, got %d", kept)
	}
	// An empty message has no deliverable items.
	if _, k := markdownBody(Message{}, 4096); k != 0 {
		t.Fatalf("empty message should report zero items, got %d", k)
	}
}

func TestTelegramPackingUsesRuneBudget(t *testing.T) {
	m := batchMsg(200)
	text, kept := telegramHTML(m)
	// Telegram limits characters; counting bytes would reduce capacity for multibyte text.
	if n := utf8.RuneCountInString(text); n > telegramTextLimit {
		t.Fatalf("body of %d characters exceeds limit %d", n, telegramTextLimit)
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("expected a partial batch, got %d", kept)
	}
	if !strings.Contains(text, "다음 메시지") {
		t.Fatalf("expected an omitted-items notice:\n%.300s", text)
	}
}

func TestFeishuPackingReportsKept(t *testing.T) {
	m := batchMsg(2000)
	_, kept := feishuCard(m)
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("card should contain a partial batch, got %d", kept)
	}
}

func TestWebhookAndEmailReportAllItems(t *testing.T) {
	// These two channels do not truncate the body, so the full batch is delivered.
	m := batchMsg(7)
	if n := len(m.Items); n != 7 {
		t.Fatal("test precondition failed")
	}
	// Verify indirectly through the renderer that markdownBody(0) keeps all items.
	if _, k := markdownBody(m, 0); k != len(m.Items) {
		t.Fatalf("unlimited length should keep all items, got %d", k)
	}
}

// TestMarkdownEscapesUntrustedContent ensures untrusted content cannot change
// message structure. Names and summaries come from model output, and asset
// names come from target URLs.
func TestMarkdownEscapesUntrustedContent(t *testing.T) {
	cases := []struct {
		name  string
		item  Item
		must  []string // Escaped forms that must appear.
		wrong []string // Unescaped forms that must not appear.
	}{
		{
			name: "newline and external link in title",
			item: Item{
				Severity: "high",
				Name:     "Login SQL injection\n[Urgent: verify account](http://attacker.tld)",
			},
			// Collapse newlines to prevent forged list items or quotes, and
			// escape brackets and parentheses to prevent clickable links.
			must:  []string{`\[Urgent: verify account\]`, `\(http://attacker.tld\)`},
			wrong: []string{"\n[Urgent", "\n\n[Urgent"},
		},
		{
			name: "image beacon in title",
			item: Item{
				Severity: "high",
				Name:     "Finding ![](http://attacker.tld/beacon)",
			},
			must:  []string{`\!`, `\(http://attacker.tld/beacon\)`},
			wrong: []string{"![]("},
		},
		{
			name: "emphasis and quote in asset name",
			item: Item{
				Severity: "high",
				Name:     "Ordinary title",
				Assets:   []string{"a.com/*injection*>quote"},
			},
			must:  []string{`\*injection\*`, `\>`},
			wrong: []string{"*injection*"},
		},
		{
			name: "backticks and pipe in summary",
			item: Item{
				Severity: "high",
				Name:     "Title",
				Summary:  "`code` | table",
			},
			must:  []string{"\\`code\\`", `\|`},
			wrong: []string{"`code`"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Message{Items: []Item{tc.item}}
			// writeItem is the shared single-item path for three Markdown channels.
			var b strings.Builder
			writeItem(&b, tc.item, "", true)
			got := b.String()
			for _, want := range tc.must {
				if !strings.Contains(got, want) {
					t.Errorf("missing escaped form %q:\n%s", want, got)
				}
			}
			for _, bad := range tc.wrong {
				if strings.Contains(got, bad) {
					t.Errorf("found unescaped form %q, which can inject structure or a link:\n%s", bad, got)
				}
			}
			_ = m
		})
	}
}

// TestMarkdownEscapeBackslashFirst fixes the escape order: backslashes must
// be processed first to avoid double-escaping those added later.
func TestMarkdownEscapeBackslashFirst(t *testing.T) {
	if got := markdownEscape(`a\b*c`); got != `a\\b\*c` {
		t.Fatalf("wrong escape order, got %q", got)
	}
}

// TestTelegramTitleHasNoMarkdownEscapes prevents Markdown escapes from
// leaking into Telegram's HTML output. A previous shared title function
// made visible backslashes appear in Telegram messages.
func TestTelegramTitleHasNoMarkdownEscapes(t *testing.T) {
	m := Message{Items: []Item{{Severity: "high", Name: "alert(1) *priority*"}}}
	text, _ := telegramHTML(m)
	if strings.Contains(text, `\(`) || strings.Contains(text, `\*`) {
		t.Fatalf("Telegram body contains Markdown backslash escapes:\n%s", text)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
