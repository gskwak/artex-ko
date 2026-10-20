package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestMaskedValueHidesBodyButKeepsTailHint(t *testing.T) {
	const secret = "https://oapi.dingtalk.com/robot/send?access_token=abcdef123456"
	got := MaskedValue(secret)
	if strings.Contains(got, "abcdef123456") {
		t.Fatalf("masked value leaks the full credential: %q", got)
	}
	if strings.Contains(got, "oapi.dingtalk.com") {
		t.Fatalf("masked value must not expose the address: %q", got)
	}
	// Keep the last six characters so users can identify the bot.
	if !strings.HasSuffix(got, "123456") {
		t.Fatalf("expected the last six characters as an identification hint: %q", got)
	}
	if !IsMasked(got) {
		t.Fatalf("IsMasked must recognize the masked value: %q", got)
	}
}

func TestMaskedValueShortSecretGivesNoHint(t *testing.T) {
	// Showing the last six characters of a short credential would expose it entirely.
	for _, s := range []string{"abc", "abcdef", ""} {
		got := MaskedValue(s)
		if got != MaskedPrefix {
			t.Fatalf("credential of length %d must not show a tail hint, got %q", len(s), got)
		}
		if s != "" && strings.Contains(got, s) {
			t.Fatalf("masked value contains the original value: %q", got)
		}
	}
}

func TestMaskConfigMasksOnlySecrets(t *testing.T) {
	cfg := map[string]any{
		"webhook": "https://example.com/hook?token=SECRETVALUE",
		"secret":  "SECtest123456",
		"port":    float64(587),
		"host":    "smtp.example.com",
	}
	masked := MaskConfig(KindDingTalk, cfg)
	for _, k := range []string{"webhook", "secret"} {
		s, _ := masked[k].(string)
		if !IsMasked(s) {
			t.Errorf("%s should be masked, got %q", k, s)
		}
	}
	// Preserve noncredential fields so the UI can display them.
	if masked["port"] != float64(587) {
		t.Errorf("noncredential field port should remain unchanged: %v", masked["port"])
	}
}

func TestMaskConfigUnknownKindReturnsEmpty(t *testing.T) {
	// For unknown channel types, return an empty config instead of exposing possible credentials.
	got := MaskConfig("nope", map[string]any{"webhook": "https://x/y?token=LEAK"})
	if len(got) != 0 {
		t.Fatalf("unknown channel type should return an empty config, got %v", got)
	}
}

func TestMaskConfigDoesNotMutateInput(t *testing.T) {
	// Masking is for display and must not change the stored value.
	cfg := map[string]any{"webhook": "https://example.com/hook", "secret": "SECtest123456"}
	_ = MaskConfig(KindDingTalk, cfg)
	if IsMasked(cfg["secret"].(string)) {
		t.Fatal("MaskConfig mutated its input, which would replace the real credential with a mask")
	}
}

func TestMergeConfigKeepsStoredOnMaskedIncoming(t *testing.T) {
	stored := map[string]any{"webhook": "https://real/hook", "secret": "REALSECRET", "method": "POST"}
	// The user changed only method; the browser submits masked values plus the new method.
	incoming := map[string]any{
		"webhook": MaskedValue("https://real/hook"),
		"secret":  MaskedValue("REALSECRET"),
		"method":  "PUT",
	}
	got := MergeConfig(stored, incoming)
	if got["webhook"] != "https://real/hook" || got["secret"] != "REALSECRET" {
		t.Fatalf("masked fields should retain their stored values, got %v", got)
	}
	if got["method"] != "PUT" {
		t.Fatalf("changed field should take effect, got %v", got["method"])
	}
}

func TestMergeConfigEmptyStringClears(t *testing.T) {
	stored := map[string]any{"webhook": "https://real/hook", "secret": "REALSECRET"}
	got := MergeConfig(stored, map[string]any{"secret": ""})
	if _, ok := got["secret"]; ok {
		t.Fatalf("empty string should clear the field, got %v", got)
	}
	// Retain omitted fields for partial updates.
	if got["webhook"] != "https://real/hook" {
		t.Fatalf("omitted fields should be retained, got %v", got)
	}
}

