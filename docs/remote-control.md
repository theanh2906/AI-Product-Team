# ProductCrew Automation v1

The `pc` CLI and the local REST API provide the same ProductCrew board behavior
used by the desktop UI. They call the existing Kanban, planning, and sequence
services; they never edit `boards.json` directly.

## Build and configure

```powershell
make cli
$env:PRODUCTCREW_URL = "http://127.0.0.1:8081"
$env:PRODUCTCREW_PROJECT = "AI-Product-Team"
```

`--project` and `PRODUCTCREW_PROJECT` accept an exact project id, a unique
case-insensitive project name, or an imported project path. The CLI selects the
project automatically only when ProductCrew contains exactly one project.

## CLI commands

List imported projects:

```powershell
./dist/pc.exe projects list
```

Create a backlog item through the same validation and request-artifact path as the UI:

```powershell
./dist/pc.exe new `
  --project AI-Product-Team `
  --type todo `
  --title "CLI and REST bridge" `
  --description "Expose deterministic local automation for ProductCrew." `
  --delivery backend `
  --acceptance "Remote agents can create validated backlog items." `
  --acceptance "Remote agents receive deterministic JSON."
```

Public backlog types are `todo`, `feature`, and `bug`. `todo` keeps the
existing `TODO-###` key format. Compatibility aliases such as `TASK` may still
work for older scripts, but they are no longer the documented public contract.

Start one ticket or every eligible Backlog/Planning ticket in Autopilot:

```powershell
./dist/pc.exe autopilot --list
./dist/pc.exe autopilot --id FEAT-013
./dist/pc.exe autopilot --all
```

`autopilot --list` prints only open Backlog tickets using the same key accepted
by `--id`:

```text
BUG-004 : Fix duplicate task numbering
FEAT-013 : CLI and REST API for remote backlog and Autopilot control
```

Use `autopilot --list --json` when another agent or script needs a structured
list instead of the compact operator view.

Autopilot is asynchronous. For an open backlog item it starts Team Lead
planning, waits for a valid plan, records automatic approval, and schedules the
ordered sequence. Repeating the command while planning attaches to the existing
plan; repeating it after a sequence exists is idempotent.

Read one ticket or a filtered board status:

```powershell
./dist/pc.exe explore --id FEAT-013
./dist/pc.exe status
./dist/pc.exe status --status blocked
./dist/pc.exe status --type bug --status backlog
```

Successful commands emit JSON to stdout except the compact `autopilot --list`
view; add `--json` when its output is consumed by automation. Validation errors
exit with code `2`; API or connectivity failures exit with code `1` and emit
this shape to stderr:

```json
{
  "apiVersion": "v1",
  "error": {
    "code": "ticket_not_found",
    "message": "Ticket not found.",
    "status": 404
  }
}
```

## REST API

The API listens on ProductCrew's local address, `http://127.0.0.1:8081` by
default.

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/api/projects` | Resolve imported projects. |
| `POST` | `/api/projects/{projectID}/automation/backlog` | Create a validated `todo`, `feature`, or `bug` backlog item. |
| `GET` | `/api/projects/{projectID}/automation/status` | List backlog items and board summary. Supports `status` and `type` filters. |
| `GET` | `/api/projects/{projectID}/automation/explore/{ticketRef}` | Explore one backlog item, plan, tasks, documents, execution evidence, and blocked reason. |
| `POST` | `/api/projects/{projectID}/automation/autopilot` | Start or attach Autopilot for one backlog item or all eligible items. |

Create a backlog item:

```powershell
$body = @{
  type = "task"
  source = "remote-agent"
  title = "Inspect remote workflow"
  description = "Inspect the ProductCrew remote workflow and report evidence."
  deliveryTarget = "backend"
  acceptanceCriteria = @("The result is returned as deterministic JSON.")
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8081/api/projects/$projectId/automation/backlog" `
  -ContentType "application/json" `
  -Body $body
```

Start Autopilot for one ticket:

```powershell
$body = @{ id = "FEAT-013"; reviewer = "Remote agent" } | ConvertTo-Json
Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8081/api/projects/$projectId/automation/autopilot" `
  -ContentType "application/json" `
  -Body $body
```

Start all eligible tickets with `{ "all": true }`. Exactly one of `id` or
`all=true` is required.

Read status or explore one item:

```powershell
Invoke-RestMethod -Method Get -Uri "http://127.0.0.1:8081/api/projects/$projectId/automation/status?status=backlog&type=todo"
Invoke-RestMethod -Method Get -Uri "http://127.0.0.1:8081/api/projects/$projectId/automation/explore/FEAT-013"
```

## Safety boundaries

- The API is local-first and exposes no stored credentials or provider secrets.
- Backlog creation uses ProductCrew's existing validation, numbering, dedupe, and
  request-artifact persistence.
- Autopilot uses Team Lead planning and `ApprovePlanSequence`; invalid sequences
  remain at the planning checkpoint with a visible error.
- The API is currently intended for the local ProductCrew process. Do not bind
  the service to a public interface without adding authentication and transport
  security.
