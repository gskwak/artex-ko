# Blue team mode architecture

ARTEX's existing task engine remains the red-team workflow. Blue-team mode shares authentication, model configuration, agent management, and the application shell, but owns its own asset inventory and investigation workflow. A prompt rewrite alone cannot turn an offensive tool executor into a defensive analyst.

## Workflow and data

1. Import security events and alerts from files, an authenticated API, or an administrator-configured HTTP JSONL pull source. Preserve the original record alongside normalized time, source, event type, asset, severity, and message fields.
2. Evaluate enabled detection rules as events are imported and create deduplicated alerts with the rule and matching event as evidence. Analysts can test or run a rule against stored events separately.
3. Group alerts and events in cases. Analysts can triage, add observations, track an evidence timeline, change status, and record decisions.
4. A dedicated blue-team agent may analyze only the case's recorded evidence. Its output distinguishes facts, hypotheses, and unknowns, and proposes investigation steps, detection improvements, and containment/remediation actions. It has no offensive shell or network tools.
5. Response proposals enter a pending approval queue. The current implementation records approval and operator-reported completion; it does not execute external actions. A future execution connector would need scoped credentials and an audit trail.
6. Confirmed red-team findings can become linked blue-team validation cases. A reverse handoff from blue-team missed detections to red-team test goals remains a design direction; it is not implemented. Keep both records linked without sharing their incompatible graph schemas.

Blue-team entities are assets, event sources, events, rules, alerts, cases, observations, and action requests. They are distinct from the red-team asset, exploration node, and vulnerability finding types.

`blue_assets` gives defensive records a stable ID with `host`, `ip`, `domain`, `service`, `application`, or `other` kind and an active/archived status. It is separate from task-scoped red-team `assets`; no red-team scope or execution permission is inherited. An event may reference one blue asset, a case may have multiple explicit asset links, and a response proposal may reference a case-associated asset. The original event `asset` and action `target` strings remain immutable evidence snapshots. Case assets are the union of explicit links and assets on currently linked events, so moving an event does not leave an event-derived case link behind. An action asset remains in the asset's activity history but does not create case membership. Archiving blocks new links but retains foreign keys and history; there is no hard-delete API. Existing asset strings are not guessed into IDs or backfilled. Manual imports can submit a validated `asset_id`, but pulled JSONL payloads cannot directly set one.

HTTP sources are explicit administrator configuration, not URLs inferred from uploaded telemetry. Each source keeps an opaque cursor and a polling interval (30 seconds by default). The server adds a `cursor` query parameter after the first GET, accepts bounded JSONL with stable `external_id` values, and requires exactly one `X-ARTEX-Next-Cursor` response header. An explicit JSONL `source` remains the event source used by filters and rules; omitted `source` uses the stable `pull:<source ID>` fallback rather than the configured source name. Imported events are deduplicated by source and external ID, so changing an explicit source label can create a separate event for the same external ID. The cursor advances only after storage succeeds. The source can be disabled, synchronized manually, reset to the start, or deleted without deleting imported evidence. Sync returns `processed`, the number of handled rows including replayed duplicates, rather than a count of new events. URL credentials and redirects are rejected; the request has a 10-second timeout and an 8 MiB/500-record limit. Since an administrator-supplied URL can reach private addresses, registration must be limited to trusted feeds. The current connector does not provide per-source authentication headers or network packet capture.

## Initial HTTP contract

All routes use the existing JWT middleware and JSON error conventions.

| Route | Purpose |
| --- | --- |
| `GET /api/blue/overview` | Counts of cases, events, alerts, rules, and pending actions |
| `GET, POST /api/blue/cases` | List or create cases |
| `POST /api/blue/cases/from-finding` | Create a linked defensive case from a red-team finding |
| `GET, PATCH /api/blue/cases/{id}` | Read or update one case |
| `GET, POST /api/blue/assets`, `GET, PATCH /api/blue/assets/{id}` | List, create, read, edit, or archive blue-team assets |
| `GET /api/blue/assets/{id}/activity` | Read an asset's linked cases, events, and actions |
| `GET, POST /api/blue/cases/{id}/assets`, `DELETE /api/blue/cases/{id}/assets/{assetID}` | Read or change explicit case links; event-derived links remain until the event changes |
| `GET /api/blue/events`, `GET /api/blue/events/{id}` | Filter events by case or source, or read one event |
| `POST /api/blue/events/import` | Import a bounded batch of normalized/raw events and evaluate enabled rules |
| `PATCH /api/blue/events/{id}/asset` | Set or clear an event's validated blue-team asset ID |
| `GET, POST /api/blue/sources` | List or register HTTP JSONL event sources |
| `PATCH, DELETE /api/blue/sources/{id}` | Change source settings, disable or reset a source, or remove the configuration |
| `POST /api/blue/sources/{id}/sync` | Fetch one source immediately and return processed-row and detection counts |
| `POST /api/blue/events/{id}/move-case` | Move an event and its alerts to a case when existing observations do not cite them |
| `GET, POST /api/blue/rules` | List or create detection rules; create accepts a supported `sigma_yaml` subset |
| `GET, PATCH /api/blue/rules/{id}` | Read or update a rule; patch accepts a supported `sigma_yaml` subset |
| `POST /api/blue/rules/{id}/test` | Evaluate a rule on stored events without changing alerts |
| `POST /api/blue/rules/{id}/run` | Evaluate and persist alerts |
| `GET /api/blue/alerts` | Filter alerts by case, status, severity |
| `GET, PATCH /api/blue/alerts/{id}` | Read or triage an alert and attach it to a case |
| `GET, POST /api/blue/cases/{id}/observations` | Evidence-bound analyst notes |
| `POST /api/blue/cases/{id}/analyze` | Queue a read-only blue analyst run with optional case-bound event and alert IDs |
| `GET /api/blue/cases/{id}/analyses`, `GET /api/blue/analyses/{id}` | Read analysis run history and one outcome |
| `GET /api/blue/actions`, `GET, POST /api/blue/cases/{id}/actions` | List or propose response actions |
| `POST /api/blue/actions/{id}/decision` | Approve or reject a proposed action |
| `POST /api/blue/actions/{id}/complete` | Record a manually performed response action and evidence |

The UI includes `/blue/dashboard`, `/blue/chat`, `/blue/tasks`, `/blue/cases`, `/blue/cases/detail?id=...`, `/blue/events`, `/blue/traffic`, `/blue/detection`, `/blue/actions`, and `/blue/assets`. The query-based case detail route is required by the static Next.js export. The top-left two-position switch selects Red or Blue and changes the navigation and landing route; the selection persists in the browser. Direct links into either mode also select the correct navigation.

## Boundaries

The blue agent must not receive the SDK's general-purpose Bash/Write/Edit tools. Uploaded and pulled telemetry is untrusted evidence, never instructions. A remote feed's asset name or `asset_id` does not grant a blue-asset link. Analysis must cite stored event/alert IDs and uses bounded case evidence rather than unrestricted database access. Case analysis and task execution accept up to 12 explicitly selected event IDs and 12 alert IDs from that case; the selected records take priority within a 40-per-type input limit, and the run records the selection. Blue chat applies the same case-membership check within a 12-per-type snapshot limit. A generated detection rule is a draft until tested against events. Approving an action records a decision; it does not imply external execution. Current authentication uses one administrator account, so the recorded approval is not independent two-person authorization.
