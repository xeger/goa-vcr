# Stackable Scenarios (v2.0)

- **Date:** 2026-04-21
- **Status:** Design approved; awaiting implementation plan
- **Branch:** `v2`
- **Release target:** `v2.0.0` (clean break; no compatibility shims)

## Summary

In v2.0, goa-vcr's generated per-service `Scenario` becomes a Goa `Service` implementation. A scenario is constructed with a reference to the `Service` "below" it, carries its own per-method handler queues, and delegates to the layer below when it has no handler set for a given call. This makes scenarios **stackable**: each layer is a `Service` that wraps the `Service` beneath it, with a stub-backed background `Service` at the bottom. Handlers for unary methods receive the layer below as a `next` parameter so they can call through and post-process results. Streaming handlers remain terminal in v2.0; wrapping is deferred to v3.0.

The loopback apparatus (`LoopbackMiddleware`, `IsLoopback`, `NewLoopbackClient`, the `X-Vcr-Loopback` header) is removed. Its sole remaining purpose — letting scenario code re-enter the service for cross-method calls — is obviated by the typed `next Service` reference.

## Motivation

Today's design has three rough edges:

1. **Scenarios are override-or-fallback, never middleware.** A scenario handler for `GetThing` either replaces the background stub completely or not at all. There is no supported way to "call the recorded background, then tweak the result" without going out of band.

2. **The out-of-band workaround (loopback) is awkward.** A scenario that needs background data today must use `NewLoopbackClient`, make an HTTP round-trip to itself, have that hop be detected by header-matching middleware, and be routed away from scenario dispatch. This is expensive and obscures the intent of the test.

3. **Single-layer scenarios don't compose.** If two concerns need to be expressed simultaneously — "override `GetThing`" and "assert `ListThings` is called at least once" — they must live in the same `Scenario` object. Layer boundaries that exist naturally in the test setup cannot be reflected in the runtime.

Making scenarios implement `Service` and chain explicitly resolves all three: middleware becomes the native pattern, `next` is a typed in-process call, and stacking is ordinary function composition.

## Non-goals

