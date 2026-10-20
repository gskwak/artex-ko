package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/Autumn-27/artex/selfupdate"
)

// HTTP interface for one-click updates. The selfupdate package handles download,
// verification, and installation. This layer handles authorization, concurrency,
// progress broadcasts, and telling main when the process should exit.
//
// This process does not restart itself. After staging a new version, it exits with
// selfupdate.ExitRestart and the supervisor (start.sh, start.bat, or Docker ENTRYPOINT)
// starts it again.

// 사용자에게 노출되는 업데이트 메시지(SSE 진행 메시지·검사 사유·에러 응답).
// 프런트엔드(system/settings 의 update-card)가 progress.message·reason·writeErr
// 본문을 그대로 렌더하므로 한국어로 둔다. 운영 로그와 주석은 영어로 둔다.
const (
	updateMsgPreparing          = "준비 중…"
	updateMsgFailed             = "업데이트 실패"
	updateMsgStaged             = "새 버전이 준비되었습니다. 재시작 중…"
	updateErrNotReleaseFmt      = "현재 버전 %q 은(는) 정식 릴리스 버전이 아니므로 원클릭 업데이트를 사용할 수 없습니다"
	updateErrAlreadyLatestFmt   = "이미 최신 버전입니다 (%s)"
	updateErrInProgress         = "이미 업데이트가 진행 중입니다"
	updateErrRollbackInProgress = "업데이트가 진행 중이어서 롤백할 수 없습니다"
)

// restartCh closes when an update is staged or rollback completes; main then exits with ExitRestart.
var (
	restartOnce sync.Once
	restartCh   = make(chan struct{})
)

// RestartRequested returns a channel that closes when the supervisor should restart this process.
func RestartRequested() <-chan struct{} { return restartCh }

func requestRestart() { restartOnce.Do(func() { close(restartCh) }) }

// bootState stores the result of selfupdate.Bootstrap for this start (update succeeded,
// rollback occurred, or staged update was discarded). Main sets it so /api/update/check
// can report the previous update's outcome to the frontend.
var (
	bootStateMu sync.Mutex
	bootState   selfupdate.State
)

// SetBootUpdateState is called once by main at startup.
func SetBootUpdateState(st selfupdate.State) {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	bootState = st
}

func bootUpdateState() selfupdate.State {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	return bootState
}

// releaseCache caches the latest GitHub release lookup.
//
// The top bar checks for a new version on each full page load, while GitHub's
// unauthenticated API permits only 60 requests per IP per hour. Without caching,
// a few tabs or reloads could exhaust that quota. An explicit update check can
// bypass the cache with force.
type releaseCache struct {
	mu  sync.Mutex
	rel *selfupdate.Release
	err error
	at  time.Time
	// fetch is a test injection point; nil uses the real GitHub lookup.
	fetch func(context.Context, *http.Client) (*selfupdate.Release, error)
}

const (
	releaseTTL = 30 * time.Minute
	// Cache failures briefly so each page load does not wait for a timeout while GitHub
	// is unavailable. A short TTL lets the lookup recover soon after the network does.
	releaseErrTTL = 2 * time.Minute
	// Lookup timeout. NewClient's 30-minute timeout is for full downloads, not version checks.
	releaseTimeout = 20 * time.Second
)

var relCache = &releaseCache{}

// get returns the latest release without a network request on a cache hit.
//
// Hold the lock during the lookup so concurrent requests share one result instead
// of each hitting GitHub (especially when several tabs load at once).
func (c *releaseCache) get(ctx context.Context, client *http.Client, force bool) (*selfupdate.Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force {
		ttl := releaseTTL
		if c.err != nil {
			ttl = releaseErrTTL
		}
		if !c.at.IsZero() && time.Since(c.at) < ttl {
			return c.rel, c.err
		}
	}

	fetch := c.fetch
	if fetch == nil {
		fetch = selfupdate.FetchLatest
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	rel, err := fetch(ctx, client)
	// A canceled request (for example, a closed tab) does not indicate a GitHub failure.
	// Do not cache it, or the next visitor could receive a spurious cancellation error.
	if err != nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
		return c.rel, err
	}
	c.rel, c.err, c.at = rel, err, time.Now()
	return rel, err
}