func TestMergeConfigKeepsUnmentionedStoredKeys(t *testing.T) {
	stored := map[string]any{"host": "smtp.example.com", "port": float64(587), "password": "pw"}
	got := MergeConfig(stored, map[string]any{"port": float64(465)})
	if got["host"] != "smtp.example.com" || got["password"] != "pw" {
		t.Fatalf("omitted fields should be retained, got %v", got)
	}
	if got["port"] != float64(465) {
		t.Fatalf("supplied field should be updated, got %v", got["port"])
	}
}

// TestPrepareConfigUpdateBlocksDestinationSwap enforces the key security invariant:
// changing the destination must not carry old credentials to the new one.
//
// These cases use attack-shaped inputs: change only the destination while omitting
// credentials. Testing only valid updates would not detect a broken guard.
func TestPrepareConfigUpdateBlocksDestinationSwap(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		stored   map[string]any
		incoming map[string]any
		// wantMissing is the credential key that the error should identify.
		wantMissing string
	}{
		{
			name: "generic Webhook destination change reuses Authorization header",
			kind: KindWebhook,
			stored: map[string]any{
				"url":     "https://legit.example.com/hook",
				"headers": map[string]any{"Authorization": "Bearer REAL-TOKEN"},
			},
			incoming:    map[string]any{"url": "https://attacker.tld/c"},
			wantMissing: "headers",
		},
		{
			name:        "Telegram base_url change sends Bot Token to another endpoint",
			kind:        KindTelegram,
			stored:      map[string]any{"bot_token": "123456:REAL", "chat_id": "1", "base_url": "https://api.telegram.org"},
			incoming:    map[string]any{"base_url": "https://attacker.tld"},
			wantMissing: "bot_token",
		},
		{
			name:        "email SMTP host change exposes password",
			kind:        KindEmail,
			stored:      map[string]any{"host": "smtp.corp.com", "port": 587, "password": "REALPW", "from": "a@b.c", "to": []any{"d@e.f"}},
			incoming:    map[string]any{"host": "smtp.attacker.tld"},
			wantMissing: "password",
		},
		{
			name:        "email TLS change requires password confirmation",
			kind:        KindEmail,
			stored:      map[string]any{"host": "smtp.corp.com", "port": 587, "tls": false, "password": "REALPW", "from": "a@b.c", "to": []any{"d@e.f"}},
			incoming:    map[string]any{"tls": true},
			wantMissing: "password",
		},
		{
			// A masked value means reuse the old credential and must be rejected when the destination changes.
			name:        "masked credential plus new destination",
			kind:        KindTelegram,
			stored:      map[string]any{"bot_token": "123456:REAL", "chat_id": "1", "base_url": "https://api.telegram.org"},
			incoming:    map[string]any{"base_url": "https://attacker.tld", "bot_token": MaskedValue("123456:REAL")},
			wantMissing: "bot_token",
		},
		{
			name:        "DingTalk Webhook change reuses signing secret",
			kind:        KindDingTalk,
			stored:      map[string]any{"webhook": "https://oapi.dingtalk.com/robot/send?access_token=OLD", "secret": "REALSEC"},
			incoming:    map[string]any{"webhook": "https://attacker.tld/hook"},
			wantMissing: "secret",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := PrepareConfigUpdate(tc.kind, tc.stored, tc.incoming)
			if err == nil {
				t.Fatalf("destination changed without confirming credentials; expected rejection, got config %v", merged)
			}
			var target *ErrDestinationChangedWithoutCredentials
			if !errors.As(err, &target) {
				t.Fatalf("expected the dedicated error type for actionable API feedback, got %T: %v", err, err)
			}
			found := false
			for _, m := range target.Missing {
				if m == tc.wantMissing {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected missing credential key %q, got %v", tc.wantMissing, target.Missing)
			}
			// The error should tell the operator which field to fix.
			if !strings.Contains(err.Error(), tc.wantMissing) {
				t.Errorf("error should mention %q: %v", tc.wantMissing, err)
			}
		})
	}
}

