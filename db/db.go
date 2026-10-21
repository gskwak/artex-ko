// Package db is the PostgreSQL data source for ARTEX, replacing the old
// single-file graph SQLite database. It opens connections, applies the schema,
// and seeds built-in agents and their variable catalogs.
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/artex/config"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver ("pgx")
)

//go:embed schema.sql
var schemaSQL string

const schemaMigrationLockKey int64 = 7337741001

// maxOpenConns 는 연결 풀 상한이다(`Open` 참조). PostgreSQL 기본 max_connections(100)
// 보다 충분히 낮고, 애플리케이션 자신의 중첩 커넥션 깊이(기동기의 스키마 advisory lock 이
// 커넥션 하나를 쥔 채 seedBuiltins 가 다른 커넥션을 잡는다)보다는 충분히 높아 자기 잠금에
// 빠지지 않는다.
const maxOpenConns = 32

var schemaDeadlockRetryDelays = [...]time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
}

type schemaExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func isPostgresDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func applySchemaWithRetry(ctx context.Context, execer schemaExecer, sleep func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		if _, err := execer.ExecContext(ctx, schemaSQL); err != nil {
			if !isPostgresDeadlock(err) || attempt >= len(schemaDeadlockRetryDelays) {
				return err
			}
			sleep(schemaDeadlockRetryDelays[attempt])
			continue
		}
		return nil
	}
}

// withSchemaMigrationLock pins the session-level lock to one checked-out
// connection. Running pg_advisory_lock through *sql.DB is incorrect because a
// later schema or unlock call may use a different pooled PostgreSQL session.
func withSchemaMigrationLock(ctx context.Context, sqlDB *sql.DB, action func(*sql.Conn) error) (err error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, schemaMigrationLockKey); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaMigrationLockKey); unlockErr != nil && err == nil {
			err = fmt.Errorf("advisory unlock: %w", unlockErr)
		}
	}()
	return action(conn)
}

// coordinateWithSchemaMigration makes long, multi-table archive transactions
// mutually exclusive with startup DDL while allowing ordinary runtime queries
// to continue normally.
func coordinateWithSchemaMigration(tx *sql.Tx) error {
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, schemaMigrationLockKey); err != nil {
		return fmt.Errorf("coordinate with schema migration: %w", err)
	}
	return nil
}

// DSN resolves the PostgreSQL connection string and reports where it came from.
// Precedence: env ARTEX_PG_DSN > config file (config.json). There is no
// built-in default — it errors if neither source is configured.
func DSN() (dsn, source string, err error) {
	return config.PostgresDSN()
}

// DB wraps the shared *sql.DB. PG handles its own connection pool + concurrency
// (MVCC), so unlike the old SQLite store there is no process-wide write mutex.
type DB struct{ *sql.DB }

// ensureDatabase connects to the postgres system database and creates the target
// database if it does not exist. dsn must be a postgres:// URL.
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil // unparseable DSN — let the normal Open fail with a clear error
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || dbName == "postgres" {
		return nil
	}
	// connect to the postgres maintenance database instead
	adminDSN := *u
	adminDSN.Path = "/postgres"
	admin, err := sql.Open("pgx", adminDSN.String())
	if err != nil {
		return nil // best-effort; let Open surface the real error
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		return nil
	}
	var exists bool
	_ = admin.QueryRow(`SELECT true FROM pg_database WHERE datname=$1`, dbName).Scan(&exists)
	if !exists {
		if _, err := admin.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
			return fmt.Errorf("create database %q: %w", dbName, err)
		}
	}
	return nil
}

// Open connects, applies the schema (idempotent), and seeds builtin rows.
func Open(dsn string) (*DB, error) {
	if err := ensureDatabase(dsn); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// database/sql 은 기본적으로 커넥션 수를 제한하지 않아, 풀에 유휴 커넥션이 없으면 무조건
	// 새로 열어 PostgreSQL 의 max_connections(기본 100)까지 밀어붙이고 나서야 거절당한다.
	// 그러면 부하가 높을 때 쿼리는 "읽을 값이 없음"이 아니라 `FATAL: sorry, too many clients
	// already` 라는 오류를 받고, 이 오류는 아래 인증 경로의 "읽기 실패"와 같은 모양이 된다.
	// 상한을 두면 초과분은 유휴 커넥션을 기다려(오류 대신 느려짐), reset-password.sh·psql 을
	// 위한 여유도 남는다. max_connections 를 낮춰 운영하면 이 값도 함께 낮춘다.
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxOpenConns)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping postgres (%s): %w", config.Redact(dsn), err)
	}
	d := &DB{sqlDB}
	// pgx runs multi-statement Exec via the simple protocol when there are no args.
	// Keep the dedicated lock connection checked out until both DDL and seeding
	// finish so concurrent application instances cannot initialize out of order.
	err = withSchemaMigrationLock(context.Background(), sqlDB, func(conn *sql.Conn) error {
		if err := applySchemaWithRetry(context.Background(), conn, time.Sleep); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		if err := d.seedBuiltins(); err != nil {
			return fmt.Errorf("seed builtins: %w", err)
		}
		return nil
	})
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

