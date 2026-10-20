package server

import (
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
)

// Server-side resolution of the retry policy has five layers:
//   - Connection, empty-response, and same-provider safety-window retries follow
//     the endpoint. Each LLM profile may override global defaults; an unset
//     profile field inherits the global value, which in turn may use a built-in default.
//   - Circuit breaking and intent replay are process-wide settings.
//
// The global policy reads one settings row per call. Calls occur on infrequent
// paths (provider construction, work completion, and configuration saves), so
// another cache is unnecessary. Circuit-breaker parameters are the exception:
// applyRetryPolicy pushes them into the registry to avoid DB reads on failures.

// retryPolicy reads the global policy; a nil DB yields the zero policy (all
// layers on their built-in defaults).
func (s *Server) retryPolicy() db.LLMRetryPolicy {
	if s.m == nil || s.m.pg == nil {
		return db.LLMRetryPolicy{}
	}
	return s.m.pg.LLMRetryPolicy()
}

// resolveRetry layers one profile's override on top of the global policy and
// converts the result into the form agent.Config carries. Rules combine field by
// field, so a profile that only pins an interval still inherits the global count.
func resolveRetry(o db.RetryOverride, pol db.LLMRetryPolicy) agent.RetryConfig {
	connect := o.Connect.Or(pol.Connect)
	empty := o.Empty.Or(pol.Empty)
	stream := o.Stream.Or(pol.Stream)
	return agent.RetryConfig{
		// Preserve the original count semantics: zero means the default and
		// negative disables retries. The SDK's MaxRetries and EmptyResponseRetries
		// use the same semantics, so let the SDK resolve them.
		ConnectAttempts: connect.Attempts, ConnectInterval: connect.Interval(),
		EmptyAttempts: empty.Attempts, EmptyInterval: empty.Interval(),
		StreamAttempts: stream.Attempts, StreamInterval: stream.Interval(),
	}
}

// applyProfileRetry fills cfg.Retry for a profile read from the DB.
func (s *Server) applyProfileRetry(cfg *agent.Config, p *db.LLMProfile) {
	if p == nil {
		return
	}
	cfg.Retry = resolveRetry(p.Retry, s.retryPolicy())
}

// Circuit-breaker (polling cooldown) defaults match llmpool's built-in values;
// override them only when the user configured a value. Intent replay defaults
// are modelErrorRetries and modelErrorRetryBackoff in engine.go.

// applyRetryPolicy pushes the process-wide layers of the policy into the objects
// that consume them on a hot path: the circuit-breaker registry. Called at
// startup and whenever the policy is saved.
func (s *Server) applyRetryPolicy() {
	pol := s.retryPolicy()
	if s.llmHealth != nil {
		s.llmHealth.SetPolicy(pol.Breaker.Attempts, pol.Breaker.Interval())
	}
}

// modelErrorRetryPolicy resolves the intent-level replay knobs (layer ⑤): how
// many times a model_error work is re-run and how long to back off between runs.
func (e *Engine) modelErrorRetryPolicy() (retries int, backoff time.Duration) {
	retries, backoff = modelErrorRetries, modelErrorRetryBackoff
	if e == nil || e.m == nil || e.m.pg == nil {
		return retries, backoff
	}
	rule := e.m.pg.LLMRetryPolicy().Intent
	if rule.Attempts != 0 {
		retries = max(rule.Attempts, 0)
	}
	if d := rule.Interval(); d > 0 {
		backoff = d
	}
	return retries, backoff
}

// emptyTurnNudgeLimit resolves how many empty-turn continuations one work may
// inject (see steerHooks.Stop). It shares layer ②'s empty-response retry count:
// both recover when the model produces no substantive content. The SDK retries
// an identical request when it receives no content blocks. This layer handles
// a response with thinking but no answer or tool call, appending an instruction
// to continue from the existing thinking. Replaying the same context would not
// help with that kind of stall. The SDK counts emitted events, including thinking
// deltas, so its definition of an empty response differs. Sharing the count
// matches the user's expectation that empty output gets another attempt.
//
// Read the global policy instead of a profile override: failover can switch
// profiles during a run, but this total limit for the intent must stay fixed.
// The semantics match the SDK's emptyRetries(): zero uses
// defaultEmptyTurnNudges, negative disables continuation, and positive uses
// the configured limit.
func (e *Engine) emptyTurnNudgeLimit() int {
	if e == nil || e.m == nil || e.m.pg == nil {
		return defaultEmptyTurnNudges
	}
	switch n := e.m.pg.LLMRetryPolicy().Empty.Attempts; {
	case n == 0:
		return defaultEmptyTurnNudges
	case n < 0:
		return 0
	default:
		return n
	}
}
