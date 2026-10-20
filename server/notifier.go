package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global setting keys stored in the settings key-value table; no new table is needed.
const (
	// settingNotifyEnabled is the master switch for notifications. It defaults to on
	// and allows notifications to be stopped during maintenance. Configuring a channel
	// is what actually enables the feature.
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the external URL used for links back to finding
	// details (for example, https://artex.example.com). An empty value omits the
	// link button from messages. No reusable external URL setting exists elsewhere.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest interval in minutes.
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery engine's polling interval. Three seconds is its
	// maximum real-time delay and the main delay between saving a finding and
	// delivering a message to the IM service.
	notifyTick = 3 * time.Second
	// notifyBackgroundDisabledEnv 가 설정되면 서버가 백그라운드 delivery 루프
	// (newNotifier(s).Run)를 띄우지 않는다. 통합 테스트는 stepRealtime·stepDigest 를
	// 직접 호출해 분배 결과를 결정론적으로 검증하는데, 3 초 주기의 백그라운드 루프가
	// 같은 채널을 동시에 처리하면 집계가 타이밍에 따라 흔들린다. 이를 막기 위한
	// 테스트 전용 스위치이며, 변수가 없으면 평소대로 기동한다(프로덕션 동작 불변).
	notifyBackgroundDisabledEnv = "ARTEX_NOTIFY_BACKGROUND_DISABLED"
	// notifyLease is the lease duration when claiming deliveries. It must be much
	// longer than the worst-case duration of one send (the notify package's HTTP
	// client times out after 15 seconds), or two dispatchers may send the same row.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick limits events dispatched per tick, so enabling a channel
	// does not expand the entire historical backlog into delivery tasks at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick caps deliveries per tick for a channel without a
	// rate limit. It prevents a large backlog from blocking one tick for too long.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick caps sends per channel in one tick.
	//
	// The limit follows from the lease duration: each claimed row has a three-minute
	// lease (notifyLease). If serial sends take longer than that in the worst case,
	// later rows expire before they are sent. One process is safe because Run is a
	// single goroutine and ticks do not overlap. With two processes sharing a DB,
	// the other process could reclaim expired rows, send duplicates, increment
	// attempts twice, and mark a delivery failed while the first process is still
	// sending it.
	//
	// A three-minute lease divided by a 30-second send timeout is six sends, which
	// leaves no margin. Five sends take at most 150 seconds, leaving 30 seconds.
	// TestNotifyTickBudgetFitsWithinLease enforces this relationship among
	// notifyLease, notifySendTimeout, and this limit.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout bounds one send. Its product with the preceding limit must
	// not exceed notifyLease; see TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is the retry delay sequence indexed by attempts already made.
// The three attempts (including the first) must match db.MaxNotifyAttempts.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// 전달 실패·지연 사유. notification_deliveries.last_error 에 저장되어
// notify_api.go 의 전달 이력 응답을 거쳐 알림 페이지 「전달 기록」 표(delivery-list.tsx)에
// 그대로 표시된다. 따라서 사용자 노출 문구라 한국어로 둔다(진단 로그는 영어로 둔다).
const (
	errDeliveryChannelKindUnregistered = "등록되지 않은 채널 유형입니다: %q"
	errDeliverySnapshotUnrenderable    = "이벤트 스냅샷을 해석하지 못해 이 취약점을 메시지로 만들지 못했습니다"
	errDeliveryChannelLengthCapped     = "이 메시지가 채널 길이 상한에 도달해 앞의 %d건만 전송했고, 나머지는 다음 배치로 미룹니다"
	errDeliveryNoDeliveredCount        = "채널이 전송 건수를 보고하지 않았습니다(delivered=%d)"
	errDeliveryRetryExhausted          = "%d회 재시도 후에도 실패했습니다: %s"
	errDeliverySnapshotEmpty           = "전달 항목 #%d 의 이벤트 스냅샷이 비어 있습니다"
	errDeliverySnapshotParse           = "전달 항목 #%d 의 이벤트 스냅샷을 해석하지 못했습니다: %w"
	errDeliveryBatchAllUnparseable     = "요약 배치의 전달 %d건을 모두 해석하지 못했습니다"
)

// Notifier is the delivery engine for finding notifications.
//
// It runs beside Scheduler in its own goroutine (see server.New). It does not
// reuse Scheduler's tick because notifications need a three-second interval,
// while triggers run at a different pace. Notification failures must not delay
// agent triggers.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu guards buckets. There are few channels and little contention, so one
	// mutex is sufficient.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is the token bucket for one channel.
//
// A window that resets its counter each minute allows bursts at its boundary:
// 20 messages at the end of one window and 20 at the start of the next can be
// 40 messages in one second, triggering the platform's rate limit. A token
// bucket refills at a steady rate and avoids that burst.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx ends. It is started once by server.New.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step runs one tick: dispatch new events, then send due deliveries.
//
// Any failure is logged without stopping the loop. A notification failure must
// not become a process failure; the next tick will retry independently.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to fan out events: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] failed to list channels: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// The token bucket counts messages (HTTP requests), not findings.
		// Real-time mode sends one message per finding. Digest mode combines a
		// batch of findings into one message, so it consumes one token.
		//
		// In both modes, check the bucket before claiming deliveries. Reversing
		// the order would consume retry attempts for rate-limited deliveries.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns this tick's token cost and maximum batch size for a
// digest channel.
//
// The two return values measure different things:
//
//   - tokens counts messages. A batch of findings makes one message and one HTTP
//     request, so it is always 1. rate_per_min still limits digest messages.
//   - claimLimit caps findings in the batch. It is bounded by memory, not by
//     the request budget.
//
// Previously, the per-tick request budget (notifyMaxSendsPerChannelPerTick,
// based on the lease) was passed as the batch size to apply rate_per_min to
// digests. A channel with rate_per_min=20 refills one token in a three-second
// tick, so each digest contained just one finding. Readers then received a
// series of one-finding digests and db.MaxDigestBatchSize was never reached.
//
// The existing end-to-end tests pass a large limit directly to stepDigest and
// bypass step's budget calculation. This helper makes the decision testable by
// TestDigestTickPlanDecouplesBatchSizeFromSendBudget.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and sends one message per finding for a channel.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim realtime deliveries channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// Rendering failure means bad local data; retrying will not fix it.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest combines a channel's pending deliveries into one message when
// its batch is due.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to check digest batch due time channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Explicitly fail deliveries with invalid snapshots that were omitted from
	// the message. Otherwise they are absent from both included and the failure
	// list. A successful send would leave them in sending until the lease expires,
	// causing them to be claimed repeatedly.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := errDeliverySnapshotUnrenderable
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark deliveries with invalid snapshots as failed channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparseable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Pass only included deliveries to send: included[i] corresponds exactly to
	// msg.Items[i]. send needs this mapping to mark the first K rows correctly
	// when the channel reports that only K items fit.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers a message and updates delivery states based on the result.
//
// A batch of deliveries (possibly dozens in digest mode) shares one send
// result: all are delivered or the entire batch is retried. Retrying individual
// items would change the meaning of a digest message.
//
// The exception is splitting at the channel's length limit. If the channel
// reports that only the first K items fit, later items must remain for the
// next batch. Marking them successful would silently lose those findings.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Bound one send so a stalled channel does not block all other channels.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// A channel cannot deliver more items than there are deliveries. If it
			// reports otherwise, log the rendering error and treat all as delivered
			// rather than corrupting the delivery records.
			log.Printf("[notify] channel reported %d delivered items for %d deliveries channel=%s; treating all as delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark deliveries as sent channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// The message reached the channel's length limit. Queue the remainder
			// immediately for the next tick. Use DeferDeliveries instead of
			// RescheduleDeliveries because this is not a failure and must not consume
			// a retry attempt; the optimistic increment on claim is undone there.
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf(trMsg(errDeliveryChannelLengthCapped), delivered)); err != nil {
				log.Printf("[notify] failed to queue remaining deliveries after channel length limit channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// The channel returned no error but reported no delivered items. Treat
		// that as a failure with backoff so the delivery does not remain unmarked.
		err = fmt.Errorf(trMsg(errDeliveryNoDeliveredCount), delivered)
	}

	// Decide failure handling per delivery, not by the batch's maximum attempts.
	//
	// Previously, `if maxAttempts(deliveries) >= MaxNotifyAttempts` failed the
	// whole batch. Attempts differ among items, so an older delivery with two
	// attempts could cause a new delivery with one attempt to fail permanently
	// without getting its own retries.
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] failed to mark deliveries as failed channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf(trMsg(errDeliveryRetryExhausted), db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] failed to mark deliveries as failed channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Group reschedules by delay. There are only three backoff intervals, so
	// this avoids a separate UPDATE for every item in a large batch.
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] failed to reschedule deliveries channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent_failures=%d retries_exhausted=%d retry_delay_groups=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns items in all but not keep, comparing pointers.
// It finds deliveries omitted from a message so they can be handled explicitly.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt finds the channel implementation and parses its configuration.
// ok=false means the channel kind is unregistered; the delivery should fail
// immediately instead of retrying forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// Use an empty map if parsing fails. The channel's Validate method
		// reports missing fields, which is more useful to the user than a JSON
		// parsing error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders one finding notification.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch renders a digest. It parses each snapshot separately and skips
