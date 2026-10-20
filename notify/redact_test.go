package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// This file tests the invariant that errors from any channel implementation must never contain credentials.
//
// The original channel tests covered success and platform errors, but not transport
// failures. Transport errors (connection refused, DNS failure, timeout) are especially
// risky: the *url.Error returned by http.Client.Do includes the full URL, and these
// channels put credentials in that URL. They could then reach four destinations:
//
//	notification_deliveries.last_error  -> stored in plaintext
//	GET /api/notify/deliveries response -> returned to the browser without config masking
//	server logs                        -> often exported and retained
//	test-send endpoint's 502 response  -> displayed directly in the UI
//
// Each channel therefore sends a request that must fail, then checks that the resulting
// error does not contain the credential.

// credentialCases covers every channel that puts credentials in a URL:
// DingTalk and WeCom in the query, Feishu at the end of the path, and Telegram in the middle.
var credentialCases = []struct {
	name   string
	ch     Channel
	cfg    map[string]any
	secret string
}{
	{
		name:   "DingTalk access_token in query",
		ch:     dingTalkChannel{},
		cfg:    map[string]any{"webhook": "http://127.0.0.1:1/robot/send?access_token=" + leakProbeToken},
		secret: leakProbeToken,
	},
	{
		name:   "WeCom key in query",
		ch:     weComChannel{},
		cfg:    map[string]any{"webhook": "http://127.0.0.1:1/cgi-bin/webhook/send?key=" + leakProbeToken},
		secret: leakProbeToken,
	},
	{
		name:   "Feishu hook ID at end of path",
		ch:     feishuChannel{},
		cfg:    map[string]any{"webhook": "http://127.0.0.1:1/open-apis/bot/v2/hook/" + leakProbeToken},
		secret: leakProbeToken,
	},
	{
		name:   "Telegram bot token in middle of path",
		ch:     telegramChannel{},
		cfg:    map[string]any{"bot_token": leakProbeToken, "chat_id": "1", "base_url": "http://127.0.0.1:1"},
		secret: leakProbeToken,
	},
	{
		name:   "DingTalk signing secret",
		ch:     dingTalkChannel{},
		cfg:    map[string]any{"webhook": "http://127.0.0.1:1/robot/send", "secret": leakProbeToken},
		secret: leakProbeToken,
	},
}

// leakProbeToken is a sentinel that cannot be a real credential; tests search errors for it.
const leakProbeToken = "LEAKPROBE0123456789abcdef"

// TestChannelErrorsNeverLeakCredentials checks the core invariant.
func TestChannelErrorsNeverLeakCredentials(t *testing.T) {
	for _, tc := range credentialCases {
		t.Run(tc.name, func(t *testing.T) {
			// Nothing listens on 127.0.0.1:1, so the request takes the connection-refused path.
			_, err := tc.ch.Send(context.Background(), tc.cfg, Message{
				Items: []Item{{FindingID: 1, Severity: "high", Name: "Leak probe"}},
			})
			if err == nil {
				t.Fatal("expected an error for an unreachable address")
			}
			assertNoSecret(t, err.Error(), tc.secret)
		})
	}
}

// TestChannelErrorsNeverLeakCredentialsInPermanentPath covers permanent failures.
// URL validation and platform errors can also expose error text and must omit credentials.
func TestChannelErrorsNeverLeakCredentialsInPermanentPath(t *testing.T) {
	cases := []struct {
		name string
		ch   Channel
		cfg  map[string]any
	}{
		// Each URL contains a credential but uses an invalid scheme, exercising URL validation.
		{"invalid DingTalk URL", dingTalkChannel{}, map[string]any{"webhook": "file:///" + leakProbeToken}},
		{"invalid WeCom URL", weComChannel{}, map[string]any{"webhook": "gopher://" + leakProbeToken}},
		{"invalid Feishu URL", feishuChannel{}, map[string]any{"webhook": "ftp://" + leakProbeToken + "/hook"}},
		{"invalid Telegram API URL", telegramChannel{}, map[string]any{"bot_token": "tok", "chat_id": "1", "base_url": "file://" + leakProbeToken}},
		{"invalid generic webhook URL", webhookChannel{}, map[string]any{"url": "javascript:" + leakProbeToken}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.ch.Send(context.Background(), tc.cfg, Message{Items: []Item{{Severity: "high"}}})
			if err == nil {
				t.Fatal("expected an error for invalid configuration")
			}
			assertNoSecret(t, err.Error(), leakProbeToken)
		})
	}
}