// updateProgress is one progress event sent to the frontend.
type updateProgress struct {
	Phase   selfupdate.Phase `json:"phase"`
	Percent int              `json:"percent"` // meaningful only while downloading; -1 otherwise
	Message string           `json:"message"`
	Version string           `json:"version,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// updateHub tracks one update and broadcasts progress to SSE subscribers.
//
// running also guards against concurrent updates. A second POST to /api/update/apply
// returns 409 so two goroutines cannot write to the same artex.new file.
type updateHub struct {
	mu      sync.Mutex
	running bool
	cur     updateProgress
	subs    map[chan updateProgress]struct{}
}

var updHub = &updateHub{
	cur:  updateProgress{Phase: selfupdate.PhaseIdle, Percent: -1},
	subs: map[chan updateProgress]struct{}{},
}

// begin claims the update slot and returns false when an update is already running.
func (h *updateHub) begin(version string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.running = true
	h.cur = updateProgress{Phase: selfupdate.PhaseDownload, Percent: 0, Message: updateMsgPreparing, Version: version}
	h.fanout(h.cur)
	return true
}

// finish completes an update; nil err means staging succeeded and restart is pending.
func (h *updateHub) finish(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	if err != nil {
		h.cur = updateProgress{Phase: selfupdate.PhaseFailed, Percent: -1, Message: updateMsgFailed, Error: err.Error(), Version: h.cur.Version}
	} else {
		h.cur = updateProgress{Phase: selfupdate.PhaseStaged, Percent: 100, Message: updateMsgStaged, Version: h.cur.Version}
	}
	h.fanout(h.cur)
}

func (h *updateHub) publish(ph selfupdate.Phase, pct int, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cur = updateProgress{Phase: ph, Percent: pct, Message: msg, Version: h.cur.Version}
	h.fanout(h.cur)
}

// fanout must be called while holding h.mu. Subscriber channels are buffered;
// drop events when full so a stalled SSE connection cannot block the update.
func (h *updateHub) fanout(p updateProgress) {
	for ch := range h.subs {
		select {
		case ch <- p:
		default:
		}
	}
}

func (h *updateHub) snapshot() (updateProgress, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur, h.running
}

func (h *updateHub) subscribe() (<-chan updateProgress, func()) {
	ch := make(chan updateProgress, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// updateCheck compares the current version with the latest stable GitHub release.
//
// The frontend can also query api.github.com directly (GitHub allows CORS), but
// this endpoint is authoritative. The backend performs downloads and must be able
// to reach GitHub. A browser may have access while the server does not, such as on
// a private network or with a browser-only proxy, so report that failure here.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion
	mode := "binary"
	if selfupdate.InDocker() {
		mode = "docker"
	}
	boot := bootUpdateState()
	out := map[string]any{
		"current":     current,
		"mode":        mode,
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"has_backup":  selfupdate.HasBackup(),
		"repo":        selfupdate.Repo,
		"boot_notice": boot.Detail,
		"rolled_back": boot.RolledBack,
	}

	// Top bar checks use the cache; an explicit update check uses force=1.
	force := r.URL.Query().Get("force") != ""
	client := selfupdate.NewClient(s.m.GlobalProxy())
	rel, err := relCache.get(r.Context(), client, force)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, 200, out)
		return
	}

	latest := rel.TagName
	out["latest"] = latest
	out["notes"] = rel.Body
	out["html_url"] = rel.HTMLURL
	if !rel.PublishedAt.IsZero() {
		out["published_at"] = rel.PublishedAt.Format(time.RFC3339)
	}

	asset := selfupdate.AssetName(latest, runtime.GOOS, runtime.GOARCH)
	out["asset"] = asset
	if a, ok := rel.FindAsset(asset); ok {
		out["asset_available"] = true
		out["size"] = a.Size
	} else {
		out["asset_available"] = false
	}

	cmp, comparable := selfupdate.CompareVersions(current, latest)
	out["comparable"] = comparable
	out["has_update"] = comparable && cmp < 0
	if !comparable {
		// Development builds (dev or a suffixed git describe) lack a comparable version.
		// Reject updates to avoid replacing a locally debugged binary with a release.
		out["reason"] = fmt.Sprintf(updateErrNotReleaseFmt, current)
	}
	writeJSON(w, 200, out)
}

// updateApply downloads and stages a new version, then exits for the supervisor to restart it.
//
// Return 202 immediately and perform the work in a background goroutine. A full
// download can take minutes and exceed a reverse proxy timeout. Progress is sent
// through /api/update/stream.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion

	// Use the cache to install the version the user saw and confirmed in the UI.
	client := selfupdate.NewClient(s.m.GlobalProxy())
	rel, err := relCache.get(r.Context(), client, false)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	cmp, comparable := selfupdate.CompareVersions(current, rel.TagName)
	if !comparable {
		writeErr(w, 400, fmt.Sprintf(updateErrNotReleaseFmt, current))
		return
	}
	if cmp >= 0 {
		writeErr(w, 400, fmt.Sprintf(updateErrAlreadyLatestFmt, current))
		return
	}
	if !updHub.begin(rel.TagName) {
		writeErr(w, 409, updateErrInProgress)
		return
	}

	go func() {
		// Use s.ctx instead of the request context: the request ends when its HTTP
		// response returns, which would immediately cancel a download tied to it.
		err := selfupdate.Stage(s.ctx, client, rel, current, func(ph selfupdate.Phase, pct int, msg string) {
			updHub.publish(ph, pct, msg)
		})
		updHub.finish(err)
		if err != nil {
			log.Printf("[update] update failed: %v", err)
			return
		}
		log.Printf("[update] staged %s → %s; exiting to complete the update", current, rel.TagName)
		// Allow time to send the final progress event before triggering exit.
		time.Sleep(1500 * time.Millisecond)
		requestRestart()
	}()

	writeJSON(w, 202, map[string]any{"ok": true, "target": rel.TagName})
}

// updateRollback restores the prior version backed up as artex.old before replacement.
func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if _, running := updHub.snapshot(); running {
		writeErr(w, 409, updateErrRollbackInProgress)
		return
	}
	if err := selfupdate.Rollback(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	log.Printf("[update] manually rolled back to the previous version; exiting to complete the switch")
	writeJSON(w, 202, map[string]any{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		requestRestart()
	}()
}

// updateStream sends update progress via SSE.
func (s *Server) updateStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := updHub.subscribe()
	defer unsub()

	send := func(p updateProgress) {
		b, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	// Send the current state first so a refreshed page immediately shows the update in progress.
	cur, _ := updHub.snapshot()
	send(cur)

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			send(p)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
