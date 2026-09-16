# Design QA — CLI Control Center

## Comparison target

- Source visual truth: `C:\Users\anh.tang\.codex\generated_images\019fe767-42eb-7ea1-bb7b-1b1cddac03d5\exec-f3802d4e-199e-4598-9025-59e9f42e7c8e.png`
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\design-assets\cli-tools-implementation-dark-final.png`
- Settings summary gap fix: `E:\Projects\AI-Product-Team\design-assets\settings-summary-gap-fixed.png`
- Combined full-view comparison: `E:\Projects\AI-Product-Team\design-assets\cli-tools-design-comparison-final.png`
- Source pixels: 1487 × 1058. Implementation pixels: 1425 × 1013.
- CSS viewport: 1440 × 1024 at 1× density. The combined comparison normalizes both captures to 1440 × 1024.
- State: dark theme, GitHub CLI selected, installed version behind the latest version, installation details collapsed.

## Full-view comparison evidence

- The implementation preserves the selected mock's master-detail composition, health strip, search and status filters, five-tool inventory, status colors, version facts, action hierarchy, readiness checklist, and collapsed installation disclosure.
- The component intentionally appears inside the existing Settings document rather than replacing Workspace, Git authentication, and Agent instructions. The implementation capture is scrolled to align the CLI section with the mock's primary content region.
- Existing product tokens, typography, Material Symbols, sidebar, radii, and borders are retained instead of introducing a second design system.

## Focused-region comparison evidence

- The combined comparison is readable at the tool row, facts, action, and checklist level; an additional crop was not required.
- First pass found a P2 density mismatch: inventory rows and checklist rows were visibly shorter than the source, causing adjacent Agent instructions to enter the viewport too early.
- The console minimum height changed from 540 px to 650 px, inventory rows from 67 px to 76 px, and checklist rows from 62 px to 68 px. The revised capture restores the intended breathing room and scan rhythm.
- First pass also exposed the browser's default white focus outline on the selected tool. It was replaced with a design-token teal focus treatment while preserving keyboard visibility.

## Required fidelity surfaces

- Fonts and typography: existing Inter/system stack, weights, hierarchy, line-height, truncation, and small-label optical balance match the product and remain close to the mock.
- Spacing and layout rhythm: master-detail proportions, 290 px inventory rail, facts split, action alignment, row density, radii, and vertical rhythm match the target. The adjacent Agent card is an intentional existing-page context difference.
- Colors and visual tokens: dark surfaces, teal selection/action states, amber update warnings, green ready states, muted copy, and borders use existing app tokens and provide sufficient contrast.
- Image quality and asset fidelity: the product logo is retained. CLI representations use the app's existing Material Symbols icon system; no placeholders, emoji, handcrafted SVG, gradients, or CSS drawings were introduced.
- Copy and content: labels and diagnostics reflect live local data. Installed/latest versions, executable path, active GitHub account, package manager, and last-check time come from the backend rather than mock constants.

## Findings and iteration history

1. P2: Tool inventory and checklist density was too compressed compared with the selected mock.
   - Fix: increased console and row heights while preserving the existing desktop layout.
   - Post-fix evidence: `design-assets/cli-tools-implementation-dark-final.png`.
2. P2: Selected-row focus used an inconsistent white browser outline.
   - Fix: added a teal `:focus-visible` outline based on the existing brand token.
   - Post-fix evidence: selected GitHub CLI row in the final capture.
3. P2: The expanded configuration summary was taller than the settings form, forcing the following CLI section below the grid and leaving an excessive empty gap.
   - Fix: retained the four import-critical summary values and grouped six technical storage paths in a native, accessible `Data locations` disclosure.
   - Post-fix evidence: `design-assets/settings-summary-gap-fixed.png`; the CLI health strip now follows the settings grid with the standard section gap, while every path remains available on demand.

No actionable P0, P1, or P2 findings remain. The existing Settings sections visible outside the focused CLI region are accepted product context, not design drift.

## Interaction and diagnostics

- All, Attention, and Ready filters work; Attention returned Codex CLI, GitHub CLI, and Node.js from live status data.
- Tool selection updates facts and readiness checks.
- Installation details expands and reports the actual management source.
- Configuration summary `Data locations` expands to all six persisted paths and collapses without layout errors.
- Codex Desktop-managed CLI correctly disables automatic update instead of installing a conflicting winget copy.
- GitHub CLI exposes its winget update action; Node.js is recognized as NVM-managed.
- Run diagnostics completed and refreshed status.
- Browser console warnings/errors: none.
- Go tests: passed for `./internal/toolchain` and `./internal/web`.
- Angular tests: 14 passed across 5 files.
- Angular production build: passed.

final result: passed

---

# Design QA — Email Notification Control Consistency

## Comparison target

- Source visual truth: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-d8a99a8f-b609-4a1b-9838-c93200611cd8.png` (1154 × 556 px, dark theme, Email notifications card).
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\frontend\design-output-settings-email-controls.png` (1176 × 560 px, 1176 × 560 CSS px, device scale factor 1, dark theme).
- State: disabled Email notifications configuration with the four two-column field rows visible.

## Findings and comparison history

1. P1 before fix: `email` and `number` inputs were absent from the shared ProductCrew input selectors, so Destination email, Sender address, and SMTP port fell back to native gray browser controls.
2. P2 before fix: Settings selects were 39 px high while paired text-like inputs were 42 px high, producing a visible row-alignment drift.
3. Fix: include `email` and `number` in the shared base/focus selectors, normalize Settings-card selects to 42 px, and give field labels a stable 18 px line height.
4. After fix: the focused browser comparison shows all eight controls sharing the dark surface, border, radius, typography, height, and aligned row starts. No P0/P1/P2 mismatch remains in the requested control region.

## Fidelity surfaces

- Fonts and typography: existing ProductCrew font inheritance, weights, placeholder tone, and label hierarchy are preserved; label line height is normalized.
- Spacing and layout rhythm: the existing two-column grid and 16 px body rhythm remain unchanged; controls are 42 px tall and vertically aligned.
- Colors and visual tokens: every text-like control now uses `--control`, `--text`, `--line-strong`, and the existing brand focus ring.
- Image quality and assets: no product imagery is present; the existing icon system is unchanged.
- Copy and content: field labels, placeholders, values, and helper copy are unchanged.

## Interaction and diagnostics

- Focus behavior for email and number inputs now matches the existing text, URL, password, and search controls.
- `npx playwright test e2e/settings-email-layout.spec.ts`: 1/1 passed.
- `npm run test:ui`: 6/6 passed.
- `npm test -- --watch=false`: 13 files, 126 tests passed.
- `npm run build:go`: passed; existing non-fatal bundle budget warnings remain.

final result: passed

---

# Design QA — Git Delivery Command Center

## Comparison target

- Source visual truth: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-fc43c7c0-84c9-46a4-b109-2f460b26cc59.png`.
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\frontend\test-results\git-delivery-layout-Git-de-3c067--hierarchy-without-overflow\git-delivery-approved.png`.
- Source and implementation viewport: 1488 x 1058 at 1x density.
- State: dark theme, three completed tickets, commit completed, push/recovery active, detailed log collapsed.

## Full-view and focused comparison evidence

- The implementation preserves the approved centered desktop modal, repository context header, ticket/file table, four-stage delivery track, data-integrity strip, activity/recovery timeline, detailed-log disclosure, safe-stop control, and passive running action.
- The real Angular route was also opened from the Work Board launcher against a freshly built embedded bundle. At 1488 x 1058 the modal measured 1168 x 780.6 CSS px with `scrollWidth === clientWidth` and `scrollHeight === clientHeight`.
- The Work Board remains visible behind the modal and no inline Git panel is inserted into the Kanban body.

## Required fidelity surfaces

- Fonts and typography: ProductCrew's existing font stack, compact metadata, heading hierarchy, monospace trace/commit values, and ellipsis behavior are retained.
- Spacing and layout rhythm: the modal uses the selected two-column composition, bounded ticket table, evenly distributed delivery stages, and persistent footer actions.
- Colors and visual tokens: all surfaces, borders, teal progress states, amber failures, muted copy, and focus states reuse ProductCrew tokens.
- Image quality and asset fidelity: the production component uses the existing Material Symbols icon system; no new raster or placeholder asset was introduced.
- Copy and content: project, branch, delivery ID, ticket names, file counts, commit SHA, status, recovery, errors, and trace events come from persisted backend delivery state.

## Findings and comparison history

1. P1: the legacy Production Deploy `Configure` selector still forced the icon-only link to span the full CSS Grid row, placing it below the card.
   - Fix: explicitly restore normal grid placement and zero padding for compact Build and Deploy icon actions.
2. P1: Git status occupied Work Board body space even though the accepted direction requires a separate delivery command center.
   - Fix: replace the banner/expandable panel with a compact title-row launcher and modal.
3. P1: a running delivery could become ambiguous after process restart.
   - Fix: persist every transition and recover interrupted queued/running/recovering records into a traceable failed state that can be reviewed and retried.

No actionable P0, P1, or P2 visual finding remains at the supported desktop viewport.

## Interaction and diagnostics

- Browser layout checks verify the four Work Board actions remain one row and one height, including unconfigured Build/Deploy anchor actions.
- Browser layout checks verify the Git modal has no horizontal or vertical overflow at 1488 x 1058.
- Live embedded route verified the separate launcher, modal dialog semantics, disabled preflight action on a project without eligible ticket evidence, and exact modal geometry.
- Angular tests: 13 files, 126 tests passed.
- Playwright UI regression tests: 5 passed.
- Angular production and embedded Go bundle builds passed; only existing non-fatal bundle/component CSS budget warnings remain.
- Focused Go delivery, storage, Kanban, and developer-provider tests passed. The broad Go suite reached the existing Windows asynchronous `TempDir RemoveAll` cleanup race in `TestBoardPlanningReviewAndRoleSafeMoveAPI`; the impacted packages and focused test passed independently.

final result: passed

---

# Design QA — Project Atlas

## Comparison target

- Source visual truth: `C:\Users\anh.tang\.codex\generated_images\019fe767-42eb-7ea1-bb7b-1b1cddac03d5\exec-e6deba0d-0b4b-429a-aa43-24ade2934a16.png`.
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\design-qa-project-atlas.png`.
- Combined comparison: `E:\Projects\AI-Product-Team\design-qa-project-atlas-comparison.png`.
- Source pixels: 1487 x 1058. Implementation pixels: 1582 x 904 at 1x density.
- State: dark theme, AI-Product-Team selected, provider unavailable, persisted partial study, Architecture lens selected, evidence collapsed.

