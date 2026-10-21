// Package subscription manages locally authorized subscription connections.
// Credentials never leave the backend and are separate from LLM profile API keys.
package subscription

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const resource = "https://api.openai.com/v1"
const issuer = "https://auth.openai.com"
const planScope = "chatgpt.tokens.use.direct"

type account struct {
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
}
type diskState struct {
	HostID   string              `json:"host_id"`
	Active   string              `json:"active"`
	Accounts map[string]*account `json:"accounts"`
}
type pending struct {
	State, Nonce, Verifier, ClientID, Redirect string
	Expires                                    time.Time
	Server                                     *http.Server
}
type Manager struct {
	mu        sync.Mutex // Also serializes refresh-token rotation for this runtime.
	dir       string
	disk      diskState
	pending   *pending
	lastError string
	initErr   error
	client    *http.Client
	authBase  string
}
type AccountStatus struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Connected bool   `json:"connected"`
}
type Status struct {
	Active    string          `json:"active"`
	Accounts  []AccountStatus `json:"accounts"`
	Connected bool            `json:"connected"`
	Pending   bool            `json:"pending"`
	Error     string          `json:"error,omitempty"`
}

func New(dir string) *Manager {
	m := &Manager{dir: dir, authBase: issuer, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	m.disk.Accounts = map[string]*account{}
	b, err := os.ReadFile(filepath.Join(dir, "chatgpt.json"))
	if err == nil {
		err = json.Unmarshal(b, &m.disk)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.initErr = fmt.Errorf("cannot read subscription credentials")
		return m
	}
	if m.disk.Accounts == nil {
		m.disk.Accounts = map[string]*account{}
	}
	if m.disk.HostID == "" {
		m.disk.HostID = "urn:uuid:" + uuid.NewString()
		m.initErr = m.saveLocked()
	}
	return m
}

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(m.disk)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".chatgpt-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(m.dir, "chatgpt.json"))
}
func permitted(scope string) bool {
	for _, s := range strings.Fields(scope) {
		if s == planScope {
			return true
		}
	}
	return false
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Active: m.disk.Active, Accounts: []AccountStatus{}, Pending: m.pending != nil, Error: m.lastError}
	if m.initErr != nil {
		s.Error = m.initErr.Error()
	}
	for id, a := range m.disk.Accounts {
		connected := a.AccessToken != "" && permitted(a.Scope) && (time.Now().Before(a.ExpiresAt) || a.RefreshToken != "")
		s.Accounts = append(s.Accounts, AccountStatus{ID: id, Email: a.Email, Connected: connected})
		if id == s.Active {
			s.Connected = connected
		}
	}
	return s
}
func randomString() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Begin starts a separate loopback callback listener. Docker publishes this port
// on host 127.0.0.1; remote/browser-hosted deployments are intentionally excluded.
func (m *Manager) Begin(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return "", m.initErr
	}
	if m.pending != nil {
		return "", errors.New("a ChatGPT sign-in is already pending; cancel it or wait ten minutes")
	}
	clientID := "dynamic_agent_client"
	if id != "" {
		a := m.disk.Accounts[id]
		if a == nil {
			return "", errors.New("unknown ChatGPT account")
		}
		clientID = a.ClientID
	}
	addr := os.Getenv("ARTEX_CHATGPT_CALLBACK_ADDR")
	if addr == "" {
		addr = "127.0.0.1:1455"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("cannot open ChatGPT callback listener: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	p := &pending{State: randomString(), Nonce: randomString(), Verifier: randomString(), ClientID: clientID, Redirect: fmt.Sprintf("http://127.0.0.1:%d/auth/callback", port), Expires: time.Now().Add(10 * time.Minute)}
	p.Server = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(m.callback)}
	m.pending, m.lastError = p, ""
	go func() { _ = p.Server.Serve(ln) }()
	go func() {
		timer := time.NewTimer(10 * time.Minute)
		defer timer.Stop()
		<-timer.C
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pending == p {
			m.pending = nil
			m.lastError = "ChatGPT sign-in expired; try again"
		}
		_ = p.Server.Close()
	}()
	hash := sha256.Sum256([]byte(p.Verifier))
	q := url.Values{"client_id": {clientID}, "ext_agent_host_id": {m.disk.HostID}, "response_type": {"code"}, "redirect_uri": {p.Redirect}, "scope": {"openid profile email offline_access resource.invoke " + planScope}, "resource": {resource}, "state": {p.State}, "nonce": {p.Nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
	if id == "" {
		q.Set("agent_name_hint", "ARTEX")
	} else if a := m.disk.Accounts[id]; a.Email != "" {
		q.Set("login_hint", a.Email)
	}
	return m.authBase + "/api/accounts/authorize?" + q.Encode(), nil
}
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		_ = m.pending.Server.Close()
		m.pending = nil
	}
	m.lastError = ""
}