// builtinAgent describes one of the fixed agents and its prompt-variable catalog.
type builtinAgent struct {
	key, name, role, desc string
	vars                  []promptVar
	interactiveShell      bool // Default interactive shell setting for new rows; ON CONFLICT preserves later user changes.
	runSeconds            *int // Per-run wall-clock limit in seconds; nil uses the seed default (1200), 0 means unlimited.
}

type promptVar struct{ name, desc, example, source string }

// intp returns a pointer to v for optional builtinAgent fields such as runSeconds.
func intp(v int) *int { return &v }

// builtinAgents mirrors docs §5(a). Built-in tools are not stored in the DB;
// this seeds only agents and their variable catalogs. The interactive shell
// defaults for planner/worker/mainagent/auto are enabled by the
// interactive_shell_default_v1 block below (preserving later user toggles).
// interactiveShell here is only for new agents that need it enabled on creation.
var builtinAgents = []builtinAgent{
	{"goals", "Goal breakdown", "goals", "Break down a penetration testing objective into independent, verifiable subgoals.", []promptVar{
		{"EngagementDescription", "Task description (target/background)", "Test the example.com site", "exploration"},
		// Now is a global runtime variable (see server.globalPromptVars), so it is
		// not repeated in each agent catalog; that would collide in withGlobalVars.
	}, false, nil},
	{"planner", "Planning", "planner", "Review the current state and objectives, then add exploration intents only for genuinely uncovered directions (one planning loop per task).", []promptVar{
		{"Goal", "Overall task objective", "Gain administrator access to example.com", "exploration"},
		{"AssetSummary", "Asset counts and type distribution (optional)", "domain:3 ip:5 site:2", "distilled"},
	}, false, nil},
	{"mainagent", "Main", "main", "Human interface: monitor progress and turn user requests into hints or high-priority intents.", []promptVar{
		{"Goal", "Current task objective", "Gain administrator access to example.com", "exploration"},
		{"AssetSummary", "Initial asset summary (optional)", "domain:3 ip:5", "distilled"},
		{"FindingsSummary", "Confirmed findings summary (optional)", "high:1 medium:2", "distilled"},
	}, false, nil},
	{"worker", "Execution", "worker", "Execute one assigned intent, write discovered facts and vulnerabilities to the knowledge graph, then stop.", []promptVar{
		{"ProxyAddr", "Recording proxy address (controls conditional prompt text)", "127.0.0.1:8080", "runtime"},
		{"WorkerName", "Worker identifier (optional)", "worker-1", "runtime"},
	}, false, nil},
	// Auto is a built-in platform-management agent driven from the chat page;
	// it does not participate in the penetration testing orchestration loop.
	{"auto", "Auto", "assistant", "Platform assistant: use tools to manage tasks (create/view/pause/hint) and assets, and create or modify skills, custom tools, and MCP servers.", nil, false, nil},
	// Pentest is a built-in independent penetration testing agent driven from
	// the chat page. It plans, executes, and verifies the full workflow itself.
	// The interactive shell is enabled by default.
	{"pentest", "Penetration testing", "assistant", "Independent penetration testing agent: reconnoiter, identify attack surfaces, investigate and exploit, verify results, and wrap up; plan, execute, and challenge its own findings.", nil, true, intp(0)},
	// Blue analyst runs only through the case-analysis direct-LLM path. A unique
	// role keeps it out of the general conversational SDK and its tool bindings.
	{"blue_analyst", "Blue analyst", "blue_analyst", "Analyze stored case evidence, separate facts from hypotheses, and propose detection and response steps with verified citations.", nil, false, nil},
}

