package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// emailDialTimeout and emailSessionTimeout bound connection setup and the entire
// SMTP session, respectively. net/smtp has no timeout mechanism of its own.
// Without both limits, an unresponsive peer could block the delivery goroutine
// indefinitely and stall the single-goroutine dispatcher and all notifications.
const (
	emailDialTimeout    = 10 * time.Second
	emailSessionTimeout = 45 * time.Second
)

// emailChannel implements SMTP email delivery.
type emailChannel struct{}

func (emailChannel) Kind() string { return KindEmail }

// Email has no platform rate limit, but should not be used to flood recipients.
// Use a generous default.
func (emailChannel) DefaultRatePerMin() int { return 60 }

// Mask only the password. The SMTP host, account, and recipients are not secrets;
// masking them would only make editing harder.
func (emailChannel) SecretKeys() []string { return []string{"password"} }

// host and port determine which server receives the password; tls determines
// whether it is sent over an encrypted connection. Changing any of these
// requires the password to be supplied again, so disabling TLS also requires
// explicit credentials rather than being an incidental edit.
func (emailChannel) DestinationKeys() []string { return []string{"host", "port", "tls"} }

func (emailChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "host") == "" {
		return errors.New("SMTP 서버 주소가 없습니다")
	}
	port := cfgInt(cfg, "port")
	if port <= 0 || port > 65535 {
		return errors.New("SMTP 포트가 올바르지 않습니다 (1-65535 범위여야 합니다)")
	}
	if cfgString(cfg, "from") == "" {
		return errors.New("발신자 주소가 없습니다")
	}
	if len(cfgStrings(cfg, "to")) == 0 {
		return errors.New("수신자 주소가 최소 하나 필요합니다")
	}
	return nil
}

func (c emailChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	host := cfgString(cfg, "host")
	port := cfgInt(cfg, "port")
	from := cfgString(cfg, "from")
	to := cfgStrings(cfg, "to")
	username := cfgString(cfg, "username")
	password := cfgString(cfg, "password")
	implicitTLS := cfgBool(cfg, "tls")

	msg, err := buildEmailMessage(from, to, m)
	if err != nil {
		return 0, Permanent(err)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := emailDial(ctx, addr, host, implicitTLS)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	// Upgrade with STARTTLS if supported. Credentials cannot be sent in a
	// plaintext session (see the authentication handling below).
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return 0, fmt.Errorf("STARTTLS 실패: %w", err)
			}
		}
	}
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			// smtp.PlainAuth refuses to send credentials over an unencrypted
			// connection unless the target is localhost. Preserve that security
			// behavior, but explain the error so users know how to resolve it.
			if strings.Contains(err.Error(), "unencrypted connection") {
				return 0, Permanent(fmt.Errorf("자격 증명 전송을 거부했습니다: 연결이 암호화되지 않았습니다. TLS 를 켜거나, 465 포트(암시적 TLS)를 사용하거나, 「TLS 사용」을 체크하세요 (%w)", err))
			}
			return 0, Permanent(fmt.Errorf("SMTP 인증 실패: %w", err))
		}
	}
	if err := client.Mail(from); err != nil {
		return 0, smtpStageError(fmt.Sprintf("발신자 %s 주소가 거부되었습니다", from), err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return 0, smtpStageError(fmt.Sprintf("수신자 %s 주소가 거부되었습니다", rcpt), err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return 0, fmt.Errorf("SMTP DATA 실패: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return 0, fmt.Errorf("메일 본문 쓰기 실패: %w", err)
	}
	if err := w.Close(); err != nil {
		return 0, fmt.Errorf("메일 제출 실패: %w", err)
	}
	// A failed Quit does not change the fact that the server accepted the message.
	_ = client.Quit()
	// The full HTML body is sent without truncation, so count every item as delivered.
	return len(m.Items), nil
}

// emailDial establishes an SMTP connection.
//
// implicitTLS=true uses TLS from the start, as on port 465; false connects in
// plaintext, as on ports 25/587, then uses STARTTLS. Mixing the two modes fails:
// a plaintext greeting sent to port 465 causes the connection to be closed.
//
// Set the session deadline while establishing the connection. net/smtp.Client
// hides its connection in an unexported field, so a deadline cannot be added
// after handing it over. This also bounds a stalled handshake.
// The dialer's Control hook applies blockInternalDial, the same guard used by
// HTTP channels. Without it, SMTP would bypass SSRF protection: a host such as
// 169.254.169.254 or 127.0.0.1 could be reached directly. On a failed
// smtp.NewClient handshake, the peer's response line can enter last_error and
// be echoed by delivery history, allowing partial response disclosure. Timing
// differences between refused and timed-out connections can also probe ports.
// Checking at dial time also covers DNS rebinding.
func emailDial(ctx context.Context, addr, host string, implicitTLS bool) (*smtp.Client, error) {
	d := &net.Dialer{Timeout: emailDialTimeout, Control: blockInternalDial}
	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("SMTP 서버 연결 실패: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(emailSessionTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP 핸드셰이크 실패: %w", err)
	}
	return client, nil
}

// smtpStageError classifies a stage failure as retryable or permanent based on
// the SMTP reply code.
//
// SMTP 4xx and 5xx replies have different meanings:
//   - 4xx (450 greylisting, 451 local error, 452 insufficient storage) means a
//     temporary refusal; retrying later is appropriate, especially for greylisting.
//   - 5xx (550 unknown user, 553 invalid address) is permanent; retrying is futile.
//
// Treating every error as permanent would fail each first attempt against a
// greylisting server, precisely where automatic retries are needed. The code
// comes from the first three digits of the error text. If it cannot be parsed,
// treat the error as retryable rather than discard a potentially transient failure.
func smtpStageError(what string, err error) error {
	code := smtpReplyCode(err.Error())
	if code >= 500 && code < 600 {
		return Permanent(fmt.Errorf("%s: %w", what, err))
	}
	return fmt.Errorf("%s: %w", what, err)
}

// smtpReplyCode extracts the leading three-digit reply code from SMTP error
// text, returning 0 if none is present. net/smtp does not export the code;
// the error text has a form such as "450 4.7.1 ...".
func smtpReplyCode(text string) int {
	if len(text) < 3 {
		return 0
	}
	n, err := strconv.Atoi(text[:3])
	if err != nil {
		return 0
	}
	return n
}

// buildEmailMessage assembles a complete RFC 5322 message.
//
// Base64 encoding keeps each line within SMTP's 1000-byte limit, which an HTML
// body (especially a digest) can otherwise exceed. It also avoids lines that
// begin with ".", so no SMTP dot-stuffing is needed.
func buildEmailMessage(from string, to []string, m Message) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	// Encode non-ASCII subjects with RFC 2047 so mail clients display them correctly.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", htmlTitle(m)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	// Email has no hard length limit, so leave the body intact.
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(htmlBody(m, 0)))
	// Wrap base64 at 76 characters per line as required by RFC 2045.
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return b.String(), nil
}