func (m *Manager) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method != http.MethodGet || r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pending
	if p == nil || time.Now().After(p.Expires) || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(p.State)) != 1 {
		http.Error(w, "Invalid or expired sign-in state", 400)
		return
	}
	m.pending = nil // One-time callback, including errors and denied consent.
	defer func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = p.Server.Shutdown(ctx)
		}()
	}()
	if err := m.finishLocked(r.Context(), p, r.URL.Query()); err != nil {
		m.lastError = err.Error()
		http.Error(w, m.lastError, 400)
		return
	}
	m.lastError = ""
	fmt.Fprint(w, "ChatGPT connected. You can close this tab and return to ARTEX / ChatGPT 연결 완료. ARTEX로 돌아가세요.")
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (m *Manager) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	var t tokenResponse
	form.Set("resource", resource)
	req, _ := http.NewRequestWithContext(ctx, "POST", m.authBase+"/api/accounts/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := m.client.Do(req)
	if err != nil {
		return t, errors.New("OpenAI authorization service unavailable; try again")
	}
	defer res.Body.Close()
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t); err != nil {
		return t, errors.New("invalid OpenAI token response")
	}
	if res.StatusCode != 200 {
		if t.Error == "invalid_grant" {
			return t, errors.New("ChatGPT authorization expired or revoked; sign in again")
		}
		return t, fmt.Errorf("OpenAI token request failed (HTTP %d)", res.StatusCode)
	}
	if t.AccessToken == "" || t.ExpiresIn <= 0 || !strings.EqualFold(t.TokenType, "Bearer") {
		return t, errors.New("incomplete OpenAI token response")
	}
	return t, nil
}
func (m *Manager) finishLocked(ctx context.Context, p *pending, q url.Values) error {
	if q.Get("error") != "" {
		return errors.New("ChatGPT sign-in was declined; no connection was changed")
	}
	if q.Get("code") == "" {
		return errors.New("missing authorization code")
	}
	id := p.ClientID
	if id == "dynamic_agent_client" {
		id = q.Get("client_id")
		if !strings.HasPrefix(id, "oaiapp_") {
			return errors.New("registration returned no issued client ID")
		}
		if _, exists := m.disk.Accounts[id]; exists {
			return errors.New("registration already exists; select the saved account and sign in again")
		}
		m.disk.Accounts[id] = &account{ClientID: id}
		if err := m.saveLocked(); err != nil {
			return errors.New("cannot save ChatGPT registration")
		}
	} else if q.Get("client_id") != "" && q.Get("client_id") != id {
		return errors.New("ChatGPT client ID changed during sign-in")
	}
	t, err := m.token(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {q.Get("code")}, "code_verifier": {p.Verifier}, "redirect_uri": {p.Redirect}})
	if err != nil {
		return err
	}
	claims, err := m.verifyIdentity(ctx, t.IDToken, id, p.Nonce)
	if err != nil {
		return err
	}
	old := m.disk.Accounts[id]
	if old != nil && old.Subject != "" && old.Subject != claims.Subject {
		return errors.New("ChatGPT account does not match the selected registration")
	}
	if !permitted(t.Scope) {
		return errors.New("ChatGPT plan permission was not granted; sign in again and enable plan usage")
	}
	a := &account{ClientID: id, Subject: claims.Subject, Email: claims.Email, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second), Scope: t.Scope}
	oldActive := m.disk.Active
	m.disk.Accounts[id], m.disk.Active = a, id
	if err := m.saveLocked(); err != nil {
		m.disk.Accounts[id], m.disk.Active = old, oldActive
		return errors.New("cannot save ChatGPT credentials")
	}
	return nil
}

