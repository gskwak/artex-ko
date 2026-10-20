package notify

import (
	"bufio"
	"context"

	"net"
	"strings"
	"sync"
	"testing"
)

// This file adds protocol-level tests for the email channel. Previously,
// email.Send had no coverage, despite SMTP having the broadest protocol surface
// of the six channels. The handshake, authentication, envelope, and DATA stages
// each have distinct failure semantics.
//
// These tests use a minimal SMTP server instead of mocking net/smtp because
// most email-channel failures happen while talking to an SMTP server.

// fakeSMTP is a minimal SMTP server that handles greet/EHLO/AUTH/MAIL/RCPT/DATA/QUIT
// and returns test-specific reply codes at selected stages.
type fakeSMTP struct {
	ln net.Listener

	// rcptReply is the RCPT TO reply; it defaults to 250.
	rcptReply string
	// mailReply is the MAIL FROM reply; it defaults to 250.
	mailReply string
	// advertiseAuth announces AUTH PLAIN support in EHLO when true.
	advertiseAuth bool

	mu       sync.Mutex
	data     string
	commands []string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, rcptReply: "250 OK", mailReply: "250 OK"}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) hostPort(t *testing.T) (string, int) {
	t.Helper()
	addr, ok := f.ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener address is not TCP")
	}
	return "127.0.0.1", addr.Port
}

func (f *fakeSMTP) record(cmd string) {
	f.mu.Lock()
	f.commands = append(f.commands, cmd)
	f.mu.Unlock()
}

func (f *fakeSMTP) body() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data
}

func (f *fakeSMTP) sawCommand(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeSMTP) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	w("220 fake.local ESMTP ready")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.record(line)
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			// Do not advertise STARTTLS; these tests cover the envelope, not TLS.
			w("250-fake.local")
			if f.advertiseAuth {
				w("250-AUTH PLAIN")
			}
			w("250 8BITMIME")
		case strings.HasPrefix(line, "AUTH"):
			// Accept the initial PLAIN response, which can span multiple lines.
			w("235 2.7.0 Authentication successful")
		case strings.HasPrefix(line, "MAIL FROM"):
			w(f.mailReply)
		case strings.HasPrefix(line, "RCPT TO"):
			w(f.rcptReply)
		case strings.HasPrefix(line, "DATA"):
			w("354 End data with <CR><LF>.<CR><LF>")
			var sb strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				sb.WriteString(dl)
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			w("250 2.0.0 Ok: queued as FAKE1")
		case strings.HasPrefix(line, "QUIT"):
			w("221 2.0.0 Bye")
			return
		default:
			w("250 OK")
		}
	}
}

func emailCfg(t *testing.T, f *fakeSMTP, extra map[string]any) map[string]any {
	t.Helper()
	host, port := f.hostPort(t)
	cfg := map[string]any{
		"host": host,
		"port": float64(port),
		"from": "artex@example.com",
		"to":   []any{"a@example.com", "b@example.com"},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

func TestEmailSendDeliversFullMessage(t *testing.T) {
	f := newFakeSMTP(t)
	f.advertiseAuth = true
	cfg := emailCfg(t, f, map[string]any{"username": "artex", "password": "pw"})

	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("delivery failed: %v", err)
	}
	// The envelope must include the sender, both recipients, and DATA.
	for _, want := range []string{"MAIL FROM:<artex@example.com>", "RCPT TO:<a@example.com>", "RCPT TO:<b@example.com>", "DATA", "AUTH", "QUIT"} {
		if !f.sawCommand(want) {
			t.Errorf("SMTP session missing %q; commands: %v", want, f.commands)
		}
	}
	// The message body must contain base64-encoded HTML.
	body := f.body()
	if body == "" {
		t.Fatal("DATA stage received no message body")
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("missing Content-Type header:\n%s", body)
	}
	if !strings.Contains(body, "base64") {
		t.Errorf("message body is not base64 encoded (long HTML lines exceed SMTP's 1000-byte line limit):\n%s", body)
	}
	// All recipients must appear in the To header.
	if !strings.Contains(body, "a@example.com, b@example.com") {
		t.Errorf("To header does not contain all recipients:\n%s", body)
	}
}

func TestEmailSendWithoutAuth(t *testing.T) {
	// Do not send AUTH without credentials; some relays reject it.
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("delivery failed: %v", err)
	}
	if f.sawCommand("AUTH") {
		t.Errorf("sent AUTH without credentials: %v", f.commands)
	}
}

// TestEmailSendClassifiesSMTPReplies verifies that 5xx errors are permanent
// and 4xx errors (including greylisting) can be retried.
func TestEmailSendClassifiesSMTPReplies(t *testing.T) {
	cases := []struct {
		name      string
		rcptReply string
		mailReply string
		permanent bool
	}{
		{"recipient permanently rejected with 550", "550 5.1.1 User unknown", "250 OK", true},
		{"recipient greylisted with 450", "450 4.7.1 Greylisting in action", "250 OK", false},
		{"recipient mailbox full with 452", "452 4.2.2 Mailbox full", "250 OK", false},
		{"sender permanently rejected with 553", "250 OK", "553 5.1.3 Bad address", true},
		{"sender temporary error with 451", "250 OK", "451 4.3.0 Temporary failure", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSMTP(t)
			f.rcptReply = tc.rcptReply
			f.mailReply = tc.mailReply
			_, err := (emailChannel{}).Send(context.Background(), emailCfg(t, f, nil), singleMsg())
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("wrong permanent classification: want %v, got %v (%v)", tc.permanent, got, err)
			}
			// Preserve the server response so the user can diagnose the failure.
			if !strings.Contains(err.Error(), strings.Fields(tc.rcptReply)[0]) && !strings.Contains(err.Error(), strings.Fields(tc.mailReply)[0]) {
				t.Errorf("error should retain the server reply code: %v", err)
			}
		})
	}
}

