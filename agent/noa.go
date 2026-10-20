package agent

import (
	"log"
	"path/filepath"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/noaadapter"
)

// noaWarn returns a diagnostics sink tagging non-fatal noa messages with the
// session, routed through the package logger (agents have no per-instance one).
func noaWarn(session string) func(string) {
	return func(msg string) { log.Printf("[noa] %s: %s", session, msg) }
}

// noa is model-driven context compaction introduced in norma v0.4.0. Users
// toggle this experimental feature in system settings. It is exclusive with
// built-in compaction: noaadapter.Enable installs the Compactor, Compress tool,
// and three persistent prompt segments. Without Enable, built-in compaction
// continues. Each agent's noaEnabledFn reads the setting once per run, so a
// change affects later runs without rebuilding the agent.

// enableNoa adds noa to opts when enabled. archiveRoot is the persistent base
// directory for original compressed text (global workDir; all agents share
// <workDir>/noa rather than splitting by task/intent). sessionID names a
// globally unique archive subdirectory beneath it.
//
// This experiment must not interrupt real tasks if setup fails: report via
// onWarn and fall back to built-in compaction. On success, clear opts.Compaction
// to avoid an agentcore warning about two context managers.
func enableNoa(opts *agentcore.Options, enabled func() bool, archiveRoot, sessionID string, onWarn func(string)) {
	if enabled == nil || !enabled() {
		return
	}
	if opts.OnWarn == nil {
		opts.OnWarn = onWarn
	}
	if err := noaadapter.Enable(opts, noaadapter.Options{
		ArchiveBaseDir: filepath.Join(archiveRoot, "noa"),
		SessionID:      sessionID,
		OnWarn:         onWarn,
	}); err != nil {
		if onWarn != nil {
			onWarn("Failed to enable noa compaction; falling back to built-in compaction: " + err.Error())
		}
		return
	}
	// Compactor supersedes Compaction; clear it to avoid agentcore warnings.
	opts.Compaction = nil
}
