package db

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestBlueLLMRecordsAreScopedBeforePaginationAndDetail(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	if err := d.EnsureLLMRecordsTable(); err != nil {
		t.Fatal(err)
	}

	model := "blue-record-scope-" + uuid.NewString()
	defer func() {
		if _, err := d.Exec(`DELETE FROM llm_records WHERE model=$1`, model); err != nil {
			t.Logf("cleanup: %v", err)
		}
	}()
	sessions := []string{"exp78-main", "blue-case-12-analysis-34", "blue-chat-56"}
	ids := make([]int64, len(sessions))
	for i, session := range sessions {
		if err := d.InsertLLMRecord(&LLMRecord{Model: model, SessionID: session, Status: "ok", RequestBody: session}); err != nil {
			t.Fatal(err)
		}
		if err := d.QueryRow(`SELECT id FROM llm_records WHERE model=$1 AND session_id=$2`, model, session).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}

	first, total, err := d.ListBlueLLMRecords(model, "", 0, 1)
	if err != nil || total != 2 || len(first) != 1 || first[0].SessionID != sessions[2] {
		t.Fatalf("blue first page = %+v, total=%d, err=%v", first, total, err)
	}
	second, total, err := d.ListBlueLLMRecords(model, "", 1, 1)
	if err != nil || total != 2 || len(second) != 1 || second[0].SessionID != sessions[1] {
		t.Fatalf("blue second page = %+v, total=%d, err=%v", second, total, err)
	}
	filtered, total, err := d.ListBlueLLMRecords(model, "blue-case-", 0, 10)
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != ids[1] {
		t.Fatalf("blue session filter = %+v, total=%d, err=%v", filtered, total, err)
	}
	if _, err := d.GetBlueLLMRecord(ids[0]); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("red detail through blue scope: %v", err)
	}
	got, err := d.GetBlueLLMRecord(ids[1])
	if err != nil || got.SessionID != sessions[1] || got.RequestBody != sessions[1] {
		t.Fatalf("blue detail = %+v, err=%v", got, err)
	}
	all, count, err := d.ListLLMRecords(model, "", "", 0, 10)
	if err != nil || count != 3 || len(all) != 3 {
		t.Fatalf("shared red view unexpectedly changed: count=%d len=%d err=%v", count, len(all), err)
	}
}
