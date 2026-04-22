# ADR 0001: Scenarios are Goa Services

- **Date:** 2026-04-22
- **Status:** Accepted — shipped in v2.0.0
- **Supersedes:** the v1 "Scenario as handler-queue wrapper" design

## Context

`goa-vcr` generates per-service glue that serves recorded HTTP stubs and lets tests override individual method responses via a `Scenario` object. In v1, the generated `Scenario` was a thin wrapper around a `goa.design/clue/mock.Mock` — a bag of name-keyed handler queues. Dispatch was **override-or-fallback**: if a scenario handler was registered for a method, it ran; otherwise the call fell through to a stub-backed background client.

Three problems accumulated under that model:

1. **No middleware.** A handler could replace a response entirely but could not "call the recorded background and then tweak the result." Post-processing patterns — field redaction, mutation assertions, latency injection — weren't supported.

2. **Loopback as an out-of-band workaround.** To reach the background from inside a scenario, the handler had to use `NewLoopbackClient`, which sent a real HTTP request back at the playback server with an `X-Vcr-Loopback: 1` header. A middleware on the playback handler detected the header, stamped the context, and the per-method `makeEndpoint*` dispatcher used `IsLoopback(ctx)` to skip scenario lookup. This was an HTTP round-trip to substitute for a function call — expensive, opaque, and split across four files.

3. **Single-layer scenarios.** Two orthogonal concerns ("override `GetThing`" and "assert `ListThings` was called at least once") had to share one `Scenario` object. There was no way to express layer boundaries that were natural in test setup.

Additionally, the generated plugin had grown a five-way `makeEndpoint*` switch per endpoint shape (streaming / SkipResponseBodyEncodeDecode / viewed-result / plain-result / no-result), each branching on loopback-state, scenario-presence, and background fallback. The control flow was correct but dense, and each new endpoint shape multiplied template complexity.

## Decision

**The generated per-service `Scenario` implements the Goa `Service` interface directly.** A `Scenario` holds (a) a `clue/mock`-backed handler queue and (b) a reference to the `Service` "below" it, called `next`. Each method on `*Scenario` checks its queue; if a handler is registered, it runs (with `next` passed as the final argument for unary methods); otherwise the call delegates to `s.next.Method(...)`. The stub-backed background is itself a `Service` implementation, sitting at the bottom of the stack.

Composition is ordinary function composition. A `Stack(bg, outer, middle, inner)` helper applies layers outer-first, producing `outer(middle(inner(bg)))`. Layers are anonymous `func(next Service) Service` — no named `ScenarioFactory` typedef is needed.

Unary handler signature:

```go
type ServiceGetThingFunc func(ctx, payload, next Service) (result, error)
```

Streaming handler signature stays terminal in v2.0:

```go
type ServiceWatchFunc func(ctx, payload, stream) error
```

Streaming wrapping (intercepting `Send`/`Recv` to interpose on frames flowing through `next`) is deferred to a later release — it requires a stream-interceptor API that hasn't been designed yet, and the current "first layer with a handler wins" cascade is sufficient for existing use cases.

The loopback apparatus (`LoopbackHeader`, `LoopbackMiddleware`, `IsLoopback`, `NewLoopbackClient`, `loopbackDoer`, `BuildScenario`) is deleted entirely. Its one purpose — preventing scenario recursion when a handler re-entered the service via HTTP — is made moot by `next` being a typed in-process reference.

## Consequences

### Positive

- **Middleware becomes native.** "Call background, tweak result" is `next.GetThing(ctx, p)` followed by a mutation and return — a normal Go expression, not an HTTP round-trip.
- **Cross-layer calls are typed and in-process.** No HTTP hop, no header-detection middleware, no loopback-specific error modes to debug.
- **Composition is cheap.** Expressing two concerns as two layers is a line of code (`Stack(bg, concernA, concernB)`) rather than a single `Scenario` with all handlers merged.
- **Plugin internals collapse.** `makeEndpoint*`'s five-way switch disappears — `NewPlaybackHandler` now just wraps a `Service` via Goa's own `NewEndpoints(svc)`. Per-endpoint code becomes a trivial forwarder.
- **Vocabulary aligns with Goa.** A `Scenario` *is* a `Service`, not a sibling abstraction. Users reason about one interface, not two.

### Negative / breaking

- **v1 API is removed without shims.** `ScenarioFactory`, `BuildScenario`, `PlaybackOptions`, `NewLoopbackClient`, and `NewBackgroundClient` (returning `*Client`) are gone. Consumers migrating from v1 update their scenario factories, handler signatures, and `NewPlaybackHandler` call sites.
- **Every unary handler signature changes.** Handlers gain a `next Service` parameter, even when they don't use it.
- **`ScenarioRegistry`'s value type changes.** Was `map[string]ScenarioFactory`, now `map[string]func(*vcrruntime.VCR) Service` — each registered entry is a complete playback definition that constructs its own background and stack.
- **Viewed-result handlers return plain results.** The `Scenario`'s method wraps to the viewed type; users constructing fresh data return the plain type, but handlers that call `next.GetThingViewed(...)` get the viewed type back — unwrapping to plain from inside a handler is awkward. This is documented in the spec's "Open items deferred to implementation" section.

### Neutral

- **Streaming composition is limited.** A layer either handles a streaming method or doesn't; it cannot wrap. Sufficient today; v3 will revisit.
- **Background has no recorded-stream fallback.** Streaming methods on `backgroundService` return an explicit error (`"vcr: no scenario handler for X and no recorded-stream background is available"`) rather than panicking, timing out, or returning 404. Behavior is intentional; tests relying on streams must provide handlers.

## Alternatives considered

1. **Keep v1 shape, expose a `next` callback.** Add a per-handler `next` argument but leave `Scenario` as a handler-queue bag; composition still happens through loopback HTTP. Rejected: doesn't solve the out-of-band workaround, doesn't enable stacking.

2. **Two-phase migration.** v2.0 changes factory input (`Service` instead of `*httpclient.Client`) without stacking; v2.1 adds `next` to handlers and the `Stack` helper. Rejected: the two changes converge structurally when `Scenario` *is* `Service` — splitting them produces two breaking releases for users with no intermediate payoff.

3. **Named `ScenarioFactory` typedef for layers.** Export `type ScenarioFactory func(next Service) Service` for stack middleware. Rejected: a typedef adds jargon without behavior; anonymous `func(Service) Service` at call sites reads clearly.

4. **Stream wrapping in v2.0.** Include a wrappable stream-handler signature and a `Send`/`Recv` interceptor utility. Rejected: designing stream wrapping correctly requires its own ADR — the shape of "pass a wrapped stream down to `next`" has multiple plausible variants, and no current consumer is blocked by its absence. Deferred to v3.0 with the v2.0 structure explicitly left open for that path.

## References

Shipped in commits between `9be64a5` (main) and `0a1fb05` on branch `v2-stacking`. The design process produced a detailed spec and a step-by-step implementation plan that were retired into git history once this ADR was written; recover them with `git log --diff-filter=D -- docs/superpowers/` if you need the full paper trail.