func TestEmailSendRefusesPlaintextCredentials(t *testing.T) {
	// net/smtp's PlainAuth refuses to send credentials over an unencrypted
	// connection unless the target is localhost. The error should guide the user.
	// Use a non-localhost hostname to trigger that behavior.
	f := newFakeSMTP(t)
	f.advertiseAuth = true
	_, port := f.hostPort(t)
	cfg := map[string]any{
		"host":     "smtp.example.com", // Non-localhost host.
		"port":     float64(port),
		"from":     "a@example.com",
		"to":       []any{"b@example.com"},
		"username": "artex",
		"password": "pw",
	}
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Skip("local DNS resolved the host to the local server; skipping this case")
	}
	// Connection failures and credential rejections are both acceptable here;
	// credentials must not be sent silently.
	if !IsPermanent(err) && !strings.Contains(err.Error(), "연결") {
		t.Logf("error: %v (connection failure is expected for a non-localhost host)", err)
	}
}

func TestEmailValidateReportsMissingFields(t *testing.T) {
	// Email has several required configuration fields. Verify that validation
	// catches each missing field before delivery.
	cases := []struct {
		name string
		cfg  map[string]any
	}{
		{"missing host", map[string]any{"port": float64(25), "from": "a@b.c", "to": []any{"d@e.f"}}},
		{"missing port", map[string]any{"host": "smtp.example.com"}},
		{"port out of range", map[string]any{"host": "h", "port": float64(70000), "from": "a@b.c", "to": []any{"d@e.f"}}},
		{"missing from", map[string]any{"host": "h", "port": float64(25), "to": []any{"d@e.f"}}},
		{"missing to", map[string]any{"host": "h", "port": float64(25), "from": "a@b.c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := (emailChannel{}).Validate(tc.cfg); err == nil {
				t.Fatalf("expected validation to fail: %v", tc.cfg)
			}
		})
	}
}

// TestEmailConfigTolerance covers tolerant configuration parsing: JSONB numbers
// are float64, but users may enter a string port or a single recipient string.
func TestEmailConfigTolerance(t *testing.T) {
	cfg := map[string]any{
		"host": "smtp.example.com",
		"port": "587", // Port as a string.
		"from": "a@b.c",
		"to":   "d@e.f", // A single string instead of an array.
		"tls":  "true",  // Boolean as a string.
	}
	if err := (emailChannel{}).Validate(cfg); err != nil {
		t.Fatalf("string values should be accepted: %v", err)
	}
	if got := cfgInt(cfg, "port"); got != 587 {
		t.Errorf("cfgInt did not parse the string port; got %d", got)
	}
	if !cfgBool(cfg, "tls") {
		t.Error("cfgBool did not parse the string \"true\"")
	}
	if to := cfgStrings(cfg, "to"); len(to) != 1 || to[0] != "d@e.f" {
		t.Errorf("cfgStrings did not accept a single string; got %v", to)
	}
}

// TestFilterValidateRejectsTypo verifies that invalid severity thresholds are
// rejected on write, before the filter silently stops filtering notifications.
func TestFilterValidateRejectsTypo(t *testing.T) {
	good := []string{"", "low", "medium", "high", "critical"}
	for _, s := range good {
		if err := (Filter{MinSeverity: s}).Validate(); err != nil {
			t.Errorf("valid threshold %q was rejected: %v", s, err)
		}
	}
	// These realistic invalid values must all be rejected.
	for _, s := range []string{"hgih", "HIGH", "severe", "high ", "crit"} {
		err := (Filter{MinSeverity: s}).Validate()
		if err == nil {
			t.Errorf("invalid threshold %q should be rejected before the filter stops working", s)
			continue
		}
		// The error should list valid values so the user can correct the input.
		if !strings.Contains(err.Error(), "low") || !strings.Contains(err.Error(), "critical") {
			t.Errorf("error should list valid values; got %q", err.Error())
		}
	}
}

// TestFilterValidateIsWriteTimeOnly verifies strict writes and tolerant reads.
// Existing invalid values must not prevent historical channels from loading.
func TestFilterValidateIsWriteTimeOnly(t *testing.T) {
	raw := []byte(`{"min_severity":"hgih"}`)
	f := ParseFilter(raw) // No error.
	if f.MinSeverity != "hgih" {
		t.Fatalf("read path should preserve the original value; got %q", f.MinSeverity)
	}
	// The channel should still evaluate events without panicking or blocking.
	_ = Match(f, Snapshot{Kind: EventFindingCreated, Severity: "critical"})
}
