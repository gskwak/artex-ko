# Blue team mode architecture

ARTEX's existing task engine remains the red-team workflow. Blue-team mode shares authentication, model configuration, asset inventory, agent management, and the application shell, but owns a separate investigation workflow. A prompt rewrite alone cannot turn an offensive tool executor into a defensive analyst.

## Workflow and data

1. Import security events and alerts from files or an authenticated API. Preserve the original record alongside normalized time, source, event type, asset, severity, and message fields.
2. Evaluate enabled detection rules as events are imported and create deduplicated alerts with the rule and matching event as evidence. Analysts can test or run a rule against stored events separately.
3. Group alerts and events in cases. Analysts can triage, add observations, track an evidence timeline, change status, and record decisions.
4. A dedicated blue-team agent may analyze only the case's recorded evidence. Its output distinguishes facts, hypotheses, and unknowns, and proposes investigation steps, detection improvements, and containment/remediation actions. It has no offensive shell or network tools.
5. Response proposals enter a pending approval queue. The current implementation records approval and operator-reported completion; it does not execute external actions. A future execution connector would need scoped credentials and an audit trail.
6. Confirmed red-team findings can become linked blue-team validation cases. A reverse handoff from blue-team missed detections to red-team test goals remains a design direction; it is not implemented. Keep both records linked without sharing their incompatible graph schemas.

Blue-team entities are events, rules, alerts, cases, observations, and action requests. They are distinct from the red-team exploration node and vulnerability finding types.

## Initial HTTP contract

All routes use the existing JWT middleware and JSON error conventions.

| Route | Purpose |
| --- | --- |
| `GET /api/blue/overview` | Counts of cases, events, alerts, rules, and pending actions |
| `GET, POST /api/blue/cases` | List or create cases |
| `POST /api/blue/cases/from-finding` | Create a linked defensive case from a red-team finding |
| `GET, PATCH /api/blue/cases/{id}` | Read or update one case |
| `GET /api/blue/events`, `GET /api/blue/events/{id}` | Filter events by case or source, or read one event |
| `POST /api/blue/events/import` | Import a bounded batch of normalized/raw events and evaluate enabled rules |
| `POST /api/blue/events/{id}/move-case` | Move an event and its alerts to a case when existing observations do not cite them |
| `GET, POST /api/blue/rules` | List or create detection rules; create accepts a supported `sigma_yaml` subset |
| `GET, PATCH /api/blue/rules/{id}` | Read or update a rule; patch accepts a supported `sigma_yaml` subset |
| `POST /api/blue/rules/{id}/test` | Evaluate a rule on stored events without changing alerts |
| `POST /api/blue/rules/{id}/run` | Evaluate and persist alerts |
| `GET /api/blue/alerts` | Filter alerts by case, status, severity |
| `GET, PATCH /api/blue/alerts/{id}` | Read or triage an alert and attach it to a case |
| `GET, POST /api/blue/cases/{id}/observations` | Evidence-bound analyst notes |
| `POST /api/blue/cases/{id}/analyze` | Queue a read-only blue analyst run |
| `GET /api/blue/cases/{id}/analyses`, `GET /api/blue/analyses/{id}` | Read analysis run history and one outcome |
| `GET /api/blue/actions`, `GET, POST /api/blue/cases/{id}/actions` | List or propose response actions |
| `POST /api/blue/actions/{id}/decision` | Approve or reject a proposed action |
| `POST /api/blue/actions/{id}/complete` | Record a manually performed response action and evidence |

The first UI route set is `/blue/dashboard`, `/blue/cases`, `/blue/cases/detail?id=...`, `/blue/alerts`, `/blue/events`, `/blue/rules`, and `/blue/actions`. The query-based detail route is required by the static Next.js export. The top-left two-position switch selects Red or Blue and changes the navigation and landing route; the selection persists in the browser. Direct links into either mode also select the correct navigation.

## Boundaries

The blue agent must not receive the SDK's general-purpose Bash/Write/Edit tools. Uploaded telemetry is untrusted evidence, never instructions. Analysis must cite stored event/alert IDs and currently reads at most 40 recent events and 40 recent alerts per case. A generated detection rule is a draft until tested against events. Approving an action records a decision; it does not imply external execution. Current authentication uses one administrator account, so the recorded approval is not independent two-person authorization.