// TestPrepareConfigUpdateAllowsLegitimateEdits verifies that normal edits are accepted,
// so the guard does not interrupt routine use.
func TestPrepareConfigUpdateAllowsLegitimateEdits(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		stored   map[string]any
		incoming map[string]any
	}{
		{
			name:     "change only name with unchanged config",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://legit.example.com/hook", "headers": map[string]any{"Authorization": "Bearer REAL"}},
			incoming: map[string]any{"url": MaskedValue("https://legit.example.com/hook")},
		},
		{
			name:     "change only request method without changing destination or credentials",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://legit.example.com/hook", "method": "POST"},
			incoming: map[string]any{"method": "PUT"},
		},
		{
			name:     "change destination and supply new credentials",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://old.example.com/hook", "headers": map[string]any{"Authorization": "Bearer OLD"}},
			incoming: map[string]any{"url": "https://new.example.com/hook", "headers": map[string]any{"Authorization": "Bearer NEW"}},
		},
		{
			name:     "change destination and explicitly clear credentials",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://old.example.com/hook", "headers": map[string]any{"Authorization": "Bearer OLD"}},
			incoming: map[string]any{"url": "https://new.example.com/hook", "headers": ""},
		},
		{
			name:     "change Telegram chat_id without changing destination",
			kind:     KindTelegram,
			stored:   map[string]any{"bot_token": "t", "chat_id": "1", "base_url": "https://api.telegram.org"},
			incoming: map[string]any{"chat_id": "-100200"},
		},
		{
			name:     "change email recipient without changing destination",
			kind:     KindEmail,
			stored:   map[string]any{"host": "smtp.corp.com", "port": 587, "password": "PW", "from": "a@b.c", "to": []any{"x@y.z"}},
			incoming: map[string]any{"to": []any{"new@y.z"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := PrepareConfigUpdate(tc.kind, tc.stored, tc.incoming)
			if err != nil {
				t.Fatalf("valid edit was rejected: %v", err)
			}
			if merged == nil {
				t.Fatal("expected merged result")
			}
		})
	}
}

// TestPrepareConfigUpdatePortTypeTolerance covers a subtle false positive:
// the frontend sends a JSON number and the database reads it as float64, but
// another caller may supply an int. Direct == comparison would treat an
// unchanged port as changed and wrongly ask the user to reenter the password.
func TestPrepareConfigUpdatePortTypeTolerance(t *testing.T) {
	stored := map[string]any{"host": "smtp.corp.com", "port": float64(587), "password": "PW"}
	// Submit the same port as an int.
	if _, err := PrepareConfigUpdate(KindEmail, stored, map[string]any{"port": 587}); err != nil {
		t.Fatalf("same port value with a different type should not count as destination change: %v", err)
	}
	// A real port change must be blocked.
	if _, err := PrepareConfigUpdate(KindEmail, stored, map[string]any{"port": 25}); err == nil {
		t.Fatal("port change should be blocked")
	}
}

// TestPrepareConfigUpdateSurvivesRepeatedSaveWithBlankDestination covers an
// optional destination field: blank Telegram base_url means the official API.
//
// Previously, the second and later saves always failed:
//
//	Creation stored base_url:"" without calling MergeConfig.
//	First save: MergeConfig deleted the key because the value was blank.
//	Second save: incoming still had "", stored had no key, so the destination appeared changed.
//	bot_token was a masked value, triggering a 400 asking for credentials again.
//
// The user could not save an unchanged channel without pasting the Bot Token again.
func TestPrepareConfigUpdateSurvivesRepeatedSaveWithBlankDestination(t *testing.T) {
	stored := map[string]any{"bot_token": "123:ABC", "chat_id": "-100", "base_url": ""}

	// The frontend's buildConfig() submits every field: masks for credentials
	// and empty strings for blank text inputs. Reproduce that full submission.
	submit := func() map[string]any {
		return map[string]any{
			"bot_token": MaskedValue("123:ABC"),
			"chat_id":   "-100",
			"base_url":  "",
		}
	}

	// First save: only the channel name changed, and config is sent unchanged.
	merged, err := PrepareConfigUpdate(KindTelegram, stored, submit())
	if err != nil {
		t.Fatalf("first save was incorrectly rejected: %v", err)
	}
	if _, ok := merged["base_url"]; ok {
		t.Fatal("precondition changed: MergeConfig should delete the blank value so this test covers the missing-key case")
	}

	// Second save: the user submits the same values without any changes.
	merged2, err := PrepareConfigUpdate(KindTelegram, merged, submit())
	if err != nil {
		t.Fatalf("second unchanged save was incorrectly rejected: %v", err)
	}
	// Third save: confirm the channel remains saveable.
	if _, err := PrepareConfigUpdate(KindTelegram, merged2, submit()); err != nil {
		t.Fatalf("third save was incorrectly rejected: %v", err)
	}
	// The blank-value handling must retain the credential through every save.
	if got := merged2["bot_token"]; got != "123:ABC" {
		t.Fatalf("Bot Token should retain its original value, got %v", got)
	}
}

