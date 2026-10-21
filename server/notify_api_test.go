package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// This file covers the end-to-end notification flow: persist a finding → create an event → fan out → send an HTTP request.
//
// Safety note: these tests do not call the global Notifier.step(). They call
// stepRealtime/stepDigest only for channels they create. step() visits every
// enabled channel, so on a development database with real DingTalk or WeCom
// bots configured, it could send test findings to those groups. Per-channel
// calls limit the impact to the fake receivers created by these tests.
//
// Cleanup deletes events created by each test (cascading to deliveries) and
// their channels, so no backlog remains for real channels.
//
// stepRealtime/stepDigest return no value and log internally, so assertions
// check observable behavior (what fake receivers got and delivery row states).
// This follows the real call path more closely than stubbing return values.

// notifyFixture is shared by the tests in this file.
type notifyFixture struct {
	s       *Server
	pg      *db.DB
	request func(method, path, body string) *httptest.ResponseRecorder
	n       *Notifier
	// A dedicated task/exploration stores findings separately from other tests.
	taskID int64
	expID  int64
	// Cleanup also deletes events created after cleanupMark.
	cleanupMark int64
}

func newNotifyFixture(t *testing.T) *notifyFixture {
	t.Helper()
	// All fake receivers in this file run on 127.0.0.1. Delivery rejects
	// loopback addresses by default to prevent SSRF against local services and
	// cloud metadata. This test enables local targets explicitly; ssrf_test.go
	// in the notify package covers the default rejection behavior.
	t.Setenv(notify.AllowLocalTargetsEnv, "1")
	// 백그라운드 delivery 루프(3초 tick)를 끈다. 이 파일의 케이스들은 stepRealtime·
	// stepDigest 를 직접 호출해 한 번의 분배 결과(앞 K건 송달·나머지 보류)를 검증하는데,
	// 백그라운드 루프가 같은 채널을 동시에 처리하면 집계가 타이밍에 따라 흔들려(느린 CI
	// 에서 간헐 실패) 결정성이 깨진다. 이 변수는 trafficEvidenceServer 가 서버를 세우기
	// 전에 설정돼야 효과가 있다.
	t.Setenv(notifyBackgroundDisabledEnv, "1")
	s, _, request := trafficEvidenceServer(t)
	pg := s.m.pg

	// Create a task here: the shared trafficEvidenceServer fixture does not
	// expose its task's exploration ID, which is required to record findings.
	task, err := s.m.CreateTask("通知推送测试", "推送行为验证", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Exec(`DELETE FROM tasks WHERE id=$1`, taskID) })

	var mark int64
	if err := pg.QueryRow(`SELECT COALESCE(max(id),0) FROM notification_events`).Scan(&mark); err != nil {
		t.Fatal(err)
	}
	// Isolate the fixture by marking pre-existing events as already fanned out.
	//
	// FanOutPendingEvents is global: it distributes every pending event in the
	// database to all matching channels. trafficEvidenceServer records an initial
	// finding, and other tests may leave events behind. Without isolation, these
	// stray events reach this test's channels and make delivery-count assertions
	// depend on test order.
	if _, err := pg.Exec(`UPDATE notification_events SET fanned_out = true WHERE id <= $1 AND NOT fanned_out`, mark); err != nil {
		t.Fatal(err)
	}

	f := &notifyFixture{s: s, pg: pg, request: request, n: newNotifier(s), taskID: taskID, expID: task.ExpID, cleanupMark: mark}
	t.Cleanup(func() {
		if _, err := pg.Exec(`DELETE FROM notification_events WHERE id > $1`, f.cleanupMark); err != nil {
			t.Logf("failed to clean up notification events: %v", err)
		}
	})
	// Ensure the global switch is on; other tests may have disabled it.
	if err := pg.SetBool(settingNotifyEnabled, true); err != nil {
		t.Fatal(err)
	}
	return f
}

// record persists a finding through the real evidence path and returns its ID.
// That path registers the notification event in the same transaction.
func (f *notifyFixture) record(t *testing.T, vulnclass, severity string) int64 {
	t.Helper()
	out, err := f.s.evidenceStore().Record(context.Background(), db.RecordFindingInput{
		TaskID:        f.taskID,
		ExplorationID: f.expID,
		Worker:        "test",
		VulnClass:     vulnclass,
		Name:          vulnclass,
		Severity:      severity,
		Summary:       vulnclass + " 的摘要",
		Evidence:      "poc",
	}, nil)
	if err != nil {
		t.Fatalf("failed to record finding: %v", err)
	}
	return out.FindingID
}

// channel reads the configuration for per-channel stepX calls.
func (f *notifyFixture) channel(t *testing.T, id int64) *db.NotificationChannel {
	t.Helper()
	ch, err := f.pg.NotificationChannelByID(context.Background(), id)
	if err != nil {
		t.Fatalf("failed to read channel: %v", err)
	}
	return ch
}

