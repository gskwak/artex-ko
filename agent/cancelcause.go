package agent

import (
	"context"
	"errors"
	"fmt"
)

// AbortCause names why an agent run's context was cancelled. Every cancellation
// site should attach one so the activity trace can report the real initiator.
type AbortCause struct {
	Code  string
	Short string
	Text  string
}

func (c *AbortCause) Error() string { return c.Text }

func cause(code, short, text string) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: text}
}

// Causef builds a cause that includes runtime-specific detail.
func Causef(code, short, format string, args ...any) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: fmt.Sprintf(format, args...)}
}

var (
	// Task-level execution context.
	AbortPausedByUser = cause("paused_by_user", "User paused the task",
		"The user paused the task through the task control API (POST /api/tasks/{id}/control, action=pause). This Planner/Worker run was canceled; running intents return to frontier(open) and will be claimed and restarted when the task resumes.")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "Orchestrator paused the task",
		"The orchestrator agent paused this task with pause_task. This Planner/Worker run was canceled; running intents return to frontier(open) and will restart after resumption.")
	AbortTaskDeleted = cause("task_deleted", "Task deleted",
		"The task is being deleted (DELETE /api/tasks/{id}). The deletion barrier canceled its running Planner, Workers, and main agent; this run's result will no longer be used.")
	AbortPausedOnReload = cause("paused_on_reload", "Backend restored the task's paused state",
		"At startup, the backend restored the task's paused state from the database. This run was canceled; normally no agent is running during restoration.")
	AbortGoalMet = cause("goal_met", "Planner marked task goal met",
		"The planner determined that the task goal was met and marked the task done, then canceled any remaining Workers. Their intents will be marked stopped, not failed.")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "Task timeout grace period expired",
		"After the task timed out, the system waited for running Workers to settle. The 90-second drain grace period was insufficient, so they were forcibly canceled. Their intents will be marked exhausted; facts and assets already recorded during settlement remain.")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "Planner stopped this intent",
		"The planner stopped this intent with kill_work, usually because it went off course or no longer has value. It will be marked stopped and will not be claimed again automatically.")
	AbortWorkPausedByUser = cause("work_paused_by_user", "User paused this Worker intent",
		"The user paused the running Worker. This call was canceled and the intent moved to paused. Recorded intents, facts, findings, and activities remain; execution restarts from the beginning after resumption.")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "User deleted this Worker intent",
		"The user deleted the running Worker. This call was canceled. After the Worker leaves the write section, the server applies the selected deletion mode: soft deletion only marks it deleted and keeps all outputs; hard deletion also removes the intent and downstream nodes supported solely by it.")
	AbortWorkFinished = cause("work_finished", "Worker finished and released its context",
		"The Worker finished normally, and the engine released its context in detachWork. This is not a runtime interruption; if it appears in an interruption message, cancellation raced with completion.")
	AbortPausedRaceGuard = cause("paused_race_guard", "New run blocked while task is paused",
		"While the task is paused, the engine refuses a new execution context to prevent a race between claiming work and pausing from starting a Worker. Claimed intents return to frontier.")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "User stopped this chat turn",
		"The user clicked Stop and ended this main-agent or chat-agent turn. Existing activities remain, and another message can be sent.")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "Task pause stopped the main-agent chat",
		"When the user paused the task, the running main-agent chat was also canceled. Existing activities remain; resuming the task will not replay this message automatically.")
	AbortChatTurnFinished = cause("chat_turn_finished", "Chat turn finished; context released",
		"The chat turn finished normally, and the server is releasing its context. This is not a runtime interruption; if it appears in an interruption message, cancellation raced with completion.")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "Backend process is shutting down",
		"The backend received SIGINT or SIGTERM and is restarting, updating, or shutting down. All running agents will be canceled; remaining running intents will reset to open and rerun after restart.")
	AbortRunHardTimeout = cause("run_hard_timeout", "Run hard timeout triggered",
		"The run exceeded its soft wall-clock budget and additional grace period. A model request or tool likely did not return in time to settle at a normal turn boundary. Check the last tool call without a result before the interruption.")
)

// AbortReason resolves the named cause attached to a cancelled run context.
func AbortReason(ctx context.Context) (code, short, text string, ok bool) {
	c := context.Cause(ctx)
	if c == nil {
		return "", "", "", false
	}
	var ac *AbortCause
	if errors.As(c, &ac) {
		return ac.Code, ac.Short, ac.Text, true
	}
	switch {
	case errors.Is(c, context.DeadlineExceeded):
		return "deadline_exceeded", "Upstream context reached its deadline",
			"The upstream context reached its deadline, but its creator did not attach a named cause with WithTimeoutCause: " + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", "Cancellation has no named cause",
			"The upstream context was canceled without a named cause from context.WithCancelCause; register the cause in agent/cancelcause.go and attach it at the cancellation site.", true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}
