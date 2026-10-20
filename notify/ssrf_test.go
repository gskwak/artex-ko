package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// This file covers two related protections:
//   1. Delivery targets must not use the server to access loopback or cloud metadata (SSRF).
//   2. URL validation errors must not expose credentials in the URL.
//
// Many package tests use httptest receivers on 127.0.0.1, which the guard blocks by
// default. TestMain enables AllowLocalTargetsEnv for those tests. Each SSRF test below
// explicitly clears it to verify the default-deny behavior.

func TestMain(m *testing.M) {
	// Allow regular tests to reach local receivers; SSRF tests temporarily clear this.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// TestDialGuardRejectsLoopbackByDefault verifies the core SSRF protection:
// the connection layer must reject delivery to loopback under default settings.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // Disable the opt-in to restore default behavior.
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("delivery to loopback should be denied by default")
	}
	if hit {
		t.Fatal("request reached the local server; dial guard did not work")
	}
	// The error should explain the opt-in for legitimate local SMTP relays.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("denial error should explain how to opt in: %v", err)
	}
}

// TestDialGuardAllowsLoopbackWhenOptedIn verifies that the opt-in permits local
// Postfix and internal relays used by legitimate deployments.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("delivery should work after explicit opt-in: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // Cloud metadata endpoint; the main reason for this guard.
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // Normalize IPv4-mapped addresses before checking to prevent bypass.
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	// RFC1918 private addresses are deliberately allowed: self-hosted Mattermost and
	// SMTP relays are common legitimate targets. This assertion makes that policy
	// explicit so a future change to private-address handling requires review.
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed (private addresses can be legitimate delivery targets)", s)
		}
	}
}

// TestValidateHTTPURLRejectsLiteralPrivateTargets checks that disallowed literal IPs
// are rejected during configuration, before the first delivery attempt.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s should be rejected during configuration", raw)
		}
	}
	// Public and private addresses pass; private addresses are allowed at dial time too.
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s should pass validation: %v", raw, err)
		}
	}
}

// TestValidateHTTPURLErrorNeverLeaksCredentials covers a previously missed branch.
//
// When url.Parse fails, it returns *url.Error whose Error() includes the full input URL.
// Redacting only http.Client.Do errors misses this path. Tests with file://, gopher://,
// and ftp:// exercise the scheme branch because url.Parse accepts them; passing those
// tests alone does not establish that parse failures are safe.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // Invalid percent escape.
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // Non-numeric port.
		"http://[::1?access_token=" + leakProbeToken,                  // Unmatched bracket.
	}
	for _, raw := range cases {
		// Confirm that this input actually fails url.Parse, or the test may silently
		// exercise a different branch and give false confidence.
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q should fail parsing to exercise the intended branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q should fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// Confirm that the channel's error wrapper does not expose the URL either.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("invalid URL should fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestEmailDialGuardRejectsLoopbackByDefault covers the SMTP channel's dial guard.
//
// The email channel previously used a bare net.Dialer, leaving a gap in SSRF protection:
// a host of 169.254.169.254 or 127.0.0.1 could be reached directly. If the
// smtp.NewClient handshake failed, the remote reply could enter last_error and be
// returned by the delivery-history endpoint. Timing differences between connection
// refusal and timeout could also be used to probe ports.
//
// TestMain enables AllowLocalTargetsEnv for other local-receiver tests, so this test
// must clear it. Otherwise it would pass regardless of whether the guard works.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // Restore default behavior.
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("email delivery to loopback should be denied by default")
	}
	// The Control hook should block the connection before EHLO can be sent.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("SMTP session was established; dial guard did not work")
	}
	// The error should explain the opt-in for legitimate local Postfix relays.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("denial error should explain how to opt in: %v", err)
	}
}

// TestEmailDialGuardAllowsLoopbackWhenOptedIn verifies that explicit opt-in allows
// delivery through self-hosted SMTP and local relays.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("local SMTP delivery should work after explicit opt-in: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("EHLO was not seen; SMTP session was not established")
	}
}