// deliver fans out events and runs one delivery round for the given channel.
func (f *notifyFixture) deliver(t *testing.T, chID int64, baseURL string) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := f.pg.FanOutPendingEvents(ctx, 500); err != nil {
		t.Fatalf("failed to fan out events: %v", err)
	}
	f.n.stepRealtime(ctx, f.channel(t, chID), 50, baseURL)
}

// createChannel uses the HTTP API so the tests also exercise API validation.
func (f *notifyFixture) createChannel(t *testing.T, payload map[string]any) int64 {
	t.Helper()
	raw, _ := json.Marshal(payload)
	r := f.request("POST", "/api/notify/channels", string(raw))
	if r.Code != 200 {
		t.Fatalf("failed to create channel (%d): %s", r.Code, r.Body)
	}
	var res struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &res); err != nil || res.ID == 0 {
		t.Fatalf("unexpected create-channel response: %s (%v)", r.Body, err)
	}
	t.Cleanup(func() { f.pg.Exec(`DELETE FROM notification_channels WHERE id=$1`, res.ID) })
	return res.ID
}

// fakeWebhook records the request bodies it receives.
type fakeWebhook struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
}

func newFakeWebhook(t *testing.T) *fakeWebhook {
	t.Helper()
	f := &fakeWebhook{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeWebhook) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *fakeWebhook) body(t *testing.T, i int) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.bodies) {
		t.Fatalf("fake receiver got only %d requests; cannot access request %d", len(f.bodies), i)
	}
	return f.bodies[i]
}

func (f *fakeWebhook) last(t *testing.T) map[string]any {
	t.Helper()
	if f.count() == 0 {
		t.Fatal("fake receiver did not receive any requests")
	}
	return f.body(t, f.count()-1)
}

// markdownText extracts message text despite provider-specific field names:
// DingTalk markdown and ActionCard use `text`; WeCom markdown uses `content`.
func markdownText(t *testing.T, body map[string]any) string {
	t.Helper()
	for _, key := range []string{"markdown", "actionCard"} {
		section, ok := body[key].(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"text", "content"} {
			if s, ok := section[field].(string); ok && s != "" {
				return s
			}
		}
	}
	t.Fatalf("request body has no recognized message text: %v", body)
	return ""
}

// agePendingBatch makes this channel's pending deliveries old enough for a digest.
func (f *notifyFixture) agePendingBatch(t *testing.T, chID int64) {
	t.Helper()
	if _, err := f.pg.Exec(`UPDATE notification_deliveries SET created_at = now() - interval '2 hours'
WHERE channel_id=$1 AND state=$2`, chID, db.NotifyStatePending); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyEndToEndRealtimeDelivery(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "实时推送",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
	})
	f.record(t, "SQL注入", "high")
	f.deliver(t, chID, "")

	if hook.count() != 1 {
		t.Fatalf("expected 1 message, got %d", hook.count())
	}
	text := markdownText(t, hook.last(t))
	// "SQL注入" 은 입력으로 넣은 취약점 제목이라 카드에 그대로 에코된다(사용자 데이터,
	// 번역 대상 아님). "높음"·"개요" 는 렌더 라벨이다 — 심각도는 notify.SeverityLabel("high")
	// 가 내는 "🟠 높음", 요약 머리글은 writeItem 이 붙이는 "**개요**:" 에 각각 들어 있다.
	for _, want := range []string{"SQL注入", "높음", "개요"} {
		if !strings.Contains(text, want) {
			t.Fatalf("message body is missing %q:\n%s", want, text)
		}
	}
	// The delivery should move to sent.
	var pending int
	if err := f.pg.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE channel_id=$1 AND state <> $2`,
		chID, db.NotifyStateSent).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("%d deliveries were not marked sent", pending)
	}
}

func TestNotifyChannelAPIMasksSecretsAndPreservesOnUpdate(t *testing.T) {
	f := newNotifyFixture(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "掩码用例",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": "https://oapi.dingtalk.com/robot/send?access_token=abc123456", "secret": "SECabcdef123456"},
	})

	r := f.request("GET", "/api/notify/channels", "")
	if r.Code != 200 {
		t.Fatalf("failed to list channels (%d): %s", r.Code, r.Body)
	}
	if strings.Contains(r.Body.String(), "abc123456") || strings.Contains(r.Body.String(), "SECabcdef123456") {
		t.Fatalf("API response exposed credentials: %s", r.Body)
	}
	var listed struct {
		Channels []struct {
			ID         int64          `json:"id"`
			Config     map[string]any `json:"config"`
			SecretKeys []string       `json:"secret_keys"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	var mine *struct {
		ID         int64          `json:"id"`
		Config     map[string]any `json:"config"`
		SecretKeys []string       `json:"secret_keys"`
	}
	for i := range listed.Channels {
		if listed.Channels[i].ID == chID {
			mine = &listed.Channels[i]
		}
	}
	if mine == nil {
		t.Fatal("new channel did not appear in the list")
	}
	if !notify.IsMasked(fmt.Sprint(mine.Config["webhook"])) || !notify.IsMasked(fmt.Sprint(mine.Config["secret"])) {
		t.Fatalf("credential fields should be masked: %v", mine.Config)
	}
	if len(mine.SecretKeys) == 0 {
		t.Fatal("API should identify credential fields for the frontend")
	}

	// PATCH changes the name while echoing masked credentials; real credentials must remain intact.
	body, _ := json.Marshal(map[string]any{
		"name":   "改名后",
		"config": map[string]any{"webhook": fmt.Sprint(mine.Config["webhook"]), "secret": fmt.Sprint(mine.Config["secret"])},
	})
	if r := f.request("PATCH", fmt.Sprintf("/api/notify/channels/%d", chID), string(body)); r.Code != 200 {
		t.Fatalf("failed to update channel (%d): %s", r.Code, r.Body)
	}
	cfg := f.channelConfig(t, chID)
	if cfg["webhook"] != "https://oapi.dingtalk.com/robot/send?access_token=abc123456" {
		t.Fatalf("echoing a masked value overwrote the real credential: %v", cfg["webhook"])
	}
	if cfg["secret"] != "SECabcdef123456" {
		t.Fatalf("echoing a masked value overwrote the secret: %v", cfg["secret"])
	}
	if f.channel(t, chID).Name != "改名后" {
		t.Fatal("channel name was not updated")
	}

	// Explicitly clearing the secret should work; echoing a mask preserves it.
	body, _ = json.Marshal(map[string]any{"config": map[string]any{"secret": ""}})
	if r := f.request("PATCH", fmt.Sprintf("/api/notify/channels/%d", chID), string(body)); r.Code != 200 {
		t.Fatalf("failed to clear secret (%d): %s", r.Code, r.Body)
	}
	if _, still := f.channelConfig(t, chID)["secret"]; still {
		t.Fatal("an empty string should clear the secret")
	}
}