// TestPrepareConfigUpdateStillGuardsBlankDestinationChanges pairs with the
// previous test: treating blank and missing as equivalent must not allow an
// actual destination change. Telegram puts the Bot Token in the URL path, so
// changing base_url can send it to a new endpoint.
func TestPrepareConfigUpdateStillGuardsBlankDestinationChanges(t *testing.T) {
	// Change from blank (official API) to a custom endpoint.
	official := map[string]any{"bot_token": "123:ABC", "chat_id": "-100"}
	if _, err := PrepareConfigUpdate(KindTelegram, official, map[string]any{
		"bot_token": MaskedValue("123:ABC"),
		"base_url":  "https://tg-proxy.attacker.tld",
	}); err == nil {
		t.Fatal("switching from the official API to a custom endpoint must require a new Token")
	}

	// Clearing a custom endpoint to restore the official API is also a destination change.
	proxied := map[string]any{"bot_token": "123:ABC", "base_url": "https://proxy.internal/bot"}
	if _, err := PrepareConfigUpdate(KindTelegram, proxied, map[string]any{
		"bot_token": MaskedValue("123:ABC"),
		"base_url":  "",
	}); err == nil {
		t.Fatal("clearing a custom endpoint must also require a new Token")
	}
}

func TestDestinationKeysDeclaredForEveryKind(t *testing.T) {
	// Every channel must declare destination keys so PrepareConfigUpdate can protect them.
	for kind, ch := range registry {
		if len(ch.DestinationKeys()) == 0 {
			t.Errorf("channel %s has no destination keys; credential protection is ineffective", kind)
		}
		if len(ch.SecretKeys()) == 0 {
			t.Errorf("channel %s has no credential keys", kind)
		}
	}
}

func TestSecretKeysDeclaredForEveryKind(t *testing.T) {
	// The compiler requires SecretKeys, but this also verifies that each
	// channel declares at least one key. An empty slice would expose credentials.
	expect := map[string]bool{
		KindDingTalk: true, KindFeishu: true, KindWeCom: true,
		KindWebhook: true, KindTelegram: true, KindEmail: true,
	}
	for kind, ch := range registry {
		if !expect[kind] {
			t.Errorf("channel %s has no registered masking expectation in this test", kind)
			continue
		}
		if len(ch.SecretKeys()) == 0 {
			t.Errorf("channel %s declares no credential fields; its config would be exposed", kind)
		}
	}
}

// TestPrepareConfigUpdateRejectsMaskedInContainer covers a masking gap:
// MergeConfig recognizes masked strings but would otherwise store a mask
// nested in a nonstring value such as webhook.headers as a literal header,
// silently breaking authentication.
func TestPrepareConfigUpdateRejectsMaskedInContainer(t *testing.T) {
	stored := map[string]any{
		"url":     "https://legit.example.com/hook",
		"headers": map[string]any{"Authorization": "Bearer REAL"},
	}
	// Nest a mask sentinel inside the object.
	incoming := map[string]any{
		"headers": map[string]any{"Authorization": MaskedPrefix},
	}
	if _, err := PrepareConfigUpdate(KindWebhook, stored, incoming); err == nil {
		t.Fatal("a nested mask sentinel should be rejected instead of stored literally")
	}
	// Accept a full object containing genuine new values.
	ok := map[string]any{"headers": map[string]any{"Authorization": "Bearer NEW"}}
	if _, err := PrepareConfigUpdate(KindWebhook, stored, ok); err != nil {
		t.Fatalf("new request headers should be accepted: %v", err)
	}
}
