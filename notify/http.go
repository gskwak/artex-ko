package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets controls whether messages may be sent to loopback or
// link-local addresses.
//
// Reject these addresses by default. IM bots and public mail servers do not
// normally use them, but they can reach sensitive targets: another service's
// administration port on the same host, or a cloud metadata endpoint such as
// 169.254.169.254 that may expose instance credentials. Although administrators
// configure delivery addresses, an XSS/CSRF-abused admin session or another
// person sharing the same JWT could change the configuration and read part of
// the response. doJSON records the first 200 bytes of a 4xx/5xx response in
// last_error, and delivery history echoes it, providing partial read access.
//
// A local SMTP relay, such as Postfix on 127.0.0.1:25, is a common legitimate
// setup. Provide an explicit opt-in instead of blocking it unconditionally:
// set ARTEX_NOTIFY_ALLOW_LOCAL=1 to permit local targets.
//
// Export AllowLocalTargetsEnv so tests can opt in explicitly. Tests in this
// package and server use httptest receivers on 127.0.0.1, which this guard
// would otherwise block.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP reports whether a destination IP is blocked by default.
//
// Reject only loopback, link-local (including cloud metadata at
// 169.254.169.254), unspecified, and multicast addresses. Allow RFC 1918
// private networks: internal Mattermost instances and SMTP relays are common
// legitimate destinations. Blocking them would break normal deployments.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Convert IPv4-mapped IPv6 addresses (such as ::ffff:127.0.0.1) to IPv4
	// before checking, or they could bypass the guard.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the http.Transport dialer's Control hook. It checks
// the destination address when the connection is established.
//
// Dial time is the point where this check takes effect. It also handles cases
// that can bypass validation when configuration is saved: DNS rebinding (a
// public IP at validation time, an internal IP at connection time) and
// redirects. Cross-host redirects are rejected, but same-host redirects can
// still change the target path.
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("대상 주소를 해석할 수 없습니다: %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("로컬 또는 링크 로컬 주소 %s 로는 전송하지 않습니다 (로컬 서비스로 보내야 하면 %s=1 로 설정하세요)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport adds only the dial guard to the default Transport.
// Clone preserves its connection pooling, HTTP/2, timeouts, proxy settings,
// and other defaults.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is shared by all delivery channels.
//
// Do not reuse the project's global outbound proxy (GlobalProxy in server).
// That proxy is for traffic to penetration-test targets and may be an unstable
// tunnel; notification delivery should not depend on target-network conditions.
// Connect to IM services directly, with a 15-second timeout.
//
// Reject cross-host redirects. Delivery targets are fixed endpoints that do
// not normally redirect to another host. Credentials for DingTalk
// (access_token), WeCom (key), and Telegram (bot token) are in the URL; a
// cross-host redirect could disclose them to its destination. Same-host
// redirects, such as adding a trailing slash, are allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("리다이렉트가 너무 많습니다")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("다른 호스트로의 리다이렉트는 허용하지 않습니다 (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit caps response-body reads. A faulty peer might return an
// enormous body, while delivery history needs only a status and short error.
const respBodyLimit = 8 << 10

// doJSON sends one request and returns the size-limited response body.
//
// A nil payload sends an empty body (for GET or platforms that do not require
// one). Header keys and values are added as provided for custom webhook headers.
//
// Error classification is central here: network failures and 5xx/408/429
// responses are retryable; other 4xx responses are permanent. Retrying a 403
// would only log the same error repeatedly.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// A serialization failure is a local bug (such as an invalid config
			// field type); a retry cannot fix it.
			return nil, Permanent(fmt.Errorf("요청 본문을 만들지 못했습니다: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// An invalid URL is usually a misconfigured address and is permanent.
		// Do not return err directly: url.Parse errors contain the full URL.
		return nil, Permanent(fmt.Errorf("요청 주소가 올바르지 않습니다: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Refused connections, DNS failures, and timeouts are often temporary;
		// let the backoff policy retry them.
		//
		// Redact errors before exposing them. http.Client.Do returns *url.Error,
		// whose Error() includes the full URL. These channels put credentials in
		// URLs (DingTalk access_token, WeCom key, Feishu hook ID, Telegram
		// /bot<token>/). Without redaction, the error could leak credentials to
		// notification_deliveries.last_error, the delivery-history API (bypassing
		// channel-config masking), server logs, and the test-send API's 502 response.
		return nil, fmt.Errorf("요청에 실패했습니다: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("응답을 읽지 못했습니다: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// Retry 429 (rate limiting) and 408 (timeout). Other 4xx responses indicate
	// configuration or permission problems that a retry cannot fix.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("상대 서버가 요청을 제한하거나 응답이 지연되었습니다 (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("상대 서버에 오류가 발생했습니다 (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("상대 서버가 요청을 거부했습니다 (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet condenses a response body into one short line for an error message.
// Newlines and excess whitespace would otherwise disrupt delivery-history layout.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget reduces a delivery URL to "scheme://host/…" for errors.
//
// This is the package's single URL-redaction policy. Discard everything except
// the scheme and host because credentials can appear in different URL parts:
//
//	DingTalk  query        /robot/send?access_token=xxx
//	WeCom     query        /cgi-bin/webhook/send?key=xxx
//	Feishu    path suffix  /open-apis/bot/v2/hook/<hook_id>
//	Telegram  path middle  /bot<token>/sendMessage
//
// Preserving more useful URL detail would require channel-specific handling;
// missing one channel could expose credentials. The host is enough to diagnose
// DNS, connection, and certificate failures. The masked suffix in the channel
// configuration helps identify the particular bot.
//
// Return a fixed placeholder on parse failure; never echo the raw string.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(주소를 해석할 수 없음)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError removes the URL from a transport error, retaining the
// underlying cause.
//
// *url.Error contains {Op, URL, Err}, and Error() includes the URL. Read Err
// directly instead. This is more reliable than replacing strings after the
// fact, which can miss encoded or escaped URL variants.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: 알 수 없는 오류", uerr.Op, host)
	}
	// Errors other than *url.Error, such as redirect-policy errors, may also
	// contain URLs; redact them as well.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) URLs in text with redacted forms.
//
// This covers errors without structured fields, such as redirect-policy or
// third-party library errors. Recognize only http/https prefixes and split
// at whitespace or quotes, which cannot occur unescaped in a URL.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
