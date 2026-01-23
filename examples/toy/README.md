## examples/toy

This is a tiny Goa design package used to **doc-test** the goa-vcr plugin.

### What it proves

- `goa gen` generates normal Goa `gen/` + `gen/http/` output **plus** `gen/http/toy/vcr` (because the design blank-imports the plugin).
- The generated VCR glue **compiles** and the key playback rules **work**:
  - unary scenario handler optional (fallback to stub-backed background)
  - loopback header bypasses unary scenarios
  - streaming scenario handler required (SSE example)

### How to run it locally

From the repo root:

```bash
# Use a Goa CLI version compatible with the repo (this repo pins goa to v3.23.4).
go run goa.design/goa/v3/cmd/goa@v3.23.4 gen github.com/xeger/goa-vcr/examples/toy/design -o /tmp/goa-vcr-toy
cd /tmp/goa-vcr-toy && go test ./...
```

