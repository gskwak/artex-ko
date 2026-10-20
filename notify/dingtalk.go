package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements a DingTalk custom bot.
//
// Platform behavior shaping this implementation:
//   - A single bot is limited to 20 messages per minute. Excess messages may
//     be silently dropped even with HTTP 200, so the client must enforce the
//     rate limit; see DefaultRatePerMin.
//   - Security options are signing, custom keywords, or an IP allowlist.
//     Signing is the only option independent of message content, so this
//     adapter supports signing and unsecured webhooks.
//   - Both success and failure can return HTTP 200. Check errcode in the body
//     to avoid recording failed deliveries as successful.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// DingTalk webhook URLs contain access_token and must be masked in full.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// The DingTalk webhook URL is the destination. Changing it requires the user
// to re-enter the signing key for the new destination.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Webhook 주소가 없습니다")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Webhook 주소가 올바르지 않습니다: %w", err)
	}
	return nil
}

// Send delivers one message. It uses an ActionCard with a button for a single
// item with a detail link, and markdown otherwise.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// DingTalk does not specify a byte limit for markdown bodies, but cap
	// unusually large evidence fields anyway.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    "상세 보기",
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk can return application errors in an HTTP 200 response.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("DingTalk 응답을 해석하지 못했습니다: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 means signature verification failed; 310000 means keywords
		// did not match. Both are configuration errors that retries cannot fix.
		return 0, Permanent(fmt.Errorf("DingTalk에서 오류가 발생했습니다 (%d): %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL appends timestamp and sign parameters to a webhook URL
// according to DingTalk's signing rules.
//
// The string to sign is timestamp + "\n" + secret, and the HMAC-SHA256 key
// is also secret. The result is base64-encoded and then URL-encoded. The
// timestamp is in milliseconds. An empty secret leaves the URL unchanged for
// bots without signing enabled.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Do not propagate err: url.Parse includes the full URL and its
		// access_token in the error text.
		return "", fmt.Errorf("Webhook 주소를 해석하지 못했습니다: %s", redactRequestTarget(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL checks that an address is usable, uses a supported scheme,
// and does not directly target a blocked IP address.
//
// Two important details:
//
//  1. Errors must be redacted. url.Parse may return *url.Error whose Error()
//     includes the full original URL. These providers embed credentials in
//     their URLs: DingTalk access_token, WeCom key, Telegram bot token, and
//     Feishu hook ID. Returning that error directly once leaked credentials
//     into the test endpoint's 400 response, stored last_error values,
//     server logs, and the delivery history API.
//
//  2. Check literal IP addresses here. Domain names are checked at dial time
//     by blockInternalDial, the final enforcement point that also handles DNS
//     rebinding. This early check gives feedback when saving the configuration
//     instead of waiting until the first delivery attempt fails.
//
// Restricting schemes is defensive: unsupported schemes such as file or
// gopher could cause unexpected http.Client behavior.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("주소를 해석할 수 없습니다 (%s)", redactRequestTarget(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("http 또는 https 만 지원합니다. 받은 값: %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("호스트 이름이 없습니다")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return fmt.Errorf("로컬 또는 링크 로컬 주소 %s 로는 전송하지 않습니다 (로컬 서비스로 보내야 하면 %s=1 로 설정하세요)", ip, AllowLocalTargetsEnv)
	}
	return nil
}