## Full-view and focused comparison evidence

- The implementation preserves the selected direction's dedicated Atlas workspace, project knowledge strip, five visualization lenses, large exploration canvas, persistent inspector, compact evidence disclosure, and role-guidance action.
- The screen is integrated into ProductCrew's real sidebar, global project context, typography, Material Symbols, theme tokens, and existing desktop page gutters rather than copying the concept's unrelated navigation shell.
- The real provider-unavailable state remains useful: source indexing produces architecture and folder-map content, the limitation is explicit, and empty Data Model, Sequence, and Workflow lenses remain safe to open.
- The side-by-side comparison shows no page-level horizontal overflow, clipped controls, overlapping labels, or inspector collapse at the verified desktop viewport.

## Required fidelity surfaces

- Fonts and typography: existing ProductCrew font stack, uppercase section labels, compact metadata, readable cards, and inspector hierarchy remain consistent with the app.
- Spacing and layout rhythm: the project strip, lens rail, 3-column architecture grid, canvas, and 332 px inspector form one bounded desktop workspace with consistent borders and radii.
- Colors and visual tokens: both themes use existing surface, line, brand, warning, danger, success, and muted tokens. No gradients or separate palette were introduced.
- Image quality and asset fidelity: the existing ProductCrew logo and Material Symbols icon set are retained. No placeholder image, handcrafted SVG, emoji, CSS drawing, or fake generated diagram asset was added.
- Copy and content: status, evidence counts, freshness, paths, modules, confidence, and warnings come from the persisted study result rather than mock constants.

