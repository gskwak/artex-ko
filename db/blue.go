package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Blue-team records are independent from red-team exploration nodes. All
// timestamps use UTC when supplied by callers; PostgreSQL stores timestamptz.
type BlueCase struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Status       string    `json:"status"`
	Severity     string    `json:"severity"`
	Summary      string    `json:"summary"`
	RedFindingID *int64    `json:"red_finding_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type BlueCasePatch struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	Severity    *string `json:"severity,omitempty"`
	Summary     *string `json:"summary,omitempty"`
}

// ErrBlueConflict indicates a valid record exists but its current state does
// not allow the requested transition.
var ErrBlueConflict = errors.New("blue-team state conflict")

// ErrBlueInvalidEvidence is returned when supplied citations cannot be used
// for the specified case. Callers may safely surface it as a client error.
var ErrBlueInvalidEvidence = errors.New("invalid blue-team evidence")

const blueEventImportLockKey int64 = 7337741003

type BlueEvent struct {
	ID         int64           `json:"id"`
	CaseID     *int64          `json:"case_id,omitempty"`
	Source     string          `json:"source"`
	ExternalID string          `json:"external_id,omitempty"`
	EventType  string          `json:"event_type"`
	Severity   string          `json:"severity"`
	Asset      string          `json:"asset"`
	Message    string          `json:"message"`
	OccurredAt time.Time       `json:"occurred_at"`
	Raw        json.RawMessage `json:"raw"`
	CreatedAt  time.Time       `json:"created_at"`
}

type BlueRule struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Field       string          `json:"field"`
	Operator    string          `json:"operator"`
	Value       string          `json:"value"`
	Definition  json.RawMessage `json:"definition"`
	Severity    string          `json:"severity"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type BlueRulePatch struct {
	Name        *string          `json:"name,omitempty"`
	Description *string          `json:"description,omitempty"`
	Field       *string          `json:"field,omitempty"`
	Operator    *string          `json:"operator,omitempty"`
	Value       *string          `json:"value,omitempty"`
	Definition  *json.RawMessage `json:"definition,omitempty"`
	Severity    *string          `json:"severity,omitempty"`
	Enabled     *bool            `json:"enabled,omitempty"`
}

type BlueAlert struct {
	ID        int64     `json:"id"`
	RuleID    int64     `json:"rule_id"`
	EventID   int64     `json:"event_id"`
	CaseID    *int64    `json:"case_id,omitempty"`
	Title     string    `json:"title"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Created   bool      `json:"-"` // transient result from UpsertBlueAlert; not persisted
}

type BlueAlertPatch struct {
	CaseID    *int64  `json:"case_id,omitempty"`
	ClearCase bool    `json:"-"`
	Status    *string `json:"status,omitempty"`
	Reason    *string `json:"reason,omitempty"`
}

type BlueObservation struct {
	ID               int64           `json:"id"`
	CaseID           int64           `json:"case_id"`
	EventID          *int64          `json:"event_id,omitempty"`
	AlertID          *int64          `json:"alert_id,omitempty"`
	EvidenceEventIDs []int64         `json:"evidence_event_ids"`
	EvidenceAlertIDs []int64         `json:"evidence_alert_ids"`
	Kind             string          `json:"kind"`
	Summary          string          `json:"summary"`
	Detail           string          `json:"detail"`
	Assessment       json.RawMessage `json:"assessment"`
	Author           string          `json:"author"`
	CreatedAt        time.Time       `json:"created_at"`
}

type BlueAction struct {
	ID                 int64           `json:"id"`
	CaseID             int64           `json:"case_id"`
	Kind               string          `json:"kind"`
	Target             string          `json:"target"`
	Rationale          string          `json:"rationale"`
	Params             json.RawMessage `json:"params"`
	Status             string          `json:"status"`
	DecisionNote       string          `json:"decision_note"`
	RequestedBy        string          `json:"requested_by"`
	DecidedBy          string          `json:"decided_by"`
	CompletionNote     string          `json:"completion_note"`
	CompletionEvidence string          `json:"completion_evidence"`
	CompletedBy        string          `json:"completed_by"`
	CreatedAt          time.Time       `json:"created_at"`
	DecidedAt          *time.Time      `json:"decided_at,omitempty"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
}

type BlueOverview struct {
	CaseCount          int64 `json:"case_count"`
	OpenCaseCount      int64 `json:"open_case_count"`
	EventCount         int64 `json:"event_count"`
	AlertCount         int64 `json:"alert_count"`
	NewAlertCount      int64 `json:"new_alert_count"`
	RuleCount          int64 `json:"rule_count"`
	PendingActionCount int64 `json:"pending_action_count"`
}