- **Stream wrapping.** Streaming handlers stay terminal in v2.0 — dispatch cascades across layers at the "which layer claimed this method" level, but once a handler runs it cannot invoke a layer below. v3.0 will introduce a wrappable stream handler signature alongside a stream-interceptor utility.
- **Strict-mode queue semantics.** A drained `Add`-queue at layer N delegates to layer N+1 (same as today's single-layer fallback to background). An opt-in strict mode that errors when a layer's queue drains is possible future work but not in v2.0.
- **Backwards compatibility shims.** v2.0 is a clean break. No aliases, no deprecation warnings, no dual API.
- **Stacked scenarios from the CLI.** The `--scenario` flag selects one named entry. Each entry is a complete playback definition that composes whatever stack it wants internally.

## Design

### Types and interfaces

Per service, the plugin generates:

```go
// Scenario implements serviceA.Service. Each method checks its handler
// queue; if non-empty, the handler is invoked (with next as its last arg
// for unary methods). If empty, the call delegates to s.next.
type Scenario struct {
    queue vcrruntime.Scenario  // clue/mock-backed name-keyed handler queue
    next  serviceA.Service     // the layer below
}

func NewScenario(next serviceA.Service) *Scenario
```

(The runtime `Scenario` type stays as-is in v2 and is used here as a private field rather than embedded, to avoid a `Next` method / `next` field name collision. Generated typed `Set*`/`Add*` methods wrap `s.queue.Set(name, handler)` / `s.queue.Add(name, handler)`.)

Per-method typed handler signatures (unary gains a `next` argument; streaming does not):

```go
// Unary.
type ServiceGetThingFunc func(
    ctx context.Context,
    p *serviceA.GetThingPayload,
    next serviceA.Service,
) (*serviceA.Thing, error)

// Streaming (terminal).
type ServiceWatchThingsFunc func(
    ctx context.Context,
    p *serviceA.WatchThingsPayload,
    stream serviceA.WatchThingsServerStream,
) error
```

Per-method typed setters on `*Scenario`:

```go
func (s *Scenario) SetGetThing(f ServiceGetThingFunc)
func (s *Scenario) AddGetThing(f ServiceGetThingFunc)
// ...one Set/Add pair per method.
```

`Set` installs a persistent handler (re-returned on each call); `Add` appends to a FIFO queue, dequeued one per call. This matches today's `clue/mock` semantics.

Background:

```go
// NewBackground returns a serviceA.Service backed by recorded HTTP stubs.
// Streaming methods return an explicit error (no recorded-stream fallback exists).
func NewBackground(store *vcrruntime.VCR) serviceA.Service
```

Stack helper:

```go
// Stack applies layers to bg, with the first layer outermost and the last
// layer innermost: Stack(bg, outer, middle, inner) produces
// outer(middle(inner(bg))). Matches the standard middleware convention
// where the first registered layer is first to run on a call.
// Layers are arbitrary middleware functions — no named ScenarioFactory
// type exists. Users write ordinary func(serviceA.Service) serviceA.Service.
func Stack(bg serviceA.Service, layers ...func(serviceA.Service) serviceA.Service) serviceA.Service
```

Playback handler:

```go
// NewPlaybackHandler wraps any serviceA.Service as an HTTP handler using
// the Goa-generated HTTP server for that service.
func NewPlaybackHandler(svc serviceA.Service) (http.Handler, error)
```

### Dispatch semantics

Every generated method on `*Scenario` follows one of two patterns.

**Unary:**

```go
func (s *Scenario) GetThing(ctx context.Context, p *serviceA.GetThingPayload) (*serviceA.Thing, error) {
    if h := s.queue.Next("GetThing"); h != nil {
        fn, ok := h.(ServiceGetThingFunc)
        if !ok {
            return nil, fmt.Errorf("vcr: scenario handler for GetThing has unexpected type %T", h)
        }
        return fn(ctx, p, s.next)
    }
    return s.next.GetThing(ctx, p)
}
```

**Streaming:**

```go
func (s *Scenario) WatchThings(ctx context.Context, p *serviceA.WatchThingsPayload, stream serviceA.WatchThingsServerStream) error {
    if h := s.queue.Next("WatchThings"); h != nil {
        fn, ok := h.(ServiceWatchThingsFunc)
        if !ok {
            return fmt.Errorf("vcr: scenario handler for WatchThings has unexpected type %T", h)
        }
        return fn(ctx, p, stream)
    }
    return s.next.WatchThings(ctx, p, stream)
}
```

Key properties:

- **`Set` vs. `Add`:** `Set` persists (queue never empties); `Add` drains one handler per call. When the queue is empty (never set, or all `Add`s consumed), dispatch delegates to `s.next`. This mirrors today's single-layer behavior of falling through to background.
- **Terminal fallback:** the bottom of the stack is always `NewBackground(store)`. For unary methods, the background serves a stub-decoded response. For streaming methods, the background returns an explicit error: `"vcr: no scenario handler for <method> and no recorded-stream background is available"`.
- **No ctx-based branching.** Dispatch is purely a function of the handler-queue state; there is no `IsLoopback(ctx)` check and no header-based bypass.

### Background implementation

`NewBackground(store)` returns a small generated wrapper type that embeds the Goa-generated HTTP client configured with `StubDoer`. The wrapper:

- Forwards unary methods to the HTTP client (which decodes the stubbed response into the Goa result type). This is the same plumbing as today's `NewBackgroundClient`, just renamed and typed as `serviceA.Service` at the interface boundary.
- Returns an explicit error from streaming methods.

For methods with viewed results (Goa's `ViewedResultInit` family), the wrapper's method handles view-name resolution inline, replacing today's logic scattered across `makeEndpoint*`.

Implementation detail: background may be implemented either as (a) a dedicated wrapper struct, or (b) a populated `*serviceA.Client` with streaming endpoint fields set to error-producing closures. The implementation plan will pick whichever produces less generated code; both satisfy the same interface contract.

### Streaming (v2.0)

- Streaming handler signature: `func(ctx, payload, stream) error`. No `next` parameter.
- Layer-level dispatch: if a layer has a handler registered for a streaming method, it runs (terminal). If not, dispatch delegates to the layer below.
- No layer has a handler → bottoms out at background → error.
- Intra-handler cascading is not supported in v2.0. A handler cannot invoke a layer below to wrap or observe streams. v3.0 will introduce this.

### CLI and scenario registry

The generated CLI (`render_cli.go`) continues to accept `--scenario <name>`. The registry's value type changes to a "top-of-stack builder":

```go
// The CLI config struct carries a ScenarioRegistry field with this type.
// Whether that type is given a named alias or used inline at the struct
// field is an implementation-plan decision with no semantic impact.
map[string]func(store *vcrruntime.VCR) serviceA.Service
```

Each registered entry is a complete playback definition — it constructs its own background and composes whatever stack it wants. The CLI flow:

1. Look up `cfg.ScenarioRegistry[*scenarioFlag]`.
2. Invoke the function with the loaded `*vcrruntime.VCR` store.
3. Pass the returned `Service` to `NewPlaybackHandler(svc)`.

`PlaybackOptions.ScenarioName` is removed (it was plumbed through `makeEndpoint*` but had no remaining consumer).

`DefaultScenario` is preserved as the default for the `--scenario` flag.

### Loopback removal

v2.0 removes all of the following:

- `runtime/loopback.go` (the entire file).
- `runtime.LoopbackHeader`, `runtime.LoopbackMiddleware`, `runtime.IsLoopback`.
- Generated `NewLoopbackClient`, `loopbackDoer`, `BuildScenario`.
- The `LoopbackMiddleware` wrap inside `NewPlaybackHandler`.
- The `IsLoopback(ctx)` branch in every `makeEndpoint*` template.

Justification: the header's sole purpose was preventing scenario-driven HTTP loopback calls from recursing into scenario dispatch. Under the Scenario-as-Service model, scenarios reach the layer below via a typed in-process reference (`s.next`), not an HTTP round-trip, so there is nothing to detect and nothing to bypass. `NewLoopbackClient` stamped a header that nothing reads; it is dead code.

Users who want to exercise the HTTP encoding path of the playback stack can use a normal Goa-generated client pointed at the playback server. Users who want to reach the background specifically over HTTP can mount a second handler wrapping `NewBackground(store)` directly.

## Example end-to-end usage

```go
import (
    toy "example.com/gen/toy"
    toyvcr "example.com/gen/http/toy/vcr"
    "github.com/xeger/goa-vcr/runtime"
)

func happyOverrides(next toy.Service) toy.Service {
    s := toyvcr.NewScenario(next)
    s.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
        t, err := next.GetThing(ctx, p)
        if err != nil {
            return nil, err
        }
        t.Name = "patched-by-scenario"
        return t, nil
    })
    return s
}

func happyAssertions(next toy.Service) toy.Service {
    s := toyvcr.NewScenario(next)
    var listCalls int
    s.SetListThings(func(ctx context.Context, p *toy.ListThingsPayload, next toy.Service) (*toy.ThingList, error) {
        listCalls++
        return next.ListThings(ctx, p)
    })
    // ... inspect listCalls from test scope.
    return s
}

// Register the scenario as a complete playback definition.
cfg.ScenarioRegistry = map[string]func(*runtime.VCR) toy.Service{
    "happy": func(store *runtime.VCR) toy.Service {
        bg := toyvcr.NewBackground(store)
        // First arg is outermost; these two scenarios touch disjoint
        // methods so stacking order is behaviourally irrelevant here,
        // but the convention is: the layer whose transformations should
        // be visible to the caller goes first.
        return toyvcr.Stack(bg, happyAssertions, happyOverrides)
    },
}
```

## Plugin-internal simplifications

The `makeEndpoint*` templates collapse. Today there are five shape-specific variants (streaming / `SkipResponseBodyEncodeDecode` / viewed-result / result / no-result), each branching on `IsLoopback(ctx)`, then `scenario.Next(name)`, then `bg.Method(...)`. In v2.0 each becomes a trivial forwarder:

```go
func makeEndpointGetThing(svc serviceA.Service) goa.Endpoint {
    return func(ctx context.Context, v any) (any, error) {
        p, ok := v.(*serviceA.GetThingPayload)
        if !ok {
            return nil, fmt.Errorf("vcr: unexpected GetThing payload %T", v)
        }
        return svc.GetThing(ctx, p)
    }
}
```

Goa's own endpoint helpers handle the shape-specific wrapping (viewed results, `SkipResponseBodyEncodeDecode`, etc.) when given a `Service`; the plugin no longer duplicates that logic.

## Migration plan

All work is internal (the project has one consumer).

**Runtime package (`runtime/`):**

- Delete `runtime/loopback.go`.
- Keep `runtime/scenario.go` unchanged; it becomes the internal queue used by the generated `*Scenario`.
- Keep `VCR`, `StubDoer`, `Endpoint`, route matching, `RecordingTransport`, policy as-is.

**Plugin codegen (`plugin/vcr/internal/vcrgen/`):**

- Rewrite `render.go` template to emit the new `Scenario` type, per-method `Set*/Add*`, updated `Service*Func` types, `NewBackground`, `Stack`, trivial `makeEndpoint*`, simplified `NewPlaybackHandler`.
- Remove `NewLoopbackClient`, `loopbackDoer`, `BuildScenario`, the `ScenarioFactory` typedef, the `LoopbackMiddleware` wrap, and the five-way `makeEndpoint*` switch.
- Update `render_cli.go`: change `ScenarioRegistry` value type, drop loopback baseURL plumbing, simplify the scenario-to-handler path.
- Update `render_test.go` and `render_cli_test.go` with revised golden assertions.

**Example and integration test:**

- `examples/toy/design` — unchanged (design is upstream of the plugin).
- Regenerate `examples/toy/gen/` via `goa gen`.
- Rewrite `plugin/vcr/integration_toy_test.go` against the new API. The two notable existing cases:
  - "no scenario handler set → falls back to background stub" — restated as "empty Scenario → delegates through to background."
  - "loopback bypass forces background, even if scenario handler exists" — deleted; loopback no longer exists.

**Docs:**

- Rewrite `README.md` sections on Scenario dispatch and Loopback. Replace loopback-client-for-streaming-scenarios with a Stack/layering example.
- `AGENTS.md` unchanged.

**Versioning:**

- Tag `v2.0.0` when merged to `main`.

## Future work

### Stream wrapping (v3.0)

v3.0 will add support for wrapping streams — a handler observes or transforms frames flowing between the caller and a layer below. The likely shape:

- New handler signature: `func(ctx, payload, stream, next serviceA.Service) error`.
- Stream-interceptor utility: wrap `Send`/`Recv` on the incoming stream, pass the wrapped stream to `next.StreamMethod(ctx, payload, wrapped)`.
- Registration split: `Set*WrappingFunc` / `Add*WrappingFunc` for wrappable handlers; the existing `Set*`/`Add*` continue to accept terminal handlers.

Nothing about the v2.0 structure precludes this path. The `*Scenario` dispatch branch for streaming can grow a second case that invokes a wrapping handler with `s.next` as its fourth argument.

### Strict queue mode

A per-layer or per-method opt-in "strict" flag: when a layer's `Add`-queue drains, error rather than delegating. Useful for catching off-by-one mismatches in expected call counts. Small addition, not needed in v2.0.

## Open items deferred to implementation

- **Background implementation form.** Wrapper struct vs. populated `*serviceA.Client`. Pick whichever produces less generated code.
- **CLI defaults and error formatting around `--scenario`.** User flagged "we can riff on the CLI and playback / default stuff in a bit" — left to the implementation plan.
