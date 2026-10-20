// Package notify adapts IM and email push channels for finding notifications.
//
// This is a **leaf package** that depends only on the standard library. It knows
// neither the database nor the server. Channel configuration arrives as
// map[string]any (the notification_channels.config JSONB column), and content
// arrives as Message. This allows signature calculation, UTF-8 truncation, and
// filtering to be unit tested without PostgreSQL; the server orchestrates delivery.
//
// Concurrency contract: Channel implementations must be **stateless**. One Channel
// instance is reused concurrently across channel configurations and even multiple
// robots of the same type. Pass all credentials through cfg; do not cache webhook
// URLs or similar configuration in implementation fields.
package notify

// Channel kind identifiers also define the allowed notification_channels.kind
// values. The server validates the allowlist, as with findings.status, instead
// of using a DB CHECK so new channels can be added easily.
const (
	KindDingTalk = "dingtalk" // DingTalk custom robot
	KindFeishu   = "feishu"   // Feishu (including Lark) custom robot
	KindWeCom    = "wecom"    // WeCom group robot
	KindWebhook  = "webhook"  // Generic webhook: custom method, headers, and JSON template
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email
)

// Event kinds correspond to notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind is the fallback when config.kind is empty.
const InitKind = KindDingTalk

// severityRank maps finding severities to comparable ranks. Unknown values have
// rank 0, so any min_severity setting excludes them rather than sending uncertain
// notifications that could flood the channel.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns a severity's rank, or 0 for an unknown severity.
func SeverityRank(severity string) int { return severityRank[severity] }

// Lang, when wired by the server, returns the active user-facing output language
// ("en"/"ko"/"zh"/"es") for the strings this leaf package renders into push /
// email messages (severity and status labels). Kept as an injected func so the
// package stays standard-library-only: it does not import config/db/server. nil
// (unit tests / standalone) → labelLang falls back to "ko", preserving the Korean
// localization golden tests.
var Lang func() string

func labelLang() string {
	if Lang != nil {
		switch v := Lang(); v {
		case "en", "ko", "zh", "es":
			return v
		}
	}
	return "ko"
}

// severityLabels / statusLabels hold the per-language display strings. "ko" keeps
// the exact Korean values the localization golden tests pin.
var severityLabels = map[string]map[string]string{
	"ko": {"critical": "🔴 심각", "high": "🟠 높음", "medium": "🟡 중간", "low": "🔵 낮음"},
	"en": {"critical": "🔴 Critical", "high": "🟠 High", "medium": "🟡 Medium", "low": "🔵 Low"},
	"zh": {"critical": "🔴 严重", "high": "🟠 高危", "medium": "🟡 中危", "low": "🔵 低危"},
	"es": {"critical": "🔴 Crítico", "high": "🟠 Alto", "medium": "🟡 Medio", "low": "🔵 Bajo"},
}

var statusLabels = map[string]map[string]string{
	"ko": {"pending": "처리 대기", "in_progress": "처리 중", "confirmed": "확인됨", "resolved": "처리됨", "fixed": "수정됨", "false_positive": "오탐", "ignored": "무시", "duplicate": "중복", "risk_accepted": "위험 수용"},
	"en": {"pending": "Pending", "in_progress": "In progress", "confirmed": "Confirmed", "resolved": "Resolved", "fixed": "Fixed", "false_positive": "False positive", "ignored": "Ignored", "duplicate": "Duplicate", "risk_accepted": "Risk accepted"},
	"zh": {"pending": "待处理", "in_progress": "处理中", "confirmed": "已确认", "resolved": "已处理", "fixed": "已修复", "false_positive": "误报", "ignored": "忽略", "duplicate": "重复", "risk_accepted": "接受风险"},
	"es": {"pending": "Pendiente", "in_progress": "En curso", "confirmed": "Confirmado", "resolved": "Resuelto", "fixed": "Corregido", "false_positive": "Falso positivo", "ignored": "Ignorado", "duplicate": "Duplicado", "risk_accepted": "Riesgo aceptado"},
}

// SeverityLabel returns an emoji-prefixed severity in the active output language
// for message titles and card colors. Unknown severities are returned unchanged.
func SeverityLabel(severity string) string {
	if v, ok := severityLabels[labelLang()][severity]; ok {
		return v
	}
	return severity
}

// StatusLabel translates a disposition status for status-change messages in the
// active output language. Unknown statuses are returned unchanged.
func StatusLabel(status string) string {
	if v, ok := statusLabels[labelLang()][status]; ok {
		return v
	}
	return status
}

// AtLeast reports whether severity meets min. An empty min passes everything.
// Unknown severities have rank 0 and fail any nonempty min (see severityRank).
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}