## Findings and comparison history

1. P1: Empty AI collections were serialized as `null`, which could break lens counts and list rendering in the partial-result UI.
   - Fix: normalize every study collection to an array before persistence and API delivery, including old cached results.
2. P2: The first deterministic fallback prioritized dot/config folders ahead of real implementation modules.
   - Fix: prioritize frontend, command, backend, desktop, packaging, and script roots for the architecture canvas.
3. P2: Binary screenshots and design-QA artifacts participated in the source fingerprint, so capturing a mockup could immediately mark knowledge stale.
   - Fix: fingerprint relevant source and configuration files only, while ignoring binaries, screenshots, generated dependencies, caches, and build output.

No actionable P0, P1, or P2 findings remain.

## Interaction and diagnostics

- Global project selection loads that project's cached study without changing unrelated Settings selectors.
- Study and Study again use REST to start work and SSE to stream phase, progress, logs, and completion.
- Architecture cards update the inspector; all five lenses render valid real, partial, or empty states.
- Study evidence is collapsed by default and remains expandable.
- Agent Skills exposes learned guidance only when available and requires explicit Apply before updating project Agent Studio.
- Angular tests: 12 files, 99 tests passed.
- Focused Go Project Atlas and Agent Studio tests: passed.
- Embedded production build: passed; only the existing non-fatal 500 kB initial bundle budget warning remains.
- Full Go suite reached unrelated existing Windows/environment failures in Edge PNG rendering and asynchronous TempDir cleanup; focused impacted tests passed.

final result: passed

---

# Design QA — Create Product SPA Asset Recovery

## Comparison target

- Reported broken state: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-db2daae7-2e30-4fc7-bb59-2485b5292fd8.png`.
- Verified route: `http://127.0.0.1:18082/projects/new` after a normal browser reload against the restarted Go server.
- Comparison scope: AppShell, Create Product page layout, typography, controls, and responsive composition.

## Findings and evidence

- The reported state had global theme rules but no Create Product component layout even though its lazy Angular content had rendered. The initial cache-only diagnosis was disproved by a second report captured after the server fix.
- Root cause: Create Product was the only new surface relying exclusively on styles injected from its lazy JavaScript chunk in the affected runtime.
- The page stylesheet is now compiled into the fingerprinted linked global CSS bundle and scoped under `app-create-product-page`, while the lazy chunk contains behavior and markup only.
- A fresh 1280 × 720 browser document reports AppShell columns of `196px 1068.8px`, Create Product padding of `24px 30px 56px`, and the intended `660.8px 330px` main/rail layout.
- Browser-visible result: no recurrence of the raw list, default gray buttons, full-width unstyled textarea, or collapsed form alignment shown in the report.

## Diagnostics

- Fresh document assets: `styles-Y7IDFYB7.css` and `main-4VHS3TB6.js`.
- Angular production `build:go`: passed; Create Product rules are present only as globally linked, page-scoped selectors.
- Angular tests: 97 passed across 11 files.
- Focused cache/stale-asset tests and the full `internal/web` suite: passed.

final result: passed

---

# Design QA — Create Product From Scratch

## Comparison target

- Source visual truth: `C:\Users\anh.tang\.codex\generated_images\019fe767-42eb-7ea1-bb7b-1b1cddac03d5\exec-ab8eebaa-345d-48c8-ac19-8359f45a8409.png` for Idea entry and `C:\Users\anh.tang\.codex\generated_images\019fe767-42eb-7ea1-bb7b-1b1cddac03d5\exec-bbba82f1-0369-482a-874d-df776f00be53.png` for Blueprint Review.
- Browser-rendered implementation: live Codex in-app browser at `http://127.0.0.1:18082/projects/new` using an isolated ProductCrew data directory.
- Source pixels: 1487 x 1058 for both visual targets. Implementation viewport: 1265 x 710 at 1x density; the comparison was normalized by evaluating the same content regions rather than browser chrome.
- State: light theme, no imported projects, Angular + Go foundation, valid local destination, Idea and Blueprint Review states.

## Full-view comparison evidence

- The implementation preserves the approved flow and hierarchy: Projects entry, five-stage progress rail, outcome-first composer, starting-point choices, project foundation, safety explanation, Blueprint Review, architecture cards, milestones, decisions, and the approval CTA.
- ProductCrew's existing shell, typography, Material Symbols, field shapes, brand teal, borders, and light-theme surfaces are reused rather than introducing a parallel design system.
- The narrower implementation viewport keeps the main/rail composition legible without horizontal overflow; the rail becomes non-sticky and stacks at the existing responsive breakpoint.

## Focused-region comparison evidence

- Idea composer, starting-point cards, foundation fields, architecture cards, criteria chips, and approval rail were readable in the browser captures, so no additional focused crop was required.
- The browser state transition from Idea to Blueprint Review was exercised with a real draft API call. The title, stage indicator, foundation profile, destination, tool preflight, and approval affordance all updated coherently.

## Required fidelity surfaces

- Fonts and typography: existing ProductCrew font stack, compact eyebrow labels, display heading weight, form copy, code paths, wrapping, and hierarchy match the approved direction.
- Spacing and layout rhythm: 1fr/330 px desktop composition, 16–18 px section gaps, 8–11 px radii, compact cards, and sticky review rail reproduce the target density without crowding.
- Colors and visual tokens: brand teal, muted copy, semantic warning/error colors, surfaces, controls, and borders use existing application tokens in both light and dark themes.
- Image quality and asset fidelity: no new raster asset was required. Existing ProductCrew branding and Material Symbols are retained; no emoji, handcrafted SVG, CSS drawing, or placeholder imagery was introduced.
- Copy and content: all labels describe implemented behavior. The review explicitly states atomic staging, local persistence, toolchain checks, Work Board import, and the no-secrets boundary.

