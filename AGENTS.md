# AI-Product-Team Agent Instructions

This repository builds ProductCrew, a local desktop app with an Angular frontend,
Go backend service, and Tauri shell. Follow the user's latest request first, then
use these project-local instructions to keep implementation and verification
consistent.

## Regression Guard

After any implementation that changes production behavior, run the Regression
Guard checklist in `.codex/agents/regression-guard.md` before reporting the task
as complete.

Use the Regression Guard report to decide whether the change impacted existing
ProductCrew behavior:

- If the report is `passed`, include its focused evidence in the final response.
- If the report is `failed`, fix only the impacted behavior identified by the
  report, then run the relevant Regression Guard checks again.
- If the report is `blocked`, state the blocker clearly and do not claim full
  regression safety.

The Regression Guard is not a product QA replacement. Product QA verifies the
assigned feature or bug scope. Regression Guard protects existing behavior that
could plausibly be affected by the changed files.

## Regression Case Maintainer

After a new feature or behavior change has passed implementation verification
and Regression Guard, run the Regression Case Maintainer instructions in
`.codex/agents/regression-case-maintainer.md`.

Use it to update `.codex/regression-cases.md` so future Regression Guard runs
know which ProductCrew behavior must stay protected.

- When a feature is added or changed, add or update the smallest durable
  regression cases that protect the accepted behavior.
- When a feature is removed, remove or retire regression cases that only covered
  the removed behavior.
- Do not delete regression cases for still-supported behavior just because the
  current task did not touch them.
- Do not change production code while acting as Regression Case Maintainer.