func assertNoSecret(t *testing.T, text, secret string) {
	t.Helper()
	if strings.Contains(text, secret) {
		t.Fatalf("error text leaked credential %q:\n    %s", secret, text)
	}
}

func TestRedactRequestTargetKeepsOnlySchemeAndHost(t *testing.T) {
	cases := map[string]string{
		"https://oapi.dingtalk.com/robot/send?access_token=S1":    "https://oapi.dingtalk.com/…",
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=S2": "https://qyapi.weixin.qq.com/…",
		"https://open.feishu.cn/open-apis/bot/v2/hook/S3":         "https://open.feishu.cn/…",
		"https://api.telegram.org/botS4/sendMessage":              "https://api.telegram.org/…",
		"http://10.0.0.5:8080/hook":                               "http://10.0.0.5:8080/…",
	}
	for in, want := range cases {
		got := redactRequestTarget(in)
		if got != want {
			t.Errorf("redactRequestTarget(%q) = %q, want %q", in, got, want)
		}
		// The redacted result must contain no path or query fragment from the original URL.
		if parts := strings.SplitN(in, "://", 2); len(parts) == 2 {
			if hostAndRest := strings.SplitN(parts[1], "/", 2); len(hostAndRest) == 2 && hostAndRest[1] != "" {
				if strings.Contains(got, hostAndRest[1]) {
					t.Errorf("redacted URL still contains path/query fragment %q: %q", hostAndRest[1], got)
				}
			}
		}
	}
	// Unparseable input must never be echoed back.
	for _, bad := range []string{"", "://", "not a url", "http://"} {
		if got := redactRequestTarget(bad); strings.Contains(got, bad) && bad != "" {
			t.Errorf("unparseable input %q was echoed back as %q", bad, got)
		}
	}
}

// TestRedactTransportErrorStripsURL checks *url.Error directly. It is returned by
// http.Client.Do and is the first place the URL can leak.
func TestRedactTransportErrorStripsURL(t *testing.T) {
	inner := errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	uerr := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot" + leakProbeToken + "/sendMessage",
		Err: inner,
	}
	got := redactTransportError(uerr)
	assertNoSecret(t, got, leakProbeToken)
	if !strings.Contains(got, "api.telegram.org") {
		t.Errorf("expected host to remain for debugging, got %q", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("expected underlying cause to remain for debugging, got %q", got)
	}
	// Keep the operation too; distinguishing POST from GET helps debugging.
	if !strings.Contains(got, "Post") {
		t.Errorf("expected operation name to remain, got %q", got)
	}
}

// TestRedactURLsInTextHandlesFallback covers custom errors that are not *url.Error,
// such as redirect-policy errors. URLs in them must also be removed.
func TestRedactURLsInTextHandlesFallback(t *testing.T) {
	in := fmt.Sprintf("cross-host redirect refused (a.example -> http://b.example/bot%s/send)", leakProbeToken)
	got := redactURLsInText(in)
	assertNoSecret(t, got, leakProbeToken)
	if !strings.Contains(got, "http://b.example/…") {
		t.Errorf("expected URL to be redacted, got %q", got)
	}
	// Leave text without URLs unchanged.
	if plain := "dial tcp: connection refused"; redactURLsInText(plain) != plain {
		t.Error("text without URLs should be unchanged")
	}
}

// TestCrossHostRedirectRefused checks that following a redirect to a different host
// cannot disclose a credential in the URL. The two httptest servers use different
// ports on 127.0.0.1, so they have different Host values.
func TestCrossHostRedirectRefused(t *testing.T) {
	var hit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/robot/send?access_token="+leakProbeToken, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": redirector.URL + "/robot/send?access_token=" + leakProbeToken},
		Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("expected cross-host redirect to be refused")
	}
	if hit {
		t.Fatal("redirect target was contacted; credential leaked with redirect")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestSameHostRedirectAllowed checks that same-host redirects, such as adding a trailing
// slash, continue to work.
func TestSameHostRedirectAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robot/send" {
			// Redirect on the same host and port.
			http.Redirect(w, r, "/robot/send/", http.StatusTemporaryRedirect)
			return
		}
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"},
		Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("same-host redirect should not be refused: %v", err)
	}
}