func (f *notifyFixture) channelConfig(t *testing.T, id int64) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal(f.channel(t, id).Config, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNotifyChannelAPICreateValidation(t *testing.T) {
	f := newNotifyFixture(t)
	cases := []struct {
		name    string
		payload map[string]any
		wantSub string
	}{
		// wantSub 는 server/notify_api.go 가 내보내는 한국어 검증 오류와 일치시킨다
		// (notifyErrKindInvalidFmt·notifyErrNameMissing·notifyErrModeInvalid 의 안정 문구).
		// webhook 두 건은 notify/dingtalk.go 의 Validate 가 내는 오류인데, 그 webhook 주소
		// 검증 문구는 이미 한국어로 현지화됐으므로(notify/dingtalk_feishu_wecom_localized_test.go
		// 가 "Webhook 주소가 없습니다"·"Webhook 주소가 올바르지 않습니다" 로 검증한다) 기대
		// 문자열도 한국어 안정 문구로 맞춘다.
		{"invalid kind", map[string]any{"name": "x", "kind": "nope", "config": map[string]any{}}, "채널 유형이 올바르지 않습니다"},
		{"missing name", map[string]any{"kind": notify.KindDingTalk, "config": map[string]any{"webhook": "https://e.com/h"}}, "채널 이름을 입력하세요"},
		{"missing webhook", map[string]any{"name": "x", "kind": notify.KindDingTalk, "config": map[string]any{}}, "Webhook 주소가 없습니다"},
		{"invalid webhook scheme", map[string]any{"name": "x", "kind": notify.KindDingTalk, "config": map[string]any{"webhook": "file:///etc/passwd"}}, "Webhook 주소가 올바르지 않습니다"},
		{"invalid mode", map[string]any{"name": "x", "kind": notify.KindDingTalk, "mode": "sometimes", "config": map[string]any{"webhook": "https://e.com/h"}}, "발송 모드가 올바르지 않습니다"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.payload)
			r := f.request("POST", "/api/notify/channels", string(raw))
			if r.Code != 400 {
				t.Fatalf("expected 400, got %d: %s", r.Code, r.Body)
			}
			if !strings.Contains(r.Body.String(), tc.wantSub) {
				t.Fatalf("error should mention %q, got %s", tc.wantSub, r.Body)
			}
		})
	}
	if r := f.request("DELETE", "/api/notify/channels/99999999", ""); r.Code != 404 {
		t.Fatalf("deleting a nonexistent channel should return 404, got %d", r.Code)
	}
}

