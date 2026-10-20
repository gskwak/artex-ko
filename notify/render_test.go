package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateBytesKeepsValidUTF8(t *testing.T) {
	// This is the package's key invariant. WeCom limits messages by byte count;
	// Korean syllables use three UTF-8 bytes each. Cutting at an arbitrary byte
	// can split a character and produce invalid UTF-8 that the platform rejects.
	// Exercise every possible cut point with mixed-width inputs.
	inputs := []string{
		"한글 테스트 내용",
		"혼합 mixed 내용 content",
		"a한b글c테d스e",
		"🔴🟠🟡🔵", // Four-byte emoji make a bad cut even more apparent.
		strings.Repeat("취약점", 100),
	}
	for _, in := range inputs {
		for max := 1; max <= len(in)+2; max++ {
			got := TruncateBytes(in, max)
			if !utf8.ValidString(got) {
				t.Fatalf("input %q max=%d: produced invalid UTF-8 %q", in, max, got)
			}
			if len(got) > max {
				t.Fatalf("input %q max=%d: result has %d bytes, exceeding the limit", in, max, len(got))
			}
			// Content must remain unchanged when no truncation is needed.
			if len(in) <= max && got != in {
				t.Fatalf("input %q max=%d: changed content below the limit -> %q", in, max, got)
			}
		}
	}
}

func TestTruncateBytesZeroMeansUnlimited(t *testing.T) {
	long := strings.Repeat("x", 10000)
	if got := TruncateBytes(long, 0); got != long {
		t.Fatal("max=0 should mean unlimited")
	}
	if got := TruncateBytes(long, -5); got != long {
		t.Fatal("max<0 should mean unlimited")
	}
}

func TestTruncateBytesEllipsisBudget(t *testing.T) {
	// Appending an ellipsis must not exceed max, even when max is shorter than the ellipsis.
	got := TruncateBytes("abcdefgh", 1)
	if len(got) > 1 {
		t.Fatalf("max=1: result %q exceeds the limit with length %d", got, len(got))
	}
	// A normally truncated result should include an ellipsis.
	if got := TruncateBytes("abcdefgh", 5); !strings.HasSuffix(got, ellipsis) {
		t.Fatalf("expected an ellipsis, got %q", got)
	}
}

func TestTruncateRunesCountsCharactersNotBytes(t *testing.T) {
	// Preserve the distinction from TruncateBytes: Telegram limits characters,
	// while a byte limit would leave only about a third as many Korean syllables.
	s := "가나다라마바사아자차"
	got := TruncateRunes(s, 5)
	if n := utf8.RuneCountInString(got); n != 5 {
		t.Fatalf("expected 5 characters, got %d (%q)", n, got)
	}
	// The same string should be much shorter under a byte limit.
	if utf8.RuneCountInString(TruncateBytes(s, 5)) >= 5 {
		t.Fatal("byte limit should not yield as many characters as the character limit")
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	got := OneLine("first line\n\nsecond line\twith tab   extra spaces", 0)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("expected all whitespace to collapse, got %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("expected no consecutive spaces, got %q", got)
	}
	// Truncation must still produce readable, valid text.
	got = OneLine("가나다라마바사아자차", 4)
	if n := utf8.RuneCountInString(got); n != 4 {
		t.Fatalf("expected 4 characters, got %d (%q)", n, got)
	}
}

func TestTruncateHTMLNeverCutsTagInHalf(t *testing.T) {
	// Truncating raw HTML can leave a fragment like `<a href="htt`, causing
	// the platform to reject the entire message.
	s := `<b>Title 제목</b>Body 본문본문<a href="https://example.com/very/long/path">View details</a>`
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		if n := utf8.RuneCountInString(got); max > 0 && n > max {
			t.Fatalf("max=%d: result has %d characters, exceeding the limit", max, n)
		}
		// The tail must not contain an opening `<` without a matching `>`.
		if lt := strings.LastIndex(got, "<"); lt >= 0 && !strings.Contains(got[lt:], ">") {
			t.Fatalf("max=%d: trailing tag was cut off -> %q", max, got)
		}
	}
}

func TestAssetLineOmitsExcess(t *testing.T) {
	if got := assetLine(nil, 3); got != "" {
		t.Fatalf("expected an empty string for no assets, got %q", got)
	}
	if got := assetLine([]string{"a", "b"}, 3); got != "a, b" {
		t.Fatalf("expected all assets below the limit, got %q", got)
	}
	// Include the total when the list is truncated so readers know how many assets were omitted.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	if !strings.Contains(got, "등 5개") {
		t.Fatalf("expected total count 5, got %q", got)
	}
}

func TestSeverityAndStatusLabels(t *testing.T) {
	if AtLeast("", "low") {
		t.Fatal("empty severity has rank 0 and should fail every threshold")
	}
	if !AtLeast("critical", "") {
		t.Fatal("empty threshold should accept any severity")
	}
	if got := StatusLabel("fixed"); got != "수정됨" {
		t.Fatalf("unexpected label for fixed status: %q", got)
	}
	// Unknown statuses should be returned unchanged, without inventing a label.
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Fatalf("unknown status should be returned unchanged, got %q", got)
	}
}

// TestTruncateHTMLNeverCutsEntity covers an audit finding: truncation must
// avoid splitting HTML entities as well as tags.
//
// If `&amp;` becomes `&amp`, an entity-aware parser may reject the entire
// message. Long summary messages are common enough for this to matter.
func TestTruncateHTMLNeverCutsEntity(t *testing.T) {
	s := "aaaa&amp;bbbb&lt;cccc&quot;dddd"
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		// The tail must not contain an `&` without its matching `;`.
		if amp := strings.LastIndex(got, "&"); amp >= 0 && !strings.Contains(got[amp:], ";") {
			t.Fatalf("max=%d: trailing entity fragment %q", max, got[amp:])
		}
		if strings.Contains(got, "&amp\x00") {
			t.Fatalf("max=%d: malformed entity", max)
		}
	}
}
