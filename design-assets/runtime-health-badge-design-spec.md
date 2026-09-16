# Runtime Health Badge — Design Spec

Task: DSN-001. Component: sidebar footer badge, `.local-status` in
`frontend/src/app/shared/app-shell/app-shell.html:70-73` and
`frontend/src/styles.css:167-170`.

## Current state (confirmed in repo)

- `RuntimeHealthService` (`frontend/src/app/core/runtime-health.service.ts`) already exists,
  already polls `/api/health` every 30s, and already exposes a `status` signal of
  `{ state: 'connected' | 'unavailable' | 'unreachable', aiProvider: string | null }`. Its
  spec (`runtime-health.service.spec.ts`) is green.
- **It is not wired into the shell yet.** `app-shell.ts:24-28` only calls
  `theme.load()`, `notifications.initialize()`, `updates.initialize()` — no
  `RuntimeHealthService` injection. `app-shell.html:70-73` still hardcodes:
  ```html
  <div class="local-status">
    <span class="status-dot"></span>
    <div><strong>Local runtime ready</strong><small>AI CLI connected</small></div>
  </div>
  ```
- This spec defines the copy and visual treatment Developer needs to wire the three
  states from the signal into this markup. No flow/navigation change — the badge is
  passive, non-interactive except the link in the unreachable state.

## Provider label mapping (reuse, do not reimplement)

Reuse the exact mapping in `settings-page.ts:240-244` (`aiProviderLabel`):
`'codex'` → `"Codex"`, `'claude-code'` → `"Claude Code"`, anything else → `"None"`.

## The three states

| State | Title (`<strong>`) | Subtitle (`<small>`) | Dot | Halo |
|---|---|---|---|---|
| **Connected** — `state === 'connected'` | `Local runtime ready` (unchanged) | `{Provider} connected` e.g. `Codex connected` | `#2fa36b` (unchanged) | `#e7f6ee` (unchanged) |
| **Unavailable** — `state === 'unavailable'` | `Runtime unavailable` | `Selected provider: {Provider}` e.g. `Selected provider: Claude Code` | `var(--amber)` | `color-mix(in srgb, var(--amber) 12%, transparent)` |
| **Unreachable** — `state === 'unreachable'` | `Local service unreachable` | `Check <a routerLink="/settings">Settings</a>` | `var(--red)` | `color-mix(in srgb, var(--red) 12%, transparent)` |

Edge case: if `aiProvider` is an unrecognized/empty string while `state` is `connected`
(shouldn't happen per the backend contract, but the label mapping resolves to `"None"`),
fall back to the subtitle `AI CLI connected` rather than rendering the nonsensical
`None connected`.

### Why these colors

- **Connected stays exactly as-is.** `#2fa36b`/`#e7f6ee` is not one of the
  `--brand`/`--amber`/`--red` families, but it's the app's established "healthy" dot
  color already reused in two other places (`.tool-status-dot` default in
  `styles.css:595` and `.scan-runtime .status-dot` in `styles.css:650`). It is not a new
  color, so per the brief's "reuse existing tokens, don't add new colors" instruction it
  stays untouched — the connected state does not need `--brand` teal, which is reserved
  for interactive/selection accents (nav active state, unread dot), not health status.
- **Unavailable → `--amber`/amber halo.** Matches the brief's explicit direction and the
  same amber-for-"needs attention but not broken" meaning already used at
  `.tool-status-dot.update_available` (`styles.css:596`) and `.ui-badge`
  (`styles.css:199`).
- **Unreachable → `--red`/red halo.** Matches `.tool-status-dot.not_installed` /
  `.check_failed` (`styles.css:597`) and `.notice.error` (`styles.css:182`) — the app's
  existing "this is actually broken" signal.
- **Halo mechanics: copy `.tool-status-dot`'s `color-mix(in srgb, <color> 12%,
  transparent)` pattern, not `.local-status`'s current opaque `#e7f6ee`.** The current
  green halo is a hardcoded light-mode-only color with no dark-theme override — safe to
  leave for the unchanged connected state, but do **not** carry that opaque-hex approach
  forward into the new amber/red variants, or they'll look wrong in dark mode (verify:
  `styles.css` root has no `.status-dot` override under `html[data-theme="dark"]`). The
  `color-mix(... , transparent)` form used by `.tool-status-dot` is theme-safe since it's
  translucent over whatever surface sits behind it — use that mechanism for the two new
  variants:
  ```css
  .local-status .status-dot.unavailable { background: var(--amber); box-shadow: 0 0 0 3px color-mix(in srgb, var(--amber) 12%, transparent); }
  .local-status .status-dot.unreachable { background: var(--red); box-shadow: 0 0 0 3px color-mix(in srgb, var(--red) 12%, transparent); }
  ```

