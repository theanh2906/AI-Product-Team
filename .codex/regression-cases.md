# AI-Product-Team Regression Cases

This is the canonical regression catalog for ProductCrew behavior in this
repository. Regression Guard uses these cases to decide which existing behavior
could be impacted by a change. Regression Case Maintainer updates this file only
after a feature or behavior change has passed implementation verification and
Regression Guard, or when a supported feature is intentionally removed.

## Workboard And Request Intake

### RG-WORKBOARD-001 New Request Draft Safety

Status: active
Area: Request Intake
Source: `frontend/src/app/features/work-items/**`, `internal/web/board_handlers.go`
Trigger: User fills New Request and cancels or clicks outside the modal.
Expected: Backdrop click does not close the modal; explicit cancel can save a
draft; Continue restores the draft; Discard clears it.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies focused UI state behavior when covered.
- Inspect Workboard modal handlers and draft persistence paths.

### RG-WORKBOARD-002 Add To Backlog Request Encoding

Status: active
Area: Request Intake
Source: `frontend/src/app/core/work-item-api.service.ts`, `internal/web/board_handlers.go`
Trigger: User submits New Request with or without attachments.
Expected: The request body is valid JSON for no attachments and valid multipart
form data for attachments; the server does not return `request body must be a
valid JSON object`.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: covers request submission behavior when present.
- `go test ./internal/web -run "TestBoard"`: covers board API request handling when relevant.

### RG-WORKBOARD-003 Compact Task Card Quick Actions

Status: active
Area: Workboard
Source: `frontend/src/app/features/work-items/work-item-page.html`, `frontend/src/app/features/work-items/work-item-page.css`, `frontend/src/app/features/work-items/work-item-page.spec.ts`
Trigger: A task card shows inline actions such as Backlog Plan, Backlog Remove,
drag handle, live/build log, manual QA verify, Queue, or Re-queue.
Expected: Quick actions render as icon-only controls on the compact card;
action labels and disabled reasons are exposed through ProductCrew-styled custom
tooltips, not visible text labels or native browser `title` attributes. Backlog
cards do not repeat the implicit `Ready for planning` status in the footer. Hidden
custom tooltips must not create horizontal overflow or scrollbars inside narrow
kanban columns, and tooltip text remains readable without being clipped at the
card or column edge.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies icon-only quick actions and custom tooltip attributes.
- Inspect the task-card footer at narrow column widths to confirm action labels
do not wrap or crowd status/document indicators, Backlog Plan/Remove stay
icon-only, and no column-level horizontal scrollbar appears when tooltips are
not active. Hover compact card tooltips near the left and right card edges to
confirm the text wraps and remains visible.

### RG-WORKBOARD-004 PM-Selected Request Type

Status: active
Area: Request Intake
Source: `frontend/src/app/features/work-items/work-item-page.*`, `frontend/src/app/core/work-item-api.service.ts`, `internal/web/intake.go`, `internal/web/board_handlers.go`
Trigger: User creates a New Request whose text mentions bugs, fixes, or errors
while the selected request type is Feature or Task.
Expected: The PM-selected type is the source of truth for the Backlog item;
Smart Intake and fallback questions must not reclassify the request into Bug
based only on keywords in the title or description. Request type radio inputs
remain visually hidden behind the ProductCrew card picker so native browser
controls do not appear beside or above the custom cards.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies the selected type is sent to Smart Intake and used for backlog creation.
- `go test ./internal/web -run "TestCreateBacklogKeepsExplicitType|TestFallbackIntakeKeepsSelectedWorkType"`: verifies backend fallback and request handling preserve explicit type.
- Open New Request on `/work-items` and inspect the request type picker: verifies only the styled Feature/Bug/Task cards are visible while card selection still changes the underlying radio value.

### RG-WORKBOARD-005 Drawer Action Layout

Status: active
Area: Workboard
Source: `frontend/src/app/features/work-items/work-item-page.html`, `frontend/src/app/features/work-items/work-item-page.css`, `frontend/e2e/task-drawer-layout.spec.ts`
Trigger: User opens a Backlog item detail drawer, a blocked task detail drawer,
or a manual Designer review task drawer and uses the drawer actions.
Expected: Backlog and task drawer actions live in the fixed drawer header.
Backlog keeps its compact split action: the primary Send to Team Lead action is
visible without clipping, the caret opens a styled action menu containing
Remove, and the menu width matches the split action width. Blocked task
Restart/Ignore actions and Designer Approve design/Send feedback actions stay in
the task drawer header while the body alert/review panels contain explanatory
content only, so no sticky footer or inline body action bar crowds the drawer
content. When Send feedback opens the revision composer, its Back/Requeue
controls use ProductCrew-styled buttons, hover states, icons, and disabled
styling instead of browser-native controls. Unlinked Git Evidence actions keep
Link branch or commit and Create branch in one horizontal row with equal flex
widths instead of stacking or making the primary action span the full drawer.
Suggested checks:
- `Set-Location frontend; npm run build:go`: verifies the drawer CSS compiles
into the embedded frontend bundle.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies blocked and Designer review actions render in the task drawer header, not inside body alert/review panels.
- Inspect a Backlog detail drawer at the standard desktop drawer width and
confirm the header split action plus dropdown menu stay within the drawer.
- Inspect blocked and manual Designer review task drawers at the standard
desktop drawer width and confirm Restart/Ignore and Approve/Send feedback stay
inside the header without horizontal overflow.
- Inspect a task drawer with no linked branch/commit and confirm the Git Evidence
Link branch or commit and Create branch actions sit side by side.

### RG-WORKBOARD-006 Feature Library Live Rollup

