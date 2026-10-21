package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// A BlueAsset is a durable defensive identity. It does not grant a red-team
// task permission to inspect or act on the corresponding host.
type BlueAsset struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	Identifier string    `json:"identifier"`
	Label      string    `json:"label"`
	Status     string    `json:"status"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type BlueAssetPatch struct {
	Kind       *string `json:"kind,omitempty"`
	Identifier *string `json:"identifier,omitempty"`
	Label      *string `json:"label,omitempty"`
	Status     *string `json:"status,omitempty"`
	Notes      *string `json:"notes,omitempty"`
}

// BlueCaseAsset distinguishes an analyst's explicit case link from an asset
// that appears only because a current case event is attributed to it.
type BlueCaseAsset struct {
	BlueAsset
	ExplicitLink bool `json:"explicit_link"`
}

type BlueAssetActivity struct {
	Cases       []BlueCase   `json:"cases"`
	Events      []BlueEvent  `json:"events"`
	Actions     []BlueAction `json:"actions"`
	CaseCount   int64        `json:"case_count"`
	EventCount  int64        `json:"event_count"`
	ActionCount int64        `json:"action_count"`
}

var ErrBlueAssetUnavailable = errors.New("blue-team asset is missing or archived")
var ErrBlueAssetNotInCase = errors.New("blue-team asset is not linked to the case")

const blueAssetColumns = `id,kind,identifier,label,status,notes,created_at,updated_at`

func scanBlueAsset(row blueRow) (*BlueAsset, error) {
	a := &BlueAsset{}
	if err := row.Scan(&a.ID, &a.Kind, &a.Identifier, &a.Label, &a.Status, &a.Notes,
		&a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

func (d *DB) CreateBlueAsset(ctx context.Context, a BlueAsset) (*BlueAsset, error) {
	a.Kind = strings.ToLower(strings.TrimSpace(a.Kind))
	a.Identifier = strings.TrimSpace(a.Identifier)
	a.Label = strings.TrimSpace(a.Label)
	if a.Status == "" {
		a.Status = "active"
	}
	return scanBlueAsset(d.QueryRowContext(ctx, `INSERT INTO blue_assets
		(kind,identifier,label,status,notes) VALUES ($1,$2,$3,$4,$5)
		RETURNING `+blueAssetColumns, a.Kind, a.Identifier, a.Label, a.Status, a.Notes))
}

func (d *DB) GetBlueAsset(ctx context.Context, id int64) (*BlueAsset, error) {
	a, err := scanBlueAsset(d.QueryRowContext(ctx,
		`SELECT `+blueAssetColumns+` FROM blue_assets WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (d *DB) ListBlueAssets(ctx context.Context, query, status string, limit, offset int) ([]BlueAsset, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueAssetColumns+` FROM blue_assets
		WHERE ($1='' OR strpos(lower(identifier),lower($1))>0
			OR strpos(lower(label),lower($1))>0
			OR strpos(id::text,$1)>0)
		AND ($2='' OR status=$2)
		ORDER BY updated_at DESC,id DESC LIMIT $3 OFFSET $4`,
		strings.TrimSpace(query), status, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueAsset{}
	for rows.Next() {
		a, err := scanBlueAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *DB) PatchBlueAsset(ctx context.Context, id int64, p BlueAssetPatch) (*BlueAsset, error) {
	if p.Kind != nil {
		*p.Kind = strings.ToLower(strings.TrimSpace(*p.Kind))
	}
	if p.Identifier != nil {
		*p.Identifier = strings.TrimSpace(*p.Identifier)
	}
	if p.Label != nil {
		*p.Label = strings.TrimSpace(*p.Label)
	}
	a, err := scanBlueAsset(d.QueryRowContext(ctx, `UPDATE blue_assets SET
		kind=COALESCE($2,kind),identifier=COALESCE($3,identifier),
		label=COALESCE($4,label),status=COALESCE($5,status),notes=COALESCE($6,notes)
		WHERE id=$1 RETURNING `+blueAssetColumns,
		id, blueOptionalString(p.Kind), blueOptionalString(p.Identifier),
		blueOptionalString(p.Label), blueOptionalString(p.Status), blueOptionalString(p.Notes)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// blueRequireActiveAssetTx takes a shared row lock. Archiving the asset must
// wait until a link/import/action using it has committed or rolled back.
func blueRequireActiveAssetTx(ctx context.Context, tx *sql.Tx, assetID *int64) error {
	if assetID == nil {
		return nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM blue_assets
		WHERE id=$1 AND status='active' FOR SHARE`, *assetID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBlueAssetUnavailable
	}
	return err
}

func blueCaseHasAssetTx(ctx context.Context, tx *sql.Tx, caseID, assetID int64) (bool, error) {
	var linkedID int64
	err := tx.QueryRowContext(ctx, `SELECT asset_id FROM blue_case_assets
		WHERE case_id=$1 AND asset_id=$2 FOR SHARE`, caseID, assetID).Scan(&linkedID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM blue_events
		WHERE case_id=$1 AND asset_id=$2 LIMIT 1 FOR SHARE`, caseID, assetID).Scan(&linkedID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (d *DB) BlueCaseHasAsset(ctx context.Context, caseID, assetID int64) (bool, error) {
	var linked bool
	err := d.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM blue_case_assets WHERE case_id=$1 AND asset_id=$2
		UNION ALL
		SELECT 1 FROM blue_events WHERE case_id=$1 AND asset_id=$2
	)`, caseID, assetID).Scan(&linked)
	return linked, err
}

// Explicit links coexist with inferred links from current events. A moved
// event stops contributing to its former case without touching manual links.
func (d *DB) ListBlueCaseAssets(ctx context.Context, caseID int64) ([]BlueCaseAsset, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueAssetColumns+`,
		EXISTS (SELECT 1 FROM blue_case_assets bca
			WHERE bca.case_id=$1 AND bca.asset_id=blue_assets.id)
		FROM blue_assets
		WHERE id IN (
			SELECT asset_id FROM blue_case_assets WHERE case_id=$1
			UNION
			SELECT asset_id FROM blue_events WHERE case_id=$1 AND asset_id IS NOT NULL
		)
		ORDER BY label,id`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueCaseAsset{}
	for rows.Next() {
		var a BlueCaseAsset
		if err := rows.Scan(&a.ID, &a.Kind, &a.Identifier, &a.Label, &a.Status,
			&a.Notes, &a.CreatedAt, &a.UpdatedAt, &a.ExplicitLink); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) LinkBlueCaseAsset(ctx context.Context, caseID, assetID int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := blueRequireActiveAssetTx(ctx, tx, &assetID); err != nil {
		return err
	}
	var existingCase int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM blue_cases WHERE id=$1 FOR SHARE`, caseID).Scan(&existingCase); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO blue_case_assets(case_id,asset_id)
		VALUES ($1,$2) ON CONFLICT DO NOTHING`, caseID, assetID); err != nil {
		return err
	}
	return tx.Commit()
}

// Only an explicit link is removed. A current event or action may still
// legitimately relate the asset to the case.
func (d *DB) UnlinkBlueCaseAsset(ctx context.Context, caseID, assetID int64) (bool, error) {
	result, err := d.ExecContext(ctx,
		`DELETE FROM blue_case_assets WHERE case_id=$1 AND asset_id=$2`, caseID, assetID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// SetBlueEventAsset changes analyst attribution only. The imported asset text
// and raw payload remain an immutable snapshot of the original event.
func (d *DB) SetBlueEventAsset(ctx context.Context, eventID int64, assetID *int64) (*BlueEvent, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := blueRequireActiveAssetTx(ctx, tx, assetID); err != nil {
		return nil, err
	}
	event, err := scanBlueEvent(tx.QueryRowContext(ctx, `UPDATE blue_events
		SET asset_id=$2 WHERE id=$1 RETURNING `+blueEventColumns, eventID, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return event, tx.Commit()
}

// GetBlueAssetActivity returns a bounded, independently paged recent slice of
// each record type plus full counts. Cases are deduplicated across direct
// links, current events, and response actions.
func (d *DB) GetBlueAssetActivity(ctx context.Context, assetID int64, limit int) (*BlueAssetActivity, error) {
	limit = blueLimit(limit)
	a := &BlueAssetActivity{Cases: []BlueCase{}, Events: []BlueEvent{}, Actions: []BlueAction{}}
	const relatedCases = `(
		SELECT case_id FROM blue_case_assets WHERE asset_id=$1
		UNION
		SELECT case_id FROM blue_events WHERE asset_id=$1 AND case_id IS NOT NULL
		UNION
		SELECT case_id FROM blue_actions WHERE asset_id=$1
	)`
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM `+relatedCases+` AS linked`, assetID).Scan(&a.CaseCount); err != nil {
		return nil, err
	}
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM blue_events WHERE asset_id=$1`, assetID).Scan(&a.EventCount); err != nil {
		return nil, err
	}
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM blue_actions WHERE asset_id=$1`, assetID).Scan(&a.ActionCount); err != nil {
		return nil, err
	}
	cases, err := d.QueryContext(ctx, `SELECT `+blueCaseColumns+` FROM blue_cases
		WHERE id IN `+relatedCases+` ORDER BY updated_at DESC,id DESC LIMIT $2`, assetID, limit)
	if err != nil {
		return nil, err
	}
	for cases.Next() {
		c, err := scanBlueCase(cases)
		if err != nil {
			cases.Close()
			return nil, err
		}
		a.Cases = append(a.Cases, *c)
	}
	if err := cases.Err(); err != nil {
		cases.Close()
		return nil, err
	}
	cases.Close()
	events, err := d.QueryContext(ctx, `SELECT `+blueEventColumns+` FROM blue_events
		WHERE asset_id=$1 ORDER BY occurred_at DESC,id DESC LIMIT $2`, assetID, limit)
	if err != nil {
		return nil, err
	}
	for events.Next() {
		e, err := scanBlueEvent(events)
		if err != nil {
			events.Close()
			return nil, err
		}
		a.Events = append(a.Events, *e)
	}
	if err := events.Err(); err != nil {
		events.Close()
		return nil, err
	}
	events.Close()
	actions, err := d.QueryContext(ctx, `SELECT `+blueActionColumns+` FROM blue_actions
		WHERE asset_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, assetID, limit)
	if err != nil {
		return nil, err
	}
	defer actions.Close()
	for actions.Next() {
		item, err := scanBlueAction(actions)
		if err != nil {
			return nil, err
		}
		a.Actions = append(a.Actions, *item)
	}
	return a, actions.Err()
}
