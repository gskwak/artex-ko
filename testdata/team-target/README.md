# Private ARTEX Red/Blue test target

This fixture provides one disposable HTTP asset with **fake data only**. Its
`/debug/config` endpoint deliberately exposes a dummy configuration so a Red
workflow can produce a harmless finding. Requests to its practice endpoints
write normalized JSONL events for Blue event import and traffic investigation. Its
`/logs/events` endpoint serves those events as a cursor-based JSONL feed.
The Compose file
joins the existing `artex-ko_default` network and publishes **no host port**.
Set `ARTEX_DOCKER_NETWORK` if your ARTEX Compose project uses another network.

From the repository root in PowerShell:

```powershell
New-Item -ItemType Directory -Force test-results/team-target | Out-Null
docker compose -f testdata/team-target/compose.yaml up --build -d
docker compose -f testdata/team-target/compose.yaml ps
```

Use `http://artex-lab-target:8080` as the Red target URL from the ARTEX
container. Requests to `/`, `/api/items`, `/debug/config`, and `/admin` create
events in `test-results/team-target/access.jsonl`. Docker health probes and
feed reads do not add events. Each event includes source and destination IPs,
ports, HTTP method, URL, and status for Blue **Investigation > Traffic**.

To collect events automatically, open Blue **Investigation > Events** and add
an automatic source with a name such as `lab-target` and this URL:

```text
http://artex-lab-target:8080/logs/events
```

Make a request to the target, then choose **Sync now** or wait for the default
30-second poll. Check **Investigation > Traffic** for the new record. The source
adds a `cursor` query parameter after the first GET; the feed
returns JSONL with an `X-ARTEX-Next-Cursor` header that points to the next
byte position. This fixture keeps both its explicit `source` label and
`external_id` stable. ARTEX preserves that label for filters and rules and
uses the `(source, external_id)` pair to avoid duplicates on retries. Changing
the explicit source label can therefore create a separate event for an older
external ID. The Sync now result's `processed` value counts handled rows,
including replayed duplicates, rather than only new events. ARTEX stores the
cursor after a successful import. Disable the source to stop
automatic polling; **Sync now** still works for a disabled source. Resetting
the cursor replays the feed from the start without creating duplicate events.
Only register URLs you trust: ARTEX fetches them from its own container,
including addresses on its private Docker network.

Manual import remains available. Upload `test-results/team-target/access.jsonl`
as JSONL under Blue **Investigation > Events**. This does not require an
automatic source. The feed serves at most 500 events and 8 MiB per request;
the default polling interval can be changed from 10 to 3,600 seconds.

With Node.js 22 or later, run `node testdata/team-target/smoke.mjs` from the
repository root to check the Red/Blue API workflow against a disposable ARTEX
app and PostgreSQL database. It uses the local `artex-ko:ssh` image by default;
set `ARTEX_E2E_IMAGE` to test another built image. The script creates and
removes only its own app, database, and private network. It verifies Red task
creation without a model and Blue event import, traffic, detection, case,
response-record, task, and chat boundaries. It does not test a live subscription
or API model. See the script output for the exact paths covered by the current
version; Red agent execution and the automatic-source poller require separate
checks.

The fixture requires the network created by the local ARTEX Compose project.
To remove the test container without touching ARTEX or PostgreSQL:

```powershell
docker compose -f testdata/team-target/compose.yaml down
```

`test-results/` is ignored by Git; remove its generated log separately when
it is no longer needed.
