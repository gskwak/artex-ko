package notify

// Snapshot defines the notification_events.snapshot JSONB contract. The db layer's
// finding transaction writes it; the server's delivery engine and filters read it.
// It belongs here because it is a notification payload. The db layer serializes it
// without interpreting the fields.
//
// Store finding fields redundantly rather than looking them up while rendering:
// names, severities, and statuses may change later, but a notification must show
// the conclusion **at the time of the event**. A later lookup could misleadingly
// show a severity since changed to low. Fan-out and rendering also avoid joining
// findings, tasks, and assets.
type Snapshot struct {
	// Event kind: finding_created / finding_status_changed.
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Nonempty only when kind=finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is a finding awaiting push notification, for channel rendering.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets contains resolved display names (such as domains or IPs). The server
	// fills them in because this package does not access the database.
	Assets []string
	// DetailURL links back to finding details; omit it when public_base_url is unset.
	DetailURL string
	// Status-change events use these fields; if both are set, render a transition
	// such as "Pending → Fixed".
	FromStatus string
	ToStatus   string
}

// IsStatusChange reports whether this item represents a status-change event.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title returns the item's display title, preferring a human-assigned name, then
// the vulnerability class. It uses a placeholder if both are empty.
func (i Item) Title() string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return "(이름 없는 취약점)"
}

// Message contains the complete content of one channel send.
type Message struct {
	// Items contains one finding for an individual push or an entire digest batch.
	// An empty slice is invalid; callers must provide at least one item.
	Items []Item
	// Batch=true renders a digest with a different title, time window, and count.
	Batch bool
	// WindowMinutes is the digest window, used only when Batch=true for "last N
	// minutes" text. Passing the configured value instead of computing time.Since
	// keeps rendering deterministic and easy to test.
	WindowMinutes int
	// HomeURL is the platform dashboard (global public_base_url); omit the dashboard
	// link when it is empty.
	HomeURL string
}
