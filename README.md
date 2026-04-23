## goa-vcr

`github.com/xeger/goa-vcr` provides:

- **`runtime/`**: transport-agnostic VCR primitives (policy, stub store, route matching, stub doer, recording transport).
- **`plugin/vcr/`**: a Goa v3 codegen plugin that generates per-service glue into `gen/http/<service>/vcr`.

### Try it on a service (minimal)

The Goa plugin model is: your plugin must be linked into the generator binary that runs during `goa gen` ([plugin guide](https://pkg.go.dev/goa.design/plugins/v3)).

1. Add a blank import in your design module (any file in the design package):

```go
import _ "github.com/xeger/goa-vcr/plugin/vcr"
```

2. Run generation:

```bash
# IMPORTANT: your `goa` CLI must be compatible with the `goa.design/goa/v3` module
# version used by your repo. If you see a compile error like:
#   "not enough arguments in call to generator.Generate"
# it means your installed `goa` binary is older/newer than the module.
#
# This repo pins goa to v3.23.4, so this is a safe invocation:
go run goa.design/goa/v3/cmd/goa@v3.23.4 gen <design-import-path> -o .
```

### Scenarios as Service layers

The generated per-service `Scenario` implements your Goa `Service` interface. A scenario holds a per-method handler queue plus a reference to the `Service` "below" it (another Scenario, or the stub-backed background). Calls to a method dispatch to the queued handler if one is set; otherwise they delegate to the layer below. Handlers for unary methods receive the layer below as a `next` argument so they can call through and post-process results.

```go
import (
    toy "<your-module>/gen/toy"
    toyvcr "<your-module>/gen/http/toy/vcr"
    vcrruntime "github.com/xeger/goa-vcr/runtime"
)

// Background: stub-backed Service.
bg := toyvcr.NewBackground(store)

// Single scenario: override one method, delegate the rest to bg.
sc := toyvcr.NewScenario(bg)
sc.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
    t, err := next.GetThing(ctx, p)
    if err != nil { return nil, err }
    t.Name = "patched"
    return t, nil
})

handler, _ := toyvcr.NewPlaybackHandler(sc)
```

### Stacking scenarios

`Stack(bg, outer, middle, inner)` composes layers with the first argument outermost (`outer(middle(inner(bg)))`). Layers are ordinary middleware-style functions `func(next Service) Service`:

```go
func addLogging(next toy.Service) toy.Service {
    s := toyvcr.NewScenario(next)
    s.SetListThings(func(ctx context.Context, p *toy.ListThingsPayload, next toy.Service) (*toy.ThingList, error) {
        log.Printf("ListThings called")
        return next.ListThings(ctx, p)
    })
    return s
}

func patchSingle(next toy.Service) toy.Service {
    s := toyvcr.NewScenario(next)
    s.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
        t, err := next.GetThing(ctx, p)
        if err != nil { return nil, err }
        t.Name = "patched"
        return t, nil
    })
    return s
}

svc := toyvcr.Stack(bg, addLogging, patchSingle)
handler, _ := toyvcr.NewPlaybackHandler(svc)
```

### Streaming methods

Streaming handlers are terminal: they receive `(ctx, payload, stream)` and do not get a `next` argument. If a layer has a handler registered for a streaming method, it runs; otherwise the call delegates to the next layer. If no layer handles it, the background returns an explicit error — there are no recorded streams to fall back to.

Stream wrapping (intercepting `Send`/`Recv`) is planned for a future release.

### CLI scenario registry

The generated CLI's `--scenario <name>` flag selects a named entry from the registry. Each entry is a complete playback definition:

```go
cfg := toyvcr.CLIConfig{
    AppName: "toy-vcr",
    ScenarioRegistry: map[string]func(*vcrruntime.VCR) toy.Service{
        "happy": func(store *vcrruntime.VCR) toy.Service {
            bg := toyvcr.NewBackground(store)
            return toyvcr.Stack(bg, addLogging, patchSingle)
        },
        "bare": toyvcr.NewBackground,
    },
    DefaultScenario: "bare",
}
os.Exit(toyvcr.RunCLI(os.Args[1:], cfg))
```

### VCR Policy (`vcr.json`)

Each VCR stub directory contains a `vcr.json` policy file that configures recording behavior:

```json
{
  "upstream": "https://example.com",
  "authorization": {
    "claims": {
      "sub": "deadbeef"
    }
  },
  "endpoints": {
    "GetThing": {
      "variant": {
        "query": false
      }
    }
  }
}
```

**Policy fields:**

- **`upstream`** (required): Base URL of the upstream server to proxy to during recording.
- **`authorization.claims`** (optional): Map of required JWT claim names to their required values. When recording, if an `Authorization: Bearer <token>` header is present, the decoded JWT payload (without signature verification) must contain matching claims. If no `Authorization` header is present, recording proceeds normally. Claim values must be JSON scalars (string, number, bool, null).
- **`endpoints.<name>.variant.query`** (optional): Controls whether query strings participate in stub variants. Defaults to `true` if not specified.
- **`endpoints.<name>.variant.path`** (optional): Controls whether route params participate in stub variants. Defaults to `true` if not specified.

**Authorization gate:** the `authorization.claims` policy only affects **recording** (via `RecordingTransport`). Playback does not enforce authorization claims; it serves stubs based on endpoint matching and diversifiers only.
