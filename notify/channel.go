package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel is an adapter for a notification channel. Implementations must be
// stateless: multiple channel configurations reuse the same instance concurrently,
// and credentials must always come from cfg.
type Channel interface {
	// Kind returns the channel type identifier, which must match a registry key.
	Kind() string
	// Validate checks required fields and formats when a configuration is saved.
	// Errors are shown directly to the user, so name the missing field instead
	// of returning a generic "invalid configuration" message.
	Validate(cfg map[string]any) error
	// Send delivers one message and returns the number of items actually delivered
	// and any error.
	//
	// Every platform has a message length limit, so a batch summary may be
	// truncated. If the caller marked the entire batch as delivered, omitted
	// items would disappear while the delivery history still reported success.
	// The caller marks only the first kept items as delivered and leaves the rest
	// for the next batch.
	//
	// An error indicates delivery failure; *PermanentError means it should not
	// be retried. On failure, kept has no meaning and the caller should ignore it.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin returns the platform's recommended per-minute delivery
	// limit, used as the default for new channel configurations. Zero means no
	// known limit.
	DefaultRatePerMin() int
	// SecretKeys returns the configuration keys containing credentials. Their
	// values are masked in API responses, and a masked value in an update keeps
	// the stored value. Each adapter must identify its own secrets: WeCom's
	// entire webhook URL is a credential, while DingTalk also has a secret.
	SecretKeys() []string
	// DestinationKeys returns the configuration keys that determine where
	// messages are sent.
	//
	// This matters for security just like SecretKeys. If a destination could be
	// changed while keeping its stored credentials, anyone able to edit a
	// channel could send those credentials to a server they control, defeating
	// credential masking. See PrepareConfigUpdate.
	DestinationKeys() []string
}

// registry explicitly lists every channel instead of relying on init() side
// effects. This keeps the available channels visible in one place and exposes
// omissions when a channel is added.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get returns the implementation for a channel type.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether kind is a supported channel type.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns all supported channel types in alphabetical order for stable
// display in UI dropdowns.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks a delivery failure that should not be retried, such as
// invalid credentials, destination rejection, or an invalid request body.
// Retries help only with transient failures such as network issues, rate limits,
// or remote 5xx errors. Retrying permanent failures obscures the real error in
// retry logs without succeeding.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as a permanent failure. It returns nil for a nil error,
// allowing calls such as `return Permanent(someCheck())`.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether the error chain contains a permanent failure.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// ---- Configuration readers ----
//
// Channel configuration comes from a database JSONB column. After decoding
// with encoding/json, it is a map[string]any containing float64 numbers and
// []any arrays. These helpers normalize those values and tolerate type
// differences from UI forms, such as a port entered as a string.

// cfgString reads a string setting and trims surrounding whitespace, which
// often accompanies values pasted into web forms.
func cfgString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// cfgInt reads an integer setting from either a float64 (the JSON default)
// or a string.
func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// cfgBool reads a boolean setting and accepts strings such as "true" and "1".
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// cfgStrings reads a string array setting, trims whitespace, and drops empty
// strings.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// Also accept a single string for forms that submit only one value.
		if s := cfgString(cfg, key); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cfgMap reads a string map setting (such as custom HTTP headers), trimming
// keys and dropping empty keys.
func cfgMap(cfg map[string]any, key string) map[string]string {
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}
