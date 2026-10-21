package subscription

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRegistrationIdentityAndRefresh(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir())
	var refreshes atomic.Int32
	var nonce = "nonce"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/jwks.json":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/api/accounts/oauth/token":
			r.ParseForm()
			if r.Form.Get("client_id") != "oaiapp_test" || r.Form.Get("resource") != resource {
				t.Error("incorrect client/resource")
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshes.Add(1)
				if r.Form.Get("refresh_token") != "refresh-1" {
					t.Error("wrong refresh credential")
				}
				json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access-2", RefreshToken: "refresh-2", TokenType: "Bearer", ExpiresIn: 3600})
				return
			}
			if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") != "http://127.0.0.1:1455/auth/callback" {
				t.Error("PKCE/redirect changed")
			}
			claims := identityClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{"oaiapp_test"}, Subject: "subject", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now())}, Nonce: nonce, Email: "example@example.invalid"}
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			tok.Header["kid"] = "test"
			raw, _ := tok.SignedString(key)
			json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access-1", RefreshToken: "refresh-1", IDToken: raw, TokenType: "Bearer", ExpiresIn: 3600, Scope: planScope})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	m.authBase = ts.URL
	p := &pending{ClientID: "dynamic_agent_client", Nonce: "nonce", Verifier: "verifier", Redirect: "http://127.0.0.1:1455/auth/callback"}
	if err := m.finishLocked(context.Background(), p, url.Values{"client_id": {"oaiapp_test"}, "code": {"code"}}); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Connected {
		t.Fatal("not connected after verified authorization")
	}
	m.disk.Accounts[m.disk.Active].ExpiresAt = time.Now().Add(-time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := m.AccessToken(context.Background())
			if err != nil || tok != "access-2" {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("rotating token raced: %d refreshes", refreshes.Load())
	}
	reloaded := New(m.dir)
	if reloaded.disk.Accounts["oaiapp_test"].RefreshToken != "refresh-2" || reloaded.disk.HostID != m.disk.HostID {
		t.Fatal("renewal/host ID not persisted")
	}
	public, _ := json.Marshal(m.Status())
	if strings.Contains(string(public), "access-") || strings.Contains(string(public), "refresh-") {
		t.Fatal("credentials exposed in status")
	}
	info, _ := os.Stat(filepath.Join(m.dir, "chatgpt.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential file not private")
	}
	// A signed token with a different nonce must not replace the working account.
	nonce = "wrong"
	p.ClientID = "oaiapp_test"
	if err := m.finishLocked(context.Background(), p, url.Values{"code": {"code"}}); err == nil {
		t.Fatal("wrong nonce accepted")
	}
	if m.disk.Accounts["oaiapp_test"].AccessToken != "access-2" {
		t.Fatal("failed authorization replaced the active credential")
	}
}

func TestAuthorizationStateAndCancellation(t *testing.T) {
	t.Setenv("ARTEX_CHATGPT_CALLBACK_ADDR", "127.0.0.1:0")
	m := New(t.TempDir())
	u, err := m.Begin("")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cancel()
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if q.Get("client_id") != "dynamic_agent_client" || q.Get("code_challenge_method") != "S256" || q.Get("ext_agent_host_id") == "" || !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatal("incorrect registration parameters")
	}
	r := httptest.NewRequest("GET", "/auth/callback?state=wrong&code=x", nil)
	w := httptest.NewRecorder()
	m.callback(w, r)
	if w.Code != 400 || !m.Status().Pending {
		t.Fatal("invalid state consumed legitimate attempt")
	}
	r = httptest.NewRequest("GET", "/auth/callback?state="+q.Get("state")+"&error=access_denied", nil)
	w = httptest.NewRecorder()
	m.callback(w, r)
	if w.Code != 400 || m.Status().Pending || m.Status().Connected {
		t.Fatal("denied permission accepted")
	}
	_, err = m.AccessToken(context.Background())
	if err == nil {
		t.Fatal("unauthorized inference accepted")
	}
}

func TestIdentityRejectsBadClaims(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	m := New(t.TempDir())
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"keys":[{"kty":"RSA","kid":"test","n":"%s","e":"AQAB"}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()))
	}))
	defer ts.Close()
	m.authBase = ts.URL
	for _, kind := range []string{"issuer", "audience", "expired", "signature", "nonce", "subject"} {
		t.Run(kind, func(t *testing.T) {
			c := identityClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{"oaiapp_test"}, Subject: "s", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now())}, Nonce: "n"}
			signing := key
			switch kind {
			case "issuer":
				c.Issuer = "https://example.invalid"
			case "audience":
				c.Audience = jwt.ClaimStrings{"another"}
			case "expired":
				c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			case "nonce":
				c.Nonce = "wrong"
			case "subject":
				c.Subject = ""
			case "signature":
				signing, _ = rsa.GenerateKey(rand.Reader, 2048)
			}
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
			tok.Header["kid"] = "test"
			raw, _ := tok.SignedString(signing)
			if _, err := m.verifyIdentity(context.Background(), raw, "oaiapp_test", "n"); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}

func TestScopeAndMalformedStorageFailClosed(t *testing.T) {
	dir := t.TempDir()
	m := New(dir)
	m.disk.Active = "oaiapp_test"
	m.disk.Accounts[m.disk.Active] = &account{ClientID: m.disk.Active, AccessToken: "secret", Scope: "openid email", ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := m.AccessToken(context.Background()); err == nil {
		t.Fatal("identity-only token used for inference")
	}
	if err := os.WriteFile(filepath.Join(dir, "chatgpt.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir).Begin(""); err == nil {
		t.Fatal("corrupt storage overwritten")
	}
}