## Findings and comparison history

- No actionable P0, P1, or P2 mismatch remained in the first rendered comparison.
- The generated concept showed a voice/reference affordance. It is intentionally omitted from V1 because the approved core flow does not yet persist pre-project attachments or own a cross-WebView speech contract; adding a static non-working control would reduce product quality.
- Structured AI enrichment is constrained by a strict blueprint schema and read-only runtime. When the selected provider is unavailable or returns unsafe/incomplete data, the same screen shows a clear warning and falls back to the deterministic profile blueprint.

## Interaction and diagnostics

- Idea form validation keeps the primary CTA disabled until name, destination, and description are valid.
- Blank, Guided brief, and Desktop template choices update the brief/profile state.
- Destination picker is wired through the existing native folder-selection API.
- A real blueprint was persisted in isolated app data and rendered in Blueprint Review without writing source files.
- Approval is disabled when required CLI tools are missing; backend tests verify destination collision, draft persistence, deterministic scaffold shape, staging cleanup, project registration, and initial backlog creation.
- Browser console warnings/errors: none.
- Angular tests: 11 files, 97 tests passed.
- Angular production and embedded Go bundle builds passed.
- Focused Product Creation Go tests passed. Full serial Go tests reached two known Windows asynchronous `TempDir` cleanup races; both affected legacy tests passed immediately in isolation.

final result: passed

---

# Design QA — Multi-Repository Workspace Mode

## Comparison target

- Source visual truth: `C:\Users\anh.tang\.codex\generated_images\019fe767-42eb-7ea1-bb7b-1b1cddac03d5\exec-fe10a137-ed05-430b-9e5a-cab7e3e7a982.png`.
- Browser-rendered implementation: live Codex in-app browser at `http://127.0.0.1:8000/work-items` (browser capture evidence; filesystem export was unavailable).
- Source pixels: 1487 × 1058. Implementation pixels: 1487 × 1058.
- CSS viewport: 1487 × 1058 at 1× density; no density normalization was required.
- State: dark theme, Workspace scope selected, two-repository workspace active, empty workspace board. The source uses a populated board, so fidelity conclusions are limited to the shared shell, header, scope switch, command bar, and board composition rather than task-card content.

## Full-view comparison evidence

- The implementation preserves the selected mock's integrated scope switch, compact orchestration header, primary New request action, single horizontal workspace command bar, repository summary, access summary, execution policy, and Manage workspace action.
- The existing ProductCrew navigation and visual system are retained. The workspace-specific label and explanatory copy replace the project-specific header copy without changing Project mode.
- The empty-board state centers one clear first action in the remaining canvas instead of fabricating task data to visually mimic the populated concept.

## Focused-region comparison evidence

- The header and workspace command bar were inspected at their native 1487 × 1058 viewport. Labels, badges, selector affordance, access count, build warning, and primary CTA remain readable without clipping or horizontal overflow.
- The workspace manager was opened and inspected separately at the same viewport. Workspace name, selected repository count, primary repository, access toggles, local-storage explanation, serial execution policy, and create/save actions are visible in one bounded modal.
- Additional crops were not required because the relevant command-bar and dialog text is readable in the native captures; board task cards were not compared because the implementation test workspace intentionally has no task data.

## Required fidelity surfaces

- Fonts and typography: the existing ProductCrew font stack, heading hierarchy, button weights, muted helper copy, uppercase eyebrow labels, truncation, and line heights remain consistent with the app and close to the source concept.
- Spacing and layout rhythm: the scope switch is inline with the page identity, the command bar forms one compact scan line, and the modal uses a bounded scroll-safe layout. The implementation intentionally uses the existing 196 px sidebar and ProductCrew page gutters rather than copying the concept's unrelated navigation proportions.
- Colors and visual tokens: dark surfaces, teal active states, amber build warning, muted metadata, green health state, borders, and focus treatments use existing app tokens.
- Image quality and asset fidelity: the ProductCrew logo and existing Material Symbols icon system are retained. No placeholder imagery, emoji, handcrafted SVG, or CSS-drawn substitute assets were introduced.
- Copy and content: workspace copy describes real persisted and provider behavior. The implementation labels access as read-only/read-write, identifies the primary repository, and explicitly states local `.workspace` persistence and serial execution.

## Findings and comparison history

- No actionable P0, P1, or P2 visual mismatch was found in the shared states.
- The source's populated task-card state cannot be compared against the intentionally empty QA workspace. This is an accepted state difference rather than evidence of card-layout drift because Workspace mode reuses the existing Work Board renderer.
- P3 follow-up: a future populated multi-repository fixture would allow a direct comparison of repository badges and cross-repository task-card density against the concept.

## Interaction and diagnostics

- Project and Workspace scope tabs switch independently from the global sidebar project selector.
- Workspace creation and edit both persist through the REST API; the manager restores repository order, primary selection, and access modes.
- New request, build verification, board loading, SSE, queue controls, and existing task drawers continue through the shared Work Board surface.
- Browser console warnings/errors after reload: none.
- Go test suite: passed with serial package execution (`go test -p 1 ./...`).
- Angular tests: 97 passed across 11 files.
- Embedded frontend build: passed (`npm run build:go`).

final result: passed

---

# Design QA — AI Session Intelligence

## Comparison target

- Source visual truth: `C:\Users\anh.tang\.codex\generated_images\019fe767-42eb-7ea1-bb7b-1b1cddac03d5\exec-a9e8f5ca-b051-45a8-ba0e-caada8d89a47.png`.
- Browser-rendered implementation: live Codex in-app browser at `http://127.0.0.1:8000/observability` (browser capture evidence; no filesystem export).
- State: dark theme, All projects, Last 30 days, real copied ProductCrew event and board data.
- Comparison scope: AI Session Intelligence hero, KPI strip, session command center, and provider comparison. Existing delivery-health analytics and Trace Explorer intentionally remain below the new surface.

