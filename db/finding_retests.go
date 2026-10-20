package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const FindingRetestAgentKey = "retester"

// 재검증(finding_retest) 종결 사유. finding_retests.error 컬럼에 저장돼 재검증 패널
// (finding-retest-panel) 의 item.error 로 노출된다(사용자 노출, server/conversations.go 의
// 형제 사유 convRetest* 와 같은 컬럼·패널이라 함께 한국어로 둔다 — F9). retestNoConclusionReason
// 은 에이전트가 결론 없이 완료했을 때 caller 가 넘긴 사유를 덮어쓰는 폴백이고,
// retestServiceRestartReason 은 재시작 복구(RecoverFindingRetests)가 미완 재검증을 봉인할 때 쓴다.
// 작은따옴표 없는 상수라 SQL 리터럴 자리에 그대로 이어 붙여도 안전하다.
const (
	retestNoConclusionReason   = "에이전트가 재검증 결론을 저장하지 않았습니다. 대화를 확인한 뒤 다시 재검증해 주세요"
	retestServiceRestartReason = "서비스가 재시작되어 재검증이 중단되었습니다. 다시 시작해 주세요"
)

var ErrRetestNotRunning = errors.New("This retest has ended or has not started; start a new retest from the finding details")

// FindingRetest is an immutable historical test once its conversation turn ends.
// Snapshot is only loaded for the agent, never sent with the history list.
type FindingRetest struct {
	ID             int64           `json:"id"`
	FindingID      int64           `json:"finding_id"`
	ConversationID *int64          `json:"conversation_id"`
	Status         string          `json:"status"`
	Verdict        string          `json:"verdict"`
	Notes          string          `json:"notes"`
	Snapshot       json.RawMessage `json:"snapshot,omitempty"`
	Summary        string          `json:"summary"`
	Evidence       string          `json:"evidence"`
	Error          string          `json:"error"`
	CreatedAt      time.Time       `json:"created_at"`
	StartedAt      *time.Time      `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
}

const retestCols = `id, finding_id, conversation_id, status, verdict, notes, summary, evidence, error, created_at, started_at, finished_at`

// ActiveFindingRetest is the small status payload polled by the findings list.
// Finding IDs use the same string representation as the findings API.
type ActiveFindingRetest struct {
	ID             int64  `json:"id"`
	FindingID      int64  `json:"finding_id,string"`
	ConversationID int64  `json:"conversation_id"`
	Status         string `json:"status"`
}

func (d *DB) ListActiveFindingRetests(ctx context.Context) ([]ActiveFindingRetest, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, finding_id, conversation_id, status FROM finding_retests
	WHERE status IN ('pending','running') AND conversation_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ActiveFindingRetest{}
	for rows.Next() {
		var item ActiveFindingRetest
		if err := rows.Scan(&item.ID, &item.FindingID, &item.ConversationID, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanRetest(row interface{ Scan(...any) error }) (*FindingRetest, error) {
	r := &FindingRetest{}
	err := row.Scan(&r.ID, &r.FindingID, &r.ConversationID, &r.Status, &r.Verdict, &r.Notes,
		&r.Summary, &r.Evidence, &r.Error, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// CreateFindingRetest atomically snapshots the source, creates its conversation
// and persists the first message. A finding row lock deduplicates simultaneous
// clicks across clients; an existing active run is returned without dispatching.
func (d *DB) CreateFindingRetest(ctx context.Context, findingID int64, notes string) (*FindingRetest, *Conversation, bool, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	var title string
	var snapshot []byte
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(f.name,''), NULLIF(f.vulnclass,''), 'Uncategorized'),
	jsonb_build_object('finding', to_jsonb(f),
	 'assets', COALESCE((SELECT jsonb_agg(to_jsonb(a)) FROM assets a WHERE f.asset_ids @> to_jsonb(ARRAY[a.id])), '[]'::jsonb),
	 'constraints', COALESCE((SELECT jsonb_agg(to_jsonb(c)) FROM task_constraints c JOIN tasks t ON t.exploration_id=c.exploration_id WHERE t.id=f.task_id), '[]'::jsonb))
	FROM findings f WHERE f.id=$1 FOR UPDATE OF f`, findingID).Scan(&title, &snapshot)
	if err != nil {
		return nil, nil, false, err
	}
	r, err := scanRetest(tx.QueryRowContext(ctx, `SELECT `+retestCols+` FROM finding_retests WHERE finding_id=$1 AND status IN ('pending','running')`, findingID))
	if err != nil {
		return nil, nil, false, err
	}
	if r != nil {
		return r, nil, false, nil
	}
	// Keep the title within the same limit as ordinary conversations.
	if runes := []rune(title); len(runes) > 100 {
		title = string(runes[:100])
	}
	c, err := scanConv(tx.QueryRowContext(ctx, `INSERT INTO conversations(agent_key,title) VALUES ($1,$2) RETURNING `+convCols,
		FindingRetestAgentKey, fmt.Sprintf("Retest #%d · %s", findingID, title)))
	if err != nil {
		return nil, nil, false, err
	}
	r, err = scanRetest(tx.QueryRowContext(ctx, `INSERT INTO finding_retests(finding_id,conversation_id,notes,snapshot) VALUES ($1,$2,$3,$4) RETURNING `+retestCols,
		findingID, c.ID, strings.TrimSpace(notes), snapshot))
	if err != nil {
		return nil, nil, false, err
	}
	msg := r.InitialMessage()
	_, err = tx.ExecContext(ctx, `INSERT INTO conversation_activities(conversation_id,worker,kind,summary,detail) VALUES ($1,$2,'user',$3,$4)`,
		c.ID, FindingRetestAgentKey, fmt.Sprintf("Retest finding #%d", findingID), msg)
	if err != nil {
		return nil, nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return r, &c, true, nil
}