func TestNotifyFilterBlocksBelowThreshold(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "仅严重",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
		"filter": map[string]any{"min_severity": "critical"},
	})
	f.record(t, "低危问题", "low")
	if _, _, err := f.pg.FanOutPendingEvents(context.Background(), 500); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.pg.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE channel_id=$1`, chID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("finding below the threshold should produce no deliveries, got %d", n)
	}
	f.n.stepRealtime(context.Background(), f.channel(t, chID), 50, "")
	if hook.count() != 0 {
		t.Fatal("filtered finding should not send a message")
	}
}

func TestNotifyDigestBatchesMultipleFindingsIntoOneMessage(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "汇总推送",
		"kind":   notify.KindDingTalk,
		"mode":   db.NotifyModeDigest,
		"config": map[string]any{"webhook": hook.URL},
	})
	for i := 0; i < 3; i++ {
		f.record(t, fmt.Sprintf("汇总漏洞%d", i+1), "high")
	}
	ctx := context.Background()
	if _, _, err := f.pg.FanOutPendingEvents(ctx, 500); err != nil {
		t.Fatal(err)
	}
	ch := f.channel(t, chID)

	// Do not send before the digest batch expires.
	f.n.stepDigest(ctx, ch, 50, "")
	if hook.count() != 0 {
		t.Fatal("digest batch was sent before it expired")
	}

	// After aging the batch, combine three findings into one message.
	f.agePendingBatch(t, chID)
	f.n.stepDigest(ctx, ch, 50, "")
	if got := hook.count(); got != 1 {
		t.Fatalf("expected one digest message for three findings, got %d", got)
	}
	text := markdownText(t, hook.last(t))
	// 시간창이 있는 다이제스트 머리말은 markdown.go 가 "**최근 N분간 신규 취약점 N건**"
	// 으로 렌더한다(digestInterval 기본 30분이라 WindowMinutes>0).
	if !strings.Contains(text, "최근") || !strings.Contains(text, "신규 취약점 3건") {
		t.Fatalf("digest message is missing the count or time window:\n%s", text)
	}
	for i := 1; i <= 3; i++ {
		if !strings.Contains(text, fmt.Sprintf("汇总漏洞%d", i)) {
			t.Fatalf("digest message is missing finding %d:\n%s", i, text)
		}
	}
	// All deliveries in one batch should share a batch_id.
	var distinct, total int
	if err := f.pg.QueryRow(`SELECT count(DISTINCT batch_id), count(*) FROM notification_deliveries WHERE channel_id=$1`, chID).Scan(&distinct, &total); err != nil {
		t.Fatal(err)
	}
	if total != 3 || distinct != 1 {
		t.Fatalf("three deliveries should share one batch_id, got distinct=%d total=%d", distinct, total)
	}
}

func TestNotifyDisabledChannelDoesNotSend(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":    "停用渠道",
		"kind":    notify.KindDingTalk,
		"enabled": false,
		"config":  map[string]any{"webhook": hook.URL},
	})
	f.record(t, "停用期间的漏洞", "critical")
	if _, _, err := f.pg.FanOutPendingEvents(context.Background(), 500); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.pg.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE channel_id=$1`, chID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("disabled channel should produce no deliveries, got %d", n)
	}
}

func TestNotifyStatusChangeDelivery(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "状态变更订阅",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
		"filter": map[string]any{"on_status_change": true},
	})
	finding := f.record(t, "状态变更用例", "high")
	r := f.request("PATCH", fmt.Sprintf("/api/exploration/findings/%d", finding), `{"status":"fixed"}`)
	if r.Code != 200 {
		t.Fatalf("failed to update status (%d): %s", r.Code, r.Body)
	}
	f.deliver(t, chID, "")

	// There may be two messages: the fixed event records the status change, and
	// finding_created may be sent in the same round. Search all messages instead
	// of relying on their creation order.
	found := false
	for i := 0; i < hook.count(); i++ {
		text := markdownText(t, hook.body(t, i))
		// 상태 변경 카드는 markdown.go 가 "**상태 변경**: %s → %s" 로, 상태값은
		// notify.StatusLabel 이 렌더한다(fixed → "수정됨").
		if strings.Contains(text, "상태 변경") && strings.Contains(text, "수정됨") {
			found = true
		}
	}
	if !found {
		t.Fatalf("did not receive a status-change-to-fixed message among %d messages", hook.count())
	}
}

func TestNotifyStatusChangeSuppressedByDefault(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "不订阅状态变更",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
	})
	finding := f.record(t, "不订阅变更", "high")
	if r := f.request("PATCH", fmt.Sprintf("/api/exploration/findings/%d", finding), `{"status":"false_positive"}`); r.Code != 200 {
		t.Fatalf("failed to update status (%d): %s", r.Code, r.Body)
	}
	if _, _, err := f.pg.FanOutPendingEvents(context.Background(), 500); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.pg.QueryRow(`SELECT count(*) FROM notification_deliveries d
JOIN notification_events e ON e.id = d.event_id
WHERE d.channel_id=$1 AND e.kind=$2`, chID, notify.EventFindingStatusChanged).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("channel without status-change subscription should get no status-change deliveries, got %d", n)
	}
}

