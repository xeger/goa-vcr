## goa-vcr

`github.com/xeger/goa-vcr` provides:

- **`runtime/`**: transport-agnostic VCR primitives (policy, stub store, route matching, stub doer, recording transport, loopback bypass).
- **`plugin/vcr/`**: a Goa v3 codegen plugin that generates per-service glue into `gen/http/<service>/vcr`.

### Try it on a service (minimal)

1. **Enable the plugin in your Goa design module** (blank import anywhere that is compiled by `goa gen`):

```go
import _ "github.com/xeger/goa-vcr/plugin/vcr"
```

2. **Run your normal Goa generation** (whatever wraps `goa gen ...` in that repo).

3. **Use the generated package** at `gen/http/<service>/vcr`:

- **Playback server**: `vcr.NewPlaybackHandler(store, scenario, vcr.PlaybackOptions{ScenarioName: "Happy"})`
- **Loopback client for streaming scenarios**: `vcr.NewLoopbackClient(baseURL, doer)` (client always sets `X-Vcr-Loopback: 1`)

### Notes

- **Scenario dispatch**: streaming handlers are required; unary handlers are optional (fallback is stub-backed background).
- **Loopback bypass**: requests with `X-Vcr-Loopback: 1` bypass unary scenario dispatch to prevent recursion.