## Full-view and focused comparison evidence

- The selected operations-console direction is preserved: compact header filters, four KPI cards, dense session table, status emphasis, expandable run rows, and a model/provider comparison rail.
- ProductCrew's established sidebar, typography, Material Symbols, surface tokens, borders, and teal status language are retained instead of introducing a second design system.
- The implementation uses durable project/task labels and real provider data. Decorative trend charts from the concept were omitted because time-bucketed token history is not available and fabricated analytics would be misleading.
- The table breakpoint and minimum width were tightened so the command center and comparison rail remain aligned in the available desktop content width without page-level horizontal overflow.

## Interaction and diagnostics

- Project, time-range, status, and provider filters are present and operate on one session dataset.
- Session command center is paginated at 10 runs per page. Live verification showed `Showing 1-10 of 60`, page navigation advanced while keeping 10 visible rows, and switching to the Completed filter reset the footer to `Showing 1-10 of 51`.
- Run rows expose status, elapsed time, provider/model, token availability, and an expandable event timeline.
- Expandable run details still open on filtered/paged results.
- Active elapsed time updates locally once per second; durable lifecycle changes refresh via the existing Observability SSE stream.
- Historical runs without provider usage show `Not reported` / `Unavailable`; no token estimate is fabricated.
- Browser console warnings/errors: none.
- Angular tests: 11 files, 93 tests passed.
- Embedded production build: `npm run build:go` passed; only the pre-existing Work Board stylesheet budget warning remains.
- New Go aggregation and provider-usage tests passed. The broad Go suite reached the existing Windows asynchronous `TempDir` cleanup race in `TestSubmitDesignFeedbackAPIRequeuesSameTaskAndPersistsRevisionHistory`; the same test passed immediately in isolation.

final result: passed

---

# Design QA - Create Product Foundation Controls

## Comparison target

- Source visual truth: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-2ba091f7-e494-4d1b-ab27-c4cbfddeae71.png`.
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\.codex-tmp\create-product-foundation-aligned.png`.
- Verified route: `http://127.0.0.1:18083/projects/new?assetRefresh=3`.
- CSS viewport: Codex in-app browser narrow panel capture, dark theme, foundation card scrolled into view.
- State: Create Product Idea step, no imported projects in isolated app data, Angular + Go profile, default destination parent, Initialize Git enabled.

## Full-view comparison evidence

- The previous reported state showed native gray Product name and Destination parent inputs that did not match the ProductCrew dark theme.
- The revised foundation controls render with dark ProductCrew surfaces, teal focus/hover treatment, 8 px radius, 42 px control height, and consistent spacing with the surrounding card.
- The path picker keeps the Destination parent input and Browse button visually connected without returning to native browser button styling.
- The Product name input and Application profile select now share the same first-row top position, so the helper text under Product name no longer pushes the select out of alignment.

## Focused-region comparison evidence

- Focused region was sufficient because the reported defect is contained to the Project Foundation control group.
- Computed style evidence from the live browser:
  - Product name: `rgb(23, 33, 30)` background, `rgb(58, 74, 69)` border, 42 px height, 8 px radius, 13 px font.
  - Application profile: same control background, border, height, radius, and custom dropdown affordance.
  - Destination parent: same input treatment with joined Browse button.
  - Browse button: 42 px height, dark surface background, matching border and radius on the joined edge.
- Wide viewport alignment evidence: Product name and Application profile both render at top `811` with height `42`; Destination parent and Browse both render at top `911` with height `42`.

## Required fidelity surfaces

- Fonts and typography: ProductCrew Inter/system stack, label weight, helper copy size, and control text weight remain consistent with existing Settings and Work Board controls.
- Spacing and layout rhythm: the two-column foundation grid, full-width path control, 14 px grid gap, 42 px control height, 8 px radius, and top-aligned first-row controls align with existing form density.
- Colors and visual tokens: controls now use `--control`, `--line-strong`, `--text`, `--muted`, `--brand`, `--brand-soft`, and `--surface-soft` instead of native browser gray.
- Image quality and asset fidelity: no image assets were changed. Material Symbols remain the existing icon system.
- Copy and content: labels, helper copy, profile value, destination value, and Initialize Git copy are unchanged.

## Findings and comparison history

1. P2: Product name and Destination parent inputs used native browser gray styling because bare `<input>` elements were not covered by the global typed-input selectors.
   - Fix: added scoped Create Product foundation control styles for bare inputs, selects, joined path input/button, hover, focus, placeholder, and option states.
   - Post-fix evidence: `.codex-tmp/create-product-input-style-dark.png` and live computed style inspection.
2. P2: Application profile was vertically offset from Product name because the two-column foundation grid stretched the shorter label group beside a label that included helper text.
   - Fix: pinned the foundation grid and field labels to `start` alignment so uneven helper copy does not shift control baselines.
   - Post-fix evidence: `.codex-tmp/create-product-foundation-aligned.png` and live wide-viewport bounding-box inspection.

No actionable P0, P1, or P2 findings remain.

## Interaction and diagnostics

- Browser route loaded the new `styles-GUM5OT4A.css` bundle.
- Dark theme was enabled through the app shell.
- Browser console/style inspection found the revised scoped foundation control rules applied.
- `npm run build:go`: passed, with the existing non-fatal initial bundle budget warning.
- `npm test -- --watch=false`: 11 files, 97 tests passed.
- `go test ./internal/web -run "TestWorkItemRouteServesAngularSPA|TestMissingFrontendAssetDoesNotFallBackToSPA|TestDirectIndexRequestIsNotCached"`: passed.

final result: passed

No actionable P0, P1, or P2 findings remain in the implemented session-monitoring surface.

final result: passed

---

# Design QA — Feature Library

## Comparison target