type identityClaims struct {
	jwt.RegisteredClaims
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func (m *Manager) verifyIdentity(ctx context.Context, raw, clientID, nonce string) (*identityClaims, error) {
	claims := &identityClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", m.authBase+"/.well-known/jwks.json", nil)
		res, err := m.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, errors.New("JWKS unavailable")
		}
		var keys struct {
			Keys []struct{ Kty, Kid, N, E, Alg string } `json:"keys"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&keys); err != nil {
			return nil, err
		}
		for _, k := range keys.Keys {
			if k.Kid == tok.Header["kid"] && k.Kty == "RSA" && (k.Alg == "" || k.Alg == "RS256") {
				n, e1 := base64.RawURLEncoding.DecodeString(k.N)
				e, e2 := base64.RawURLEncoding.DecodeString(k.E)
				if e1 != nil || e2 != nil || len(e) > 4 || len(e) == 0 {
					break
				}
				exp := 0
				for _, v := range e {
					exp = exp*256 + int(v)
				}
				if exp < 3 {
					break
				}
				return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}, nil
			}
		}
		return nil, errors.New("unknown signing key")
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(issuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired(), jwt.WithLeeway(30*time.Second))
	if err != nil || claims.Subject == "" || claims.IssuedAt == nil || claims.Nonce != nonce {
		return nil, errors.New("ChatGPT identity verification failed")
	}
	return claims, nil
}

// AccessToken is called for every request, including by cached providers.
func (m *Manager) AccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return "", m.initErr
	}
	a := m.disk.Accounts[m.disk.Active]
	if a == nil || a.AccessToken == "" || !permitted(a.Scope) {
		return "", errors.New("connect a ChatGPT plan in LLM settings first")
	}
	if time.Until(a.ExpiresAt) > time.Minute {
		return a.AccessToken, nil
	}
	if a.RefreshToken == "" {
		return "", errors.New("ChatGPT session expired; sign in again")
	}
	t, err := m.token(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {a.ClientID}, "refresh_token": {a.RefreshToken}})
	if err != nil {
		m.lastError = err.Error()
		return "", err
	}
	if t.Scope == "" {
		t.Scope = a.Scope
	}
	if !permitted(t.Scope) || t.RefreshToken == "" {
		return "", errors.New("ChatGPT renewal did not grant plan usage; sign in again")
	}
	// Keep the rotated credential in memory even if disk writing fails; never reuse the old refresh token.
	a.AccessToken, a.RefreshToken, a.Scope, a.ExpiresAt = t.AccessToken, t.RefreshToken, t.Scope, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)
	if err := m.saveLocked(); err != nil {
		return "", errors.New("cannot persist renewed ChatGPT credentials")
	}
	m.lastError = ""
	return a.AccessToken, nil
}
func (m *Manager) Select(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.disk.Accounts[id]
	if a == nil || a.AccessToken == "" || !permitted(a.Scope) {
		return errors.New("sign in to that ChatGPT account first")
	}
	old := m.disk.Active
	m.disk.Active = id
	if err := m.saveLocked(); err != nil {
		m.disk.Active = old
		return err
	}
	return nil
}

// Disconnect clears local tokens regardless of revocation availability.
func (m *Manager) Disconnect(ctx context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.disk.Accounts[id]
	if a == nil {
		return false, errors.New("unknown ChatGPT account")
	}
	revoked := a.RefreshToken == ""
	if a.RefreshToken != "" {
		req, _ := http.NewRequestWithContext(ctx, "GET", m.authBase+"/.well-known/openid-configuration", nil)
		if res, err := m.client.Do(req); err == nil {
			var d struct {
				Revocation string `json:"revocation_endpoint"`
			}
			_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&d)
			res.Body.Close()
			u, e := url.Parse(d.Revocation)
			if e == nil && u.Scheme == "https" && u.Host == "auth.openai.com" && u.User == nil {
				form := url.Values{"token": {a.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {a.ClientID}}
				req, _ = http.NewRequestWithContext(ctx, "POST", u.String(), strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if resp, e := m.client.Do(req); e == nil {
					revoked = resp.StatusCode == 200
					resp.Body.Close()
				}
			}
		}
	}
	a.AccessToken, a.RefreshToken = "", ""
	if m.disk.Active == id {
		m.disk.Active = ""
	}
	if !revoked {
		m.lastError = "Local sign-out completed. Remote revocation was not confirmed; disconnect ARTEX in ChatGPT Settings."
	}
	return revoked, m.saveLocked()
}
