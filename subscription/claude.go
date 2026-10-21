package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ClaudeStatus struct {
	Installed  bool   `json:"installed"`
	Connected  bool   `json:"connected"`
	Email      string `json:"email,omitempty"`
	AuthMethod string `json:"auth_method,omitempty"`
}

// ClaudeCommand always invokes the official, unmodified CLI. It owns its own
// credentials and renewal; ARTEX never reads or copies Claude OAuth tokens.
func (m *Manager) ClaudeCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin := os.Getenv("ARTEX_CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, errors.New("install Claude Code and run claude auth login in the ARTEX runtime")
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(m.dir, "claude")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	cwd := filepath.Join(m.dir, "claude-work")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	// Subscription profiles must not silently consume a globally configured API key.
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_CODE_USE_") || key == "CLAUDE_CONFIG_DIR" || key == "CLAUDE_CODE_OAUTH_TOKEN" || key == "CLAUDE_CODE_SIMPLE" {
			continue
		}
		cmd.Env = append(cmd.Env, env)
	}
	cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+dir, "DISABLE_AUTOUPDATER=1")
	return cmd, nil
}
func (m *Manager) ClaudeStatus(ctx context.Context) ClaudeStatus {
	cmd, err := m.ClaudeCommand(ctx, "auth", "status")
	if err != nil {
		return ClaudeStatus{}
	}
	s := ClaudeStatus{Installed: true}
	b, err := cmd.Output()
	if err != nil {
		return s
	}
	var r struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
		Email      string `json:"email"`
	}
	if json.Unmarshal(b, &r) == nil {
		s.AuthMethod = r.AuthMethod
		s.Email = r.Email
		s.Connected = r.LoggedIn && (r.AuthMethod == "claude.ai" || r.AuthMethod == "oauth_token")
	}
	return s
}
