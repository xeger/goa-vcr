# ADR 0002: Dynamic Active Scenario Stacks

- **Date:** 2026-04-23
- **Status:** Accepted
- **Supersedes:** none
- **Related:** `docs/adr/0001-scenarios-as-service.md`

## Context

ADR 0001 made scenarios composable service layers, but playback still chose one fixed startup selection from CLI configuration. Changing active behavior required restarting the process with different flags.

That startup-only model was too rigid for integration tests and local debugging where callers need to:

1. Inspect what scenario stack is currently active.
2. Replace it at runtime.
3. Revert to startup defaults quickly.

At the same time, the public wire format should treat scenarios as objects (not bare strings) so per-scenario parameters can be added later without another API break.

## Decision

Playback servers now expose a control API:

- `GET /__vcr__/scenarios` returns registered names plus default and active stacks.
- `PUT /__vcr__/scenarios/active` replaces the active stack from a JSON array of objects, e.g. `[{"name":"Happy"},{"name":"Sad"}]`.
- `DELETE /__vcr__/scenarios/active` restores the startup default stack.

Scenario order uses existing stack semantics from ADR 0001:

- `["Happy","Sad"]` means `Stack(bg, Happy, Sad)` (Happy outermost, Sad inside).
- `PUT []` is valid and means background-only playback.

The generated CLI now accepts repeatable `-scenario` flags to define ordered defaults at startup. When omitted, `CLIConfig.DefaultScenarios` is used.

Registry entries are now layer constructors:

```go
map[string]func(Service) Service
```

instead of full-store builders. The generated `cmdPlay` path composes layers over `NewBackground(store)` to build each active stack.

The mutable control plane lives in shared runtime code (`runtime/active_scenarios.go`) so generated code stays focused on service-specific composition.

## Consequences

### Positive

- Runtime scenario switching no longer requires process restart.
- The control API is uniform across generated services.
- Builder failures on `PUT` do not break in-flight behavior; swap is commit-after-build.
- Object-based scenario specs keep room for future scenario parameters.

### Negative / breaking

- `CLIConfig.ScenarioRegistry` value type changed to `func(Service) Service`.
- CLI defaults move from singular `DefaultScenario` to ordered `DefaultScenarios` (with compatibility fallback for single default).

### Neutral

- Admin control endpoints share the playback server process and port.
- Playback access logging remains on service routes; admin routes are handled separately.