- Source visual truth: `C:\Users\anh.tang\.codex\generated_images\01a01e76-8c62-7632-8ad2-fbf7ac756256\exec-285071d7-117b-43a8-bae7-dced350e6470.png`.
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\design-qa-feature-library.png`.
- Responsive implementation: `E:\Projects\AI-Product-Team\design-qa-feature-library-mobile.png`.
- Combined source/implementation comparison: `E:\Projects\AI-Product-Team\design-qa-feature-library-comparison.png`.
- Source pixels: 1487 x 1058. Desktop implementation: 1280 x 720. Mobile verification viewport: 390 x 844.
- State: dark theme, AI-Product-Team selected, FEAT-014 selected, live plan/task/history data, two historical Designer HTML mockups.

## Comparison evidence

- The selected master-detail direction is preserved: compact feature index,
  readable dossier, lifecycle rail, role-grouped tickets, design artifacts,
  history, acceptance outcome, and plan documents.
- ProductCrew's real sidebar, project selector, tokens, typography, Material
  Symbols, status language, and data model replace mock-only navigation and
  invented archived states.
- Live Designer `overview.html` and `feedback-states.html` files are visible as
  thumbnails and open in a large sandboxed lightbox. Registered PNG previews
  remain supported.
- The combined comparison shows equivalent information density and hierarchy.
  Differences in ticket counts, labels, and state are intentional live-data
  differences rather than visual drift.

## Findings and iteration history

1. P1: Historical Designer HTML existed but no PNG artifact was registered, so
   Design artifacts incorrectly appeared empty.
   - Fix: added a read-only task design-source endpoint restricted to reported
     HTML files under the task artifact directory, with CSP and iframe sandboxing.
2. P2: The first 1280 px pass showed a dossier horizontal scrollbar because
   responsive breakpoints did not account for the 196 px sidebar.
   - Fix: shifted compact breakpoints to the available content width.
3. P2: The first mobile pass allowed the 620 px lifecycle track to participate
   in page intrinsic sizing and exposed a document scrollbar.
   - Fix: added inline-size containment; lifecycle keeps its own horizontal
     scroll while document and feature index widths remain bounded.

No actionable P0, P1, or P2 findings remain.

## Interaction and diagnostics

- Search reduced the live list to the matching Backup feature and updated the dossier.
- Shipped filter returned five shipped features; All restored fourteen features.
- Selecting FEAT-014 updated the dossier; delivery ticket details expanded inline.
- HTML mockup lightbox opened Overview and rendered the saved design source.
- Desktop geometry: document 1280/1280 px and dossier 789/789 px client/scroll width.
- Mobile geometry: document 375/375 px and workspace 375/375 px client/scroll width; lifecycle scroll remains internal.
- Browser console warnings/errors: none.
- Angular full suite: 11 files, 88 tests passed.
- Focused Angular route/page shell suite: 2 files, 12 tests passed.
- Focused Go design-source tests: passed.
- Embedded production build: `npm run build:go` passed.
- Full Go suite reached unrelated existing Windows runner failures in
  `internal/designartifact TestEdgeRendererSmoke` and asynchronous `TempDir`
  cleanup; changed endpoint tests and adjacent web route checks pass.

final result: passed

---

# Design QA — Collapsible Board Activity

## Comparison target

- Source visual truth: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-314da47b-1b4c-43d9-aeed-e133e4ede37c.png`.
- Browser-rendered collapsed state: `E:\Projects\AI-Product-Team\design-qa-activity-collapsed.png`.
- Browser-rendered expanded state: `E:\Projects\AI-Product-Team\design-qa-activity-expanded.png`.
- Source pixels: 1660 × 610. Implementation captures: 1280 × 720 at 1× density.
- State: dark theme, AI-Product-Team selected, eight recent board events. Comparison is scoped to the Board Activity region because the source and implementation use different full-window crops.

## Full-view and focused comparison evidence

- The collapsed state reduces Board Activity from a 370 px detail region to a 58 px summary bar and immediately exposes the Kanban columns below it.
- The summary preserves the source hierarchy and live signal while adding the event count and explicit Expand label.
- The expanded state retains the source event ordering, timestamps, type/target metadata, bounded scrollbar, semantic state icons, and Open task actions.
- Focused inspection was not needed beyond the full-width Activity region because typography, icon alignment, row spacing, and actions are readable in both 1280 × 720 captures.

## Required fidelity surfaces

- Fonts and typography: existing ProductCrew font stack, uppercase eyebrow, title weight, muted event count, and compact metadata hierarchy are preserved.
- Spacing and layout rhythm: the collapsed bar is 58 px high; expanded content uses the existing 14 px inset, 7 px row gap, 8–9 px radii, and 224 px bounded list height.
- Colors and visual tokens: surfaces, borders, brand teal, live green, muted text, and role/status colors use existing tokens. Hover and keyboard focus remain visible.
- Image quality and asset fidelity: no raster assets are required. Existing Material Symbols are retained; no placeholders, handcrafted SVGs, or CSS illustrations were introduced.
- Copy and content: Board Activity, recent event count, live status, Expand/Collapse, event messages, and Open task actions are concise and retain the source meaning.

## Interaction and diagnostics

- Activity is collapsed by default with `aria-expanded=false` and no hidden event rows rendered.
- Clicking the header expands eight rows, changes the action to Collapse, and sets `aria-expanded=true`.
- Open task actions remain available only in the expanded state.
- Board SSE data continues updating the computed recent-event list while collapsed.
- Browser console warnings/errors: none.
- Angular tests: 38 passed across 8 files.
- Angular production build: passed.

No actionable P0, P1, or P2 findings remain. The smaller implementation viewport and visible sidebar are accepted capture-context differences.

final result: passed

---

# Design QA — Smart Intake

## Comparison target

