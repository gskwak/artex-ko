package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// smokeEnv makes the smoke-test subprocess skip Bootstrap.
//
// The subprocess runs as artex.new, so the paths derived from os.Executable()
// would not overlap the real upgrade files. An explicit bypass is more robust
// and avoids an unnecessary disk check in the subprocess.
const smokeEnv = "ARTEX_SELFUPDATE_SMOKE"

// Action is the instruction Bootstrap returns to main.
type Action int

const (
	// Continue starts the server normally.
	Continue Action = iota
	// Restart exits immediately with ExitRestart so the supervisor script relaunches it.
	Restart
)

// State describes the upgrade state at startup so /api/update/check can report
// whether the previous upgrade succeeded or was rolled back.
type State struct {
	Pending     bool   // The replacement has not yet been confirmed stable.
	RolledBack  bool   // This startup performed an automatic rollback.
	FailedStage bool   // Staged binary failed verification or smoke testing and was discarded.
	Detail      string // Brief user-facing explanation.
}

// Bootstrap runs at the start of main, before opening the database or any listener.
//
// Three possible states:
//
//  1. artex.new exists: verify and smoke-test it, then install and restart;
//     discard it and continue with the old version on failure.
//  2. Only the marker exists: count this startup attempt and roll back after
//     too many consecutive failures.
//  3. Neither exists: start normally.
func Bootstrap() (Action, State) {
	if os.Getenv(smokeEnv) != "" {
		return Continue, State{}
	}
	p, err := ResolvePaths()
	if err != nil {
		log.Printf("[update] skipping bootstrap: %v", err)
		return Continue, State{}
	}

	if _, err := os.Stat(p.New); err == nil {
		return applyStaged(p)
	}

	m, ok := readMarker(p.Marker)
	if !ok {
		return Continue, State{}
	}
	return confirmOrRollback(p, m)
}

