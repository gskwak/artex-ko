# Private ARTEX Red/Blue test target

This fixture provides one disposable HTTP asset with **fake data only**. Its
`/debug/config` endpoint deliberately exposes a dummy configuration so a Red
workflow can produce a harmless finding. Every request writes a normalized
JSONL event for Blue event import and traffic investigation. The Compose file
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
events in `test-results/team-target/access.jsonl`; import that file as JSONL
under Blue **Investigation > Events**. The same events include source and
destination IPs, ports, HTTP method, URL, and status for **Investigation >
Traffic**. Docker health probes are excluded from the event log.

With Node.js 22 or later, run `node testdata/team-target/smoke.mjs` from the
repository root to check the Red/Blue API workflow against a disposable ARTEX
app and PostgreSQL database. It uses the local `artex-ko:ssh` image by default;
set `ARTEX_E2E_IMAGE` to test another built image. The script creates and
removes only its own app, database, and private network. It verifies Red task
creation without a model and Blue event import, traffic, detection, case,
response-record, task, and chat boundaries. It does not test live model output,
automatic log collection, or Red agent execution.

The fixture requires the network created by the local ARTEX Compose project.
To remove the test container without touching ARTEX or PostgreSQL:

```powershell
docker compose -f testdata/team-target/compose.yaml down
```

`test-results/` is ignored by Git; remove its generated log separately when
it is no longer needed.