type BlueAnalysisRun struct {
	ID             int64      `json:"id"`
	CaseID         int64      `json:"case_id"`
	Status         string     `json:"status"`
	ModelProfileID *int64     `json:"model_profile_id,omitempty"`
	ObservationID  *int64     `json:"observation_id,omitempty"`
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type blueRow interface{ Scan(...any) error }

func blueLimit(n int) int {
	if n <= 0 {
		return 100
	}
	if n > 1000 {
		return 1000
	}
	return n
}

func blueOffset(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func blueOptionalString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func blueOptionalBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func blueJSON(raw json.RawMessage, empty string) (string, error) {
	if len(raw) == 0 {
		return empty, nil
	}
	if !json.Valid(raw) {
		return "", errors.New("invalid JSON")
	}
	return string(raw), nil
}

const blueCaseColumns = `id,title,description,status,severity,summary,red_finding_id,created_at,updated_at`

func scanBlueCase(row blueRow) (*BlueCase, error) {
	c := &BlueCase{}
	var redID sql.NullInt64
	if err := row.Scan(&c.ID, &c.Title, &c.Description, &c.Status, &c.Severity, &c.Summary, &redID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	if redID.Valid {
		c.RedFindingID = &redID.Int64
	}
	return c, nil
}

func (d *DB) CreateBlueCase(ctx context.Context, c BlueCase) (*BlueCase, error) {
	if c.Status == "" {
		c.Status = "open"
	}
	if c.Severity == "" {
		c.Severity = "medium"
	}
	if c.RedFindingID != nil {
		created, err := scanBlueCase(d.QueryRowContext(ctx, `INSERT INTO blue_cases
			(title,description,status,severity,summary,red_finding_id) VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (red_finding_id) WHERE red_finding_id IS NOT NULL DO NOTHING
			RETURNING `+blueCaseColumns, c.Title, c.Description, c.Status, c.Severity, c.Summary, c.RedFindingID))
		if !errors.Is(err, sql.ErrNoRows) {
			return created, err
		}
		// A replay returns the original case without rewriting its details or timestamp.
		return scanBlueCase(d.QueryRowContext(ctx, `SELECT `+blueCaseColumns+` FROM blue_cases WHERE red_finding_id=$1`, c.RedFindingID))
	}
	return scanBlueCase(d.QueryRowContext(ctx, `INSERT INTO blue_cases
		(title,description,status,severity,summary,red_finding_id) VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING `+blueCaseColumns, c.Title, c.Description, c.Status, c.Severity, c.Summary, c.RedFindingID))
}

func (d *DB) GetBlueCase(ctx context.Context, id int64) (*BlueCase, error) {
	c, err := scanBlueCase(d.QueryRowContext(ctx, `SELECT `+blueCaseColumns+` FROM blue_cases WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (d *DB) ListBlueCases(ctx context.Context, status string, limit, offset int) ([]BlueCase, error) {
	return d.SearchBlueCases(ctx, status, "", "", limit, offset)
}

// SearchBlueCases applies all filters before limiting the result, so an older
// matching case cannot disappear merely because it is outside the first page.
func (d *DB) SearchBlueCases(ctx context.Context, status, severity, query string, limit, offset int) ([]BlueCase, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueCaseColumns+` FROM blue_cases
		WHERE ($1='' OR status=$1) AND ($2='' OR severity=$2)
		AND ($3='' OR strpos(lower(id::text),lower($3))>0
			OR strpos(lower(title),lower($3))>0
			OR strpos(lower(description),lower($3))>0
			OR strpos(lower(summary),lower($3))>0)
		ORDER BY updated_at DESC,id DESC LIMIT $4 OFFSET $5`,
		status, severity, query, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueCase{}
	for rows.Next() {
		c, err := scanBlueCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (d *DB) PatchBlueCase(ctx context.Context, id int64, p BlueCasePatch) (*BlueCase, error) {
	return scanBlueCase(d.QueryRowContext(ctx, `UPDATE blue_cases SET
		title=COALESCE($2,title),description=COALESCE($3,description),status=COALESCE($4,status),
		severity=COALESCE($5,severity),summary=COALESCE($6,summary)
		WHERE id=$1 RETURNING `+blueCaseColumns,
		id, blueOptionalString(p.Title), blueOptionalString(p.Description), blueOptionalString(p.Status),
		blueOptionalString(p.Severity), blueOptionalString(p.Summary)))
}

const blueEventColumns = `id,case_id,source,COALESCE(external_id,''),event_type,severity,asset,message,occurred_at,raw,created_at`

func scanBlueEvent(row blueRow) (*BlueEvent, error) {
	e := &BlueEvent{}
	var caseID sql.NullInt64
	if err := row.Scan(&e.ID, &caseID, &e.Source, &e.ExternalID, &e.EventType, &e.Severity, &e.Asset, &e.Message, &e.OccurredAt, &e.Raw, &e.CreatedAt); err != nil {
		return nil, err
	}
	if caseID.Valid {
		e.CaseID = &caseID.Int64
	}
	return e, nil
}

// ImportBlueEvents is atomic for the batch. A source/external_id pair is an
// idempotency key; replaying it returns the original immutable event.
func (d *DB) ImportBlueEvents(ctx context.Context, events []BlueEvent) (_ []BlueEvent, err error) {
	if len(events) == 0 || len(events) > 1000 {
		return nil, fmt.Errorf("event batch must contain 1..1000 records")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serializing app imports makes event IDs commit in order. The detector's
	// high watermark takes the same lock, so a late commit cannot appear below
	// a watermark and be skipped by keyset pagination.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, blueEventImportLockKey); err != nil {
		return nil, err
	}
	out := make([]BlueEvent, 0, len(events))
	for _, e := range events {
		if e.Source == "" {
			return nil, errors.New("event source is required")
		}
		if e.Severity == "" {
			e.Severity = "info"
		}
		if e.OccurredAt.IsZero() {
			e.OccurredAt = time.Now().UTC()
		}
		raw, jerr := blueJSON(e.Raw, "{}")
		if jerr != nil {
			return nil, jerr
		}
		stored, qerr := scanBlueEvent(tx.QueryRowContext(ctx, `INSERT INTO blue_events
			(case_id,source,external_id,event_type,severity,asset,message,occurred_at,raw)
			VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9::jsonb)
			ON CONFLICT (source,external_id) WHERE external_id IS NOT NULL AND external_id <> ''
			DO NOTHING
			RETURNING `+blueEventColumns,
			e.CaseID, e.Source, e.ExternalID, e.EventType, e.Severity, e.Asset, e.Message, e.OccurredAt, raw))
		if errors.Is(qerr, sql.ErrNoRows) && e.ExternalID != "" {
			stored, qerr = scanBlueEvent(tx.QueryRowContext(ctx, `SELECT `+blueEventColumns+`
				FROM blue_events WHERE source=$1 AND external_id=$2`, e.Source, e.ExternalID))
		}
		if qerr != nil {
			return nil, qerr
		}
		out = append(out, *stored)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *DB) GetBlueEvent(ctx context.Context, id int64) (*BlueEvent, error) {
	e, err := scanBlueEvent(d.QueryRowContext(ctx, `SELECT `+blueEventColumns+` FROM blue_events WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

func (d *DB) ListBlueEvents(ctx context.Context, caseID *int64, source string, limit, offset int) ([]BlueEvent, error) {
	return d.SearchBlueEvents(ctx, caseID, source, "", "", limit, offset)
}

// SearchBlueEvents uses literal substring matches rather than LIKE patterns.
// Raw telemetry is included because many imported formats keep useful fields
// only in their original JSON payload.
func (d *DB) SearchBlueEvents(ctx context.Context, caseID *int64, source, severity, query string, limit, offset int) ([]BlueEvent, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueEventColumns+` FROM blue_events
		WHERE ($1::bigint IS NULL OR case_id=$1)
		AND ($2='' OR strpos(lower(source),lower($2))>0)
		AND ($3='' OR severity=$3)
		AND ($4='' OR strpos(lower(id::text),lower($4))>0
			OR strpos(lower(source),lower($4))>0
			OR strpos(lower(COALESCE(external_id,'')),lower($4))>0
			OR strpos(lower(event_type),lower($4))>0
			OR strpos(lower(asset),lower($4))>0
			OR strpos(lower(message),lower($4))>0
			OR strpos(lower(raw::text),lower($4))>0)
		ORDER BY occurred_at DESC,id DESC LIMIT $5 OFFSET $6`,
		caseID, source, severity, query, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueEvent{}
	for rows.Next() {
		e, err := scanBlueEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// BlueEventHighWatermark fixes the upper bound for a detection pass so newly
// imported events are handled by the next pass instead of shifting pagination.
func (d *DB) BlueEventHighWatermark(ctx context.Context) (int64, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, blueEventImportLockKey); err != nil {
		return 0, err
	}
	var maxID int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM blue_events`).Scan(&maxID); err != nil {
		return 0, err
	}
	return maxID, tx.Commit()
}

func (d *DB) ListBlueEventsAfterID(ctx context.Context, afterID, maxID int64, limit int) ([]BlueEvent, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueEventColumns+` FROM blue_events
		WHERE id>$1 AND id<=$2 ORDER BY id ASC LIMIT $3`, afterID, maxID, blueLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueEvent{}
	for rows.Next() {
		e, err := scanBlueEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ListBlueEventsBeforeID scans the newest events first for a manual detection
// pass. A positive beforeID resumes with older records; maxID bounds the
// initial snapshot so newer imports do not shift the active pass.
func (d *DB) ListBlueEventsBeforeID(ctx context.Context, beforeID, maxID int64, limit int) ([]BlueEvent, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueEventColumns+` FROM blue_events
		WHERE id<=$1 AND ($2::bigint=0 OR id<$2)
		ORDER BY id DESC LIMIT $3`, maxID, beforeID, blueLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueEvent{}
	for rows.Next() {
		e, err := scanBlueEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

const blueRuleColumns = `id,name,description,field,operator,value,definition,severity,enabled,created_at,updated_at`

func scanBlueRule(row blueRow) (*BlueRule, error) {
	r := &BlueRule{}
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Field, &r.Operator, &r.Value, &r.Definition,
		&r.Severity, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return r, nil
}

func (d *DB) CreateBlueRule(ctx context.Context, r BlueRule) (*BlueRule, error) {
	if r.Operator == "" {
		r.Operator = "equals"
	}
	if r.Severity == "" {
		r.Severity = "medium"
	}
	def, err := blueJSON(r.Definition, "{}")
	if err != nil {
		return nil, err
	}
	return scanBlueRule(d.QueryRowContext(ctx, `INSERT INTO blue_rules
		(name,description,field,operator,value,definition,severity,enabled)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8) RETURNING `+blueRuleColumns,
		r.Name, r.Description, r.Field, r.Operator, r.Value, def, r.Severity, r.Enabled))
}

func (d *DB) GetBlueRule(ctx context.Context, id int64) (*BlueRule, error) {
	r, err := scanBlueRule(d.QueryRowContext(ctx, `SELECT `+blueRuleColumns+` FROM blue_rules WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (d *DB) ListBlueRules(ctx context.Context, limit, offset int) ([]BlueRule, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueRuleColumns+` FROM blue_rules
		ORDER BY updated_at DESC,id DESC LIMIT $1 OFFSET $2`, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueRule{}
	for rows.Next() {
		r, err := scanBlueRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (d *DB) PatchBlueRule(ctx context.Context, id int64, p BlueRulePatch) (*BlueRule, error) {
	var def any
	if p.Definition != nil {
		encoded, err := blueJSON(*p.Definition, "{}")
		if err != nil {
			return nil, err
		}
		def = encoded
	}
	return scanBlueRule(d.QueryRowContext(ctx, `UPDATE blue_rules SET
		name=COALESCE($2,name),description=COALESCE($3,description),field=COALESCE($4,field),
		operator=COALESCE($5,operator),value=COALESCE($6,value),definition=COALESCE($7::jsonb,definition),
		severity=COALESCE($8,severity),enabled=COALESCE($9,enabled)
		WHERE id=$1 RETURNING `+blueRuleColumns,
		id, blueOptionalString(p.Name), blueOptionalString(p.Description), blueOptionalString(p.Field),
		blueOptionalString(p.Operator), blueOptionalString(p.Value), def, blueOptionalString(p.Severity),
		blueOptionalBool(p.Enabled)))
}

const blueAlertColumns = `id,rule_id,event_id,case_id,title,severity,status,reason,created_at,updated_at`

func scanBlueAlert(row blueRow) (*BlueAlert, error) {
	a := &BlueAlert{}
	var caseID sql.NullInt64
	if err := row.Scan(&a.ID, &a.RuleID, &a.EventID, &caseID, &a.Title, &a.Severity, &a.Status, &a.Reason,
		&a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if caseID.Valid {
		a.CaseID = &caseID.Int64
	}
	return a, nil
}

// UpsertBlueAlert deduplicates one rule's match on one event. It never resets an
// analyst's triage status. A case link must agree with the event's case link.
func (d *DB) UpsertBlueAlert(ctx context.Context, a BlueAlert) (_ *BlueAlert, err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var eventCase sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT case_id FROM blue_events WHERE id=$1 FOR UPDATE`, a.EventID).Scan(&eventCase); err != nil {
		return nil, err
	}
	if !eventCase.Valid && a.CaseID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE blue_events SET case_id=$2 WHERE id=$1`, a.EventID, *a.CaseID); err != nil {
			return nil, err
		}
		eventCase = sql.NullInt64{Int64: *a.CaseID, Valid: true}
	}
	if eventCase.Valid {
		if a.CaseID != nil && *a.CaseID != eventCase.Int64 {
			return nil, errors.New("alert case does not match event case")
		}
		a.CaseID = &eventCase.Int64
	}
	var ruleName, ruleSeverity string
	var ruleEnabled bool
	if err := tx.QueryRowContext(ctx, `SELECT name,severity,enabled FROM blue_rules WHERE id=$1`, a.RuleID).
		Scan(&ruleName, &ruleSeverity, &ruleEnabled); err != nil {
		return nil, err
	}
	if !ruleEnabled {
		return nil, errors.New("cannot create alert from disabled rule")
	}
	if a.Title == "" {
		a.Title = ruleName
	}
	if a.Severity == "" {
		a.Severity = ruleSeverity
	}
	if a.Status == "" {
		a.Status = "new"
	}
	stored, err := scanBlueAlert(tx.QueryRowContext(ctx, `INSERT INTO blue_alerts
		(rule_id,event_id,case_id,title,severity,status,reason) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (rule_id,event_id) DO NOTHING
		RETURNING `+blueAlertColumns,
		a.RuleID, a.EventID, a.CaseID, a.Title, a.Severity, a.Status, a.Reason))
	created := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		stored, err = scanBlueAlert(tx.QueryRowContext(ctx, `SELECT `+blueAlertColumns+`
			FROM blue_alerts WHERE rule_id=$1 AND event_id=$2`, a.RuleID, a.EventID))
	}
	if err != nil {
		return nil, err
	}
	stored.Created = created
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}

func (d *DB) ListBlueAlerts(ctx context.Context, caseID *int64, status, severity string, limit, offset int) ([]BlueAlert, error) {
	return d.SearchBlueAlerts(ctx, caseID, status, severity, "", limit, offset)
}

func (d *DB) SearchBlueAlerts(ctx context.Context, caseID *int64, status, severity, query string, limit, offset int) ([]BlueAlert, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueAlertColumns+` FROM blue_alerts
		WHERE ($1::bigint IS NULL OR case_id=$1) AND ($2='' OR status=$2) AND ($3='' OR severity=$3)
		AND ($4='' OR strpos(lower(id::text),lower($4))>0
			OR strpos(lower(rule_id::text),lower($4))>0
			OR strpos(lower(event_id::text),lower($4))>0
			OR strpos(lower(title),lower($4))>0
			OR strpos(lower(reason),lower($4))>0)
		ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`,
		caseID, status, severity, query, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueAlert{}
	for rows.Next() {
		a, err := scanBlueAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *DB) GetBlueAlert(ctx context.Context, id int64) (*BlueAlert, error) {
	a, err := scanBlueAlert(d.QueryRowContext(ctx, `SELECT `+blueAlertColumns+` FROM blue_alerts WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (d *DB) PatchBlueAlert(ctx context.Context, id int64, p BlueAlertPatch) (*BlueAlert, error) {
	// Attaching an alert also attaches its source event so investigators can use
	// that record as case evidence. Unassigning only affects the selected alert.
	// Historical observation references are never invalidated silently.
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if p.ClearCase && p.CaseID != nil {
		return nil, errors.New("cannot set and clear alert case together")
	}
	if p.CaseID != nil {
		var eventID int64
		var existingCase, existingAlertCase sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT e.id,e.case_id,a.case_id FROM blue_events e
			JOIN blue_alerts a ON a.event_id=e.id WHERE a.id=$1 FOR UPDATE OF e,a`, id).
			Scan(&eventID, &existingCase, &existingAlertCase); err != nil {
			return nil, err
		}
		if existingCase.Valid && existingCase.Int64 != *p.CaseID {
			return nil, fmt.Errorf("event is already attached to another case: %w", ErrBlueConflict)
		}
		if existingAlertCase.Valid && existingAlertCase.Int64 != *p.CaseID {
			return nil, fmt.Errorf("alert is already attached to another case: %w", ErrBlueConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE blue_events SET case_id=$2 WHERE id=$1`, eventID, *p.CaseID); err != nil {
			return nil, err
		}
	}
	if p.ClearCase {
		// Serialize with observation creation, which takes a share lock on each
		// cited alert before validating its case and inserting the reference.
		var lockedID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM blue_alerts WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
			return nil, err
		}
		ref, _ := json.Marshal([]int64{id})
		var observations int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM blue_observations
			WHERE alert_id=$1 OR evidence_alert_ids @> $2::jsonb`, id, string(ref)).Scan(&observations); err != nil {
			return nil, err
		}
		if observations > 0 {
			return nil, fmt.Errorf("cannot detach alert referenced by case observations: %w", ErrBlueConflict)
		}
	}
	a, err := scanBlueAlert(tx.QueryRowContext(ctx, `UPDATE blue_alerts SET
		case_id=CASE WHEN $5 THEN NULL ELSE COALESCE($2,case_id) END,
		status=COALESCE($3,status),reason=COALESCE($4,reason)
		WHERE id=$1 RETURNING `+blueAlertColumns, id, p.CaseID, blueOptionalString(p.Status), blueOptionalString(p.Reason), p.ClearCase))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

// MoveBlueEventCase explicitly reassigns an event and every alert it produced.
// It refuses to move any source that has already been cited in case observations,
// preserving the meaning of previously recorded evidence.
func (d *DB) MoveBlueEventCase(ctx context.Context, eventID, targetCaseID int64) (_ *BlueEvent, err error) {
	if targetCaseID <= 0 {
		return nil, errors.New("target case ID must be positive")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var currentCase sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT case_id FROM blue_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&currentCase); err != nil {
		return nil, err
	}
	changingEventCase := !currentCase.Valid || targetCaseID != currentCase.Int64
	rows, err := tx.QueryContext(ctx, `SELECT id FROM blue_alerts WHERE event_id=$1 ORDER BY id FOR UPDATE`, eventID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if changingEventCase {
		ref, _ := json.Marshal([]int64{eventID})
		var cited bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM blue_observations o
			WHERE o.event_id=$1 OR o.evidence_event_ids @> $2::jsonb
			OR EXISTS (SELECT 1 FROM blue_alerts a WHERE a.event_id=$1
				AND (o.alert_id=a.id OR o.evidence_alert_ids @> to_jsonb(ARRAY[a.id])))
		)`, eventID, string(ref)).Scan(&cited); err != nil {
			return nil, err
		}
		if cited {
			return nil, ErrBlueConflict
		}
	} else {
		// A same-case move can fill in unassigned peer alerts without changing
		// existing observations. A peer in another case signals inconsistent data.
		var conflictingPeer bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM blue_alerts WHERE event_id=$1 AND case_id IS NOT NULL AND case_id<>$2
		)`, eventID, targetCaseID).Scan(&conflictingPeer); err != nil {
			return nil, err
		}
		if conflictingPeer {
			return nil, ErrBlueConflict
		}
	}
	e, err := scanBlueEvent(tx.QueryRowContext(ctx, `UPDATE blue_events SET case_id=$2 WHERE id=$1
		RETURNING `+blueEventColumns, eventID, targetCaseID))
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE blue_alerts SET case_id=$2 WHERE event_id=$1`, eventID, targetCaseID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return e, nil
}

const blueObservationColumns = `id,case_id,event_id,alert_id,evidence_event_ids,evidence_alert_ids,kind,summary,detail,assessment,author,created_at`

func scanBlueObservation(row blueRow) (*BlueObservation, error) {
	o := &BlueObservation{}
	var eventID, alertID sql.NullInt64
	var eventIDs, alertIDs []byte
	if err := row.Scan(&o.ID, &o.CaseID, &eventID, &alertID, &eventIDs, &alertIDs,
		&o.Kind, &o.Summary, &o.Detail, &o.Assessment, &o.Author, &o.CreatedAt); err != nil {
		return nil, err
	}
	if eventID.Valid {
		o.EventID = &eventID.Int64
	}
	if alertID.Valid {
		o.AlertID = &alertID.Int64
	}
	if err := json.Unmarshal(eventIDs, &o.EvidenceEventIDs); err != nil {
		return nil, fmt.Errorf("decode observation event evidence: %w", err)
	}
	if err := json.Unmarshal(alertIDs, &o.EvidenceAlertIDs); err != nil {
		return nil, fmt.Errorf("decode observation alert evidence: %w", err)
	}
	if o.EvidenceEventIDs == nil {
		o.EvidenceEventIDs = []int64{}
	}
	if o.EvidenceAlertIDs == nil {
		o.EvidenceAlertIDs = []int64{}
	}
	return o, nil
}

func blueEvidenceIDs(primary *int64, ids []int64) ([]int64, error) {
	if len(ids) > 100 {
		return nil, fmt.Errorf("too many evidence references: %w", ErrBlueInvalidEvidence)
	}
	seen := make(map[int64]struct{}, len(ids)+1)
	out := make([]int64, 0, len(ids)+1)
	appendID := func(id int64) error {
		if id <= 0 {
			return fmt.Errorf("evidence ID must be positive: %w", ErrBlueInvalidEvidence)
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
		return nil
	}
	if primary != nil {
		if err := appendID(*primary); err != nil {
			return nil, err
		}
	}
	for _, id := range ids {
		if err := appendID(id); err != nil {
			return nil, err
		}
	}
	if len(out) > 100 {
		return nil, fmt.Errorf("too many evidence references: %w", ErrBlueInvalidEvidence)
	}
	return out, nil
}

// CreateBlueObservation verifies every referenced event and alert belongs to the
// same case. Agent-generated citations cannot attach unrelated evidence.
func (d *DB) CreateBlueObservation(ctx context.Context, o BlueObservation) (_ *BlueObservation, err error) {
	assessment := bytes.TrimSpace(o.Assessment)
	if len(assessment) == 0 {
		assessment = []byte("{}")
	}
	if len(assessment) > 64*1024 || !json.Valid(assessment) || assessment[0] != '{' {
		return nil, fmt.Errorf("assessment must be a JSON object up to 64 KiB: %w", ErrBlueInvalidEvidence)
	}
	eventIDs, err := blueEvidenceIDs(o.EventID, o.EvidenceEventIDs)
	if err != nil {
		return nil, err
	}
	alertIDs, err := blueEvidenceIDs(o.AlertID, o.EvidenceAlertIDs)
	if err != nil {
		return nil, err
	}
	if o.Kind == "" {
		o.Kind = "fact"
	}
	if o.Author == "" {
		o.Author = "analyst"
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, id := range eventIDs {
		var linked sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT case_id FROM blue_events WHERE id=$1 FOR SHARE`, id).Scan(&linked); err != nil {
			return nil, fmt.Errorf("event evidence %d: %w", id, err)
		}
		if !linked.Valid || linked.Int64 != o.CaseID {
			return nil, fmt.Errorf("event evidence %d is not attached to case %d: %w", id, o.CaseID, ErrBlueInvalidEvidence)
		}
	}
	for _, id := range alertIDs {
		var linked sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT case_id FROM blue_alerts WHERE id=$1 FOR SHARE`, id).Scan(&linked); err != nil {
			return nil, fmt.Errorf("alert evidence %d: %w", id, err)
		}
		if !linked.Valid || linked.Int64 != o.CaseID {
			return nil, fmt.Errorf("alert evidence %d is not attached to case %d: %w", id, o.CaseID, ErrBlueInvalidEvidence)
		}
	}
	eventJSON, _ := json.Marshal(eventIDs)
	alertJSON, _ := json.Marshal(alertIDs)
	stored, err := scanBlueObservation(tx.QueryRowContext(ctx, `INSERT INTO blue_observations
		(case_id,event_id,alert_id,evidence_event_ids,evidence_alert_ids,kind,summary,detail,assessment,author)
		VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9::jsonb,$10) RETURNING `+blueObservationColumns,
		o.CaseID, o.EventID, o.AlertID, string(eventJSON), string(alertJSON), o.Kind, o.Summary, o.Detail, string(assessment), o.Author))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}

// A stopped analysis may race with storing an observation. Its run can no
// longer complete, so remove only the just-created, unattached analyst row.
// A run that did complete still references the observation and is preserved.
func (d *DB) DeleteUnattachedBlueAnalysisObservation(ctx context.Context, id int64) error {
	_, err := d.ExecContext(ctx, `DELETE FROM blue_observations o
		WHERE o.id=$1 AND o.kind='analysis' AND o.author='blue_analyst'
		AND NOT EXISTS (SELECT 1 FROM blue_analysis_runs r WHERE r.observation_id=o.id)`, id)
	return err
}

func (d *DB) ListBlueObservations(ctx context.Context, caseID int64, limit, offset int) ([]BlueObservation, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueObservationColumns+` FROM blue_observations
		WHERE case_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, caseID, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueObservation{}
	for rows.Next() {
		o, err := scanBlueObservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

const blueActionColumns = `id,case_id,kind,target,rationale,params,status,decision_note,requested_by,decided_by,completion_note,completion_evidence,completed_by,created_at,decided_at,completed_at`

func scanBlueAction(row blueRow) (*BlueAction, error) {
	a := &BlueAction{}
	var decided, completed sql.NullTime
	if err := row.Scan(&a.ID, &a.CaseID, &a.Kind, &a.Target, &a.Rationale, &a.Params,
		&a.Status, &a.DecisionNote, &a.RequestedBy, &a.DecidedBy,
		&a.CompletionNote, &a.CompletionEvidence, &a.CompletedBy, &a.CreatedAt, &decided, &completed); err != nil {
		return nil, err
	}
	if decided.Valid {
		a.DecidedAt = &decided.Time
	}
	if completed.Valid {
		a.CompletedAt = &completed.Time
	}
	return a, nil
}

func (d *DB) CreateBlueAction(ctx context.Context, a BlueAction) (*BlueAction, error) {
	params, err := blueJSON(a.Params, "{}")
	if err != nil {
		return nil, err
	}
	if a.RequestedBy == "" {
		a.RequestedBy = "analyst"
	}
	return scanBlueAction(d.QueryRowContext(ctx, `INSERT INTO blue_actions
		(case_id,kind,target,rationale,params,requested_by)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6) RETURNING `+blueActionColumns,
		a.CaseID, a.Kind, a.Target, a.Rationale, params, a.RequestedBy))
}

func (d *DB) ListBlueActions(ctx context.Context, caseID *int64, status string, limit, offset int) ([]BlueAction, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueActionColumns+` FROM blue_actions
		WHERE ($1::bigint IS NULL OR case_id=$1) AND ($2='' OR status=$2)
		ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, caseID, status, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueAction{}
	for rows.Next() {
		a, err := scanBlueAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *DB) GetBlueAction(ctx context.Context, id int64) (*BlueAction, error) {
	a, err := scanBlueAction(d.QueryRowContext(ctx, `SELECT `+blueActionColumns+` FROM blue_actions WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (d *DB) blueActionNoTransition(ctx context.Context, id int64) error {
	a, err := d.GetBlueAction(ctx, id)
	if err != nil {
		return err
	}
	if a == nil {
		return sql.ErrNoRows
	}
	return ErrBlueConflict
}

// DecideBlueAction only records a human decision. Nothing in this transition
// calls an endpoint or runs a host command.
func (d *DB) DecideBlueAction(ctx context.Context, id int64, approved bool, note, actor string) (*BlueAction, error) {
	status := "rejected"
	if approved {
		status = "approved"
	}
	if actor == "" {
		actor = "analyst"
	}
	a, err := scanBlueAction(d.QueryRowContext(ctx, `UPDATE blue_actions
		SET status=$2,decision_note=$3,decided_by=$4,decided_at=now()
		WHERE id=$1 AND status='pending' RETURNING `+blueActionColumns, id, status, note, actor))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, d.blueActionNoTransition(ctx, id)
	}
	return a, err
}

// CompleteBlueAction records an operator-confirmed outcome after approval. The
// caller supplies its evidence; this method never executes the proposed action.
func (d *DB) CompleteBlueAction(ctx context.Context, id int64, success bool, note, evidence, actor string) (*BlueAction, error) {
	status := "failed"
	if success {
		status = "completed"
	}
	if actor == "" {
		actor = "analyst"
	}
	a, err := scanBlueAction(d.QueryRowContext(ctx, `UPDATE blue_actions SET
		status=$2,completion_note=$3,completion_evidence=$4,completed_by=$5,completed_at=now()
		WHERE id=$1 AND status='approved' RETURNING `+blueActionColumns,
		id, status, note, evidence, actor))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, d.blueActionNoTransition(ctx, id)
	}
	return a, err
}

func (d *DB) BlueOverview(ctx context.Context) (*BlueOverview, error) {
	o := &BlueOverview{}
	err := d.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM blue_cases),
		(SELECT count(*) FROM blue_cases WHERE status IN ('open','investigating','contained')),
		(SELECT count(*) FROM blue_events),
		(SELECT count(*) FROM blue_alerts),
		(SELECT count(*) FROM blue_alerts WHERE status='new'),
		(SELECT count(*) FROM blue_rules),
		(SELECT count(*) FROM blue_actions WHERE status='pending')`).Scan(
		&o.CaseCount, &o.OpenCaseCount, &o.EventCount, &o.AlertCount, &o.NewAlertCount,
		&o.RuleCount, &o.PendingActionCount)
	if err != nil {
		return nil, err
	}
	return o, nil
}

const blueAnalysisColumns = `id,case_id,status,model_profile_id,observation_id,error,created_at,started_at,finished_at`

func scanBlueAnalysisRun(row blueRow) (*BlueAnalysisRun, error) {
	r := &BlueAnalysisRun{}
	var modelID, observationID sql.NullInt64
	var started, finished sql.NullTime
	if err := row.Scan(&r.ID, &r.CaseID, &r.Status, &modelID, &observationID, &r.Error,
		&r.CreatedAt, &started, &finished); err != nil {
		return nil, err
	}
	if modelID.Valid {
		r.ModelProfileID = &modelID.Int64
	}
	if observationID.Valid {
		r.ObservationID = &observationID.Int64
	}
	if started.Valid {
		r.StartedAt = &started.Time
	}
	if finished.Valid {
		r.FinishedAt = &finished.Time
	}
	return r, nil
}

func (d *DB) CreateBlueAnalysisRun(ctx context.Context, caseID int64, profileID *int64) (*BlueAnalysisRun, error) {
	r, err := scanBlueAnalysisRun(d.QueryRowContext(ctx, `INSERT INTO blue_analysis_runs (case_id,model_profile_id)
		VALUES ($1,$2) RETURNING `+blueAnalysisColumns, caseID, profileID))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_blue_analysis_runs_active_case" {
		return nil, ErrBlueConflict
	}
	return r, err
}

func (d *DB) UpdateBlueAnalysisRun(ctx context.Context, id int64, status string, observationID *int64, errorText string) (*BlueAnalysisRun, error) {
	if status != "running" && status != "completed" && status != "failed" {
		return nil, errors.New("invalid analysis run status")
	}
	return scanBlueAnalysisRun(d.QueryRowContext(ctx, `UPDATE blue_analysis_runs SET
		status=$2,observation_id=COALESCE($3,observation_id),error=$4,
		started_at=CASE WHEN $2='running' THEN now() ELSE started_at END,
		finished_at=CASE WHEN $2 IN ('completed','failed') THEN now() ELSE finished_at END
		WHERE id=$1 AND ((status='queued' AND $2 IN ('running','failed')) OR
		(status='running' AND $2 IN ('completed','failed')))
		AND ($3::bigint IS NULL OR EXISTS (
			SELECT 1 FROM blue_observations o WHERE o.id=$3 AND o.case_id=blue_analysis_runs.case_id
		))
		RETURNING `+blueAnalysisColumns, id, status, observationID, errorText))
}

// FailInterruptedBlueAnalysisRuns closes jobs that cannot resume after the
// in-process analyst worker is restarted.
func (d *DB) FailInterruptedBlueAnalysisRuns(ctx context.Context) error {
	_, err := d.ExecContext(ctx, `UPDATE blue_analysis_runs
		SET status='failed',error='analysis interrupted by server restart',finished_at=now()
		WHERE status IN ('queued','running')`)
	return err
}

func (d *DB) GetBlueAnalysisRun(ctx context.Context, id int64) (*BlueAnalysisRun, error) {
	r, err := scanBlueAnalysisRun(d.QueryRowContext(ctx, `SELECT `+blueAnalysisColumns+` FROM blue_analysis_runs WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (d *DB) ListBlueAnalysisRuns(ctx context.Context, caseID int64, limit, offset int) ([]BlueAnalysisRun, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueAnalysisColumns+` FROM blue_analysis_runs
		WHERE case_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, caseID, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueAnalysisRun{}
	for rows.Next() {
		r, err := scanBlueAnalysisRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