// seedBuiltins inserts the fixed built-in agents and their variable catalog (idempotent).
func (d *DB) seedBuiltins() error {
	for _, a := range builtinAgents {
		var agentID int64
		err := d.QueryRow(`
INSERT INTO agents(key, name, description, role, builtin, enabled, interactive_shell, run_seconds)
VALUES ($1, $2, NULLIF($3,''), $4, true, true, $5, COALESCE($6, 1200))
ON CONFLICT (key) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
RETURNING id`, a.key, a.name, a.desc, a.role, a.interactiveShell, a.runSeconds).Scan(&agentID)
		if err != nil {
			return fmt.Errorf("agent %s: %w", a.key, err)
		}
		for _, v := range a.vars {
			if _, err := d.Exec(`
INSERT INTO agent_prompt_vars(agent_id, var_name, description, example, source)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (agent_id, var_name) DO UPDATE
  SET description = EXCLUDED.description, example = EXCLUDED.example, source = EXCLUDED.source`,
				agentID, v.name, v.desc, v.example, v.source); err != nil {
				return fmt.Errorf("agent %s var %s: %w", a.key, v.name, err)
			}
		}
	}
	// Reserve the analyst key even on databases where a user previously created
	// a custom agent with that key. Model/profile and prompt edits are preserved;
	// the dedicated role prevents generic chat and tool-execution paths.
	if _, err := d.Exec(`UPDATE agents SET role='blue_analyst', builtin=true,
		interactive_shell=false, web_search=false WHERE key='blue_analyst'`); err != nil {
		return fmt.Errorf("reserve blue analyst role: %w", err)
	}
	// Drop catalog entries for variables that were renamed, so the white-list no
	// longer advertises a name templates can't resolve (EngagementTitle→Description).
	// After Now became a global runtime variable, older databases still had a
	// goals catalog entry for it. Remove it to avoid duplicate frontend keys.
	if _, err := d.Exec(`DELETE FROM agent_prompt_vars WHERE var_name IN ('EngagementTitle', 'CoverageGaps', 'Now')`); err != nil {
		return fmt.Errorf("cleanup renamed vars: %w", err)
	}
	// Default-on interactive_shell for the runtime agents (planner/worker/mainagent/auto)
	// ONCE — respects a later user toggle-off (guarded by a settings flag). goals(one-shot
	// decomposer) stays off. Runs after the column exists (schema applied before seed).
	if v, _, _ := d.GetSetting("interactive_shell_default_v1"); v != "true" {
		if _, err := d.Exec(`UPDATE agents SET interactive_shell=true WHERE key IN ('planner','worker','mainagent','auto')`); err != nil {
			return fmt.Errorf("seed interactive_shell defaults: %w", err)
		}
		_ = d.SetSetting("interactive_shell_default_v1", "true")
	}
	// Seed the built-in browser (Playwright) MCP once — disabled by default
	// (users can enable it when needed), with no proxy. The traffic-capture toggle injects/strips
	// the recording proxy + CA at runtime (server.Manager.syncBrowserMCPProxy).
	// Insert only if absent so we never clobber user edits (args/env/enabled/
	// visibility) on restart.
	if _, err := d.Exec(`
INSERT INTO mcp_servers(name, transport, command, args, env, enabled)
VALUES ('browser', 'stdio', 'npx', $1, '{}', false)
ON CONFLICT (name) DO NOTHING`,
		`["@playwright/mcp","--headless"]`); err != nil {
		return fmt.Errorf("seed browser mcp: %w", err)
	}
	// NOTE: the placeholder ScopeSentry data-source MCP (empty URL + empty X-API-Key,
	// disabled) is seeded directly in schema.sql §F so a raw `psql < schema.sql` init
	// also gets it. schema.sql is Exec'd on every startup, so it stays idempotent.
	if err := d.seedBuiltinSkillVisibility(); err != nil {
		return fmt.Errorf("seed skill visibility: %w", err)
	}
	if err := d.seedDefaultInterceptRules(); err != nil {
		return fmt.Errorf("seed intercept rules: %w", err)
	}
	if err := d.seedDefaultInterceptRulesV2(); err != nil {
		return fmt.Errorf("seed intercept rules v2: %w", err)
	}
	// Translate persisted defaults before the v3 seed checks for an existing name.
	if err := d.translateLegacyBuiltinRuleText(); err != nil {
		return fmt.Errorf("translate built-in rule text: %w", err)
	}
	if err := d.seedDefaultInterceptRulesV3(); err != nil {
		return fmt.Errorf("seed intercept rules v3: %w", err)
	}
	if err := d.seedDefaultAssetInterceptRules(); err != nil {
		return fmt.Errorf("seed asset intercept rules: %w", err)
	}
	return nil
}

