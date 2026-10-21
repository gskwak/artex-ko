# Blue-team detection rule format

Blue-team rules are evaluated against imported event records. Rule tests return matching event IDs without creating alerts; running a rule creates an alert for each new rule/event pair. The engine does not run commands or send requests.

The rule editor offers a single-field form (`field`, `operator`, `value`) and an optional advanced JSON definition. The simple form accepts normalized fields such as `event_type`, `source`, `severity`, `asset`, `message`, and `occurred_at`, plus dot paths into the original event such as `raw.process.name`.

```json
{
  "detection": {
    "failed_login": {
      "event_type": "authentication.failure",
      "message|contains": "failed"
    },
    "known_test": { "raw.test_event": true },
    "condition": "failed_login and not known_test"
  }
}
```

Each selection is an object of field checks joined with AND, or an array of such objects joined with OR. A list of values for one field means any value matches; append `|all` when every listed value must match. Conditions support `and`, `or`, `not`, parentheses, and `N of selection_*` or `all of them`. Comparisons are case-insensitive by default; append `|cased` when exact case matters.

Supported field modifiers are `contains`, `startswith`, `endswith`, `exists` (boolean), `gt`, `gte`, `lt`, `lte`, and `re` (Go RE2 regular expressions). Dot paths reach nested JSON fields. A literal dotted key takes precedence over path traversal. If a `logsource` object is included, the event must have a matching nested `logsource` object; it is not ignored silently.

This is a deliberate subset of Sigma, not a complete Sigma interpreter. Correlation, timeframe, and backend conversion features are rejected rather than treated as if they ran. Define a rule against real sample events, use Test to inspect matches, and then run it to create alerts. Tests and matches provide evidence of matching records, not a definitive incident verdict.

To import Sigma YAML through the API, send `POST /api/blue/rules` with a JSON body like this (using an authenticated session):

```json
{
  "name": "Failed sign-in",
  "severity": "medium",
  "enabled": true,
  "sigma_yaml": "title: Failed sign-in\ndetection:\n  selection:\n    event_type: authentication.failure\n    message|contains: failed\n  condition: selection\n"
}
```

`PATCH /api/blue/rules/{id}` also accepts `{"sigma_yaml":"..."}` to replace a rule's definition. Supply either `sigma_yaml` or `definition`, never both. The server converts YAML to the stored JSON `definition`; set the API's `name`, `description`, and `severity` separately because Sigma metadata is not copied into those fields. A saved definition takes precedence over the simple `field`/`operator`/`value` fields.

Sigma import supports one YAML document with field-based selections, the conditions and modifiers listed above, and optional `logsource`. It does not support correlation, timeframe, unbound keyword-only selections, or Sigma backend conversion and pipelines; unsupported constructs return a validation error. A Sigma `logsource` must match the imported event's nested `logsource` object, so importers must map log categories explicitly. Two bundled rules (`artex_enrich_user_agent.yml` and `destructive_command_hunting.yml`) are covered by compatibility tests.
