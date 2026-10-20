package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Repo is the release source. Keep it fixed instead of configurable: allowing anyone
// who can edit the configuration to change the update source would grant them a path
// to remote code execution, which is unacceptable for a penetration testing platform.
const Repo = "Autumn-27/artex"

// latestURL is GitHub's latest stable release endpoint, which skips prereleases and drafts.
const latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// allowedHosts limits the domains the update process may contact. Together with the
// redirect check below, it rejects any redirect to a host outside the list. This is
// the first defense against DNS poisoning or an intermediary replacing the binary;
// verification against SHA256SUMS is the second.
var allowedHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true, // Object storage that serves release assets.
	"release-assets.githubusercontent.com": true,
	"raw.githubusercontent.com":            true,
}

// Release contains the GitHub Release fields we need.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset is a file attached to a Release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// NewClient creates an HTTP client restricted to GitHub domains. An empty proxy
// connects directly.
//
// Do not reuse the default Transport: updates must use TLS and verify certificates,
// unaffected by settings elsewhere such as InsecureSkipVerify.
func NewClient(proxy string) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Minute, // Allow enough time to download the full package.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("리다이렉트 횟수가 너무 많습니다")
			}
			return checkURL(req.URL)
		},
	}
}

// checkURL enforces HTTPS and the domain allowlist.
func checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("HTTPS 가 아닌 주소를 거부합니다: %s", u.Scheme+"://"+u.Host)
	}
	if !allowedHosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("GitHub 도메인이 아닌 주소를 거부합니다: %s", u.Hostname())
	}
	return nil
}

// FetchLatest queries the latest stable release.
func FetchLatest(ctx context.Context, c *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "artex-selfupdate")

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub 에 접속하지 못했습니다(시스템 설정에서 전역 프록시를 설정할 수 있습니다): %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		// GitHub's unauthenticated API allows 60 requests per IP per hour;
		// shared egress IPs can reach this limit quickly.
		return nil, fmt.Errorf("GitHub API 요청 제한에 걸렸습니다(시간당 60회). 잠시 후 다시 시도하세요")
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("저장소 %s 에 아직 정식 릴리스가 없습니다", Repo)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub 응답 코드 %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("Release 해석 실패: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return nil, fmt.Errorf("Release 에 tag 가 없습니다")
	}
	return &rel, nil
}

// AssetName returns the package name for the platform, matching package_binary in
// build.sh: artex-<version>-<os>-<arch>.zip (without a leading v in the version).
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("artex-%s-%s-%s.zip", strings.TrimPrefix(tag, "v"), goos, goarch)
}

// FindAsset looks up an asset by name in a Release.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Asset{}, false
}