// seedDefaultAssetInterceptRules inserts the built-in asset blocklist (fuzzy
// domain matches for government / education sites) once on first startup. Gated
// by a settings flag so a user's later disable/delete is never resurrected on
// restart — same policy as the intercept-rule seed.
func (d *DB) seedDefaultAssetInterceptRules() error {
	if v, _, _ := d.GetSetting("asset_intercept_default_rules_v1"); v == "done" {
		return nil
	}
	rules := []struct {
		kind    string
		pattern string
		note    string
	}{
		{"fuzzy_domain", ".gov", "[Built-in] Government site (.gov)"},
		{"fuzzy_domain", ".gov.cn", "[Built-in] Government site (.gov.cn)"},
		{"fuzzy_domain", ".edu", "[Built-in] Educational site (.edu)"},
		{"fuzzy_domain", ".edu.cn", "[Built-in] Educational site (.edu.cn)"},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO asset_intercept_rules(enabled, kind, pattern, note, builtin)
VALUES (true, $1, $2, $3, true)
ON CONFLICT DO NOTHING`, r.kind, r.pattern, r.note); err != nil {
			return fmt.Errorf("asset rule %q: %w", r.pattern, err)
		}
	}
	return d.SetSetting("asset_intercept_default_rules_v1", "done")
}

// builtinSkillVisibility maps a shipped skill's directory name → the built-in
// agent keys that should see it by default. The skill FILES themselves live on the
// filesystem (SkillDir, loaded by norma at runtime); DB only carries this visibility
// binding. Skills omitted here (e.g. playwright-cli, scopesentry) ship invisible by
// default — the user turns them on per-agent when needed. scopesentry additionally
// declares `mcps: ScopeSentry`, which only takes effect once it's made visible and
// that MCP is enabled/configured.
var builtinSkillVisibility = map[string][]string{
	"api-recon": {"auto", "pentest", "worker"},
}

// seedBuiltinSkillVisibility binds the shipped built-in skills to their default
// agents. Insert-if-absent (ON CONFLICT DO NOTHING) so a user's later toggle-off is
// never resurrected on restart — matches the browser-MCP / intercept-rule seed policy.
func (d *DB) seedBuiltinSkillVisibility() error {
	for skillName, agentKeys := range builtinSkillVisibility {
		for _, key := range agentKeys {
			if _, err := d.Exec(`
INSERT INTO agent_skill_visibility(agent_id, skill_name, enabled)
SELECT id, $2, true FROM agents WHERE key=$1
ON CONFLICT (agent_id, skill_name) DO NOTHING`, key, skillName); err != nil {
				return fmt.Errorf("skill %s → agent %s: %w", skillName, key, err)
			}
		}
	}
	return nil
}

// seedDefaultInterceptRules inserts built-in safety intercept rules once on
// first startup. The seed is gated by a settings flag so user edits (disable,
// delete, re-order) are never overwritten on subsequent restarts.
func (d *DB) seedDefaultInterceptRules() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v1"); v == "done" {
		return nil
	}
	type rule struct {
		name     string
		target   string // tool_name | tool_input
		typ      string // string | regex
		pattern  string
		action   string
		message  string
		priority int
	}
	rules := []rule{
		// ── Destructive system commands (priority 100) ────────────────────
		{
			name:     "[Built-in] Recursive forced deletion with rm -rf",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\brm\b.{0,80}(?:-[a-z]*r[a-z]*f[a-z]*|-[a-z]*f[a-z]*r[a-z]*|--recursive|--no-preserve-root)`,
			action:   "deny",
			message:  "Recursive forced deletion (rm -rf / rm --recursive) is blocked because it may permanently damage the system or target environment",
			priority: 100,
		},
		{
			name:     "[Built-in] Delete critical system directories",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\brm\b[^"'\n]{0,60}["'\s](/|/etc|/bin|/usr|/boot|/var|/lib|/sys|/proc|/dev|/sbin|/root)`,
			action:   "deny",
			message:  "Deletion of critical system paths is blocked",
			priority: 100,
		},
		{
			name:     "[Built-in] Format disk with mkfs",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bmkfs\b`,
			action:   "deny",
			message:  "Disk formatting with mkfs is blocked",
			priority: 100,
		},
		{
			name:     "[Built-in] Overwrite disk device with dd",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bdd\b[^|\n]{0,100}\bof=\s*/dev/[a-zA-Z]`,
			action:   "deny",
			message:  "Overwriting a disk device with dd is blocked",
			priority: 100,
		},
		{
			name:     "[Built-in] Fork bomb",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `:\(\)\s*\{[^}]*:\|:`,
			action:   "deny",
			message:  "Executing a fork bomb is blocked",
			priority: 100,
		},
		{
			name:     "[Built-in] Shutdown / restart",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\b(?:shutdown|reboot|halt|poweroff|init\s+[06])\b`,
			action:   "deny",
			message:  "Shutdown and restart commands are blocked",
			priority: 100,
		},
		{
			name:     "[Built-in] Kill all processes",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bkill\s+-9\s+-1\b|\bkillall\s+-9\b`,
			action:   "deny",
			message:  "kill -9 -1 and killall -9 are blocked because they kill all processes",
			priority: 100,
		},
		{
			name:     "[Built-in] Wipe disk with shred / wipe",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\b(?:shred|wipe)\b[^|\n]{0,80}/dev/[a-zA-Z]`,
			action:   "deny",
			message:  "Wiping disk devices with shred or wipe is blocked",
			priority: 100,
		},
		{
			name:     "[Built-in] Flush firewall rules",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\biptables\s+(?:-F|--flush)\b|\bnft\s+flush\s+ruleset\b`,
			action:   "deny",
			message:  "Flushing firewall rules (iptables -F / nft flush) is blocked",
			priority: 100,
		},
		// ── Destructive database operations (priority 90) ────────────────
		{
			name:     "[Built-in] SQL DROP DATABASE / TABLE / SCHEMA",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bDROP\s+(?:DATABASE|TABLE|SCHEMA|INDEX|VIEW|TABLESPACE|USER|ROLE)\b`,
			action:   "deny",
			message:  "DROP is blocked because it may irreversibly destroy database objects",
			priority: 90,
		},
		{
			name:     "[Built-in] SQL TRUNCATE",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bTRUNCATE\s+(?:TABLE\s+)?\w`,
			action:   "deny",
			message:  "TRUNCATE is blocked because it may remove all data from a table",
			priority: 90,
		},
		{
			name:     "[Built-in] MongoDB drop / dropDatabase",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\.(?:dropDatabase|dropCollection|drop)\s*\(`,
			action:   "deny",
			message:  "MongoDB drop operations are blocked",
			priority: 90,
		},
		{
			name:     "[Built-in] Redis FLUSHALL / FLUSHDB",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\b(?:FLUSHALL|FLUSHDB)\b`,
			action:   "deny",
			message:  "Redis FLUSHALL / FLUSHDB is blocked because it may erase cached data",
			priority: 90,
		},
		// ── Destructive HTTP requests (priority 80) ──────────────────────
		// Three common ways an agent sends a DELETE request:
		//   1. curl -X DELETE / --request DELETE (direct Bash call or script)
		//   2. Python HTTP client .delete() method
		//   3. method: 'DELETE' / method="DELETE" in JS or other scripts
		{
			name:     "[Built-in] curl / wget HTTP DELETE request",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bcurl\b[^|\n&;"]{0,300}(?:-X\s*DELETE|--request\s+DELETE|-XDELETE)|\bwget\b[^|\n&;"]{0,300}--method[=\s]+DELETE`,
			action:   "deny",
			message:  "HTTP DELETE requests via curl or wget are blocked because they may delete target system data",
			priority: 80,
		},
		{
			name:     "[Built-in] Python HTTP client DELETE (requests/httpx/aiohttp)",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\b(?:requests|httpx|aiohttp|urllib\.request)\.delete\s*\(|session\.delete\s*\(|client\.delete\s*\(`,
			action:   "deny",
			message:  "HTTP DELETE requests via Python clients are blocked",
			priority: 80,
		},
		{
			name:     "[Built-in] HTTP DELETE method in scripts (JS/other)",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)axios\.delete\s*\(|method\s*[:=]\s*['"]DELETE['"]`,
			action:   "deny",
			message:  "Sending HTTP DELETE requests from scripts is blocked",
			priority: 80,
		},
		{
			name:     "[Built-in] Bulk clear / purge endpoint path",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)/(?:clear|wipe|flush|purge|truncate|drop|destroy|factory[-_]reset|reset[-_]all)(?:[/?#"'\s]|$)`,
			action:   "deny",
			message:  "Bulk clear or destructive endpoints (/clear, /wipe, /flush, /purge, etc.) are blocked",
			priority: 80,
		},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
VALUES ($1, true, $2, $3, $4, $5, $6, $7, false, 60, 'deny')
ON CONFLICT DO NOTHING`,
			r.name, r.priority, r.target, r.typ, r.pattern, r.action, r.message,
		); err != nil {
			return fmt.Errorf("rule %q: %w", r.name, err)
		}
	}
	return d.SetSetting("intercept_default_rules_v1", "done")
}

// seedDefaultInterceptRulesV2 migrates the two safety patterns that used to be
// hard-coded in guard.go (destructive shell + data-exfil pipe) into ordinary
// intercept rules. Gated by its own flag so it also lands on DBs that already ran
// v1. Unlike the old guard.go floor, these are plain [Built-in] rules — the user can
// disable or delete them. The exfil rule ships DISABLED by default (its
// curl/wget/nc pipe pattern mis-fires on legitimate CTF/pentest reverse-shell and
// data-transfer pipes); enable it manually when exfil gating is actually wanted.
func (d *DB) seedDefaultInterceptRulesV2() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v2"); v == "done" {
		return nil
	}
	rules := []struct {
		name     string
		pattern  string
		action   string
		message  string
		enabled  bool
		priority int
	}{
		{
			name:     "[Built-in] Destructive system commands",
			pattern:  `(?i)\b(rm\s+-rf\s+/|mkfs|dd\s+if=|:\(\)\s*\{|shutdown|reboot|>\s*/dev/sd)`,
			action:   "deny",
			message:  "Destructive command blocked (rm -rf /, mkfs, dd, fork bomb, shutdown/restart, or disk overwrite)",
			enabled:  true,
			priority: 100,
		},
		{
			name:     "[Built-in] Data exfiltration pipeline",
			pattern:  `(?i)(curl|wget|nc|ncat)\b[^|]*\b(\|\s*(curl|wget|nc))`,
			action:   "deny",
			message:  "Possible data exfiltration pipeline blocked (command output sent through curl, wget, or nc)",
			enabled:  false,
			priority: 80,
		},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
VALUES ($1, $2, $3, 'tool_input', 'regex', $4, $5, $6, false, 60, 'deny')
ON CONFLICT DO NOTHING`,
			r.name, r.enabled, r.priority, r.pattern, r.action, r.message,
		); err != nil {
			return fmt.Errorf("rule %q: %w", r.name, err)
		}
	}
	return d.SetSetting("intercept_default_rules_v2", "done")
}

// seedDefaultInterceptRulesV3 adds the delete-endpoint path rule. The v1 HTTP rules
// only catch the DELETE *method* (curl -X DELETE, requests.delete(, method:'DELETE'),
// and v1's path rule covers only /clear /wipe /flush /purge /truncate /drop /destroy
// /factory-reset /reset-all — so a plain `curl 'http://t/api/user/delete?id=1'` (a
// delete endpoint reached with GET/POST, which is how most web apps expose deletion)
// slipped through every built-in rule. Own flag so it also lands on DBs that already
// ran v1/v2, where editing the v1 seed would have no effect.
//
// The pattern deliberately requires a separator after the verb so /delivery,
// /details, /delta and /delegate do not match, while /deleteAll, /delete_user and
// /delete-user do. destroy is re-covered here because v1's rule does not allow a
// suffix (/destroyAll was missed).
//
// Exported as a package const only so the seeded regex is unit-testable without a DB.
const deleteEndpointPathPattern = `(?i)/(?:(?:delete|remove|unlink|erase|destroy)[-\w]*|del)(?:[/?#"'\s]|$)`

func (d *DB) seedDefaultInterceptRulesV3() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v3"); v == "done" {
		return nil
	}
	const name = "[Built-in] Delete endpoint path"
	if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
SELECT $1, true, 80, 'tool_input', 'regex', $2, 'deny', $3, false, 60, 'deny'
WHERE NOT EXISTS (SELECT 1 FROM intercept_rules WHERE name = $1)`,
		name,
		deleteEndpointPathPattern,
		"Delete endpoints (/delete, /remove, /unlink, /erase, etc.) are blocked regardless of HTTP method; many applications use GET or POST to delete target data",
	); err != nil {
		return fmt.Errorf("rule %q: %w", name, err)
	}
	return d.SetSetting("intercept_default_rules_v3", "done")
}

// legacyBuiltinInterceptRuleText maps the exact text shipped by earlier
// one-time seeds to its English replacement. The old values are Unicode
// escaped so the source contains English prose while still matching stored
// rows byte for byte. Matching the original pattern and current field value
// preserves user-edited text and rule behavior.
var legacyBuiltinInterceptRuleText = []struct {
	oldName, newName, oldMessage, newMessage, pattern string
}{
	{"[\u5185\u7f6e] \u9012\u5f52\u5f3a\u5236\u5220\u9664 rm -rf", "[Built-in] Recursive forced deletion with rm -rf", "\u7981\u6b62\u6267\u884c\u9012\u5f52\u5f3a\u5236\u5220\u9664\uff08rm -rf / rm --recursive\uff09\uff0c\u53ef\u80fd\u6c38\u4e45\u635f\u574f\u7cfb\u7edf\u6216\u9776\u673a\u73af\u5883", "Recursive forced deletion (rm -rf / rm --recursive) is blocked because it may permanently damage the system or target environment", `(?i)\brm\b.{0,80}(?:-[a-z]*r[a-z]*f[a-z]*|-[a-z]*f[a-z]*r[a-z]*|--recursive|--no-preserve-root)`},
	{"[\u5185\u7f6e] \u5220\u9664\u7cfb\u7edf\u5173\u952e\u76ee\u5f55", "[Built-in] Delete critical system directories", "\u7981\u6b62\u5220\u9664\u7cfb\u7edf\u5173\u952e\u8def\u5f84", "Deletion of critical system paths is blocked", `\brm\b[^"'\n]{0,60}["'\s](/|/etc|/bin|/usr|/boot|/var|/lib|/sys|/proc|/dev|/sbin|/root)`},
	{"[\u5185\u7f6e] \u78c1\u76d8\u683c\u5f0f\u5316 mkfs", "[Built-in] Format disk with mkfs", "\u7981\u6b62\u683c\u5f0f\u5316\u78c1\u76d8\uff08mkfs\uff09", "Disk formatting with mkfs is blocked", `\bmkfs\b`},
	{"[\u5185\u7f6e] \u8986\u5199\u78c1\u76d8\u8bbe\u5907 dd", "[Built-in] Overwrite disk device with dd", "\u7981\u6b62\u4f7f\u7528 dd \u8986\u5199\u78c1\u76d8\u8bbe\u5907", "Overwriting a disk device with dd is blocked", `\bdd\b[^|\n]{0,100}\bof=\s*/dev/[a-zA-Z]`},
	{"[\u5185\u7f6e] Fork \u70b8\u5f39", "[Built-in] Fork bomb", "\u7981\u6b62\u6267\u884c Fork \u70b8\u5f39", "Executing a fork bomb is blocked", `:\(\)\s*\{[^}]*:\|:`},
	{"[\u5185\u7f6e] \u5173\u673a / \u91cd\u542f", "[Built-in] Shutdown / restart", "\u7981\u6b62\u6267\u884c\u5173\u673a\u6216\u91cd\u542f\u547d\u4ee4", "Shutdown and restart commands are blocked", `\b(?:shutdown|reboot|halt|poweroff|init\s+[06])\b`},
	{"[\u5185\u7f6e] \u6740\u6b7b\u5168\u90e8\u8fdb\u7a0b", "[Built-in] Kill all processes", "\u7981\u6b62 kill -9 -1 \u6216 killall -9\uff08\u6740\u6b7b\u6240\u6709\u8fdb\u7a0b\uff09", "kill -9 -1 and killall -9 are blocked because they kill all processes", `\bkill\s+-9\s+-1\b|\bkillall\s+-9\b`},
	{"[\u5185\u7f6e] \u78c1\u76d8\u64e6\u9664 shred / wipe", "[Built-in] Wipe disk with shred / wipe", "\u7981\u6b62\u5bf9\u78c1\u76d8\u8bbe\u5907\u6267\u884c shred/wipe \u64e6\u9664", "Wiping disk devices with shred or wipe is blocked", `\b(?:shred|wipe)\b[^|\n]{0,80}/dev/[a-zA-Z]`},
	{"[\u5185\u7f6e] \u6e05\u7a7a\u9632\u706b\u5899\u89c4\u5219", "[Built-in] Flush firewall rules", "\u7981\u6b62\u6e05\u7a7a\u9632\u706b\u5899\u89c4\u5219\uff08iptables -F / nft flush\uff09", "Flushing firewall rules (iptables -F / nft flush) is blocked", `\biptables\s+(?:-F|--flush)\b|\bnft\s+flush\s+ruleset\b`},
	{"[\u5185\u7f6e] SQL DROP DATABASE / TABLE / SCHEMA", "[Built-in] SQL DROP DATABASE / TABLE / SCHEMA", "\u7981\u6b62\u6267\u884c DROP \u64cd\u4f5c\uff0c\u53ef\u80fd\u4e0d\u53ef\u9006\u5730\u9500\u6bc1\u6570\u636e\u5e93\u5bf9\u8c61", "DROP is blocked because it may irreversibly destroy database objects", `(?i)\bDROP\s+(?:DATABASE|TABLE|SCHEMA|INDEX|VIEW|TABLESPACE|USER|ROLE)\b`},
	{"[\u5185\u7f6e] SQL TRUNCATE", "[Built-in] SQL TRUNCATE", "\u7981\u6b62\u6267\u884c TRUNCATE\uff0c\u53ef\u80fd\u6e05\u7a7a\u6570\u636e\u8868\u6240\u6709\u6570\u636e", "TRUNCATE is blocked because it may remove all data from a table", `(?i)\bTRUNCATE\s+(?:TABLE\s+)?\w`},
	{"[\u5185\u7f6e] MongoDB drop / dropDatabase", "[Built-in] MongoDB drop / dropDatabase", "\u7981\u6b62\u6267\u884c MongoDB drop \u64cd\u4f5c", "MongoDB drop operations are blocked", `(?i)\.(?:dropDatabase|dropCollection|drop)\s*\(`},
	{"[\u5185\u7f6e] Redis FLUSHALL / FLUSHDB", "[Built-in] Redis FLUSHALL / FLUSHDB", "\u7981\u6b62\u6267\u884c Redis FLUSHALL / FLUSHDB\uff0c\u53ef\u80fd\u6e05\u7a7a\u5168\u90e8\u7f13\u5b58\u6570\u636e", "Redis FLUSHALL / FLUSHDB is blocked because it may erase cached data", `(?i)\b(?:FLUSHALL|FLUSHDB)\b`},
	{"[\u5185\u7f6e] curl / wget \u53d1\u9001 DELETE \u8bf7\u6c42", "[Built-in] curl / wget HTTP DELETE request", "\u7981\u6b62\u901a\u8fc7 curl/wget \u53d1\u9001 HTTP DELETE \u8bf7\u6c42\uff0c\u53ef\u80fd\u5220\u9664\u76ee\u6807\u7cfb\u7edf\u6570\u636e", "HTTP DELETE requests via curl or wget are blocked because they may delete target system data", `(?i)\bcurl\b[^|\n&;"]{0,300}(?:-X\s*DELETE|--request\s+DELETE|-XDELETE)|\bwget\b[^|\n&;"]{0,300}--method[=\s]+DELETE`},
	{"[\u5185\u7f6e] Python HTTP \u5ba2\u6237\u7aef DELETE\uff08requests/httpx/aiohttp\uff09", "[Built-in] Python HTTP client DELETE (requests/httpx/aiohttp)", "\u7981\u6b62\u4f7f\u7528 Python HTTP \u5ba2\u6237\u7aef\u53d1\u9001 DELETE \u8bf7\u6c42", "HTTP DELETE requests via Python clients are blocked", `(?i)\b(?:requests|httpx|aiohttp|urllib\.request)\.delete\s*\(|session\.delete\s*\(|client\.delete\s*\(`},
	{"[\u5185\u7f6e] \u811a\u672c\u4e2d\u58f0\u660e HTTP DELETE \u65b9\u6cd5\uff08JS/\u901a\u7528\uff09", "[Built-in] HTTP DELETE method in scripts (JS/other)", "\u7981\u6b62\u5728\u811a\u672c\u4e2d\u58f0\u660e\u5e76\u53d1\u9001 HTTP DELETE \u8bf7\u6c42", "Sending HTTP DELETE requests from scripts is blocked", `(?i)axios\.delete\s*\(|method\s*[:=]\s*['"]DELETE['"]`},
	{"[\u5185\u7f6e] \u6279\u91cf\u6e05\u7a7a / \u6e05\u9664\u63a5\u53e3\u8def\u5f84", "[Built-in] Bulk clear / purge endpoint path", "\u7981\u6b62\u8c03\u7528\u6279\u91cf\u6e05\u7a7a\u6216\u9500\u6bc1\u7c7b\u63a5\u53e3\uff08/clear /wipe /flush /purge \u7b49\uff09", "Bulk clear or destructive endpoints (/clear, /wipe, /flush, /purge, etc.) are blocked", `(?i)/(?:clear|wipe|flush|purge|truncate|drop|destroy|factory[-_]reset|reset[-_]all)(?:[/?#"'\s]|$)`},
	{"[\u5185\u7f6e] \u7834\u574f\u6027\u7cfb\u7edf\u547d\u4ee4", "[Built-in] Destructive system commands", "\u7834\u574f\u6027\u547d\u4ee4\u88ab\u62d2\u7edd\uff08rm -rf / / mkfs / dd / fork bomb / \u5173\u673a\u91cd\u542f / \u8986\u5199\u78c1\u76d8\u8bbe\u5907\uff09", "Destructive command blocked (rm -rf /, mkfs, dd, fork bomb, shutdown/restart, or disk overwrite)", `(?i)\b(rm\s+-rf\s+/|mkfs|dd\s+if=|:\(\)\s*\{|shutdown|reboot|>\s*/dev/sd)`},
	{"[\u5185\u7f6e] \u6570\u636e\u5916\u6cc4\u7ba1\u9053", "[Built-in] Data exfiltration pipeline", "\u7591\u4f3c\u6570\u636e\u5916\u6cc4\u7ba1\u9053\u88ab\u62d2\u7edd\uff08\u547d\u4ee4\u8f93\u51fa\u7ecf curl/wget/nc \u5916\u4f20\uff09", "Possible data exfiltration pipeline blocked (command output sent through curl, wget, or nc)", `(?i)(curl|wget|nc|ncat)\b[^|]*\b(\|\s*(curl|wget|nc))`},
	{"[\u5185\u7f6e] \u5220\u9664\u7c7b\u63a5\u53e3\u8def\u5f84", "[Built-in] Delete endpoint path", "\u7981\u6b62\u8c03\u7528\u5220\u9664\u7c7b\u63a5\u53e3\uff08/delete /remove /unlink /erase \u7b49\uff09\uff0c\u4e0d\u8bba\u4f7f\u7528\u54ea\u79cd HTTP \u65b9\u6cd5\u2014\u2014\u591a\u6570\u5e94\u7528\u7684\u5220\u9664\u63a5\u53e3\u7528 GET/POST \u5c31\u80fd\u89e6\u53d1\uff0c\u540c\u6837\u4f1a\u771f\u5b9e\u5220\u9664\u76ee\u6807\u6570\u636e", "Delete endpoints (/delete, /remove, /unlink, /erase, etc.) are blocked regardless of HTTP method; many applications use GET or POST to delete target data", deleteEndpointPathPattern},
}

var legacyBuiltinAssetRuleNotes = []struct {
	pattern, oldNote, newNote string
}{
	{".gov", "[\u5185\u7f6e] \u653f\u5e9c\u7f51\u7ad9 (.gov)", "[Built-in] Government site (.gov)"},
	{".gov.cn", "[\u5185\u7f6e] \u653f\u5e9c\u7f51\u7ad9 (.gov.cn)", "[Built-in] Government site (.gov.cn)"},
	{".edu", "[\u5185\u7f6e] \u6559\u80b2\u7f51\u7ad9 (.edu)", "[Built-in] Educational site (.edu)"},
	{".edu.cn", "[\u5185\u7f6e] \u6559\u80b2\u7f51\u7ad9 (.edu.cn)", "[Built-in] Educational site (.edu.cn)"},
}

// translateLegacyBuiltinRuleText updates earlier one-time seed text without
// changing user-edited labels, messages, notes, or matching behavior. Exact
// comparisons make repeated startup runs harmless.
func (d *DB) translateLegacyBuiltinRuleText() error {
	for _, r := range legacyBuiltinInterceptRuleText {
		if _, err := d.Exec(`
UPDATE intercept_rules SET name=$1
WHERE name=$2 AND match_target='tool_input' AND match_type='regex' AND pattern=$3`,
			r.newName, r.oldName, r.pattern); err != nil {
			return fmt.Errorf("translate intercept rule name: %w", err)
		}
		if _, err := d.Exec(`
UPDATE intercept_rules SET message=$1
WHERE message=$2 AND match_target='tool_input' AND match_type='regex' AND pattern=$3`,
			r.newMessage, r.oldMessage, r.pattern); err != nil {
			return fmt.Errorf("translate intercept rule message: %w", err)
		}
	}
	for _, r := range legacyBuiltinAssetRuleNotes {
		if _, err := d.Exec(`
UPDATE asset_intercept_rules SET note=$1
WHERE note=$2 AND builtin=true AND kind='fuzzy_domain' AND pattern=$3`,
			r.newNote, r.oldNote, r.pattern); err != nil {
			return fmt.Errorf("translate asset intercept rule note: %w", err)
		}
	}
	return nil
}