// a bad item without discarding the whole batch.
//
// The returned included slice corresponds exactly to msg.Items: delivery i
// matches item i. The caller uses the channel's reported first K items to mark
// the first K deliveries sent. If a bad snapshot were skipped without removing
// its delivery from included, the indexes would shift, marking a bad item sent
// and a good one unsent. stepDigest explicitly fails the skipped deliveries.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// A bad snapshot belongs in neither the message nor included. The caller
			// explicitly marks its delivery failed instead of treating it as sent.
			log.Printf("[notify] skipped unparseable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf(trMsg(errDeliveryBatchAllUnparseable), len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders an event snapshot as a notification item, resolving asset
// names and the link to its details.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Failure to resolve asset names should not prevent notification. The
		// message can omit the asset line instead.
		log.Printf("[notify] failed to resolve asset names finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// The details route is in web/src/app/(main)/function/findings/detail/page.tsx;
		// it reads the finding ID from the id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens takes up to want tokens from a channel's bucket and returns the
// number actually taken.
//
// One token represents one message (one HTTP request). Real-time mode asks for
// as many messages as it can send; digest mode asks for one per batch.
//
// The bucket capacity equals the channel's per-minute limit and refills at a
// constant rate. ratePerMin<=0 disables the rate limit, but still returns a
// finite cap to keep a backlog from monopolizing one tick.
//
// The want cap matters because the caller also has a per-tick limit. Draining
// the entire bucket would discard extra tokens until the next refill, making
// accumulated burst capacity unavailable even when nothing was sent.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Refill based on elapsed real time at ratePerMin/60 tokens per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before converting to int. Floating-point refills such
	// as 0.5 + 0.5 may produce 0.9999999999, which int() would truncate to zero.
	// 1e-9 is much smaller than a token and cannot cover a real shortfall.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the master switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external URL for links, without a trailing slash.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the digest interval, falling back to the default when
// unset or invalid.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot decodes the event snapshot associated with a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotEmpty), dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotParse), dl.ID, err)
	}
	if snap.Kind == "" {
		// Trust the event row's kind; an older version may have written the snapshot.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
