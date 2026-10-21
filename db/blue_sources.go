package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BlueEventSource is an explicitly configured HTTP JSONL feed. Its opaque
// cursor is retained independently of the immutable events imported from it.
type BlueEventSource struct {
	ID                  int64      `json:"id"`
	Name                string     `json:"name"`
	URL                 string     `json:"url"`
	Enabled             bool       `json:"enabled"`
	PollIntervalSeconds int        `json:"poll_interval_seconds"`
	Cursor              string     `json:"cursor"`
	LastSyncedAt        *time.Time `json:"last_synced_at,omitempty"`
	LastError           string     `json:"last_error"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type BlueEventSourcePatch struct {
	Name                *string `json:"name,omitempty"`
	URL                 *string `json:"url,omitempty"`
	Enabled             *bool   `json:"enabled,omitempty"`
	PollIntervalSeconds *int    `json:"poll_interval_seconds,omitempty"`
	ResetCursor         bool    `json:"-"`
}

const blueEventSourceColumns = `id,name,url,enabled,poll_interval_seconds,cursor,last_synced_at,last_error,created_at,updated_at`

func scanBlueEventSource(row blueRow) (*BlueEventSource, error) {
	s := &BlueEventSource{}
	var lastSyncedAt sql.NullTime
	if err := row.Scan(&s.ID, &s.Name, &s.URL, &s.Enabled, &s.PollIntervalSeconds,
		&s.Cursor, &lastSyncedAt, &s.LastError, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	if lastSyncedAt.Valid {
		s.LastSyncedAt = &lastSyncedAt.Time
	}
	return s, nil
}

func (d *DB) ListBlueEventSources(ctx context.Context) ([]BlueEventSource, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+blueEventSourceColumns+` FROM blue_event_sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlueEventSource{}
	for rows.Next() {
		source, err := scanBlueEventSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *source)
	}
	return out, rows.Err()
}

func (d *DB) GetBlueEventSource(ctx context.Context, id int64) (*BlueEventSource, error) {
	source, err := scanBlueEventSource(d.QueryRowContext(ctx,
		`SELECT `+blueEventSourceColumns+` FROM blue_event_sources WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return source, err
}

func (d *DB) CreateBlueEventSource(ctx context.Context, source BlueEventSource) (*BlueEventSource, error) {
	if source.PollIntervalSeconds == 0 {
		source.PollIntervalSeconds = 30
	}
	return scanBlueEventSource(d.QueryRowContext(ctx, `INSERT INTO blue_event_sources
		(name,url,enabled,poll_interval_seconds) VALUES ($1,$2,$3,$4)
		RETURNING `+blueEventSourceColumns,
		source.Name, source.URL, source.Enabled, source.PollIntervalSeconds))
}

func (d *DB) PatchBlueEventSource(ctx context.Context, id int64, patch BlueEventSourcePatch) (*BlueEventSource, error) {
	var pollInterval any
	if patch.PollIntervalSeconds != nil {
		pollInterval = *patch.PollIntervalSeconds
	}
	source, err := scanBlueEventSource(d.QueryRowContext(ctx, `UPDATE blue_event_sources SET
		name=COALESCE($2,name), url=COALESCE($3,url), enabled=COALESCE($4,enabled),
		poll_interval_seconds=COALESCE($5,poll_interval_seconds),
		cursor=CASE WHEN $6::boolean OR ($3::text IS NOT NULL AND $3::text<>url) THEN '' ELSE cursor END,
		last_synced_at=CASE WHEN $6::boolean OR ($3::text IS NOT NULL AND $3::text<>url) THEN NULL ELSE last_synced_at END,
		last_error=CASE WHEN $6::boolean OR ($3::text IS NOT NULL AND $3::text<>url) THEN '' ELSE last_error END
		WHERE id=$1 RETURNING `+blueEventSourceColumns,
		id, blueOptionalString(patch.Name), blueOptionalString(patch.URL), blueOptionalBool(patch.Enabled), pollInterval, patch.ResetCursor))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return source, err
}

func (d *DB) DeleteBlueEventSource(ctx context.Context, id int64) (bool, error) {
	result, err := d.ExecContext(ctx, `DELETE FROM blue_event_sources WHERE id=$1`, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// RecordBlueEventSourceSync advances a cursor only if the source configuration
// and cursor still match the snapshot that was fetched. updated_at also changes
// on name, enabled, or interval edits, so a different server instance cannot
// attach a stale result to a source modified while the HTTP request was active.
// Empty lastError records a successful fetch (including an empty batch); a
// nonempty error leaves both cursor and last successful sync time untouched.
func (d *DB) RecordBlueEventSourceSync(ctx context.Context, id int64, expectedCursor, expectedURL string, expectedUpdatedAt time.Time, nextCursor, lastError string) (bool, error) {
	var result sql.Result
	var err error
	if lastError == "" {
		result, err = d.ExecContext(ctx, `UPDATE blue_event_sources
			SET cursor=$5,last_synced_at=now(),last_error=''
			WHERE id=$1 AND cursor=$2 AND url=$3 AND updated_at=$4`, id, expectedCursor, expectedURL, expectedUpdatedAt, nextCursor)
	} else {
		result, err = d.ExecContext(ctx, `UPDATE blue_event_sources
			SET last_error=$5
			WHERE id=$1 AND cursor=$2 AND url=$3 AND updated_at=$4`, id, expectedCursor, expectedURL, expectedUpdatedAt, lastError)
	}
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}