## Settings link treatment: inline text, not icon button

Use an inline text link inside the subtitle `<small>`, styled like the existing
`.notice a` link (`styles.css:180`, `font-weight: 600`) — inherit the row's red text
color rather than introducing a separate link color.

Reasoning: icon-button controls in this sidebar (`.theme-toggle`, `.notification-toggle`)
are full-width, standalone rows with their own icon + label + hit target
(`app-shell.html:19-24`, `57-68`). The unreachable state's message lives inside the
existing two-line `<strong>`/`<small>` stack, not a standalone row — an icon button
would need its own row and layout change, which isn't justified for a link that only
needs to be scannable text. Inline text matches how the rest of the app treats
recoverable-error affordances (`.notice.error a`).

```html
<small>Check <a routerLink="/settings">Settings</a></small>
```

## Behavior notes (for Developer, not a design change)

- Wire `RuntimeHealthService` into `AppShell` the same way `AppUpdateService` is —
  `constructor()` calls `this.runtimeHealth.initialize()` alongside the existing
  `theme.load()` / `notifications.initialize()` / `updates.initialize()` calls.
- Bind dot class and title/subtitle text off `runtimeHealth.status()`. While
  `status()` is `null` (before the first response lands), keep today's static markup
  or render nothing rather than a fourth "loading" visual state — the brief does not
  ask for one and the first `/api/health` call resolves near-instantly on shell init.

## Accessibility

- The dot stays `aria-hidden="true"` (unchanged) — state must be conveyed by the text,
  not color alone, which the title/subtitle copy above already does for all three states.
- Wrap the `.local-status` content in `role="status" aria-live="polite"` so a state
  transition (e.g. connected → unreachable) is announced to screen reader users without
  needing to focus the sidebar — same pattern already used for the update toast
  (`app-shell.html:92`, `role="status" aria-live="polite"`).
- The Settings link is a real `routerLink`, keyboard-focusable by default; no new
  `:focus-visible` rule needed beyond what a plain anchor already inherits — confirm it
  gets a visible outline consistent with other in-sidebar links (spot-check against
  `.theme-toggle:focus-visible`, `styles.css:106`, as the nearest sidebar precedent).

## Layout / responsive confirmation

- `.local-status { display: none; }` at the `max-width: 760px` breakpoint
  (`styles.css:848`) is unchanged and correct — the badge (all 3 states) is fully hidden
  on narrow layouts already, so none of the new copy needs to fit that breakpoint.
- Above 760px the sidebar is a fixed `196px` column (`.app-shell`,
  `styles.css:79`) with `12px` side padding on `.sidebar` (`styles.css:88`), leaving
  ~155px for the text column next to the 8px dot. Neither `.local-status strong` nor
  `small` currently declares `white-space: nowrap`/`text-overflow: ellipsis`
  (`styles.css:169-170`), so long text already wraps by design (same as it would for a
  long notification title elsewhere in the sidebar) — no truncation risk.
  `Selected provider: Claude Code` (the longest new subtitle, ~30 characters) will wrap
  to two lines at 11px in that width; this is acceptable and consistent with existing
  wrap behavior, not a regression. If two-line wrap is judged too tall in review, the
  fallback copy `Provider: Claude Code` is shorter and fits on one line more often —
  Developer/QA can pick either; both satisfy the acceptance criteria as written.

## Copy summary (for quick reference)

- Connected: **Local runtime ready** / *Codex connected* (or *Claude Code connected*)
- Unavailable: **Runtime unavailable** / *Selected provider: Codex* (or *Claude Code*)
- Unreachable: **Local service unreachable** / *Check Settings* (link)
