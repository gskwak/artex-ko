package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// weComMarkdownLimit is the hard limit for a WeCom group bot's markdown
// content, measured in bytes rather than characters. It is the tightest
// limit among the six channels and a primary reason for TruncateBytes.
const weComMarkdownLimit = 4096

// weComChannel implements a WeCom group bot.
//
// Platform behavior:
//   - Authentication uses only the key in the URL, with no signing support.
//     The full webhook URL is therefore a credential.
//   - Markdown content is limited to 4096 bytes. An oversized message is
//     rejected in full rather than truncated. Chinese characters typically
//     use three UTF-8 bytes each, leaving room for only about a thousand
//     characters; the client must truncate the body.
//   - The client also enforces the limit of 20 messages per minute.
type weComChannel struct{}

func (weComChannel) Kind() string { return KindWeCom }

func (weComChannel) DefaultRatePerMin() int { return 20 }

// WeCom has only one credential: the key in its webhook URL. It does not
// support signing, so the full URL must be masked and no other field needs it.
func (weComChannel) SecretKeys() []string { return []string{"webhook"} }

// The WeCom webhook is both destination and credential; changing it cannot
// leave a separate credential attached to the new destination.
func (weComChannel) DestinationKeys() []string { return []string{"webhook"} }

func (weComChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Webhook 주소가 없습니다")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Webhook 주소가 올바르지 않습니다: %w", err)
	}
	return nil
}

func (c weComChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	// A summary batch may be long (50 lines plus a prefix) and easily exceed
	// 4096 bytes. Truncate here so the first items are delivered instead of
	// losing the entire batch to a platform rejection.
	content, kept := markdownBody(m, weComMarkdownLimit)
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	}
	raw, err := doJSON(ctx, "POST", cfgString(cfg, "webhook"), nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("WeCom 응답을 해석하지 못했습니다: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 45009 means the API call limit was exceeded. The platform's rate
		// window moves, so retrying after backoff can work. Reaching this branch
		// indicates that the client's rate_per_min is too aggressive; retries
		// are a fallback, while lowering the channel's limit is the real fix.
		if res.ErrCode == 45009 {
			return 0, fmt.Errorf("WeCom 요청이 제한되었습니다 (%d): %s", res.ErrCode, res.ErrMsg)
		}
		// 93000 means an invalid webhook key: a permanent failure that retries
		// cannot fix.
		return 0, Permanent(fmt.Errorf("WeCom에서 오류가 발생했습니다 (%d): %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}