// applyStaged handles a staged binary: install it if verified, otherwise discard it.
//
// This is the only point in the upgrade flow that replaces the executable. The
// smoke test is the final gate against corrupt downloads, wrong architectures,
// and missing dynamic libraries. If an unusable binary gets through, the
// supervisor will keep trying to start it; Go code cannot run to roll it back.
func applyStaged(p Paths) (Action, State) {
	m, _ := readMarker(p.Marker)

	if err := verifyStaged(p); err != nil {
		log.Printf("[update] staged version failed verification and was discarded; continuing with current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "새 버전 검증에 실패해 폐기했습니다: " + err.Error()}
	}

	if err := swap(p); err != nil {
		log.Printf("[update] binary replacement failed; continuing with current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "버전 교체에 실패했습니다: " + err.Error()}
	}

	// Keep the marker so the next startup, running the new version, can confirm stability.
	m.Attempts = 0
	if m.StagedAt == 0 {
		m.StagedAt = time.Now().Unix()
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to write upgrade marker (automatic rollback unavailable): %v", err)
	}
	log.Printf("[update] switched to %s; exiting for restart (exit %d)", orUnknown(m.To), ExitRestart)
	return Restart, State{Pending: true}
}

// confirmOrRollback counts startup attempts after replacement and restores the old version if the limit is exceeded.
//
// The count increases only after Go code starts, covering failures during
// initialization such as incompatible configuration, an occupied port, or a
// failed database migration. The pre-install smoke test handles binaries that
// cannot execute at all.
func confirmOrRollback(p Paths, m marker) (Action, State) {
	m.Attempts++
	if m.Attempts > maxAttempts {
		if err := rollback(p); err != nil {
			// If rollback fails, clear the marker and avoid an endless restart loop.
			// Continue in the current state so the logs can expose any startup failure.
			log.Printf("[update] new version failed to start %d times and rollback failed: %v", maxAttempts, err)
			_ = os.Remove(p.Marker)
			return Continue, State{Detail: "새 버전 시작에 실패하고 롤백도 실패했습니다: " + err.Error()}
		}
		log.Printf("[update] new version failed to start %d times; rolled back to %s and exiting for restart (exit %d)",
			maxAttempts, orUnknown(m.From), ExitRestart)
		_ = os.Remove(p.Marker)
		return Restart, State{RolledBack: true, Detail: fmt.Sprintf("새 버전 시작에 실패해 이전 버전으로 롤백했습니다(%s)", orUnknown(m.From))}
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to update upgrade marker: %v", err)
	}
	log.Printf("[update] starting new version (attempt %d/%d); upgrade will be confirmed after stable operation",
		m.Attempts, maxAttempts)
	return Continue, State{Pending: true}
}

// Settle confirms that the new version has run stably and clears the upgrade marker.
//
// Main calls it after the HTTP listener has run for a delay. If the process
// does not survive that long, the marker remains and the next startup counts
// another attempt until rollback is triggered.
func Settle() {
	p, err := ResolvePaths()
	if err != nil {
		return
	}
	settle(p)
}

func settle(p Paths) {
	if _, ok := readMarker(p.Marker); !ok {
		return // No upgrade is pending.
	}
	if err := os.Remove(p.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[update] failed to remove upgrade marker: %v", err)
		return
	}
	log.Printf("[update] new version is stable; upgrade complete (previous version retained as %s)", p.Old)
}

// SettleDelay is how long the new version must run to be considered stable.
const SettleDelay = 30 * time.Second

// verifyStaged checks the staged binary's SHA256 and then runs it once.
func verifyStaged(p Paths) error {
	want, err := os.ReadFile(p.Sum)
	if err != nil {
		return fmt.Errorf("체크섬 읽기 실패: %w", err)
	}
	got, err := fileSHA256(p.New)
	if err != nil {
		return fmt.Errorf("체크섬 계산 실패: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(want)), got) {
		return errors.New("SHA256 이 일치하지 않습니다(다운로드가 손상됐거나 변조됨)")
	}
	return smokeTest(p.New)
}

// smokeTest starts the new binary with -h to confirm it can run on this system.
// It catches truncated downloads, wrong architectures, missing dependencies, and similar problems.
func smokeTest(bin string) error {
	if err := os.Chmod(bin, 0o755); err != nil {
		return fmt.Errorf("실행 권한 부여 실패: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-h")
	cmd.Env = append(os.Environ(), smokeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return errors.New("스모크 테스트 시간 초과(새 바이너리가 응답하지 않음)")
	}
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("스모크 테스트 실패: %v: %s", err, snippet)
	}
	return nil
}

// swap replaces the current binary with the staged version.
//
// Both Unix and Windows permit renaming a running executable. Windows blocks
// deleting or overwriting it, but not renaming it, so no platform-specific
// handling or preemptive shutdown is needed here.
func swap(p Paths) error {
	// Windows rename cannot overwrite a destination; remove any .old left by a prior upgrade.
	if err := os.Remove(p.Old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("이전 백업 %s 정리 실패: %w", p.Old, err)
	}
	if err := os.Rename(p.Current, p.Old); err != nil {
		return fmt.Errorf("현재 버전 백업 실패: %w", err)
	}
	if err := os.Rename(p.New, p.Current); err != nil {
		// Restore the current binary if installation fails after it was moved away.
		if rerr := os.Rename(p.Old, p.Current); rerr != nil {
			return fmt.Errorf("새 버전 설치 실패(%v), 현재 버전 복구도 실패: %w", err, rerr)
		}
		return fmt.Errorf("새 버전 설치 실패: %w", err)
	}
	_ = os.Remove(p.Sum)
	return nil
}

// rollback restores the old version backed up by swap.
func rollback(p Paths) error {
	if _, err := os.Stat(p.Old); err != nil {
		return fmt.Errorf("롤백할 백업이 없습니다(%s): %w", p.Old, err)
	}
	// Keep the failed new version as .failed for investigation.
	failed := p.Current + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(p.Current, failed); err != nil {
		return fmt.Errorf("실패한 버전 이동 실패: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		return fmt.Errorf("이전 버전 복구 실패: %w", err)
	}
	return nil
}

// Rollback implements /api/update/rollback by restoring the previous version.
// The caller exits with ExitRestart and leaves the restart to the supervisor script.
func Rollback() error {
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Old); err != nil {
		return errors.New("롤백할 이전 버전이 없습니다(" + p.Old + " 없음)")
	}
	cleanStaged(p)
	if err := smokeTest(p.Old); err != nil {
		return fmt.Errorf("이전 버전을 실행할 수 없어 롤백을 거부합니다: %w", err)
	}
	// Swap current and backup so this rollback can itself be reversed.
	tmp := p.Current + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(p.Current, tmp); err != nil {
		return fmt.Errorf("현재 버전 이동 실패: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		_ = os.Rename(tmp, p.Current)
		return fmt.Errorf("이전 버전 설치 실패: %w", err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		log.Printf("[update] failed to organize backup after rollback (operation unaffected): %v", err)
	}
	_ = os.Remove(p.Marker)
	return nil
}

// HasBackup reports whether a previous version is available for rollback, so the UI can show the rollback button.
func HasBackup() bool {
	p, err := ResolvePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.Old)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "알 수 없는 버전"
	}
	return s
}