- Source visual truth: `C:\Users\anh.tang\AppData\Local\Temp\codex-clipboard-2ec67098-ea10-40d0-bdb5-58afefb2aa1c.png`.
- Describe step: `E:\Projects\AI-Product-Team\design-assets\smart-intake-describe.png`.
- Clarify step: `E:\Projects\AI-Product-Team\design-assets\smart-intake-clarify.png`.
- Combined source/implementation comparison: `E:\Projects\AI-Product-Team\design-assets\smart-intake-comparison.png`.
- State: dark theme, AI-Product-Team selected, Codex-generated five-question clarification form.

## Full-view and focused comparison evidence

- The existing ProductCrew modal, typography, teal selection language, compact cards, sticky footer, and desktop density are preserved.
- Fixed Work type, Delivery target, user-experience, and Definition of Done controls are replaced by a two-step flow. The first step keeps title and description as the source of truth; the second renders only controls selected by AI from a safe allowlist.
- The generated form keeps one clear vertical reading path, shows provider/source and generation summary, and reports captured decision progress in the sticky footer.
- The combined comparison confirms the new screen removes irrelevant fixed options without losing the original control clarity or established dark-theme visual hierarchy.

## Required fidelity surfaces

- Typography and tokens use the existing ProductCrew system. No new dependency, icon set, or color palette was introduced.
- Layout uses the existing modal shell with bounded 6/12-column field spans, compact 7–9 px control radii, and existing Material Symbols.
- The clarify step resets its scroll position to the top after generation, keeping the AI summary and first decision visible.
- Loading, fallback, required-answer, disabled-submit, edit-request, and provider states are visible and explicit.

## Interaction and diagnostics

- New Request opens the Describe step and keeps the primary action disabled until title and description are valid.
- A real Codex request generated five request-specific questions through `POST /api/projects/{projectID}/intake/questions` with HTTP 200.
- Generated defaults make required decisions immediately legible; radio selection works and Edit request returns to Describe with scroll position 0.
- Schema and answers are never rendered as arbitrary HTML. The backend validates type, binding, option, default, answer, cardinality, and unknown-question constraints.
- Angular tests: 35 passed across 8 files.
- Focused Go packages `./internal/kanban` and `./internal/web`: passed.
- Angular production build: passed; the pre-existing Work board component-style budget now reports a warning because the component stylesheet is close to its 40 kB error threshold.
- Full `go test ./...`: all packages passed except the existing environment-dependent `internal/designartifact TestEdgeRendererSmoke`, where Edge did not produce a PNG.

No actionable P0, P1, or P2 Smart Intake findings remain.

final result: passed

---

# Design QA — Designer Output File Actions

## Comparison target

- Source visual truth: `C:\Users\anh.tang\AppData\Local\Temp\codex-clipboard-d5612627-2ab0-45ee-afd7-76598a53b09a.png`.
- Browser-rendered implementation: `E:\Projects\AI-Product-Team\design-output-download-action-full.png`.
- Browser viewport: 1280 × 720. The source is a focused 568 × 992 drawer crop, so comparison is scoped to the Designer delivery report and Changed files region.
- State: dark theme, AI-Product-Team selected, DSN-004 completed, task detail drawer open. Native file actions are intentionally disabled in the web preview and enabled in the ProductCrew desktop runtime.

## Full-view and focused comparison evidence

- The Designer delivery report remains in its existing position and retains the source typography, spacing, hierarchy, and dark-theme tokens.
- Each changed file is now a compact bordered row with a file icon, repository-relative path, and paired `download` / `open_in_new` actions on the right.
- Long paths wrap inside the available width without overlapping the action or pushing the drawer wider.
- The focused Changed files region was compared together with the supplied source image; no actionable P0, P1, or P2 visual drift remains.

## Interaction and diagnostics

- Four semantic Download buttons and four semantic Open buttons are rendered for DSN-004's four output files.
- The web preview exposes the actions as disabled with explanatory tooltips because local file operations are desktop-only.
- The Tauri commands validate the imported project ID, canonicalize the target, reject traversal/out-of-project paths, and either open the file with the operating-system default application or copy it into Downloads.
- Existing Downloads files are preserved; duplicate names receive a numbered suffix such as `handoff (1).md`.
- Browser console errors: none.
- Angular tests: 34 passed across 8 files.
- Angular production build: passed without warnings.
- Rust tests: 5 passed, including valid artifact resolution, path-traversal rejection, and collision-safe Downloads naming.

final result: passed

---

# Design QA — Live Team Lead Planning Log

## Comparison target

