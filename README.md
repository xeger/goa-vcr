## goa-vcr

`github.com/xeger/goa-vcr` provides:

- **`runtime/`**: transport-agnostic VCR primitives (policy, stub store, route matching, stub doer, recording transport, loopback bypass).
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

### Use the generated package

Use the generated package at `gen/http/<service>/vcr`:

- **Playback server**: `vcr.NewPlaybackHandler(store, scenario, vcr.PlaybackOptions{ScenarioName: "Happy"})`
- **Loopback client for streaming scenarios**: `vcr.NewLoopbackClient(baseURL, doer)` (client always sets `X-Vcr-Loopback: 1`)

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
- **`endpoints.<name>.variant.path`** (optional): Controls whether route params participate in stub variants. Defaults to `false` if not specified.

### Notes

- **Scenario dispatch**: streaming handlers are required; unary handlers are optional (fallback is stub-backed background).
- **Loopback bypass**: requests with `X-Vcr-Loopback: 1` bypass unary scenario dispatch to prevent recursion.
- **Authorization gate**: The `authorization.claims` policy only affects **recording** (via `RecordingTransport`). Playback does not enforce authorization claims; it serves stubs based on endpoint matching and diversifiers only.

