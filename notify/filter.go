package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter defines the notification_channels.filter JSONB contract for a channel's filters.
// Every field is optional; the default is no filtering, which is also the fallback for
// malformed configuration (see ParseFilter).
type Filter struct {
	// MinSeverity is the minimum severity (low/medium/high/critical); empty means no threshold.
	MinSeverity string `json:"min_severity"`
	// Empty TaskIDs or AssetIDs impose no restriction; otherwise they must intersect the event.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// Empty VulnClassInclude accepts all classes; otherwise a keyword must match vulnclass.
	// Any match in VulnClassExclude rejects it (exclusion takes precedence).
	// Matching uses case-insensitive substrings. Unlike regexes, a bad pattern cannot silently disable a channel.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange enables finding status-change events (relevant only in realtime mode).
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses a channel's filter configuration.
//
// It never returns an error. Malformed configuration falls back to a zero-value Filter,
// which matches everything. For a vulnerability notification system, an extra message
// is preferable to silently dropping a high-severity finding. Treating parse failure
// as "send nothing" would leave a seemingly configured channel that sends nothing.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure, f stays at its zero value and does not filter.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid severity threshold (empty means none).
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks fields with restricted values when saving a channel.
//
// Match treats an unknown threshold as rank >= 0, which is always true. A typo such as
// "hgih" would silently turn the filter into "send everything". Although this avoids
// missed findings, users could unknowingly flood a channel they thought was filtered.
// Reject this silent degradation at the write boundary.
//
// Validate applies only on writes. Reads keep ParseFilter's tolerant behavior so
// existing invalid data does not make the whole channel unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("올바르지 않은 최소 심각도입니다: %q. low / medium / high / critical 중에서 선택하거나, 비워 두면 제한하지 않습니다", f.MinSeverity)
	}
	return nil
}

// Match decides whether an event should be delivered to a channel with this filter.
//
// It never returns an error, for the same reason as ParseFilter: internal anomalies
// favor a match. Checks run in order: event type, severity, task/asset scope, class keywords.
func Match(f Filter, s Snapshot) bool {
	// Status changes require explicit opt-in. By default, users expect notifications
	// for new findings rather than a running log of every status transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclusion takes precedence even when an inclusion keyword also matches.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// Both sets contain at most dozens of manually selected IDs; a linear scan is
	// cheaper than building a map.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any keyword, ignoring case.
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
