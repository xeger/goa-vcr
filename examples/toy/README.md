## examples/toy

This is a tiny Goa design package used to **doc-test** the goa-vcr plugin.

### What it proves

- `goa-vcr-goa gen` generates normal Goa `gen/` + `gen/http/` output **plus** `gen/http/toy/vcr`.
- The generated VCR glue **compiles** and the key playback rules **work**:
  - unary scenario handler optional (fallback to stub-backed background)
  - loopback header bypasses unary scenarios
  - streaming scenario handler required (SSE example)

### How to run it locally

From the repo root:

```bash
go run ./cmd/goa-vcr-goa gen github.com/xeger/goa-vcr/examples/toy/design -o /tmp/goa-vcr-toy
cd /tmp/goa-vcr-toy && go test ./...
```

