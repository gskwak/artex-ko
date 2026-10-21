package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BlueTask is a defensive work item, independent of red-team explorations.
// A case-linked task can start the evidence-bound blue analyst; the run ID
// preserves an audit trail while status and result show its latest outcome.
type BlueTask struct {
	ID            int64      `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	CaseID        *int64     `json:"case_id,omitempty"`
	Priority      string     `json:"priority"`
	Status        string     `json:"status"`
	Result        string     `json:"result"`
	AnalysisRunID *int64     `json:"analysis_run_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

type BlueTaskPatch struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	CaseID      *int64  `json:"case_id,omitempty"`
	ClearCase   bool    `json:"clear_case,omitempty"`
	Priority    *string `json:"priority,omitempty"`
	Status      *string `json:"status,omitempty"`
	Result      *string `json:"result,omitempty"`
}

const blueTaskColumns = `id,title,description,case_id,priority,status,result,analysis_run_id,created_at,updated_at,completed_at`

func scanBlueTask(row blueRow) (*BlueTask, error) {
	t := &BlueTask{}
	var caseID, runID sql.NullInt64
	var completed sql.NullTime
	if err := row.Scan(&t.ID, &t.Title, &t.Description, &caseID, &t.Priority,
		&t.Status, &t.Result, &runID, &t.CreatedAt, &t.UpdatedAt, &completed); err != nil {
		return nil, err
	}
	if caseID.Valid {
		t.CaseID = &caseID.Int64
	}
	if runID.Valid {
		t.AnalysisRunID = &runID.Int64
	}
	if completed.Valid {
		t.CompletedAt = &completed.Time
	}
	return t, nil
}

func (d *DB) CreateBlueTask(ctx context.Context, t BlueTask) (*BlueTask, error) {
	if t.Priority == "" {
		t.Priority = "medium"
	}
	return scanBlueTask(d.QueryRowContext(ctx, `INSERT INTO blue_tasks
		(title,description,case_id,priority) VALUES ($1,$2,$3,$4)
		RETURNING `+blueTaskColumns, t.Title, t.Description, t.CaseID, t.Priority))
}