Status: active
Area: Workboard
Source: `frontend/src/app/features/feature-library/**`, `frontend/src/styles.css`, `frontend/src/app/core/work-item-api.service.ts`, `internal/web/design_artifact_handlers.go`, `internal/web/server.go`
Trigger: User opens Features, searches or filters the feature index, selects a
feature, expands a delivery ticket, opens a PNG Designer mockup, or opens an
HTML Designer source artifact.
Expected: Only Feature backlog items become top-level library entries. Each
entry remains linked to its plan, ordered Designer/Developer/QA tickets,
lifecycle status, durable history, acceptance outcome, and documents while live
board events update the view, and the live sync chip clearly shows waiting,
connected, reconnecting, or stale stream state without requiring UI polling.
Registered PNG previews render read-only in the
Feature Library. Task-reported HTML mockups are listed as open actions instead
of embedded iframe previews or lightboxes so desktop WebView/CSP blocking cannot
hide the page behind a blocked-content surface; the opened HTML route remains
sandboxed, and the server rejects unreported, non-HTML, traversal, and
out-of-task paths. The page has no document-level horizontal overflow at desktop
or mobile widths. Truncated feature, ticket, artifact, and document labels expose
their full text through native tooltips, and the feature detail pane keeps
body/meta/ticket text readable on desktop dark mode. Feature Library rules are
also present in the initial app
stylesheet so the desktop WebView cannot render an unstyled lazy page, while
Work board remains the execution surface.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/feature-library/feature-library-page.spec.ts`: verifies feature-only rollup, search/filter, selection, artifacts, SSE synchronization, live stream state, and full-text tooltip attributes on truncated content.
- `go test ./internal/web -run "TestGetTaskDesign" -count=1`: verifies sandbox headers and file-boundary enforcement.
- `Set-Location frontend; npm run build:go`: verifies the route and embedded frontend bundle.
- Inspect the built `styles-*.css` and confirm it contains
  `.feature-library-page`, then load the embedded bundle and confirm the page
  uses grid/flex layout before relying on its lazy component style tag.
- Inspect `/features` at 1280 x 720 and 390 x 844, confirm HTML mockup rows
  contain an open link and no iframe/lightbox, and verify document scroll width.

### RG-WORKBOARD-008 Task Drawer Content Containment

Status: active
Area: Workboard
Source: `frontend/src/app/features/work-items/work-item-page.html`, `frontend/src/app/features/work-items/work-item-page.css`
Trigger: User opens a task detail drawer whose delivery report contains long verification notes, paths, commands, URLs, or unbroken identifiers.
Expected: Task title, blocked reason, delivery summary, verification, remaining risks, findings, dependencies, and documents remain inside the fixed-width drawer. Long delivery-report list items wrap within the available inline space, the drawer keeps `scrollLeft` at zero, and only vertical content scrolling is exposed.
Suggested checks:
- `Set-Location frontend; npm run test:ui`: verifies real-browser drawer geometry at desktop and compact desktop widths using long QA evidence and unbroken identifiers.
- `Set-Location frontend; npm test -- --watch=false`: verifies existing task drawer states and interactions.
- `Set-Location frontend; npm run build:go`: verifies containment rules compile into the embedded bundle.
- Open a task with long QA verification evidence at 1440 x 900 and confirm `.task-drawer`, `.drawer-scroll`, `.delivery-report`, and all report descendants have `scrollWidth <= clientWidth`.

### RG-WORKBOARD-007 Multi-Repository Workspace Scope

Status: active
Area: Workboard / Workspace
Source: `frontend/src/app/features/work-items/**`, `frontend/src/app/core/project-api.service.ts`, `internal/web/workspace_service.go`, `internal/web/project_handlers.go`, `internal/web/task_queue.go`, `internal/codex/appserver.go`, `internal/claude/runner.go`, `internal/copilot/runner.go`
Trigger: User switches Work Board from Project to Workspace, creates or edits a
workspace with two or more imported repositories, then plans or runs queued
agent work in that workspace.
Expected: Workspace selection is local to Work Board and does not mutate the
global active-project selector. Definitions persist as local `.workspace` files,
contain exactly one writable primary repository, and accept only currently
imported project paths. Existing board, SSE, serial queue, Safe stop, request,
and build flows use the workspace ID without changing Project mode. The primary
repository remains the task working directory and artifact owner; additional
writable repositories are exposed through provider-specific multi-root options,
while read-only repositories are never granted write access.
Suggested checks:
- `go test ./internal/web -run "Test(CreateWorkspace|Workspace)" -count=1`: verifies workspace persistence, validation, virtual project resolution, and path normalization.
- `go test ./internal/codex ./internal/claude ./internal/copilot -count=1`: verifies provider-specific multi-root configuration and scoped permissions.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies the Workspace mode loads its board without changing the global project selector.
- Exercise `GET /api/workspaces`, `POST /api/workspaces`, and `PUT /api/workspaces/{id}`, then inspect `/work-items` at desktop width: verifies the persisted API contract, workspace command bar, create/edit dialog, and serial queue presentation.

### RG-WORKBOARD-009 Git Status Does Not Hide Kanban

Status: active
Area: Workboard
Source: `frontend/src/app/features/work-items/work-item-page.html`, `frontend/src/app/features/work-items/work-item-page.ts`, `frontend/src/app/features/work-items/work-item-page.css`, `frontend/src/app/core/git-status-api.service.ts`, `internal/web/git_status.go`
Trigger: Git status, branch evidence, or Workboard header/content layout changes.
Expected: Git status stays out of the Workboard body so the Kanban board remains
the primary content. A compact launcher beside the board title shows branch and
change count and opens repository delivery details in a separate modal without
inserting or expanding an inline board panel.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies the launcher opens a separate modal, no inline Git panel renders, and the Kanban board remains present.
- `Set-Location frontend; npm run test:ui`: verifies Workboard and drawer desktop layout do not regress in a real browser.

### RG-WORKBOARD-010 Compact Header Actions

Status: active
Area: Workboard
Source: `frontend/src/app/features/work-items/work-item-page.html`, `frontend/src/app/features/work-items/work-item-page.ts`, `frontend/src/app/features/work-items/work-item-page.css`, `frontend/src/app/features/work-items/work-item-page.spec.ts`
Trigger: The Workboard header renders Queue Control, Build Verify, Production
Deploy, or New Request controls, especially when build/deploy command labels are
long or the deploy action is not configured.
Expected: All four header actions share the same compact height. Long operational
details such as queue subtitles, build command labels, deploy command labels,
and configure explanations move into ProductCrew-styled custom tooltips and
accessible labels. Inline card actions render as icon-only controls, so command
text does not get clipped in the header and unconfigured Configure links do not
add an extra row or change card height.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies compact header action rendering and tooltip-backed details.
- Open `/work-items` at 1365 x 768 with configured build, unconfigured deploy,
and a running/stopping/paused queue state, then confirm the four header actions
stay one row, same height, and free of clipped visible command text.

### RG-WORKBOARD-011 Data-Safe Git Delivery

Status: active
Area: Workboard
Source: `internal/gitdelivery/**`, `internal/web/git_delivery.go`, `internal/web/git_status.go`, `internal/storage/*git_delivery*`, `internal/kanban/service.go`, `internal/development/**`, `frontend/src/app/shared/git-delivery-modal/**`, `frontend/src/app/core/git-status*`
Trigger: User opens Git delivery, reviews the preflight snapshot, then starts,
stops, or retries commit and push for completed ticket changes.
Expected: ProductCrew includes only changed files attributed to completed Done
tickets, blocks pre-existing staged or unattributed changes, revalidates an exact
fingerprint before mutation, stages explicit paths without broad staging,
creates one traceable commit, pushes without force, reconciles ambiguous remote
results, and atomically links every included ticket after a confirmed push.
Opening Git delivery mounts a fixed viewport overlay from the compiled initial
stylesheet; it must not enter Workboard document flow or increase page width or
height when the modal is opened.
Delivery events persist across restart. Safe stop waits for the current step;
retry reuses the persisted commit instead of creating a duplicate. AI recovery
is limited to repairable hook/test failures and cannot stage, push, alter Git
history, touch credentials, or edit files outside the approved delivery scope.
Suggested checks:
- `go test ./internal/web -run "TestGitDelivery|TestDeliveryTickets|TestRepairableGitDeliveryFailure" -count=1`: verifies exact ticket attribution, snapshot drift detection inputs, and safe recovery classification.
- `go test ./internal/storage ./internal/kanban ./internal/development`: verifies durable JSON/SQLite migration, atomic ticket association, and provider implementations of the restricted repair contract.
- `Set-Location frontend; npm test -- --watch=false`: verifies API wiring and the separate modal launcher.
- `Set-Location frontend; npm run test:ui`: loads the compiled production stylesheet and verifies fixed modal containment, zero document overflow, and compact header action alignment in a real desktop browser.

## Agent Studio

### RG-AGENT-STUDIO-001 Project-Scoped Instructions

Status: active
Area: Settings / Agent Studio
Source: `frontend/src/app/features/settings/**`, `frontend/src/app/core/project-api.service.ts`, `frontend/src/app/core/project.models.ts`, `internal/web/project_service.go`, `internal/web/project_handlers.go`, `internal/web/ai_runtime.go`, `internal/web/server.go`
Trigger: User selects a project in Settings Agent Studio, edits the Working
Standard, a role profile, or a reusable skill, then runs Team Lead, Designer,
Developer, QA, Bug Scanner, or Feature Radar work for that project.
Expected: Agent Studio selection is independent from the global active project
and Build Verification project selectors. New projects inherit the global
default profile until the first project-scoped save. Saving writes
`.productcrew/agent-studio/profile.json` inside the selected project and does
not mutate global `settings.json`. Effective instructions for every agent role
prefer the selected project's profile and fall back to the global profile only
when the task project cannot be matched. The Agent Studio project picker renders
the project name/path and source as separate ProductCrew-styled content, with
the source shown as a right-aligned badge rather than inline `name · source`
separator text.
Suggested checks:
- `go test ./internal/web -run "TestProjectAgentStudioProfilesAreScopedPerProject|TestWorkingStandardSkillsAndEffectiveProfilePersist"`: verifies project profile storage, inheritance, and effective instruction composition.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/settings/settings-page.spec.ts`: verifies Settings Agent Studio uses project-scoped profile data without changing the global active project.
- Open Settings > Agent Studio, switch between two imported projects, and
confirm the project profile badge/storage path update while the sidebar active
project selector remains unchanged.

## Planning And Start Autopilot

### RG-PLANNING-001 Planning Preflight Outcomes

Status: active
Area: Planning
Source: `internal/planning/**`, `internal/kanban/service.go`, `internal/web/board_handlers.go`
Trigger: Team Lead plans a request that is already implemented or not feasible.
Expected: Already-implemented requests complete without downstream tasks;
not-feasible requests become visible as not feasible without filler tasks.
Suggested checks:
- `go test ./internal/planning ./internal/kanban -run "Preflight|AlreadyImplemented|NotFeasible"`: verifies translation and persistence when relevant.

### RG-PLANNING-002 Start Autopilot Sequence

Status: active
Area: Planning
Source: `frontend/src/app/features/work-items/**`, `internal/kanban/service.go`, `internal/web/task_queue.go`
Trigger: User clicks `Start Autopilot` in Review Team Lead plan.
Expected: The plan is approved, tasks are unlocked, sequence order is assigned,
tasks are routed to role queues, and dispatch respects the ordered sequence.
Suggested checks:
- `go test ./internal/kanban ./internal/web -run "ApprovePlanSequence|NextQueuedAgentTask|Sequence"`: verifies sequence state and queue order.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies UI action request shape.

### RG-PLANNING-003 Remote Control CLI And Autopilot API

Status: active
Area: Planning
Source: `cmd/pc/**`, `internal/pccli/**`, `internal/web/remote_control.go`, `internal/web/board_handlers.go`, `internal/kanban/service.go`
Trigger: A local agent or script creates, explores, filters, or starts Autopilot
for a ticket through the `pc` CLI or REST v1 routes.
Expected: CLI output is deterministic JSON; `TASK` uses the existing `todo`
validation and `TODO-###` key contract; project selection is explicit when
ambiguous; ticket creation uses the normal board service; and Autopilot attaches
idempotently to planning, records approval, validates the sequence, and schedules
role-ordered execution without editing board storage directly. `autopilot
--list` shows only open Backlog tickets as `KEY : title`, while `--list --json`
preserves structured output for agents.
Suggested checks:
- `go test ./internal/pccli -count=1`: verifies command parsing, project resolution, JSON output, and structured errors.
- `go test ./internal/web ./internal/kanban -run "Remote|ApprovePlanSequence|CreateBacklog" -count=1`: verifies REST envelopes, Task aliasing, planning handoff, sequence approval, and idempotence.
- `make cli; ./dist/pc.exe help`: verifies the distributable CLI builds and exposes the documented commands.

### RG-PLANNING-004 Approved Automation REST/CLI Contract

Status: active
Area: Planning
Source: `cmd/pc/**`, `internal/pccli/run.go`, `internal/web/server.go`, `internal/web/remote_control.go`, `internal/web/board_handlers.go`, `internal/kanban/service.go`
Trigger: A local agent or script uses the approved public automation surface:
`pc projects list`, `pc new --type todo|feature|bug`, `pc autopilot`,
`pc explore`, `pc status`, or the REST routes
`POST /api/projects/{projectID}/automation/backlog`,
`GET /api/projects/{projectID}/automation/status`,
`GET /api/projects/{projectID}/automation/explore/{ticketRef}`, and
`POST /api/projects/{projectID}/automation/autopilot`.
Expected: The approved `/automation/*` routes and `pc` command syntax work
against the checked-out build, return the same deterministic JSON envelopes,
validation, and planning/sequence behavior as the underlying board/planning
services, and stay documented in `README.md` and `docs/remote-control.md`.
Suggested checks:
- `go test ./internal/web -run "TestAutomation" -count=1`: verifies the approved `/automation/*` REST routes (backlog create, status filter, explore-by-ref, autopilot single/all, invalid type).
- `go test ./internal/pccli -count=1`: verifies `pc projects list`, `pc new --type todo|feature|bug`, and CLI routing to the approved `/automation/*` endpoints.
- `go build -o dist/pc.exe ./cmd/pc; ./dist/pc.exe help`: verifies the CLI help documents `pc projects list` and the approved command syntax.

## Queue And Task Lifecycle

### RG-QUEUE-001 Single Active Task Gate

Status: active
Area: Queue
Source: `internal/web/task_queue.go`, `internal/kanban/service.go`
Trigger: Multiple tasks across one or more boards are queued.
Expected: Only one task is active at a time; queued sequence work waits while any
board has an active task.
Suggested checks:
- `go test ./internal/web -run "TestNextQueuedAgentTaskWaitsWhenAnyTaskIsActive|TestNextQueuedAgentTask"`: verifies queue gating.

### RG-QUEUE-002 Restart And Interrupted Recovery

Status: active
Area: Queue
Source: `internal/kanban/service.go`, `internal/web/*worker.go`
Trigger: A blocked task is restarted, or the local service restarts while a task
is active.
Expected: Restart clears blocked execution state and requeues safely; interrupted
work is recovered as blocked with a clear reason.
Suggested checks:
- `go test ./internal/kanban -run "Restart|RecoverInterrupted"`: verifies persisted state transitions.

### RG-QUEUE-003 GitHub Copilot Structured Result Parsing

Status: active
Area: Queue
Source: `internal/copilot/runner.go`, `internal/planning/**`, `internal/design/**`, `internal/development/**`, `internal/quality/**`
Trigger: ProductCrew runs a GitHub Copilot-backed agent and the streamed CLI
output includes assistant progress text before the final result, or emits the
JSON answer in an assistant message without a separate result event.
Expected: Final/result stream events are preferred as the agent result; valid
JSON embedded in assistant messages is accepted when no final JSON result is
present; assistant prose/progress text does not become the ProductCrew JSON
payload; the structured retry repeats the original task while requiring exactly
one JSON object; Copilot model `auto` is invoked without a reasoning-effort flag;
long Copilot prompts are staged as a ProductCrew-managed prompt file and read by
the Copilot agent instead of exceeding the Windows command-line limit; legacy
Copilot sessions that still fail with the old Markdown `--attachment` prompt
transport are retried with a fresh session; and Bug Scanner plus Feature Radar
use the persisted Copilot runtime settings instead of hard-coded defaults.
Suggested checks:
- `go test ./internal/copilot -run "BuildRunArgs|ReadStreamIgnoresAssistantTextAsFinalResult|ReadStreamUsesAssistantJSONWhenNoResultEvent|RepairJSONPrompt|PreparePromptTransport|LegacyAttachment"`: verifies progress text is ignored, assistant JSON can be accepted, retry keeps task context, Copilot `auto` omits `--effort`, long prompts are written to a cleaned-up prompt file, and legacy attachment failures do not keep a resumed task stuck.
- `go test ./internal/copilot ./internal/planning ./internal/design ./internal/development ./internal/quality -count=1`: verifies all role adapters still compile and preserve structured-output contracts.

### RG-QUEUE-004 Safe Stop And Manual Continue

Status: active
Area: Queue
Source: `internal/kanban/model.go`, `internal/kanban/service.go`, `internal/web/task_queue.go`, `internal/web/board_handlers.go`, `frontend/src/app/features/work-items/work-item-page.*`, `frontend/src/app/core/work-item-api.service.ts`
Trigger: A project queue is safe-stopped while an agent task is active or queued.
Expected: Safe stop never kills the active task or clears queued tasks; the board
enters `stopping` while work is active, automatically becomes `paused` once the
active slot is released, skips dispatch for that project while paused, and only
continues dispatching after the user clicks Continue queue.
Suggested checks:
- `go test ./internal/kanban ./internal/web -run "TestQueueControl|TestPauseStopping|TestNextQueuedAgentTaskSkipsPausedProjectQueue|TestNextQueuedAgentTaskPausesStoppingQueueAfterActiveTaskFinishes"`: verifies persisted queue-control state and scheduler dispatch behavior.
- `npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies the Work Board Queue Control UI calls safe stop and continue for the selected project.

### RG-QUEUE-005 Source Recovery Guard

Status: active
Area: Queue
Source: `internal/sourceguard/**`, `internal/web/source_guard.go`, `internal/web/*worker.go`
Trigger: A Designer, Developer, QA, or build-repair agent task rewrites an
existing source/config file to empty or placeholder-sized content.
Expected: ProductCrew snapshots source-like files under the target project's
`.productcrew/recovery` before write-capable agent work, restores suspicious
placeholder rewrites, blocks the task with a clear recovery reason, emits a live
session safety event, and removes the snapshot after clean runs so recovery
storage does not grow during normal operation.
Suggested checks:
- `go test ./internal/sourceguard`: verifies placeholder rewrites are restored,
ordinary edits are allowed, and ProductCrew project-local storage is excluded.
- `go test ./internal/web -run "TestNextQueuedAgentTask|TestSequenceBuildVerificationRunsOnlyForFinalDeveloper|TestQABug"`: verifies the guarded worker integration still compiles with queue, Developer, and QA lifecycle helpers.

## Developer And Build Verification

### RG-BUILD-001 Final Developer Build Verify

Status: active
Area: Build
Source: `internal/web/developer_worker.go`, `internal/web/task_queue.go`, `internal/buildverify/**`
Trigger: Developer tasks run inside a sequence.
Expected: Earlier Developer tasks defer build verification; the final Developer
task before QA runs the configured build verification.
Suggested checks:
- `go test ./internal/web -run "TestSequenceBuildVerificationRunsOnlyForFinalDeveloper"`: verifies sequencing rule.

### RG-BUILD-002 Embedded Frontend Bundle

Status: active
Area: Build
Source: `frontend/package.json`, `frontend/scripts/copy-to-go.mjs`, `internal/web/dist`
Trigger: Frontend route, component, or style changes are shipped through the Go
server or desktop app.
Expected: `npm run build:go` builds Angular and updates the embedded Go static
bundle.
Suggested checks:
- `Set-Location frontend; npm run build:go`: verifies production frontend bundle generation.
- Inspect `internal/web/dist` for stale removed routes or labels when routes are changed.

## QA Bug Loop

### RG-QA-001 QA Bug Dedupe And Highlight

Status: active
Area: QA Bug Loop
Source: `internal/web/qa_worker.go`, `internal/kanban/service.go`, `frontend/src/app/features/work-items/**`
Trigger: QA reports a bug that already exists in active backlog or planning.
Expected: The existing QA backlog item is highlighted and repeat count updates;
no duplicate bug card is created. When the existing bug is already in Team Lead
planning, ProductCrew records the waiting QA task relationship instead of
trying to highlight a non-backlog card, and the affected QA card is presented as
a waiting/gated state rather than a red error. QA task findings render the exact
linked bug ticket key and title instead of only the raw finding description; the
linked ticket can be opened from the finding, and unresolved backlog tickets can
be moved to Team Lead or retried when their existing planning pass has failed or
needs revision.
Suggested checks:
- `go test ./internal/web ./internal/kanban -run "ExistingQABug|ReportedAgain|QABug"`: verifies backend dedupe and highlight.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/work-items/work-item-page.spec.ts`: verifies visible repeat chip behavior, linked finding ticket labels, ticket opening, and Team Lead follow-up actions.

### RG-QA-002 Original QA Requeue After Bug Fixes

Status: active
Area: QA Bug Loop
Source: `internal/kanban/service.go`, `internal/web/qa_worker.go`
Trigger: All QA-raised bugs for a blocked QA task have been fixed and completed.
Expected: The original blocked QA task is requeued automatically for another
verification pass, including when the bug being fixed was an existing planning
bug matched by title or `existingBacklogId` and linked through
`waitingQaTaskIds`. Legacy QA tasks that were already waiting before
`waitingQaTaskIds` existed recover on board load when their saved findings no
longer match any active QA bug.
Suggested checks:
- `go test ./internal/kanban -run "RequeuesOriginalQA|ResolvedQATask|CompletingSharedPlanningQABug|LegacyWaitingQA"`: verifies requeue behavior.

## Settings And Agent Studio

### RG-SETTINGS-001 CLI-Sourced Model Options

Status: active
Area: Settings
Source: `internal/web/agent_model_options.go`, `frontend/src/app/features/settings/**`
Trigger: User opens Settings and selects Codex, Claude Code, or GitHub Copilot
model options.
Expected: Model options come from local CLI inspection without one slow provider
blocking the whole catalog; fallback checkbox and selected model/effort values
reflect persisted settings. Failed or partial CLI inspections return empty
arrays plus a visible warning instead of `null` values. A persisted model that is
temporarily missing from the current CLI catalog remains visible as a configured
option so the user can recover without a broken dropdown. GitHub Copilot `auto`
renders as automatic effort with the effort selector disabled, while specific
Copilot models keep explicit effort choices.
Transient Settings feedback notices use the current theme surface and auto-dismiss after 5 seconds.
Suggested checks:
- `go test ./internal/web -run "AgentModelOptions|NormalizeAgentModelGroup|AIPlatform"`: verifies backend model option loading and persisted model/effort normalization.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/settings/settings-page.spec.ts --include src/app/core/project-api.service.spec.ts`: verifies settings UI behavior, stale configured model display, transient feedback dismissal, and model/tool diagnostics caching when touched.

### RG-SETTINGS-002 Agent Profile Composition

Status: active
Area: Agent Studio
Source: `internal/agentprofile/**`, `internal/web/project_service.go`, `frontend/src/app/features/settings/**`
Trigger: User edits role profiles, working standard, or skill assignments.
Expected: Settings persist changes, compose effective profiles, and keep Bug
Scanner and Feature Radar configurable.
Suggested checks:
- `go test ./internal/agentprofile ./internal/web -run "Profile|RoleDefinitions|EffectiveProfile|Skills"`: verifies profile persistence and composition.

### RG-SETTINGS-003 Guarded Storage Datasource Switch

Status: active
Area: Settings
Source: `internal/storage/datasource.go`, `internal/storage/migration.go`, `internal/web/datasource_service.go`, `internal/web/datasource_handlers_test.go`, `frontend/src/app/features/settings/**`, `frontend/src/app/core/project.models.ts`, `frontend/src/app/core/project-api.service.ts`
Trigger: User opens Settings, views the Storage datasource card, runs Check
status, or opens Change datasource to switch between Local JSON and SQLite.
Expected: Existing installs show `Local JSON` active by default with no forced
reconfiguration. `Check status` (active-datasource summary or a target check
inside the guarded flow) never mutates the active datasource. The guarded
change flow requires configure target → verify connection → confirm migration
in order; editing any target field after a successful check invalidates that
check and disables `Continue to migration` until a fresh check succeeds.
`Migrate and switch` is blocked until the explicit migration acknowledgement
checkbox is checked. A successful switch reports migrated counts per entity
(boards, insights, lifecycle events, notifications, build profile cache) and
shows the new active datasource. A failed migration keeps the previous
datasource active, reports rollback explicitly, and does not silently write to
another store.
Suggested checks:
- `go test ./internal/storage/... -run "Migrate"`: verifies JSON↔SQLite migration counts, rollback, and unchanged-source-on-failure behavior.
- `go test ./internal/web/... -run "StorageDatasource"`: verifies the check/switch HTTP handlers, confirmation gating, and rollback response shape.
- `Set-Location frontend; npm test -- --watch=false --include src/app/features/settings/settings-page.spec.ts`: verifies the Settings datasource card states, stale-check invalidation, guarded migration flow, and rollback messaging.

### RG-SETTINGS-004 Consistent Settings Form Controls

Status: active
Area: Settings
Source: `frontend/src/styles.css`, `frontend/src/app/features/settings/**`, `frontend/e2e/settings-email-layout.spec.ts`
Trigger: User opens a Settings card containing text, email, number, URL, or select controls in a two-column form.
Expected: Text-like controls use the shared ProductCrew background, border, radius, text, placeholder, and focus treatment instead of native browser styling. Paired Settings controls have the same 42 px height and remain vertically aligned, including the Email notifications fields.
Suggested checks:
- `Set-Location frontend; npx playwright test e2e/settings-email-layout.spec.ts`: verifies computed control styles, equal heights, row alignment, and the dark-theme Email notifications render.

## Observability

### RG-OBS-001 Durable AI Session Intelligence

Status: active
Area: Observability
Source: `internal/web/ai_sessions.go`, `internal/agentusage/**`,
`internal/codex/appserver.go`, `internal/claude/runner.go`,
`internal/copilot/runner.go`, `frontend/src/app/features/observability/**`,
`GET /api/observability/ai-sessions`
Trigger: An AI-backed planning, design, development, QA, Bug Scanner, or Feature
Radar run emits lifecycle and provider usage events, or the user opens
Observability and changes its project/time/status/provider filters, session
pages, Provider comparison pages, or Trace Explorer filters/pages.
Expected: ProductCrew groups lifecycle events per plan or task even when a
correlation ID is shared, updates the dashboard through the existing SSE stream,
reports duration and provider/model comparisons, and only shows token or cost
values supplied by the provider. Resumed Codex threads contribute per-turn usage
without double-counting cumulative thread totals. Missing historical usage stays
explicitly unavailable, stale unterminated sessions do not count as active, and
prompts/model responses are not copied into analytics. Unsupported provider
account-quota cards are not shown; ProductCrew does not present remaining quota
unless a real provider-backed app feature exists in source.
The Session command
center calculates a bounded page size from the available viewport space, keeps
page controls aligned with the dark dashboard style, resets to page 1 when
filters change, and preserves expandable run details on paged results. Provider
comparison uses the same card height as Session command center, paginates long
provider/model lists instead of growing the right column, and keeps its own page
clamped when filters or live data change. The
Trace Explorer keeps the backend event scope available for filtering while
rendering at most 20 visible events per page, resets trace pagination on scope
or filter changes, and clamps live SSE updates to a valid page.
Suggested checks:
- `go test ./internal/web -run "TestBuildAISessionOverview|TestGetAISessionOverview"`: verifies grouping, lifecycle status, aggregate metrics, and unavailable usage handling.
- `go test ./internal/codex ./internal/claude ./internal/copilot`: verifies provider-reported usage parsing, including resumed Codex turns.
- `Set-Location frontend; npm test -- --watch=false`: verifies the shared frontend contracts remain valid.
- `Set-Location frontend; npm run test:ui`: verifies desktop Observability surfaces do not overflow in a real browser.
- Open `/observability`, exercise Session command center pagination, Provider comparison pagination, Trace Explorer pagination, filters, and an expandable run, and inspect browser warnings/errors: verifies the live command-center UI and existing Observability content coexist.

## Desktop And Packaging

### RG-PROJECTS-001 Safe Product Creation Lifecycle

Status: active
Area: Workboard
Source: `internal/web/product_creation.go`, `frontend/src/app/features/create-product/**`,
`frontend/src/app/core/project-api.service.ts`, `POST /api/product-creation/blueprints`,
`POST /api/product-creation/blueprints/{blueprintID}/approve`
Trigger: User opens Projects, describes a new local product, reviews its
blueprint, and approves creation.
Expected: ProductCrew persists a pre-project draft without writing into the
destination, validates the destination and required CLI tools, scaffolds inside
a bounded staging sibling, publishes by rename only after all files are ready,
optionally initializes Git, registers the result in the existing project
registry, creates its first project-local request and Work Board backlog item,
then navigates with the new project selected. Existing destinations are never
overwritten and failed pre-publish scaffolds do not leave staging content. The
Create Product foundation inputs, profile select, path picker, and local-only
controls must render with ProductCrew form tokens in dark and light themes, stay
top-aligned across uneven helper text, and never fall back to native browser gray
controls.
Suggested checks:
- `go test ./internal/web -run "Test(BuildProductBlueprint|WriteProductFoundation|ScaffoldProduct)"`: verifies validation, draft persistence, scaffold shape, publish cleanup, project registration, and backlog creation.
- `Set-Location frontend; npm test -- --watch=false`: verifies shared Projects, router, shell, and API contracts.
- Open `/projects/new`, inspect Product name, Application profile, Destination parent, Browse, and Initialize Git in dark theme, verify the first-row controls share the same top and height, exercise Idea to Blueprint Review, and inspect browser warnings/errors: verifies the approved creation UI, styled/aligned foundation controls, and no-write-before-approval boundary.

### RG-DESKTOP-001 App Routes And Removed Pages

Status: active
Area: Desktop
Source: `frontend/src/app/app.routes.ts`, `frontend/src/app/shared/app-shell/**`, `internal/web/dist`
Trigger: A page or sidebar route is added, renamed, or removed.
Expected: App shell links and production embedded routes stay in sync; removed
pages are not present in built assets.
Suggested checks:
- `Set-Location frontend; npm run build:go`: verifies embedded assets.
- `rg -n "<removed route or label>" frontend/src internal/web/dist`: checks stale route references.

### RG-DESKTOP-002 SPA Shell And Asset Build Coherency

Status: active
Area: Desktop
Source: `internal/web/server.go`, `internal/web/server_test.go`, `frontend/angular.json`,
`frontend/src/app/features/create-product/create-product-page.css`, `internal/web/dist`
Trigger: ProductCrew is restarted or updated after the embedded Angular bundle
changes while a browser or WebView has previously opened the app.
Expected: SPA document responses are never cached across builds, valid static
assets retain their real content type, and requests for removed fingerprinted
assets return 404 instead of the SPA HTML. A normal reload therefore receives a
coherent index, stylesheet, JavaScript, and lazy-chunk set rather than rendering
unstyled HTML. Create Product layout rules remain in the linked global stylesheet
under the `app-create-product-page` scope instead of depending on runtime style
injection from its lazy chunk.
Suggested checks:
- `go test ./internal/web -run "TestWorkItemRouteServesAngularSPA|TestMissingFrontendAssetDoesNotFallBackToSPA|TestDirectIndexRequestIsNotCached"`: verifies the shell cache contract and stale-asset response.
- `Set-Location frontend; npm run build:go`: verifies Create Product styles are emitted into the fingerprinted global stylesheet and copied into the Go embed.
- Open an Angular route, restart ProductCrew with a newly built embedded bundle, then reload normally: verifies the AppShell and route component remain styled without a hard refresh.

### RG-DESKTOP-003 Collapsible App Sidebar

Status: active
Area: Desktop
Source: `frontend/src/app/shared/app-shell/**`, `frontend/src/styles.css`, `frontend/e2e/app-shell-sidebar-layout.spec.ts`
Trigger: User collapses or expands the primary left navigation while using a
desktop ProductCrew page such as Work board, Project Atlas, Settings, or
Observability.
Expected: The sidebar switches between the expanded 224px navigation and a 64px
icon rail, persists the chosen state, keeps route links, notifications, theme
toggle, runtime status, and active-project access usable, and gives the main
content column the recovered width without introducing horizontal document
overflow. The expanded brand row must keep the collapse button clear of the
ProductCrew name/version text. The saved collapsed state must not force the
narrow mobile shell into icon-only navigation.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/shared/app-shell/app-shell.spec.ts`: verifies toggle state, persistence, route links, and shell rendering.
- `Set-Location frontend; npm run test:ui`: verifies the compiled stylesheet
collapses the shell to a 64px rail, expands the main content column, and avoids
horizontal overflow in a real browser.

### RG-PACKAGING-001 Installer Update Build

Status: active
Area: Packaging
Source: `Makefile`, `build.ps1`, `scripts/**`, `src-tauri/**`
Trigger: Build, installer, update, version, or Tauri config changes.
Expected: `make installer-update` bumps version, builds installer, and writes
`installer-latest.json` only after a successful installer build. `make
deploy-installer` builds the installer into an explicit publish folder,
validates the manifest/hash/size, cleans only the configured
`s3://installers/ProductCrew/` R2 prefix, and uploads the installer plus
`installer-latest.json` without exposing R2 credentials outside the upload
process.
Suggested checks:
- `make installer-update`: verifies release update path when packaging is impacted.
- `make -n deploy-installer`: verifies the local R2 deployment target wiring.
- Parse `scripts/deploy-installer-r2.ps1`: verifies PowerShell syntax without
requiring local R2 credentials.
- `Set-Location src-tauri; cargo check --features installer-updates`: verifies Tauri compile path.

### RG-PACKAGING-002 Active Task Update Confirmation

Status: active
Area: Packaging
Source: `frontend/src/app/core/app-update.service.ts`, `frontend/src/app/core/app-update.service.spec.ts`, `frontend/src/app/shared/app-shell/**`
Trigger: User clicks the ProductCrew update/install action while any board has a
task in `in_progress` or `verifying`.
Expected: ProductCrew shows an in-app, theme-matched confirmation dialog that
updating may interrupt the active AI session. The warning must not use the
browser-native `window.confirm` dialog. Cancel, backdrop click, and Escape do
not launch the installer; confirming continues through the existing install
flow. If board status cannot be checked, the install flow stays unchanged.
Suggested checks:
- `Set-Location frontend; npm test -- --watch=false --include src/app/core/app-update.service.spec.ts --include src/app/shared/app-shell/app-shell.spec.ts`: verifies cancel, confirm, no-active-task, board-check-failure behavior, and in-app dialog rendering.
- `Set-Location frontend; npm run build:go`: verifies the update toast and
embedded frontend bundle compile.

### RG-ATLAS-001 Project Study Persistence And Freshness

Status: active
Area: Agent Studio
Source: `internal/web/project_intelligence.go`, `frontend/src/app/features/project-atlas/**`, `frontend/src/styles.css`, `frontend/e2e/project-atlas-layout.spec.ts`, `frontend/src/app/core/project-intelligence-api.service.ts`, `/api/projects/{projectID}/intelligence`
Trigger: User opens Project Atlas, starts or refreshes a project study, then changes relevant project source.
Expected: ProductCrew streams study progress, persists the latest result under the selected project's `.productcrew/project-intelligence`, restores it after restart, and reports fresh or stale from source/config files only. Generated dependencies, ProductCrew storage, build outputs, screenshots, and binary artifacts do not pollute the inventory or freshness fingerprint. Folder map payloads always serialize `children` as arrays, and the Atlas UI renders folder cards with a stable name/path/count even when opening older snapshots that omitted empty `children`. A provider failure leaves a usable partial folder and architecture map instead of a broken page, persists the concrete enrichment error, and shows that root-cause message again after the app reloads. Atlas layout rules ship in the initial stylesheet so the desktop WebView cannot expose raw unstyled HTML while loading the lazy route.
Sequences render as participant-based interaction diagrams from `from`/`to`
steps, Workflows render as compact ordered stage graphs using a two-column
snake/Z path where arrows point to the next numbered stage (right, down, left,
down) instead of pointing horizontally into empty wrapped space. Workflow
connector gaps must keep arrowheads visible between rows, and both views stay
inside the Atlas canvas without horizontal overflow. Diagram lenses render
inside a shared interactive canvas shell with zoom, fit/reset, and fullscreen
controls; opening fullscreen creates a fixed overlay instead of appending inline
content that changes the underlying page height or scroll state. Workflow
diagrams use Foblex Flow primitives (`f-flow`, `f-canvas`, `[fNode]`,
`[fConnector]`, and `f-connection`) with app-owned records, stable connector
ids, and `ngProjectAs` projection wrappers so nodes, connectors, and edges render
and remain draggable without falling back to CSS-only arrows.
Suggested checks:
- `go test ./internal/web -run "TestProjectStudy" -count=1`: verifies bounded inventory, cache persistence, non-null collections, folder child-array serialization, partial results, and freshness.
- `Set-Location frontend; npm run test:ui`: builds the initial stylesheet and verifies Atlas header, project strip, empty state, diagram shell, fullscreen overlay, spacing, overflow, and radii in a real browser.
- `Set-Location frontend; npm test -- --watch=false`: verifies Atlas loading, lens switching, visual sequence/workflow rendering, Foblex workflow primitives, fullscreen diagram behavior, project changes, SSE completion, and guidance actions.
- Open `/project-atlas`, run Study, switch lenses, collapse/expand evidence, and inspect browser warnings/errors: verifies the desktop workspace and inspector remain usable in full and partial states.

### RG-ATLAS-002 Learned Guidance Precedence

Status: active
Area: Agent Studio
Source: `internal/agentprofile/profile.go`, `internal/web/project_service.go`, `internal/web/project_intelligence.go`
Trigger: User reviews an Atlas study and applies learned guidance to the selected project.
Expected: Learned role guidance is stored only for that project and is composed after the shared Working Standard but before the project's manually edited role profile. Existing manual instructions therefore retain final precedence, and the effective profile remains reusable by every configured AI provider.
Suggested checks:
- `go test ./internal/agentprofile`: verifies shared, learned, and manual instruction ordering.
- `go test ./internal/web -run "TestApplyProjectStudy" -count=1`: verifies project scoping and preservation of manual role instructions.
