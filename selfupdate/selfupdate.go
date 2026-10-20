// Package selfupdate implements ARTEX's one-click UI update. It downloads a new
// binary from a GitHub Release, verifies and stages it, then atomically replaces
// the executable on the next startup.
//
// Responsibilities (see start.sh and start.bat):
//
//	Startup script = simple supervisor loop that decides whether to restart
//	                 the process based on its exit code.
//	This package   = download, SHA256 verification, smoke test, binary swap,
//	                 and rollback after failure.
//
// The swap happens in Go because SHA256 verification and smoke testing would
// otherwise require separate implementations in sh and bat (sha256sum, shasum,
// or certutil). These checks must be reliable: after an unusable binary is
// installed, the supervisor would repeatedly try to start it, requiring
// manual recovery on the host.
//
// A complete upgrade spans three process starts:
//
//  1. The old server receives /api/update/apply, downloads and verifies the
//     release, stages artex.new, and exits with code 75.
//  2. The script restarts the old version. Bootstrap finds artex.new,
//     verifies and smoke-tests it, swaps it in, and exits with code 75.
//  3. The script starts the new version. Bootstrap counts an attempt, and
//     the marker is cleared once startup is confirmed successful.
//
// If verification fails in step 2, the staged file is deleted and the old
// version continues. If the new version fails to survive long enough to clear
// the marker on three attempts, the next startup restores artex.old.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart tells the supervisor to restart immediately (EX_TEMPFAIL).
// It does not count toward crash backoff. Code 0 means a normal stop and ends
// the supervisor loop; other exit codes are treated as crashes.
const ExitRestart = 75

// maxAttempts is the number of startup attempts permitted after a swap. Each
// startup of the new version increments the counter; surviving SettleDelay
// clears the marker. Repeated failures trigger an automatic rollback.
const maxAttempts = 3

// Paths contains every file involved in an upgrade, all in the executable's
// directory. A service may run with / or another arbitrary working directory,
// so using CWD could stage files elsewhere and break the swap.
type Paths struct {
	Dir     string // Directory containing the executable.
	Current string // Running binary: artex or artex.exe.
	New     string // Staged binary: artex.new or artex.new.exe.
	Sum     string // Staged binary's SHA256 hex digest: artex.new.sha256 or artex.new.exe.sha256.
	Old     string // Previous binary backed up before the swap: artex.old or artex.old.exe.
	Marker  string // Upgrade state marker: artex.upgrade.json.
}

// ResolvePaths derives every upgrade path from the current executable.
//
// On Windows, .new and .old must retain the .exe suffix for the smoke test and
// execution after the swap. Strip the suffix first, then add it back after
// .new or .old to keep naming consistent across platforms.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("실행 파일 위치 확인 실패: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // ".exe" on Windows; usually empty on Unix.
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker records swap progress so an unusable new version can be rolled back.
type marker struct {
	From     string `json:"from"`     // Version before the upgrade.
	To       string `json:"to"`       // Target version.
	Attempts int    `json:"attempts"` // Startup attempts since the swap.
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes staged files after a successful swap, failed verification,
// or cancellation so artex.new is not retried on the next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions compares two versions and returns -1, 0, or 1 for a < b,
// a == b, or a > b. The boolean is false if either version cannot be compared,
// such as "dev" or "0.3.7-2-gabc1234-dirty" from git describe. In that case,
// callers should disable one-click updates so a development build is not
// replaced by a release, potentially overwriting uncommitted changes.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion parses a version like "v0.3.7" or "0.3.7" into [3]int.
//
// Accept only a plain three-part version. For builds without a tag, build.sh
// uses git describe to produce suffixes such as "0.3.7-2-gabc1234". These must
// be incomparable rather than treated as 0.3.7, or a development build could
// be mistaken for the latest release or replaced by one.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker reports whether the process runs inside a container. A swap there
// writes to the container's writable layer. Recreating the container with
// `docker compose up -d` restores the image's version, as expected when the
// user pulls a new image; the UI should explain this behavior.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}
