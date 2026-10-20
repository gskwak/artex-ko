package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// feishuChannel implements a Feishu (including Lark) custom bot using
// interactive cards.
//
// Platform behavior:
//   - The signing algorithm differs from DingTalk's and is easy to get wrong;
//     see the feishuSign comment.
//   - Like DingTalk, application errors can arrive in an HTTP 200 body
//     (code != 0).
//   - Card headers support color templates. Severity-based colors make the
//     severity visible at a glance in the message list.
type feishuChannel struct{}

func (feishuChannel) Kind() string { return KindFeishu }

// Feishu custom bots allow about five requests per second or 100 per minute.
func (feishuChannel) DefaultRatePerMin() int { return 100 }

// The final webhook URL segment uniquely identifies the bot and is a credential.
func (feishuChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// Changing the webhook URL requires the user to re-enter the signing key for
// the new destination.
func (feishuChannel) DestinationKeys() []string { return []string{"webhook"} }

func (feishuChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Webhook 주소가 없습니다")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Webhook 주소가 올바르지 않습니다: %w", err)
	}
	return nil
}

func (c feishuChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	card, kept := feishuCard(m)
	payload := map[string]any{
		"msg_type": "interactive",
		"card":     card,
	}
	// Signing parameters are siblings of the message and appear only when a
	// secret is configured.
	if secret := cfgString(cfg, "secret"); secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"] = ts
		payload["sign"] = feishuSign(ts, secret)
	}
	raw, err := doJSON(ctx, "POST", cfgString(cfg, "webhook"), nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		// Some Feishu webhook versions use these field names; support them too.
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Feishu 응답을 해석하지 못했습니다: %w (%s)", err, snippet(raw))
	}
	if res.Code != 0 {
		return 0, Permanent(fmt.Errorf("Feishu에서 오류가 발생했습니다 (%d): %s", res.Code, res.Msg))
	}
	if res.StatusCode != 0 {
		return 0, Permanent(fmt.Errorf("Feishu에서 오류가 발생했습니다 (%d): %s", res.StatusCode, res.StatusMessage))
	}
	return kept, nil
}

// feishuSign calculates a signature according to Feishu's official rules.
//
// The official example is easy to misread:
//
//	hmac.new(string_to_sign.encode(), digestmod=sha256)
//
// This means the key is timestamp + "\n" + secret and the message is empty.
// Using key=secret and message=stringToSign instead follows DingTalk's
// algorithm. The two algorithms reverse these roles; using DingTalk's version
// here causes signature verification error 19021.
func feishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// feishuSeverityTemplate maps vulnerability severity to a card header color
// template. Unknown severity uses grey to avoid confusion with blue for low.
func feishuSeverityTemplate(severity string) string {
	switch severity {
	case "critical":
		return "red"
	case "high":
		return "orange"
	case "medium":
		return "yellow"
	case "low":
		return "blue"
	default:
		return "grey"
	}
}

// feishuMaxCardBytes is a conservative card content limit. Feishu rejects an
// entire oversized card, so leave room below its official limit for JSON
// wrapper overhead.
const feishuMaxCardBytes = 24000

// feishuCard builds an interactive card and returns the number of items
// actually included. As in markdownBody, only included items should be marked
// as delivered.
func feishuCard(m Message) (map[string]any, int) {
	elements := []any{}
	kept := 0
	if m.Batch {
		// Pack complete items before building the header, which reports how
		// many remaining items will follow in the next message.
		kept = packItemCount(m.Items, feishuMaxCardBytes, markdownReservedBytes, "", byteSize, func(it Item, idx int) string {
			return feishuBatchLine(it, idx+1)
		})
		items := m.Items[:kept]
		elements = append(elements, feishuMarkdownDiv(markdownBatchIntro(m, items, len(m.Items))))
		for i, it := range items {
			elements = append(elements, feishuMarkdownDiv(feishuBatchLine(it, i+1)))
		}
		if m.HomeURL != "" {
			elements = append(elements, feishuButton("플랫폼에서 전체 보기", m.HomeURL))
		}
	} else if len(m.Items) > 0 {
		kept = 1
		it := m.Items[0]
		elements = append(elements, feishuMarkdownDiv(feishuItemLines(it)))
		if it.DetailURL != "" {
			elements = append(elements, feishuButton("상세 보기", it.DetailURL))
		}
	}

	card := map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": markdownTitle(m)}},
		"elements": elements,
	}
	if len(m.Items) > 0 {
		card["header"].(map[string]any)["template"] = feishuSeverityTemplate(m.Items[0].Severity)
	}
	return card, kept
}

func feishuMarkdownDiv(content string) map[string]any {
	return map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": content}}
}

func feishuButton(label, url string) map[string]any {
	return map[string]any{
		"tag": "action",
		"actions": []any{map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "lark_md", "content": label},
			"url":  url,
			"type": "primary",
		}},
	}
}

// feishuItemLines renders the lark_md body for one vulnerability.
//
// lark_md, like markdown, parses links and emphasis. Pass every external
// field through markdownText for single-line formatting and escaping;
// otherwise, a vulnerability title could become a clickable external link.
func feishuItemLines(it Item) string {
	out := fmt.Sprintf("**%s · %s**", SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if it.IsStatusChange() {
		out += fmt.Sprintf("\n**상태 변경**: %s → %s",
			markdownText(StatusLabel(it.FromStatus), 0), markdownText(StatusLabel(it.ToStatus), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		out += fmt.Sprintf("\n**유형**: %s", markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		out += fmt.Sprintf("\n**자산**: %s", markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			out += fmt.Sprintf("\n**개요**: %s", s)
		}
	}
	return out
}

// feishuBatchLine renders one item in a summary card.
func feishuBatchLine(it Item, index int) string {
	line := fmt.Sprintf("**%d. %s · %s**", index, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		line += " — " + markdownText(a, 0)
	}
	return line
}