func (d *DB) GetBlueTask(ctx context.Context, id int64) (*BlueTask, error) {
	t, err := scanBlueTask(d.QueryRowContext(ctx, `SELECT `+blueTaskColumns+` FROM blue_tasks WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (d *DB) ListBlueTasks(ctx context.Context, status string, caseID *int64, limit, offset int) ([]BlueTask, error) {
	return d.SearchBlueTasks(ctx, status, "", caseID, "", "updated", "desc", limit, offset)
}

// SearchBlueTasks applies search, filters, and ordering in PostgreSQL before
// pagination. The ORDER BY expression comes only from this fixed allowlist.
func (d *DB) SearchBlueTasks(ctx context.Context, status, priority string, caseID *int64, query, sort, order string, limit, offset int) ([]BlueTask, error) {
	sortColumns := map[string]string{
		"id":       "t.id",
		"title":    "lower(t.title)",
		"status":   "CASE t.status WHEN 'open' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'blocked' THEN 2 WHEN 'completed' THEN 3 ELSE 4 END",
		"priority": "CASE t.priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END",
		"updated":  "t.updated_at",
	}
	sortColumn, ok := sortColumns[sort]
	if !ok {
		sortColumn = sortColumns["updated"]
	}
	direction := "DESC"
	if order == "asc" {
		direction = "ASC"
	}
	tieBreak := ",t.id DESC"
	if sort == "id" {
		tieBreak = ""
	}
	const columns = `t.id,t.title,t.description,t.case_id,t.priority,t.status,t.result,t.analysis_run_id,t.created_at,t.updated_at,t.completed_at`
	rows, err := d.QueryContext(ctx, `SELECT `+columns+` FROM blue_tasks t
		LEFT JOIN blue_cases c ON c.id=t.case_id
		WHERE ($1='' OR t.status=$1) AND ($2='' OR t.priority=$2)
		AND ($3::bigint IS NULL OR t.case_id=$3)
		AND ($4='' OR strpos(lower(t.id::text),lower($4))>0
			OR strpos(lower(t.title),lower($4))>0
			OR strpos(lower(t.description),lower($4))>0
			OR strpos(lower(t.result),lower($4))>0
			OR strpos(lower(COALESCE(c.title,'')),lower($4))>0)
		ORDER BY `+sortColumn+` `+direction+tieBreak+` LIMIT $5 OFFSET $6`,
		status, priority, caseID, query, blueLimit(limit), blueOffset(offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueTask{}
	for rows.Next() {
		t, err := scanBlueTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// PatchBlueTask rejects edits while its analyst run is active, preventing a
// manual status change from racing with the run's eventual result. A changed
// request or case invalidates the old outcome, even when a client sends the
// previous status and result back with the edit.
func (d *DB) PatchBlueTask(ctx context.Context, id int64, p BlueTaskPatch) (*BlueTask, error) {
	t, err := scanBlueTask(d.QueryRowContext(ctx, `WITH edit AS (
		SELECT id AS task_id, ($2::text IS NOT NULL AND $2::text IS DISTINCT FROM title
			OR $3::text IS NOT NULL AND $3::text IS DISTINCT FROM description
			OR $4::boolean AND case_id IS NOT NULL
			OR $5::bigint IS NOT NULL AND $5::bigint IS DISTINCT FROM case_id) AS changed
		FROM blue_tasks WHERE id=$1 FOR UPDATE
	)
		UPDATE blue_tasks AS t SET
		title=COALESCE($2,t.title),description=COALESCE($3,t.description),
		case_id=CASE WHEN $4 THEN NULL ELSE COALESCE($5,t.case_id) END,
		priority=COALESCE($6,t.priority),
		status=CASE WHEN edit.changed THEN 'open' ELSE COALESCE($7,t.status) END,
		result=CASE WHEN edit.changed THEN '' ELSE COALESCE($8,t.result) END,
		analysis_run_id=CASE WHEN edit.changed THEN NULL ELSE t.analysis_run_id END,
		completed_at=CASE WHEN edit.changed THEN NULL WHEN $7='completed' THEN now()
			WHEN $7 IS NOT NULL THEN NULL ELSE t.completed_at END
		FROM edit WHERE t.id=edit.task_id
		AND NOT (t.status='in_progress' AND t.analysis_run_id IS NOT NULL)
		AND NOT EXISTS (
			SELECT 1 FROM blue_analysis_runs r WHERE r.id=t.analysis_run_id
			AND r.status IN ('queued','running'))
		RETURNING `+blueTaskColumns, id, blueOptionalString(p.Title), blueOptionalString(p.Description),
		p.ClearCase, p.CaseID, blueOptionalString(p.Priority), blueOptionalString(p.Status),
		blueOptionalString(p.Result)))
	if !errors.Is(err, sql.ErrNoRows) {
		return t, err
	}
	existing, lookupErr := d.GetBlueTask(ctx, id)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if existing != nil {
		return nil, ErrBlueConflict
	}
	return nil, nil
}

// AttachBlueTaskRun claims an open or blocked task after creating a case
// analysis run. The worker is launched only after this write succeeds.
func (d *DB) AttachBlueTaskRun(ctx context.Context, taskID, runID int64) (*BlueTask, error) {
	t, err := scanBlueTask(d.QueryRowContext(ctx, `UPDATE blue_tasks AS t SET
		status='in_progress',result='',analysis_run_id=$2,completed_at=NULL
		WHERE id=$1 AND status IN ('open','blocked') AND case_id=(
			SELECT case_id FROM blue_analysis_runs WHERE id=$2)
		RETURNING `+blueTaskColumns, taskID, runID))
	if !errors.Is(err, sql.ErrNoRows) {
		return t, err
	}
	existing, lookupErr := d.GetBlueTask(ctx, taskID)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if existing != nil {
		return nil, ErrBlueConflict
	}
	return nil, nil
}

// SyncBlueTaskFromRun records a completed analyst outcome. The same operation
// is used during startup recovery for jobs interrupted by a server restart.
func (d *DB) SyncBlueTaskFromRun(ctx context.Context, runID int64) error {
	_, err := d.ExecContext(ctx, `UPDATE blue_tasks t SET
		status=CASE WHEN r.status='completed' AND o.assessment->'task_outcome'->>'status'='answered'
			THEN 'completed' ELSE 'blocked' END,
		result=CASE WHEN r.status='completed' THEN COALESCE(o.detail,'') ELSE r.error END,
		completed_at=CASE WHEN r.status='completed' AND o.assessment->'task_outcome'->>'status'='answered'
			THEN r.finished_at ELSE NULL END
		FROM blue_analysis_runs r LEFT JOIN blue_observations o ON o.id=r.observation_id
		WHERE t.analysis_run_id=r.id AND r.id=$1 AND r.status IN ('completed','failed')
		AND t.status='in_progress'`, runID)
	return err
}

func (d *DB) SyncFinishedBlueTaskRuns(ctx context.Context) error {
	_, err := d.ExecContext(ctx, `UPDATE blue_tasks t SET
		status=CASE WHEN r.status='completed' AND o.assessment->'task_outcome'->>'status'='answered'
			THEN 'completed' ELSE 'blocked' END,
		result=CASE WHEN r.status='completed' THEN COALESCE(o.detail,'') ELSE r.error END,
		completed_at=CASE WHEN r.status='completed' AND o.assessment->'task_outcome'->>'status'='answered'
			THEN r.finished_at ELSE NULL END
		FROM blue_analysis_runs r LEFT JOIN blue_observations o ON o.id=r.observation_id
		WHERE t.analysis_run_id=r.id AND r.status IN ('completed','failed')
		AND t.status='in_progress'`)
	return err
}
