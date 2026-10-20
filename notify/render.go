package notify

import (
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes truncates s to at most max bytes without splitting a UTF-8 character.
//
// Why character boundaries matter: WeCom group robot Markdown has a hard limit of
// 4096 **bytes**, not characters, and a Chinese character takes three bytes. Slicing
// at an arbitrary byte can produce invalid UTF-8, causing the platform to reject
// the message or show replacement characters. Backtrack from the byte limit to the
// nearest rune start (utf8.RuneStart distinguishes continuation bytes 0b10xxxxxx).
//
// max<=0 means unlimited. Append an ellipsis unless max is too small for one.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// If max is shorter than the ellipsis, omit it so the result fits.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine collapses all whitespace into a single line, then truncates by rune count.
// IM message titles use it because line breaks in summaries can disrupt the layout.
// max<=0 means unlimited length.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes truncates s to at most max runes (not bytes), adding an ellipsis.
// max<=0 means unlimited.
//
// The platform limits differ: WeCom counts bytes, while Telegram counts characters.
// Using the wrong unit silently truncates messages too early (4096 bytes hold only
// about 1365 Chinese characters), so choose the appropriate function per channel.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML truncates an HTML fragment by rune count without leaving a partial tag.
//
// Directly truncating HTML can leave a partial tag such as `<a href="htt`. The
// platform parser may reject the entire message or consume following text as an
// attribute value. After truncating, remove any suffix that starts with an
// unclosed `<`.
//
// Do not balance tags (for example, by adding </b>): Telegram's HTML parser closes
// unclosed tags, while balancing them here would require parsing quoted attributes,
// comments, and self-closing tags for little benefit.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// Remove a trailing tag fragment: a final `<` with no subsequent `>`.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// Likewise, remove a partial HTML entity, such as `&amp` cut from `&amp;`.
	// A strict parser may reject the **entire message** over a partial entity.
	// Long digest messages are common, so avoid losing a whole notification.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount counts how many **complete** items fit within the digest budget.
//
// Packing complete items matters because truncating a fully rendered digest would
// silently drop later findings even though their delivery records said "delivered."
// Items that do not fit remain in the database for the next batch, and the caller's
// kept count reflects how many items this message actually delivered.
//
// Parameters: maxSize<=0 means unlimited; reserve budgets for the header/footer;
// size measures using the platform's unit (bytes for WeCom/DingTalk, characters
// for Telegram); render produces the actual text for the item at idx, whose
// length depends on its contents and cannot be estimated reliably.
//
// Return at least one when items remain. The caller's final truncation handles an
// unusually long item, preventing it from blocking the entire batch indefinitely.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize and runeSize make packItemCount's two units explicit at call sites.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine displays assets on one line, omitting those past limit and showing
// the total count. A finding may refer to dozens of assets that would crowd out
// the rest of the message.
func assetLine(assets []string, limit int) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return strings.Join(assets[:limit], ", ") + " 등 " + itoa(len(assets)) + "개"
}

// itoa is a small strconv.Itoa substitute for building display text without an import.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