- Source visual truth: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-1230c4f9-01db-4925-8ca5-01b61064ebc4.png`.
- Browser-rendered full view: `E:\Projects\AI-Product-Team\design-assets\planning-log-full.png`.
- Focused implementation capture: `E:\Projects\AI-Product-Team\design-assets\planning-log-implementation.png`.
- Source pixels: 1638 × 149. Focused implementation pixels: 1028 × 210.
- CSS viewport: 1280 × 720 at device pixel ratio 1.25; browser screenshot output is normalized to CSS pixels.
- State: dark theme. The source shows an analyzing plan with its log collapsed; the available persisted test board shows an awaiting-approval plan with the new log expanded. State-color and copy differences are intentional; comparison is scoped to the planning banner's hierarchy, action placement, and expanded log treatment.

## Full-view and focused comparison evidence

- The original compact three-part banner hierarchy is preserved: state/identity on the left, request content in the center, and actions on the right.
- The new `Live planning log` action sits in the existing action area rather than introducing a new route or modal.
- Opening the log expands a bordered panel inside the planning banner and preserves the board summary and Kanban board below it.
- The focused comparison was required because the full Work board capture makes the banner copy and log hierarchy too small to assess confidently.

## Required fidelity surfaces

- Fonts and typography: existing ProductCrew font stack, state-label casing, title weight, muted supporting copy, and monospace timestamps/details are retained.
- Spacing and layout rhythm: the collapsed banner keeps its existing density; the expanded panel aligns after the icon gutter and uses the same 8–9 px radii and compact spacing as task session logs.
- Colors and visual tokens: state-specific amber/brand colors and all panel surfaces use existing tokens. No new palette or gradient was introduced.
- Image quality and asset fidelity: no raster imagery is required. The feature reuses the app's Material Symbols icon system; no placeholder, emoji, handcrafted SVG, or CSS illustration was added.
- Copy and content: `Live planning log`, `Hide log`, provider, revision, live/completed/failed status, and normalized runtime events are concise and planning-specific.

## Findings and comparison history

- No actionable P0, P1, or P2 visual mismatch was found in the first comparison.
- The title truncation visible in the current awaiting-approval fixture is pre-existing banner behavior and remains appropriate for the constrained desktop action row.
- The currently installed backend predates the new planning SSE route, so the browser preview can verify the connecting state and expand/collapse interaction but cannot display live events until the rebuilt backend is run.

## Interaction and diagnostics

- `Live planning log` expands inline and changes to `Hide log` with `aria-expanded=true`.
- Switching project or planning item disposes the active EventSource subscription.
- Backend starts a session before waiting for the sequential agent slot, then streams normalized Codex or Claude Code events and a terminal completion/failure event.
- Browser console warnings/errors: none.
- Angular tests: 28 passed across 7 files.
- Angular production build: passed.
- Go tests: initial run hit the known Windows `TempDir RemoveAll` cleanup race; isolated rerun passed.

final result: passed

---

# Design QA — Task Drawer Horizontal Overflow

## Comparison target

- Reported broken state: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-8b5a892b-6c6b-4402-9d13-476338888ada.png`.
- Browser-rendered implementation: live Codex in-app browser at `http://127.0.0.1:8000/work-items?projectId=project-1786385782334104000&taskId=task-a3a4dd2039008d6c-04`.
- State: dark theme, vltk-auto selected, QA-010 blocked/waiting task, long failed QA delivery report visible.

## Findings and comparison history

1. P1: The delivery report's implicit CSS Grid column expanded to 580.96 px inside a 411 px list. That increased `.delivery-report` and `.drawer-scroll` to 622 px and exposed a horizontal scrollbar.
   - Fix: define the list track as `minmax(0, 1fr)` and allow long list items to wrap at identifiers, commands, and paths.
2. P2: The drawer scroll container did not explicitly constrain horizontal overflow, so a single oversized descendant could move all drawer content off its left edge.
   - Fix: bound the drawer and sections with `min-width: 0`, keep the drawer at `max-width: 100%`, and expose vertical scrolling only.

## Interaction and diagnostics

- Before the fix at the reported task: `.drawer-scroll` was 455/622 px client/scroll width, `.delivery-report` was 455/622 px, and the offending list was 411/600 px.
- After the fix at 1440 x 900: drawer 470/470 px, scroll region 455/455 px, delivery report 455/455 px, `scrollLeft = 0`, and no inspected descendant overflowed.
- The same task also remained contained in the narrow 418 x 470 Codex browser panel: drawer 403/403 px, scroll region 388/388 px, and no inspected descendant overflowed.
- Long title, description, blocked reason, QA summary, verification notes, remaining risks, and findings remain readable; no content is clipped to achieve containment.
- Browser console warnings/errors: none.
- Angular tests: 12 files, 99 tests passed.
- Embedded production build: passed; only the existing non-fatal initial bundle budget warning remains.

No actionable P0, P1, or P2 findings remain.

final result: passed
# Design QA — Project Atlas Initial Stylesheet Regression

## Comparison target

- Approved visual baseline: `E:\Projects\AI-Product-Team\design-qa-project-atlas.png` (1582 × 900, dark theme, completed partial-study state).
- Reported broken state: `C:\Users\ANH~1.TAN\AppData\Local\Temp\codex-clipboard-de432eea-a434-4823-a9c7-79e8ca854d36.png` (1693 × 1005, dark theme, empty/studying state).
- Browser-rendered verification: `E:\Projects\AI-Product-Team\frontend\test-results\project-atlas-layout-Proje-ab8cc-d-render-its-desktop-layout\project-atlas-global-style.png` (1440 × 900 CSS px, device scale factor 1, dark theme, empty state).
- Combined failure/fix evidence: `E:\Projects\AI-Product-Team\design-qa-project-atlas-style-regression-comparison.png` (2956 × 900).

## Findings and comparison history

1. P0 before fix: Project Atlas rendered as raw document flow because its rules existed only in the lazy component bundle. The header, project summary, empty-state steps, controls, spacing, and containment all lost their approved layout.
2. Fix: import `project-atlas-page.css` from the initial application stylesheet and remove the duplicate component `styleUrl`, keeping a single production CSS source without duplicating the lazy payload.
3. After fix: the compiled initial `styles-XYU2X7RI.css` contains `.atlas-page`; the browser verification restores the approved header hierarchy, flex project strip, bordered surface, centered grid empty state, typography, spacing, radii, dark tokens, and button treatment.

## Fidelity surfaces

- Fonts and typography: ProductCrew's existing global family, weights, hierarchy, and uppercase eyebrow treatment are restored.
- Spacing and layout rhythm: 22 px page top padding, flex header/project strip, 10 px project-strip radius, and centered grid empty state are verified through browser-computed styles.
- Colors and visual tokens: the dark ProductCrew surface, line, text, muted, brand, and brand-soft tokens are applied again.
- Image quality and assets: no raster imagery is part of this state; existing Material Symbols remain the application icon source.
- Copy and content: no product copy changed; the verification fixture preserves the visible Atlas empty-state content relevant to the regression.

## Interaction and diagnostics

- `npm run test:ui`: 3/3 passed, including the compiled initial-stylesheet Atlas test and both task-drawer containment tests.
- The Atlas test builds the app before opening the real-browser fixture, preventing a source-only assertion from passing when production CSS is absent.
- Live installed-app verification remains pending the next application rebuild/restart; the active production process was not interrupted during QA.
- No actionable P0, P1, or P2 visual differences remain within the initial-stylesheet regression scope.

final result: passed

---