func TestNotifyTestMessageEndpoint(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "测试发送",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
	})
	if r := f.request("POST", fmt.Sprintf("/api/notify/channels/%d/test", chID), ""); r.Code != 200 {
		t.Fatalf("failed to send test message (%d): %s", r.Code, r.Body)
	}
	if hook.count() != 1 {
		t.Fatalf("fake receiver should get 1 test message, got %d", hook.count())
	}
	// A test message must be clearly marked so it cannot be mistaken for a real finding.
	// 테스트 메시지 제목은 notify_api.go 의 notifyTestName("테스트 메시지 · 채널 설정 정상").
	if text := markdownText(t, hook.last(t)); !strings.Contains(text, "테스트") {
		t.Fatalf("test message should identify itself as a test: %s", text)
	}
	// Return the channel's original error when its configuration is broken.
	badID := f.createChannel(t, map[string]any{
		"name":   "坏地址",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": "http://127.0.0.1:1/hook"},
	})
	if r := f.request("POST", fmt.Sprintf("/api/notify/channels/%d/test", badID), ""); r.Code != 502 {
		t.Fatalf("failed delivery should return 502, got %d: %s", r.Code, r.Body)
	}
}

func TestNotifyDeliveriesHistoryAndRetry(t *testing.T) {
	f := newNotifyFixture(t)
	// Point to an address that will fail to produce a failed delivery.
	chID := f.createChannel(t, map[string]any{
		"name":   "失败重试",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": "http://127.0.0.1:1/hook"},
	})
	f.record(t, "会失败的推送", "high")
	ctx := context.Background()
	if _, _, err := f.pg.FanOutPendingEvents(ctx, 500); err != nil {
		t.Fatal(err)
	}
	ch := f.channel(t, chID)
	// Continue attempting delivery until the retry budget is exhausted.
	for i := 0; i < db.MaxNotifyAttempts; i++ {
		f.n.stepRealtime(ctx, ch, 50, "")
		if _, err := f.pg.Exec(`UPDATE notification_deliveries SET next_attempt_at = now() - interval '1 minute' WHERE channel_id=$1`, chID); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	if err := f.pg.QueryRow(`SELECT state FROM notification_deliveries WHERE channel_id=$1`, chID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != db.NotifyStateFailed {
		t.Fatalf("expected failed after exhausting retries, got %s", state)
	}

	r := f.request("GET", fmt.Sprintf("/api/notify/deliveries?channel_id=%d&state=failed", chID), "")
	if r.Code != 200 {
		t.Fatalf("failed to fetch delivery history (%d): %s", r.Code, r.Body)
	}
	var hist struct {
		Deliveries []struct {
			ID        int64  `json:"id"`
			State     string `json:"state"`
			LastError string `json:"last_error"`
			Attempts  int    `json:"attempts"`
			Title     string `json:"title"`
		} `json:"deliveries"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &hist); err != nil {
		t.Fatal(err)
	}
	if hist.Total != 1 || len(hist.Deliveries) != 1 {
		t.Fatalf("expected 1 failed delivery, got total=%d len=%d", hist.Total, len(hist.Deliveries))
	}
	if hist.Deliveries[0].LastError == "" {
		t.Fatal("delivery history should include the error so users can diagnose the failure")
	}
	if hist.Deliveries[0].Attempts < db.MaxNotifyAttempts {
		t.Fatalf("attempt count should be recorded, got %d", hist.Deliveries[0].Attempts)
	}
	if hist.Deliveries[0].Title != "会失败的推送" {
		t.Fatalf("delivery history should include the finding title, got %q", hist.Deliveries[0].Title)
	}

	// Manual retry should restore pending state and reset the attempt count.
	if r := f.request("POST", fmt.Sprintf("/api/notify/deliveries/%d/retry", hist.Deliveries[0].ID), ""); r.Code != 200 {
		t.Fatalf("failed to retry delivery (%d): %s", r.Code, r.Body)
	}
	var attempts int
	if err := f.pg.QueryRow(`SELECT state, attempts FROM notification_deliveries WHERE id=$1`, hist.Deliveries[0].ID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != db.NotifyStatePending || attempts != 0 {
		t.Fatalf("expected pending with attempts=0 after retry, got %s/%d", state, attempts)
	}
}

func TestNotifyMetaAndSettingsRoundTrip(t *testing.T) {
	f := newNotifyFixture(t)
	r := f.request("GET", "/api/notify/meta", "")
	if r.Code != 200 {
		t.Fatalf("failed to fetch metadata: %s", r.Body)
	}
	var meta struct {
		Kinds []struct {
			Kind       string   `json:"kind"`
			SecretKeys []string `json:"secret_keys"`
		} `json:"kinds"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Kinds) != len(notify.Kinds()) {
		t.Fatalf("metadata should list all %d channel kinds, got %d", len(notify.Kinds()), len(meta.Kinds))
	}
	for _, k := range meta.Kinds {
		if len(k.SecretKeys) == 0 {
			t.Errorf("channel kind %s did not report credential fields", k.Kind)
		}
	}

	// Round-trip three global settings. Strip the trailing slash from the base
	// URL so deep links do not contain "//redteam/...".
	if r := f.request("PUT", "/api/settings", `{"notify_public_base_url":"https://artex.example.com/","notify_digest_interval_min":15,"notify_enabled":true}`); r.Code != 200 {
		t.Fatalf("failed to update settings (%d): %s", r.Code, r.Body)
	}
	t.Cleanup(func() {
		f.pg.Exec(`DELETE FROM settings WHERE key IN ($1,$2)`, settingNotifyPublicBaseURL, settingNotifyDigestMinutes)
	})
	payload := f.s.settingsPayload()
	if payload["notify_public_base_url"] != "https://artex.example.com" {
		t.Fatalf("deep-link base URL was not normalized: %v", payload["notify_public_base_url"])
	}
	if payload["notify_digest_interval_min"] != 15 {
		t.Fatalf("digest interval did not take effect: %v", payload["notify_digest_interval_min"])
	}

	// Invalid values should be rejected.
	for _, body := range []string{
		`{"notify_public_base_url":"ftp://x"}`,
		`{"notify_digest_interval_min":0}`,
		`{"notify_digest_interval_min":99999}`,
	} {
		if r := f.request("PUT", "/api/settings", body); r.Code != 400 {
			t.Errorf("%s should return 400, got %d", body, r.Code)
		}
	}
}

// TestNotifyDeepLinkUsesPublicBaseURL checks deep-link construction: with a
// public_base_url, a single message uses an ActionCard button linking to the finding.
func TestNotifyDeepLinkUsesPublicBaseURL(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "回链",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
	})
	finding := f.record(t, "带回链的漏洞", "high")
	f.deliver(t, chID, "https://artex.example.com")

	body := hook.last(t)
	card, _ := body["actionCard"].(map[string]any)
	if card == nil {
		t.Fatalf("expected ActionCard with a deep link, got msgtype=%v", body["msgtype"])
	}
	want := fmt.Sprintf("https://artex.example.com/redteam/findings/detail?id=%d", finding)
	if card["singleURL"] != want {
		t.Fatalf("wrong deep link\nwant %s\ngot %v", want, card["singleURL"])
	}
}

