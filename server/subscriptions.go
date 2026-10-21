package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/agent"
)

func (s *Server) subscriptionStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"chatgpt": s.subscriptions.Status(), "claude": s.subscriptions.ClaudeStatus(ctx)})
}
func (s *Server) subscriptionAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action    string `json:"action"`
		AccountID string `json:"account_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, "invalid subscription action")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var err error
	switch body.Action {
	case "connect":
		var u string
		u, err = s.subscriptions.Begin(body.AccountID)
		if err == nil {
			writeJSON(w, 200, map[string]any{"url": u})
			return
		}
	case "cancel":
		s.subscriptions.Cancel()
	case "select":
		err = s.subscriptions.Select(body.AccountID)
	case "disconnect":
		var revoked bool
		revoked, err = s.subscriptions.Disconnect(r.Context(), body.AccountID)
		if err == nil {
			s.invalidateProfileAgents()
			s.reapplyActiveProfile()
			writeJSON(w, 200, map[string]any{"ok": true, "revoked": revoked})
			return
		}
	default:
		writeErr(w, 400, "unknown subscription action")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.invalidateProfileAgents()
	s.reapplyActiveProfile()
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) subscriptionModels(w http.ResponseWriter, r *http.Request, format string) {
	if format == "claude-code" {
		writeJSON(w, 200, map[string]any{"ok": true, "models": []string{"sonnet", "opus", "haiku"}})
		return
	}
	token, err := s.subscriptions.AccessToken(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), "GET", "https://api.openai.com/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		writeErr(w, 502, "ChatGPT model catalog unavailable")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		writeErr(w, 502, fmt.Sprintf("ChatGPT model catalog HTTP %d", res.StatusCode))
		return
	}
	var catalog struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&catalog) != nil {
		writeErr(w, 502, "invalid ChatGPT model catalog")
		return
	}
	models := []string{}
	for _, m := range catalog.Models {
		if m.Visibility == "list" && m.Slug != "" {
			models = append(models, m.Slug)
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "models": models})
}

func (s *Server) attachSubscriptions(cfg *agent.Config) { cfg.Subscriptions = s.subscriptions }
