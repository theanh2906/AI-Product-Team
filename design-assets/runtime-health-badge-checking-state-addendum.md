# Runtime Health Badge — Checking State Addendum

Task: DSN-002. Supersedes the "Behavior notes" guidance in the approved spec at
`design-assets/runtime-health-badge-design-spec.md` (task DSN-001), which told Developer
to leave `status() === null` rendering "today's static markup." That static markup is the
literal hardcoded string `Local runtime ready` / `AI CLI connected` this feature exists to
stop showing — so the null case needs its own visual state, not a pass-through.

## Trigger

`runtimeHealth.status() === null` — the window between `AppShell` construction and the
first `/api/health` response landing. This is the existing `RuntimeHealthStatus | null`
signal at `runtime-health.service.ts:25`.

- **No service change.** `status` already correctly initializes to `null`.
- **No new `RuntimeHealthState` enum member.** `'connected' | 'unavailable' | 'unreachable'`
  (`runtime-health.service.ts:5`) is unchanged. "Checking" is not a value the signal's inner
  `state` field ever holds — it is the shell's presentation of the outer signal itself being
  `null`, derived the same way `[class.unavailable]` / `[class.unreachable]` are already
  derived in `app-shell.html:72-73`.

## The 4th state

| State | Title (`<strong>`) | Subtitle (`<small>`) | Dot | Halo |
|---|---|---|---|---|
| **Checking** — `status() === null` | `Checking runtime…` | *(none — no `<small>` rendered)* | `var(--line-strong)` | `color-mix(in srgb, var(--muted) 12%, transparent)` |

- **Title copy: `Checking runtime…`.** Matches the app's existing ellipsis convention for
  in-flight states (`Loading notifications…`, `app-shell.html:29`).
- **No subtitle, at all, in this state.** Do not render an empty `<small>`, a "…" placeholder,
  or any provider-derived text. Any subtitle risks re-deriving a specific claim
  (`providerLabel(undefined)` → `'None'` → `'AI CLI connected'`) before the real state is
  known, which is exactly the defect being fixed. The connected/unavailable/unreachable
  states keep their two-line `<strong>`/`<small>` layout; checking is intentionally
  one line.

## Color sourcing — confirmed against `frontend/src/styles.css:1-55`

Both tokens below are declared in that exact range, in both `:root` (light) and
`html[data-theme="dark"]`, so the state is theme-safe with no overrides needed:

- **Dot fill: `var(--line-strong)`** (`styles.css:13` light `#cbd5d1`, `styles.css:41` dark
  `#3a4a45`). This token is otherwise used for borders (a neutral structural color), which is
  the correct read for "no status claim yet" — it is visually distinct from the health-status
  greens/ambers/reds and doesn't borrow a color that already means something else (e.g.
  `--muted`, which is text-colored and used for the unavailable subtitle elsewhere).
- **Halo: `color-mix(in srgb, var(--muted) 12%, transparent)`** (`--muted` at `styles.css:11`
  light `#697571`, `styles.css:39` dark `#9baaa5`). Same `color-mix(..., 12%, transparent)`
  mechanic as the existing unavailable/unreachable halos (`styles.css:169-170`) and
  `.tool-status-dot` (`styles.css:600-601`), just swapping in the neutral `--muted` token in
  place of `--amber`/`--red`.
- **No new color values or custom properties are introduced.** Every value above already
  exists in the file before this change.

```css
.local-status.checking .status-dot { background: var(--line-strong); box-shadow: 0 0 0 3px color-mix(in srgb, var(--muted) 12%, transparent); }
```

## No animation

This state is static — no pulse, no shimmer, no skeleton. Precedent: `.session-state`'s
default (no `data-status` attribute) treatment is static; only its `running` variant pulses
(`styles.css:925-928`, `soft-pulse` keyframe applied only under `[data-status="running"]`).
"Checking" means *unknown yet*, not *actively in progress with visible work happening* —
the same distinction the app already draws for session state. It is also expected to be
transient (the badge should self-correct within one `/api/health` round-trip), so a
persistent-looking animated treatment would overstate how long this state is meant to be
visible. Static keeps the badge calm on first paint instead of drawing extra attention to a
sub-second transitional state.

## Accessibility

- `.local-status` keeps its existing `role="status" aria-live="polite"`
  (`app-shell.html:74-75`) unchanged — the transition from "Checking runtime…" to the real
  state announces to screen reader users the same way any other state change does.
- The dot stays `aria-hidden="true"` (`app-shell.html:77`), unchanged — state is conveyed by
  the title text, not color, consistent with the other three states.

## Structural note (for Developer, not a new decision — restates DSN-001's intent correctly)

`app-shell.html:78-88`'s `@switch` currently has no `@case ('connected')`, so `@default`
silently catches both the real connected state and the not-yet-loaded `null`/`undefined`
state — that ambiguity is the root cause. Add `@case ('connected')` explicitly (existing
"Local runtime ready" / `connectedSubtitle()` markup, unchanged) so `@default` only ever
matches "no status yet," and renders the checking markup from this addendum. Add a
`[class.checking]="runtimeHealth.status() === null"` binding alongside the existing
`[class.unavailable]` / `[class.unreachable]` bindings (`app-shell.html:72-73`).

## Copy summary (for quick reference)

- Checking: **Checking runtime…** *(no subtitle)*
- Connected: **Local runtime ready** / *Codex connected* (or *Claude Code connected*) — unchanged, DSN-001
- Unavailable: **Runtime unavailable** / *Selected provider: Codex* (or *Claude Code*) — unchanged, DSN-001
- Unreachable: **Local service unreachable** / *Check Settings* (link) — unchanged, DSN-001