// TestNotifyNoDeepLinkWithoutBaseURL checks that missing public_base_url
// produces plain markdown instead of a broken localhost or relative link.
func TestNotifyNoDeepLinkWithoutBaseURL(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	chID := f.createChannel(t, map[string]any{
		"name":   "无回链",
		"kind":   notify.KindDingTalk,
		"config": map[string]any{"webhook": hook.URL},
	})
	f.record(t, "无回链的漏洞", "high")
	f.deliver(t, chID, "")

	body := hook.last(t)
	if body["msgtype"] != "markdown" {
		t.Fatalf("expected markdown without a public base URL, got %v", body["msgtype"])
	}
	// markdown.go renders the detail link as "[상세 보기](URL)". Without a
	// public base URL, it should omit the link entirely. Check the current Korean
	// label so this test detects a regression.
	if text := markdownText(t, body); strings.Contains(text, "상세 보기") {
		t.Fatalf("detail link should not appear without a public base URL:\n%s", text)
	}
}

// TestNotifyDigestSegmentsAndDefersRemainder is an end-to-end regression test for silent loss.
//
// Digest messages are limited by the channel's length cap (4096 bytes for WeCom).
// If a batch does not fit, split at item boundaries: mark only included items
// as sent and return the rest to the queue. A previous implementation marked
// the entire batch as sent, silently losing truncated findings while delivery
// history still reported success.
//
// Assert that only included items are sent, the rest remain pending, deferred
// items do not consume retries, and later rounds send the rest without stalling.
func TestNotifyDigestSegmentsAndDefersRemainder(t *testing.T) {
	f := newNotifyFixture(t)
	hook := newFakeWebhook(t)
	// Use WeCom: its 4096-byte markdown limit is the smallest of the six channels.
	chID := f.createChannel(t, map[string]any{
		"name":   "分段汇总",
		"kind":   notify.KindWeCom,
		"mode":   db.NotifyModeDigest,
		"config": map[string]any{"webhook": hook.URL},
	})
	const total = 60
	// Use longer titles so 60 items exceed 4096 bytes and require segmentation.
	longName := strings.Repeat("超长漏洞名称", 6)
	for i := 0; i < total; i++ {
		f.record(t, longName+strconv.Itoa(i+1), "high")
	}
	ctx := context.Background()
	if _, _, err := f.pg.FanOutPendingEvents(ctx, 500); err != nil {
		t.Fatal(err)
	}
	f.agePendingBatch(t, chID)
	ch := f.channel(t, chID)

	f.n.stepDigest(ctx, ch, 50, "")
	if hook.count() != 1 {
		t.Fatalf("expected exactly one message, got %d", hook.count())
	}

	var sent, pending int
	if err := f.pg.QueryRow(`SELECT
    count(*) FILTER (WHERE state=$2),
    count(*) FILTER (WHERE state=$3)
  FROM notification_deliveries WHERE channel_id=$1`, chID, db.NotifyStateSent, db.NotifyStatePending).
		Scan(&sent, &pending); err != nil {
		t.Fatal(err)
	}
	if sent == 0 {
		t.Fatal("expected some items to be marked sent")
	}
	if pending == 0 {
		t.Fatalf("a batch of %d items cannot fit in 4096 bytes; expected pending items; sent=%d", total, sent)
	}
	if sent+pending != total {
		t.Fatalf("item count mismatch: sent=%d pending=%d total=%d (items neither sent nor pending were lost)", sent, pending, total)
	}
	// The message body must accurately state how many items it omitted.
	// 분절 안내는 markdown.go 가 "(이 메시지에는 앞 N건만 … 나머지 N건은 다음 메시지에서 …)" 로 렌더한다.
	if text := markdownText(t, hook.last(t)); !strings.Contains(text, "나머지") {
		t.Fatalf("message should mention that some items were omitted:\n%.400s", text)
	}

	// Deferred items must not consume retry budget: claiming optimistically
	// increments attempts, so deferring must undo that increment.
	var maxAttempts int
	if err := f.pg.QueryRow(`SELECT COALESCE(max(attempts),0) FROM notification_deliveries
WHERE channel_id=$1 AND state=$2`, chID, db.NotifyStatePending).Scan(&maxAttempts); err != nil {
		t.Fatal(err)
	}
	if maxAttempts > 0 {
		t.Fatalf("deferred items must not consume retries (or they will fail after a few rounds); got attempts=%d", maxAttempts)
	}

	// Run until all items are delivered and verify it took multiple rounds.
	// This proves segmentation neither stalls nor loses the remaining items.
	rounds := 0
	for {
		var undelivered int
		if err := f.pg.QueryRow(`SELECT count(*) FROM notification_deliveries
WHERE channel_id=$1 AND state <> $2 AND state <> $3`, chID, db.NotifyStateSent, db.NotifyStateFailed).
			Scan(&undelivered); err != nil {
			t.Fatal(err)
		}
		if undelivered == 0 {
			break
		}
		rounds++
		if rounds > total+5 {
			t.Fatalf("segmented delivery did not converge: %d rounds left %d unresolved items", rounds, undelivered)
		}
		before := hook.count()
		f.n.stepDigest(ctx, ch, 50, "")
		if hook.count() == before {
			t.Fatalf("round %d made no progress; %d items would remain stuck", rounds, undelivered)
		}
	}
	if rounds < 2 {
		t.Fatalf("a 4096-byte message cannot fit %d long finding titles; expected multiple rounds, got %d", total, rounds)
	}
	// Later rounds should only send deferred items, with none rejected by the channel.
	var failed int
	if err := f.pg.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE channel_id=$1 AND state=$2`,
		chID, db.NotifyStateFailed).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Fatalf("fake receiver always succeeds; expected no failed items, got %d", failed)
	}
}

// TestNotifyBackoffTableMatchesAttemptBudget guards against configuration drift.
//
// The retry budget (db.MaxNotifyAttempts) and backoff schedule (notifyBackoff)
// live in different packages. Changing only one can silently reuse the final
// interval for later attempts and slow retries. Compare their lengths so CI
// catches this drift.
func TestNotifyBackoffTableMatchesAttemptBudget(t *testing.T) {
	if len(notifyBackoff) != db.MaxNotifyAttempts {
		t.Fatalf("backoff tier count (%d) differs from max attempts (%d); update both together",
			len(notifyBackoff), db.MaxNotifyAttempts)
	}
	// Backoff intervals must be nondecreasing, or retries worsen rate limiting.
	for i := 1; i < len(notifyBackoff); i++ {
		if notifyBackoff[i] < notifyBackoff[i-1] {
			t.Fatalf("backoff intervals must be nondecreasing: tier %d %v < tier %d %v",
				i, notifyBackoff[i], i-1, notifyBackoff[i-1])
		}
	}
}

// TestNotifyRateLimitDoesNotConsumeRetryBudget preserves the order: take tokens,
// then claim deliveries. Reversing it would count attempts blocked by rate
// limiting, eventually exhausting the retry budget while only waiting.
func TestNotifyRateLimitDoesNotConsumeRetryBudget(t *testing.T) {
	// Test only the token bucket; no Server is needed.
	n := &Notifier{buckets: map[int64]*notifyBucket{}}
	now := time.Now()
	// At one item per minute, a full bucket holds at most one token.
	if got := n.takeTokens(1, 1, notifyMaxSendsPerChannelPerTick, now); got != 1 {
		t.Fatalf("a full bucket at one item per minute should yield one token, got %d", got)
	}
	if got := n.takeTokens(1, 1, notifyMaxSendsPerChannelPerTick, now.Add(time.Millisecond)); got != 0 {
		t.Fatalf("an exhausted bucket should immediately yield zero tokens, got %d", got)
	}
	if got := n.takeTokens(1, 1, notifyMaxSendsPerChannelPerTick, now.Add(30*time.Second)); got != 0 {
		t.Fatalf("half a refill period should not produce a full token, got %d", got)
	}
	if got := n.takeTokens(1, 1, notifyMaxSendsPerChannelPerTick, now.Add(time.Minute)); got != 1 {
		t.Fatalf("a full refill period should restore one token, got %d", got)
	}
	// Unlimited-rate channels use a finite per-tick cap to bound backlog processing.
	if got := n.takeTokens(2, 0, notifyUnlimitedBurstPerTick+10, now); got != notifyUnlimitedBurstPerTick {
		t.Fatalf("unlimited-rate channel should return the per-tick cap %d, got %d", notifyUnlimitedBurstPerTick, got)
	}
	// Token buckets for different channels are independent.
	if got := n.takeTokens(1, 1, notifyMaxSendsPerChannelPerTick, now.Add(time.Millisecond)); got != 0 {
		t.Fatalf("channel 1's bucket should still be empty, got %d", got)
	}
}

// TestNotifyTakeTokensKeepsUnusedTokens takes only the requested token count.
//
// A previous implementation drained the entire bucket before the caller
// applied its limit. A channel with a full 100/min bucket using five tokens
// lost the other 95, even when no deliveries were pending. This prevented
// sending up to rate_per_min items at once when backlogged.
func TestNotifyTakeTokensKeepsUnusedTokens(t *testing.T) {
	n := &Notifier{buckets: map[int64]*notifyBucket{}}
	now := time.Now()
	// The bucket starts full (100), but this round requests only five tokens.
	if got := n.takeTokens(1, 100, 5, now); got != 5 {
		t.Fatalf("want=5 should take exactly five tokens, got %d", got)
	}
	// The remaining 95 tokens must stay in the bucket. Keep time fixed so any
	// tokens taken come from the existing balance rather than a refill.
	if got := n.takeTokens(1, 100, 95, now); got != 95 {
		t.Fatalf("remaining tokens should be available (expected 95), got %d; bucket was drained", got)
	}
	if got := n.takeTokens(1, 100, 1, now); got != 0 {
		t.Fatalf("the bucket is empty; expected zero, got %d", got)
	}
	// want<=0 must not consume tokens.
	n2 := &Notifier{buckets: map[int64]*notifyBucket{}}
	if got := n2.takeTokens(1, 20, 0, now); got != 0 {
		t.Fatalf("want=0 should return zero, got %d", got)
	}
	if got := n2.takeTokens(1, 20, 20, now); got != 20 {
		t.Fatalf("want=0 must not consume tokens; expected all 20 to remain, got %d", got)
	}
}

// TestDigestTickPlanDecouplesBatchSizeFromSendBudget separates two digest units.
//
// If batch size follows the per-tick request budget, a channel at
// rate_per_min=20 refills one token per three-second tick. Each digest then
// contains only one finding, defeating aggregation. This produces no error,
// and tests that pass a large limit directly to stepDigest would miss it.
// Assert the tick plan itself here.
func TestDigestTickPlanDecouplesBatchSizeFromSendBudget(t *testing.T) {
	tokens, claimLimit := digestTickPlan()
	// One batch equals one message, one request, and one token. Tokens count messages.
	if tokens != 1 {
		t.Fatalf("one digest batch sends one message and should consume one token, got %d", tokens)
	}
	if claimLimit != db.MaxDigestBatchSize {
		t.Fatalf("digest batch size should equal db.MaxDigestBatchSize=%d, got %d",
			db.MaxDigestBatchSize, claimLimit)
	}
	// Batch size must be much larger than the per-tick request budget so the
	// number of messages stays distinct from the number of findings per batch.
	if claimLimit <= notifyMaxSendsPerChannelPerTick {
		t.Fatalf("digest batch size %d must not be limited by per-tick request budget %d; "+
			"request budget counts requests within a lease, not findings within a batch",
			claimLimit, notifyMaxSendsPerChannelPerTick)
	}
}

// TestNotifyTickBudgetFitsWithinLease guards against configuration drift.
//
// The per-channel send limit per tick (notifyMaxSendsPerChannelPerTick) follows
// from the lease duration. Worst-case serial send time must be shorter than
// the lease, or another instance could claim and resend later deliveries.
// These three constants live in different places, so enforce their relation here.
func TestNotifyTickBudgetFitsWithinLease(t *testing.T) {
	worst := time.Duration(notifyMaxSendsPerChannelPerTick) * notifySendTimeout
	if worst >= notifyLease {
		t.Fatalf("worst-case per-channel tick duration %v must be shorter than lease %v "+
			"(notifyMaxSendsPerChannelPerTick=%d * notifySendTimeout=%v); "+
			"check the other two constants whenever one of these three changes",
			worst, notifyLease, notifyMaxSendsPerChannelPerTick, notifySendTimeout)
	}
}
