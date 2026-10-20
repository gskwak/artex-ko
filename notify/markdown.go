package notify

import (
	"fmt"
	"strings"
)

// This file contains message rendering shared by Markdown channels (DingTalk and
// WeCom). Feishu renders card JSON; Telegram and email render HTML in their adapters.

// maxAssetsShown limits the assets listed in a message. A finding may involve
// dozens of assets; listing all of them would overwhelm an IM notification.
const maxAssetsShown = 3

// maxSummaryRunes limits the summary length. An IM message points readers to
// the details; the full report is available on the platform.
const maxSummaryRunes = 120

// markdownReservedBytes reserves space for the header (digest summary, severity
// counts, and any truncation notice) and footer (platform link). Excluding this
// space from the item budget preserves the batch context and remaining-item count.
const markdownReservedBytes = 320

// markdownEscape escapes Markdown metacharacters.
//
// Finding titles, summaries, types, and asset names all come from **untrusted
// sources**. Titles and summaries are model output based on a target's response;
// asset URLs include target-controlled query strings. Without escaping, a title like
//
//	Login SQL injection\n[Urgent: verify your account](http://attacker.tld)
//
// becomes a **clickable external link** in the engineer's DingTalk/Feishu message.
// An image such as `![](http://attacker.tld/beacon)` can cause the client to make
// a request on render, revealing that the finding was read and exposing the
// reader's IP. Even harmless injected bold text or blockquotes can push later
// critical findings out of view.
//
// Escape characters that create titles, links, emphasis, lists, quotes, and
// strikethrough. Handle `\` first to avoid escaping newly inserted backslashes.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"!", `\!`,
		"#", `\#`,
		">", `\>`,
		"|", `\|`,
		"~", `\~`,
	)
	return replacer.Replace(s)
}

// markdownText collapses untrusted text to one line and escapes it for Markdown.
// Newlines could forge list items or blockquotes even if metacharacters are escaped.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns the message title for an IM title bar or card title as
// **unescaped source text**.
//
// The title is shared by Markdown, Telegram HTML, Feishu plain_text cards,
// generic Webhook JSON, and email subjects. Each output has different escaping
// rules: Markdown escaping in HTML leaves visible backslashes, and in JSON it
// pollutes the value. Escape at each output boundary; see writeItem,
// feishuItemLines, and telegramEscape. Escaping here once caused visible
// backslashes such as `\(1\)` in Telegram messages.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf("취약점 요약 · 총 %d건", len(m.Items))
	}
	if len(m.Items) == 0 {
		return "취약점 알림"
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity), OneLine(it.Title(), 0))
}

// markdownBody renders a message body and returns the **actual item count**.
//
// The caller marks only the first kept items as delivered. Items excluded by the
// channel length limit must wait for the next batch. Otherwise a truncated message
// would be recorded as fully delivered even though later items were never sent.
//
// maxBytes<=0 means unlimited.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true)
		// Send even an oversized single item, relying on final truncation.
		// Some information about a finding is better than none.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf("\n[플랫폼에서 전체 보기](%s)\n", m.HomeURL)
	}
	kept := packItemCount(m.Items, maxBytes, markdownReservedBytes, footer, byteSize, func(it Item, idx int) string {
		var b strings.Builder
		writeItem(&b, it, fmt.Sprintf("%d. ", idx+1), false)
		return b.String()
	})

	items := m.Items[:kept]
	var b strings.Builder
	b.WriteString(markdownBatchIntro(m, items, len(m.Items)))
	for i, it := range items {
		writeItem(&b, it, fmt.Sprintf("%d. ", i+1), false)
	}
	b.WriteString(footer)
	return TruncateBytes(b.String(), maxBytes), kept
}

// markdownBatchIntro renders the digest's time window, count, and severity mix.
// These let readers assess urgency without opening the platform.
//
// items contains only those **actually packed**; total is the entire batch size.
// When they differ, state how many items will follow in the next message so the
// reader does not mistake this message for the complete batch.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, "**최근 %d분간 신규 취약점 %d건**", m.WindowMinutes, total)
	} else {
		fmt.Fprintf(&b, "**신규 취약점 %d건**", total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, "(이 메시지에는 앞 %d건만 표시하며, 나머지 %d건은 다음 메시지에서 이어서 보냅니다)", len(items), extra)
	}
	// Count only items **included in this message**, so the displayed severity
	// counts match the items below and reveal any critical findings at a glance.
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", SeverityLabel(sev), n))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	b.WriteString("\n\n")
	return b.String()
}

// writeItem renders one finding.
//
// prefix numbers items in a digest. With single=true, include the summary and
// detail link. Digest items get one line each to keep large batches concise.
//
// All external content (title/type/assets/summary) passes through markdownText
// for line collapsing and escaping. The detail URL comes from the administrator's
// public_base_url and must remain clickable, so it is emitted unchanged.
func writeItem(b *strings.Builder, it Item, prefix string, single bool) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if !single {
		// Digest mode: one line, followed by shortened assets and summary.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " — " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, "**상태 변경**: %s → %s\n",
			markdownText(StatusLabel(it.FromStatus), 0), markdownText(StatusLabel(it.ToStatus), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		fmt.Fprintf(b, "**유형**: %s\n", markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		fmt.Fprintf(b, "**자산**: %s\n", markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			fmt.Fprintf(b, "**개요**: %s\n", s)
		}
	}
	if it.DetailURL != "" {
		fmt.Fprintf(b, "[상세 보기](%s)\n", it.DetailURL)
	}
}