func (r *FindingRetest) InitialMessage() string {
	msg := fmt.Sprintf("Retest finding #%d. First call get_finding_retest_context to read the original evidence and constraints linked to this session. Then perform targeted validation and call record_finding_retest_result to save your conclusion.", r.FindingID)
	if r.Notes != "" {
		msg += "\n\nAdditional instructions for this retest:\n" + r.Notes
	}
	return msg
}

func (d *DB) ListFindingRetests(findingID int64) ([]*FindingRetest, error) {
	rows, err := d.Query(`SELECT `+retestCols+` FROM finding_retests WHERE finding_id=$1 ORDER BY id DESC`, findingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*FindingRetest{}
	for rows.Next() {
		r, err := scanRetest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) FindingRetestForConversation(ctx context.Context, conversationID int64) (*FindingRetest, error) {
	r, err := scanRetest(d.QueryRowContext(ctx, `SELECT `+retestCols+` FROM finding_retests WHERE conversation_id=$1`, conversationID))
	if err != nil || r == nil {
		return r, err
	}
	err = d.QueryRowContext(ctx, `SELECT snapshot FROM finding_retests WHERE id=$1`, r.ID).Scan(&r.Snapshot)
	return r, err
}

// FailPendingRetestForConversation seals a conversation's unfinished retest when
// the runner could not even load it — the retest ID is unknown on that path, so
// the conversation ID is the only handle. Without it a transient read error
// leaves the row 'pending' forever: the findings list keeps showing "Retesting",
// and every later "Start Retest" request is deduped against a run that is not
// happening. Only a process restart (RecoverFindingRetests) can clear it.
func (d *DB) FailPendingRetestForConversation(conversationID int64, reason string) error {
	_, err := d.Exec(`UPDATE finding_retests SET status='failed', error=$2, finished_at=now()
		WHERE conversation_id=$1 AND status IN ('pending','running')`, conversationID, reason)
	return err
}

func (d *DB) StartFindingRetest(ctx context.Context, id int64) (bool, error) {
	res, err := d.ExecContext(ctx, `UPDATE finding_retests SET status='running', started_at=now() WHERE id=$1 AND status='pending'`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// RecordFindingRetestResult never accepts a finding ID: ownership comes from the
// runtime conversation. Identical retries are safe; a second verdict is refused.
func (d *DB) RecordFindingRetestResult(ctx context.Context, conversationID int64, verdict, summary, evidence string) error {
	if verdict != "reproduced" && verdict != "fixed" && verdict != "inconclusive" {
		return errors.New("verdict must be reproduced, fixed, or inconclusive")
	}
	summary, evidence = strings.TrimSpace(summary), strings.TrimSpace(evidence)
	if summary == "" || evidence == "" {
		return errors.New("summary and evidence cannot be empty; if inconclusive, describe the checks performed and what prevented a conclusion")
	}
	if len(summary) > 16000 || len(evidence) > 128000 {
		return errors.New("retest result is too long (summary ≤ 16 KB, evidence ≤ 128 KB)")
	}
	res, err := d.ExecContext(ctx, `UPDATE finding_retests SET verdict=$2,summary=$3,evidence=$4
	WHERE conversation_id=$1 AND status='running' AND (verdict='' OR (verdict=$2 AND summary=$3 AND evidence=$4))`, conversationID, verdict, summary, evidence)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrRetestNotRunning
	}
	return nil
}

// FinishFindingRetest seals the result. Cancellation/failure takes precedence
// over a staged verdict so an interrupted test cannot appear successfully fixed.
// Only a newly completed fixed verdict updates triage, in the same transaction.
func (d *DB) FinishFindingRetest(id int64, status, reason string) error {
	if status != "completed" && status != "failed" && status != "stopped" {
		return errors.New("invalid terminal retest status")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Lock the finding before the retest, matching creation and cascading deletion.
	var findingID int64
	err = tx.QueryRow(`SELECT f.id FROM findings f WHERE f.id=(SELECT finding_id FROM finding_retests WHERE id=$1) FOR UPDATE OF f`, id).Scan(&findingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Finding/retest already deleted.
	}
	if err != nil {
		return err
	}
	var finalStatus, verdict string
	err = tx.QueryRow(`UPDATE finding_retests SET
	status=CASE WHEN $2='completed' AND verdict='' THEN 'failed' ELSE $2 END,
	error=CASE WHEN $2='completed' AND verdict='' THEN '`+retestNoConclusionReason+`' ELSE $3 END,
	finished_at=now() WHERE id=$1 AND status IN ('pending','running') RETURNING status,verdict`, id, status, reason).Scan(&finalStatus, &verdict)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // A replay must not overwrite a later manual triage decision.
	}
	if err != nil {
		return err
	}
	if finalStatus == "completed" && verdict == "fixed" {
		// Use the notification-aware path so retest results follow the same
		// status-change semantics as manual edits on the finding detail page.
		// A plain UPDATE changed the status to "Fixed" without notifying
		// on_status_change channels, leaving operators unaware until they opened
		// the platform. The status change and notification event must be stored
		// together. SetFindingStatusTx also skips the event if status is unchanged.
		// Use context.Background() because this legacy function uses d.Begin(),
		// tx.QueryRow, and tx.Exec without a cancellable context. Adding a ctx
		// parameter would affect server callers and several tests.
		if _, _, _, _, err := SetFindingStatusTx(context.Background(), tx, findingID, FindingFixed); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) RecoverFindingRetests() error {
	_, err := d.Exec(`UPDATE finding_retests SET status='stopped', error='` + retestServiceRestartReason + `', finished_at=now() WHERE status IN ('pending','running')`)
	return err
}
