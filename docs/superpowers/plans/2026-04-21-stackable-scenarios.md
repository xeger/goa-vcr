# Stackable Scenarios Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the v2.0 Scenario-as-Service redesign described in `docs/superpowers/specs/2026-04-21-stackable-scenarios-design.md` — generated `Scenario` becomes a Goa `Service`, scenarios stack via typed `next` references, loopback is removed, `makeEndpoint*` helpers delegate to `toy.NewEndpoints(svc)`.

**Architecture:** The per-service generator emits a `Scenario` struct holding a `clue/mock`-backed queue plus a `next serviceA.Service` reference, per-method dispatch methods that check the queue or delegate to `next`, a `backgroundService` wrapper that implements `serviceA.Service` on top of a stub-backed `*serviceA.Client`, and a `Stack` helper. `NewPlaybackHandler` takes a `serviceA.Service` and wires it through `toy.NewEndpoints`. The loopback header/middleware/client apparatus is deleted entirely.

**Tech Stack:** Go 1.25, `goa.design/goa/v3` (codegen + runtime), `goa.design/clue/mock` (handler queue), `goa.design/clue/log`, `github.com/gorilla/websocket`.

---

## File Structure

**Modified:**
- `plugin/vcr/internal/vcrgen/render.go` — rewrite `vcrTmpl` to emit new types, dispatch, background, Stack, simplified playback handler.
- `plugin/vcr/internal/vcrgen/render_test.go` — new golden substring assertions for the new codegen shape.
- `plugin/vcr/internal/vcrgen/render_cli.go` — update `CLIConfig.ScenarioRegistry` value type, remove `BuildScenario`/loopback plumbing in `cmdPlay`.
- `plugin/vcr/internal/vcrgen/render_cli_test.go` — drop `BuildScenario(` assertion, add new ones for the registry change.
- `plugin/vcr/integration_toy_test.go` — rewrite smoke tests against the new toyvcr API.
- `README.md` — remove loopback section, add stacking example.

**Deleted:**
- `runtime/loopback.go`
- `runtime/loopback_test.go`

**Unchanged:**
- `runtime/scenario.go` (becomes the private queue type for the generated `*Scenario`).
- `runtime/vcr.go`, `runtime/stub_doer.go`, `runtime/recording_transport.go`, `runtime/route_matcher.go`, `runtime/diversifier.go`, `runtime/authorization.go`, `runtime/har.go`, `runtime/policy.go`, `runtime/storage.go`, `runtime/doc.go`.
- `plugin/vcr/plugin.go`, `plugin/vcr/doc.go`.
- `plugin/vcr/internal/vcrgen/build.go`, `plugin/vcr/internal/vcrgen/spec.go`.
- `examples/toy/design/*`.

---

## Task 1: Update `render_test.go` with new-API assertions (red phase)

**Files:**
- Modify: `plugin/vcr/internal/vcrgen/render_test.go`

- [ ] **Step 1: Replace the file contents**

Replace the entire contents of `plugin/vcr/internal/vcrgen/render_test.go` with:

```go
package vcrgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderServiceVCR_UnaryEmitsScenarioAsService(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toy",
		ServicePkgName:  "toy",
		HasWebSocket:    false,
		Endpoints: []EndpointSpec{
			{
				MethodVarName: "GetThing",
				PayloadRef:    "*toy.GetThingPayload",
				ResultRef:     "*toy.Thing",
				IsStreaming:   false,
				Routes:        []RouteSpec{{Verb: "GET", Path: "/things/{id}"}},
			},
		},
	}

	f := RenderServiceVCR(spec)
	if want := filepath.Join("gen", "http", "toy", "vcr", "vcr.go"); filepath.Clean(f.Path) != filepath.Clean(want) {
		t.Fatalf("unexpected file path: got %q want %q", f.Path, want)
	}

	outDir := t.TempDir()
	outPath, err := f.Render(outDir)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	src := string(data)

	// Scenario becomes a Service implementation.
	assertContains(t, src, `type Scenario struct {`)
	assertContains(t, src, `queue vcrruntime.Scenario`)
	assertContains(t, src, `next  toy.Service`)
	assertContains(t, src, `func NewScenario(next toy.Service) *Scenario`)

	// Per-method typed handler + Set/Add + dispatch method.
	assertContains(t, src, `type ServiceGetThingFunc func(context.Context, *toy.GetThingPayload, toy.Service) (*toy.Thing, error)`)
	assertContains(t, src, `func (s *Scenario) SetGetThing(f ServiceGetThingFunc)`)
	assertContains(t, src, `func (s *Scenario) AddGetThing(f ServiceGetThingFunc)`)
	assertContains(t, src, `func (s *Scenario) GetThing(ctx context.Context, p *toy.GetThingPayload) (*toy.Thing, error)`)
	assertContains(t, src, `if h := s.queue.Next("GetThing"); h != nil`)
	assertContains(t, src, `return fn(ctx, p, s.next)`)
	assertContains(t, src, `return s.next.GetThing(ctx, p)`)

	// Background is a Service implementation, not *toy.Client.
	assertContains(t, src, `type backgroundService struct {`)
	assertContains(t, src, `hc *toy.Client`)
	assertContains(t, src, `func NewBackground(store *vcrruntime.VCR) toy.Service`)
	assertContains(t, src, `func (b *backgroundService) GetThing(ctx context.Context, p *toy.GetThingPayload) (*toy.Thing, error)`)
	assertContains(t, src, `return b.hc.GetThing(ctx, p)`)

	// Stack helper.
	assertContains(t, src, `func Stack(bg toy.Service, layers ...func(toy.Service) toy.Service) toy.Service`)

	// Simplified NewPlaybackHandler.
	assertContains(t, src, `func NewPlaybackHandler(svc toy.Service) (http.Handler, error)`)
	assertContains(t, src, `eps := toy.NewEndpoints(svc)`)

	// Removed surface must be absent.
	assertNotContains(t, src, `IsLoopback`)
	assertNotContains(t, src, `LoopbackHeader`)
	assertNotContains(t, src, `LoopbackMiddleware`)
	assertNotContains(t, src, `NewLoopbackClient`)
	assertNotContains(t, src, `loopbackDoer`)
	assertNotContains(t, src, `BuildScenario`)
	assertNotContains(t, src, `type ScenarioFactory`)
	assertNotContains(t, src, `PlaybackOptions`)
	assertNotContains(t, src, `makeEndpoint`)
}

func TestRenderServiceVCR_StreamingHandlerStaysTerminal(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toyws",
		ServicePkgName:  "toyws",
		HasWebSocket:    true,
		Endpoints: []EndpointSpec{
			{
				MethodVarName: "StreamThings",
				PayloadRef:    "*toyws.StreamThingsPayload",
				ResultRef:     "",
				IsStreaming:   true,
				Routes:        []RouteSpec{{Verb: "GET", Path: "/stream"}},
			},
		},
	}

	f := RenderServiceVCR(spec)
	outDir := t.TempDir()
	outPath, err := f.Render(outDir)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	src := string(data)

	// Handler signature is terminal (no next arg).
	assertContains(t, src, `type ServiceStreamThingsFunc func(context.Context, *toyws.StreamThingsPayload, toyws.StreamThingsServerStream) error`)
	assertContains(t, src, `func (s *Scenario) StreamThings(ctx context.Context, p *toyws.StreamThingsPayload, stream toyws.StreamThingsServerStream) error`)
	assertContains(t, src, `return fn(ctx, p, stream)`)
	assertContains(t, src, `return s.next.StreamThings(ctx, p, stream)`)

	// Background returns an explicit "no recorded stream" error.
	assertContains(t, src, `func (b *backgroundService) StreamThings(ctx context.Context, p *toyws.StreamThingsPayload, stream toyws.StreamThingsServerStream) error`)
	assertContains(t, src, `"vcr: no scenario handler for StreamThings and no recorded-stream background is available"`)

	// WebSocket upgrader still wired.
	assertContains(t, src, `upgrader := &websocket.Upgrader{`)
	assertContains(t, src, `server.Mount(mux)`)
}

func TestRenderServiceVCR_ViewedResultWrapsInsideScenarioAndBackground(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toyviews",
		ServicePkgName:  "toyviews",
		HasWebSocket:    false,
		HasViewedResult: true,
		Endpoints: []EndpointSpec{
			{
				MethodVarName:        "GetThingViewed",
				PayloadRef:           "*toyviews.GetThingViewedPayload",
				ResultRef:            "*toyviews.ThingWithViews",
				IsStreaming:          false,
				ViewedResultInitName: "NewViewedThingWithViews",
				ViewedResultViewName: "",
				Routes:               []RouteSpec{{Verb: "GET", Path: "/things/{id}/viewed"}},
			},
		},
	}

	f := RenderServiceVCR(spec)
	outDir := t.TempDir()
	outPath, err := f.Render(outDir)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	src := string(data)

	// viewFromPayload helper retained for dynamic view selection.
	assertContains(t, src, `"reflect"`)
	assertContains(t, src, `func viewFromPayload`)

	// Background wraps plain to viewed using NewViewed*.
	assertContains(t, src, `return toyviews.NewViewedThingWithViews(res, viewFromPayload(p)), nil`)

	// Scenario handler also returns plain; Scenario's method wraps to viewed.
	assertContains(t, src, `type ServiceGetThingViewedFunc func(context.Context, *toyviews.GetThingViewedPayload, toyviews.Service) (*toyviews.ThingWithViews, error)`)
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q\n--- output ---\n%s\n--- end ---", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected output NOT to contain %q\n--- output ---\n%s\n--- end ---", needle, haystack)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:
```bash
go test ./plugin/vcr/internal/vcrgen/ -run TestRenderServiceVCR_ -v
```

Expected: FAIL. The old template contains `IsLoopback`, `BuildScenario`, etc. and does not emit `queue vcrruntime.Scenario`, `NewScenario(next`, `backgroundService`, `Stack`, or `toy.NewEndpoints`.

- [ ] **Step 3: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add plugin/vcr/internal/vcrgen/render_test.go
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
test: new-API golden assertions for Scenario-as-Service codegen

Red phase for v2.0 rewrite of render.go template. Tests target the
Scenario-as-Service shape described in the stackable-scenarios spec.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Rewrite `render.go` template (green phase for Task 1)

**Files:**
- Modify: `plugin/vcr/internal/vcrgen/render.go`

- [ ] **Step 1: Replace `vcrTmpl` and imports block**

Replace the contents of `plugin/vcr/internal/vcrgen/render.go` with:

```go
package vcrgen

import (
	"path/filepath"
	"sort"

	"goa.design/goa/v3/codegen"
)

func RenderServiceVCR(spec ServiceSpec) *codegen.File {
	p := filepath.Join(codegen.Gendir, "http", spec.ServicePathName, "vcr", "vcr.go")

	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("context"),
		codegen.SimpleImport("errors"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("net/http"),

		codegen.NewImport("vcrruntime", "github.com/xeger/goa-vcr/runtime"),
		codegen.NewImport("goahttp", "goa.design/goa/v3/http"),
		codegen.NewImport(spec.ServicePkgName, filepath.ToSlash(filepath.Join(spec.GenPkg, spec.ServicePathName))),
		codegen.NewImport("httpclient", filepath.ToSlash(filepath.Join(spec.GenPkg, "http", spec.ServicePathName, "client"))),
		codegen.NewImport("httpserver", filepath.ToSlash(filepath.Join(spec.GenPkg, "http", spec.ServicePathName, "server"))),
	}

	if spec.HasViewedResult {
		imports = append(imports, codegen.SimpleImport("reflect"))
	}
	if spec.HasWebSocket {
		imports = append(imports, codegen.SimpleImport("github.com/gorilla/websocket"))
	}

	// Keep imports stable for unit tests / diffs.
	sort.SliceStable(imports, func(i, j int) bool {
		if imports[i].Path == imports[j].Path {
			return imports[i].Name < imports[j].Name
		}
		return imports[i].Path < imports[j].Path
	})

	sections := []*codegen.SectionTemplate{
		codegen.Header("vcr", "vcr", imports),
		{
			Name:   "vcr",
			Source: vcrTmpl,
			FuncMap: func() map[string]any {
				fm := codegen.TemplateFuncs()
				fm["routesCount"] = routesCount
				return fm
			}(),
			Data: spec,
		},
	}

	return &codegen.File{Path: p, SectionTemplates: sections}
}

const vcrTmpl = `

// Endpoints returns the HTTP mountpoints for the service. It is used for
// request-to-endpoint matching when serving stubs.
func Endpoints() []vcrruntime.Endpoint {
	endpoints := make([]vcrruntime.Endpoint, 0, {{ routesCount .Endpoints }})
	{{- range .Endpoints }}
		{{- $m := .MethodVarName }}
		{{- range .Routes }}
	endpoints = append(endpoints, vcrruntime.Endpoint{
		Name:    {{ printf "%q" $m }},
		Method:  {{ printf "%q" .Verb }},
		Pattern: {{ printf "%q" .Path }},
	})
		{{- end }}
	{{- end }}
	return endpoints
}

// Scenario implements {{ .ServicePkgName }}.Service by maintaining a
// clue/mock-backed handler queue per method and delegating to an underlying
// "next" Service when no handler is set. Scenarios compose by wrapping other
// Services, enabling middleware-style stacking over the stub-backed background.
type Scenario struct {
	queue vcrruntime.Scenario
	next  {{ .ServicePkgName }}.Service
}

// NewScenario returns a new Scenario layered on next.
func NewScenario(next {{ .ServicePkgName }}.Service) *Scenario {
	return &Scenario{queue: vcrruntime.NewScenario(), next: next}
}

// Stack applies layers to bg with the first layer outermost and the last
// layer innermost. Stack(bg, outer, middle, inner) produces
// outer(middle(inner(bg))).
func Stack(bg {{ .ServicePkgName }}.Service, layers ...func({{ .ServicePkgName }}.Service) {{ .ServicePkgName }}.Service) {{ .ServicePkgName }}.Service {
	result := bg
	for i := len(layers) - 1; i >= 0; i-- {
		result = layers[i](result)
	}
	return result
}

{{- if .HasViewedResult }}
func viewFromPayload(p any) string {
	const def = "default"
	if p == nil {
		return def
	}
	rv := reflect.ValueOf(p)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return def
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return def
	}
	f := rv.FieldByName("View")
	if !f.IsValid() || f.Kind() != reflect.String {
		return def
	}
	if v := f.String(); v != "" {
		return v
	}
	return def
}
{{- end }}

// backgroundService implements {{ .ServicePkgName }}.Service using a
// stub-backed Goa HTTP client for unary methods. Streaming methods return an
// explicit error — no recorded-stream background exists.
type backgroundService struct {
	hc *{{ .ServicePkgName }}.Client
}

// NewBackground returns a {{ .ServicePkgName }}.Service backed by recorded
// stubs in store.
func NewBackground(store *vcrruntime.VCR) {{ .ServicePkgName }}.Service {
	doer := vcrruntime.NewStubDoer(store, Endpoints())
	// The scheme/host are irrelevant as StubDoer matches on verb+path.
	scheme := "http"
	host := "vcr.local"
	{{- if .HasWebSocket }}
	hc := httpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false, nil, nil)
	{{- else }}
	hc := httpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	{{- end }}
	return &backgroundService{hc: &{{ .ServicePkgName }}.Client{
		{{- range .Endpoints }}
		{{ .MethodVarName }}Endpoint: hc.{{ .MethodVarName }}(),
		{{- end }}
	}}
}

// NewPlaybackHandler wraps any {{ .ServicePkgName }}.Service as an HTTP
// handler using the Goa-generated HTTP server for the service.
func NewPlaybackHandler(svc {{ .ServicePkgName }}.Service) (http.Handler, error) {
	if svc == nil {
		return nil, errors.New("vcr: nil service")
	}
	mux := goahttp.NewMuxer()

	eps := {{ .ServicePkgName }}.NewEndpoints(svc)

	errHandler := func(ctx context.Context, w http.ResponseWriter, err error) {
		// Keep this minimal: callers may install their own goa error formatter higher up.
		_ = ctx
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	{{- if .HasWebSocket }}
	upgrader := &websocket.Upgrader{
		Subprotocols: []string{"ws", "wss", "auth.bearer"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	server := httpserver.New(eps, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, errHandler, nil, upgrader, nil)
	{{- else }}
	server := httpserver.New(eps, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, errHandler, nil)
	{{- end }}
	server.Mount(mux)

	return mux, nil
}

{{ range .Endpoints }}

// Service{{ .MethodVarName }}Func is the typed scenario handler signature for {{ .MethodVarName }}.
{{- if .IsStreaming }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}, {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error
{{- else }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}, {{ $.ServicePkgName }}.Service) ({{ .ResultRef }}, error)
{{- end }}

func (s *Scenario) Set{{ .MethodVarName }}(f Service{{ .MethodVarName }}Func) {
	s.queue.Set("{{ .MethodVarName }}", f)
}

func (s *Scenario) Add{{ .MethodVarName }}(f Service{{ .MethodVarName }}Func) {
	s.queue.Add("{{ .MethodVarName }}", f)
}

{{ if .IsStreaming }}
// {{ .MethodVarName }} dispatches to the handler queue (if any) or delegates
// to the underlying Service. Stream handlers are terminal — they do not
// receive a next argument.
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}, stream {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		return fn(ctx, p, stream)
	}
	return s.next.{{ .MethodVarName }}(ctx, p, stream)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}, stream {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error {
	return fmt.Errorf("vcr: no scenario handler for {{ .MethodVarName }} and no recorded-stream background is available")
}
{{ else if and .ResultRef .ViewedResultInitName }}
// {{ .MethodVarName }} dispatches to the handler queue (if any) or delegates.
// Handlers return the plain result type; this method wraps to the viewed type
// expected by the Service interface.
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		res, err := fn(ctx, p, s.next)
		if err != nil {
			return nil, err
		}
		{{- if .ViewedResultViewName }}
		return {{ $.ServicePkgName }}.{{ .ViewedResultInitName }}(res, {{ printf "%q" .ViewedResultViewName }}), nil
		{{- else }}
		return {{ $.ServicePkgName }}.{{ .ViewedResultInitName }}(res, viewFromPayload(p)), nil
		{{- end }}
	}
	return s.next.{{ .MethodVarName }}(ctx, p)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	res, err := b.hc.{{ .MethodVarName }}(ctx, p)
	if err != nil {
		return nil, err
	}
	{{- if .ViewedResultViewName }}
	return {{ $.ServicePkgName }}.{{ .ViewedResultInitName }}(res, {{ printf "%q" .ViewedResultViewName }}), nil
	{{- else }}
	return {{ $.ServicePkgName }}.{{ .ViewedResultInitName }}(res, viewFromPayload(p)), nil
	{{- end }}
}
{{ else if .ResultRef }}
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		return fn(ctx, p, s.next)
	}
	return s.next.{{ .MethodVarName }}(ctx, p)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	return b.hc.{{ .MethodVarName }}(ctx, p)
}
{{ else }}
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) error {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		_, err := fn(ctx, p, s.next)
		return err
	}
	return s.next.{{ .MethodVarName }}(ctx, p)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) error {
	return b.hc.{{ .MethodVarName }}(ctx, p)
}
{{ end }}

{{ end }}
`

func routesCount(endpoints []EndpointSpec) int {
	n := 0
	for _, ep := range endpoints {
		n += len(ep.Routes)
	}
	return n
}
```

Note: the no-result branch returns `(any, error)` from the handler then discards the value — this preserves the "handler signature has next as last arg" invariant while still satisfying the Service method's `error`-only return. If that proves awkward in review, change `Service*Func` for no-result unary methods to `func(ctx, p, next) error` and remove the discard. Either is fine; keep consistent across all no-result methods.

- [ ] **Step 2: Run the vcrgen tests and verify they pass**

Run:
```bash
go test ./plugin/vcr/internal/vcrgen/ -run TestRenderServiceVCR_ -v
```

Expected: PASS for all three `TestRenderServiceVCR_*` tests. If any substring assertion fails, adjust the template so the emitted source contains the expected string exactly.

- [ ] **Step 3: Also verify the file as a whole still compiles**

Run:
```bash
go build ./plugin/vcr/internal/vcrgen/
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add plugin/vcr/internal/vcrgen/render.go
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
feat: Scenario-as-Service codegen with Stack helper

Rewrite plugin/vcr/internal/vcrgen/render.go template so the generated
per-service code emits:
- Scenario struct implementing serviceA.Service, with per-method dispatch
- backgroundService implementing serviceA.Service with stream errors
- Stack helper applying layers outer-first
- NewPlaybackHandler delegating to serviceA.NewEndpoints

Drops loopbackDoer, NewLoopbackClient, BuildScenario, ScenarioFactory,
PlaybackOptions, and the makeEndpoint* switch.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Update `render_cli_test.go` expectations (red phase)

**Files:**
- Modify: `plugin/vcr/internal/vcrgen/render_cli_test.go`

- [ ] **Step 1: Replace the file contents**

Replace `plugin/vcr/internal/vcrgen/render_cli_test.go` with:

```go
package vcrgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderServiceVCRCLI_WritesCLIFile(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toy",
		ServicePkgName:  "toy",
		HasWebSocket:    false,
		Endpoints: []EndpointSpec{
			{
				MethodVarName: "GetThing",
				PayloadRef:    "*toy.GetThingPayload",
				ResultRef:     "*toy.Thing",
				IsStreaming:   false,
				Routes:        []RouteSpec{{Verb: "GET", Path: "/things/{id}"}},
			},
		},
	}

	f := RenderServiceVCRCLI(spec)
	if want := filepath.Join("gen", "http", "toy", "vcr", "cli.go"); filepath.Clean(f.Path) != filepath.Clean(want) {
		t.Fatalf("unexpected file path: got %q want %q", f.Path, want)
	}

	outDir := t.TempDir()
	outPath, err := f.Render(outDir)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	src := string(data)

	assertContains(t, src, "type CLIConfig struct")
	assertContains(t, src, "func RunCLI(")
	assertContains(t, src, "func Usage(")
	assertContains(t, src, "Endpoints()")

	// New: registry builds a Service from a store.
	assertContains(t, src, "ScenarioRegistry map[string]func(*vcrruntime.VCR) toy.Service")
	assertContains(t, src, "NewPlaybackHandler(svc)")
	assertContains(t, src, "svc := build(store)")

	// Removed: loopback plumbing.
	assertNotContains(t, src, "BuildScenario(")
	assertNotContains(t, src, "loopbackDoer")
	assertNotContains(t, src, "LoopbackHeader")
	assertNotContains(t, src, "IsLoopback")
	assertNotContains(t, src, "PlaybackOptions")
	assertNotContains(t, src, "ScenarioFactory")
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:
```bash
go test ./plugin/vcr/internal/vcrgen/ -run TestRenderServiceVCRCLI_ -v
```

Expected: FAIL. The old CLI template contains `BuildScenario(`, `vcrruntime.IsLoopback` (inside `vcrAccessLog`), `ScenarioFactory`, `PlaybackOptions`.

- [ ] **Step 3: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add plugin/vcr/internal/vcrgen/render_cli_test.go
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
test: new-API golden assertions for CLI codegen

Red phase for v2.0 CLI template updates. Replaces BuildScenario assertion
with registry/service-builder shape; disallows loopback references.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Rewrite `render_cli.go` template (green phase for Task 3)

**Files:**
- Modify: `plugin/vcr/internal/vcrgen/render_cli.go`

- [ ] **Step 1: Apply the edits below**

Edit `CLIConfig.ScenarioRegistry`'s value type:

Old:
```go
ScenarioRegistry map[string]ScenarioFactory
```
New:
```go
ScenarioRegistry map[string]func(*vcrruntime.VCR) {{ .ServicePkgName }}.Service
```

Edit the initialization in `normalizeCLIConfig`:

Old:
```go
if cfg.ScenarioRegistry == nil {
    cfg.ScenarioRegistry = map[string]ScenarioFactory{}
}
```
New:
```go
if cfg.ScenarioRegistry == nil {
    cfg.ScenarioRegistry = map[string]func(*vcrruntime.VCR) {{ .ServicePkgName }}.Service{}
}
```

Replace the body of `cmdPlay` starting at the `factory, ok := cfg.ScenarioRegistry[*scenarioFlag]` line through the end of the `NewPlaybackHandler` call.

Old:
```go
	addr := fmt.Sprintf("127.0.0.1:%d", *portFlag)
	baseURL := fmt.Sprintf("http://%s", addr)

	factory, ok := cfg.ScenarioRegistry[*scenarioFlag]
	if !ok {
		log.Errorf(ctx, fmt.Errorf("unknown scenario %q", *scenarioFlag), "invalid scenario")
		return 1
	}

	loopbackDoer := vcrruntime.NewStubDoer(store, Endpoints())
	sc, _, err := BuildScenario(baseURL, loopbackDoer, factory)
	if err != nil {
		log.Errorf(ctx, err, "failed to build scenario")
		return 1
	}

	h, err := NewPlaybackHandler(store, sc, PlaybackOptions{ScenarioName: *scenarioFlag})
	if err != nil {
		log.Errorf(ctx, err, "failed to build playback handler")
		return 1
	}
```
New:
```go
	addr := fmt.Sprintf("127.0.0.1:%d", *portFlag)

	build, ok := cfg.ScenarioRegistry[*scenarioFlag]
	if !ok {
		log.Errorf(ctx, fmt.Errorf("unknown scenario %q", *scenarioFlag), "invalid scenario")
		return 1
	}

	svc := build(store)

	h, err := NewPlaybackHandler(svc)
	if err != nil {
		log.Errorf(ctx, err, "failed to build playback handler")
		return 1
	}
```

Remove the `IsLoopback`-dependent logging inside `vcrAccessLog`. Apply these edits:

Old (inside `vcrAccessLog`):
```go
			loopback := vcrruntime.IsLoopback(ctx)

			endpointName, vars, ok := matcher.Match(r)
```
New:
```go
			endpointName, vars, ok := matcher.Match(r)
```

Old (in the "matched" debug block):
```go
					if streamType != "" {
						kvs = append(kvs, log.KV{K: "http.stream", V: streamType})
					}
					if loopback {
						kvs = append(kvs, log.KV{K: "vcr.loopback", V: true})
					}
					log.Debug(ctx, kvs...)
```
New:
```go
					if streamType != "" {
						kvs = append(kvs, log.KV{K: "http.stream", V: streamType})
					}
					log.Debug(ctx, kvs...)
```

Old (in the "unmatched" debug block):
```go
					if streamType != "" {
						kvs = append(kvs, log.KV{K: "http.stream", V: streamType})
					}
					if loopback {
						kvs = append(kvs, log.KV{K: "vcr.loopback", V: true})
					}
					log.Debug(ctx, kvs...)
```
New:
```go
					if streamType != "" {
						kvs = append(kvs, log.KV{K: "http.stream", V: streamType})
					}
					log.Debug(ctx, kvs...)
```

Leave everything else in `render_cli.go` unchanged.

- [ ] **Step 2: Run the tests and verify they pass**

Run:
```bash
go test ./plugin/vcr/internal/vcrgen/ -run TestRenderServiceVCRCLI_ -v
```

Expected: PASS.

- [ ] **Step 3: Run the full plugin-internal suite**

Run:
```bash
go test ./plugin/vcr/internal/vcrgen/ -v
```

Expected: all `TestRenderServiceVCR_*` and `TestRenderServiceVCRCLI_*` pass.

- [ ] **Step 4: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add plugin/vcr/internal/vcrgen/render_cli.go
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
feat: CLI scenario registry takes a store and returns a Service

Replace CLIConfig.ScenarioRegistry's value type with a top-of-stack
builder and remove BuildScenario + loopback-header logging from cmdPlay
and vcrAccessLog. Each registered scenario now composes its own stack.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Rewrite `integration_toy_test.go` for the new toyvcr API

**Files:**
- Modify: `plugin/vcr/integration_toy_test.go`

The integration test regenerates the toy example via `goa gen` and runs smoke tests against the generated `toyvcr` package. It needs to:
1. Drop all references to `vcrruntime.LoopbackHeader` / loopback bypass.
2. Construct scenarios via the new `NewScenario(next)` / `NewBackground(store)` / `Stack` API.
3. Call `NewPlaybackHandler(svc)` (one arg).
4. Update `TestVCRCLI_Usage` to use the new registry value type.
5. Add coverage for the new "scenario post-processes background" pattern (scenario calls `next.GetThing` then tweaks the result).

- [ ] **Step 1: Replace the file contents**

Replace `plugin/vcr/integration_toy_test.go` with:

```go
package vcr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestToyExample_GenerateAndRun(t *testing.T) {
	root := mustRepoRoot(t)

	tmp := t.TempDir()
	mod := "example.com/toyint"

	writeFile(t, filepath.Join(tmp, "go.mod"), fmt.Sprintf(`module %s

go 1.25.5

require (
	github.com/xeger/goa-vcr v0.0.0
	goa.design/goa/v3 v3.23.4
)

replace github.com/xeger/goa-vcr => %s
`, mod, filepath.ToSlash(root)))

	// Ensure go.sum exists for tool invocation and generation.
	run(t, tmp, "go", "list", "-deps", "goa.design/goa/v3/cmd/goa")

	// Generate code into tmp module using the standard Goa tool.
	// The toy design blank-imports github.com/xeger/goa-vcr/plugin/vcr, so the plugin
	// is linked into the generator binary via transitive imports.
	run(t, tmp, "go", "run", "goa.design/goa/v3/cmd/goa", "gen", "github.com/xeger/goa-vcr/examples/toy/design", "-o", ".")

	// Add a smoke test that imports and exercises the generated VCR glue.
	writeFile(t, filepath.Join(tmp, "toy_smoke_test.go"), fmt.Sprintf(`package toyint

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
	toy "%[1]s/gen/toy"
	toyvcr "%[1]s/gen/http/toy/vcr"
	toytypes "%[1]s/gen/types"
	vcrruntime "github.com/xeger/goa-vcr/runtime"
)

func TestPlayback_PolicyWithAuthorizationClaimsIsIgnored(t *testing.T) {
	stubRoot := t.TempDir()
	policyJSON := "{\"upstream\":\"https://example.com\",\"authorization\":{\"claims\":{\"sub\":\"deadbeef\"}}}"
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte(policyJSON), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	body := []byte("{\"id\":\"123\"}\n")
	if err := store.WriteStub("GetThing", vcrruntime.RequestSpec{URL: "http://example.com/things/123"}, vcrruntime.ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
	}, body); err != nil {
		t.Fatalf("write stub: %%v", err)
	}

	svc := toyvcr.NewBackground(store)
	h, err := toyvcr.NewPlaybackHandler(svc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/123", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	got := decodeThing(t, res.Body)
	if got.ID != "123" {
		t.Fatalf("unexpected id: %%q", got.ID)
	}
}

func TestPlayback_EmptyScenarioDelegatesToBackground(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	body := []byte("{\"id\":\"123\"}\n")
	if err := store.WriteStub("GetThing", vcrruntime.RequestSpec{URL: "http://example.com/things/123"}, vcrruntime.ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
	}, body); err != nil {
		t.Fatalf("write stub: %%v", err)
	}

	bg := toyvcr.NewBackground(store)
	sc := toyvcr.NewScenario(bg) // no handlers set => delegates through to bg

	h, err := toyvcr.NewPlaybackHandler(sc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/123", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	got := decodeThing(t, res.Body)
	if got.ID != "123" {
		t.Fatalf("unexpected id: %%q", got.ID)
	}
}

func TestPlayback_ScenarioOverridesUnary(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	bg := toyvcr.NewBackground(store)
	sc := toyvcr.NewScenario(bg)
	sc.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
		return &toy.Thing{ID: p.ID}, nil
	})

	h, err := toyvcr.NewPlaybackHandler(sc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/999", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	got := decodeThing(t, res.Body)
	if got.ID != "999" {
		t.Fatalf("unexpected id: %%q", got.ID)
	}
}

func TestPlayback_ScenarioPostProcessesBackground(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	body := []byte("{\"id\":\"123\"}\n")
	if err := store.WriteStub("GetThing", vcrruntime.RequestSpec{URL: "http://example.com/things/123"}, vcrruntime.ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
	}, body); err != nil {
		t.Fatalf("write stub: %%v", err)
	}

	bg := toyvcr.NewBackground(store)
	sc := toyvcr.NewScenario(bg)
	sc.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
		thing, err := next.GetThing(ctx, p)
		if err != nil {
			return nil, err
		}
		thing.ID = "patched-" + thing.ID
		return thing, nil
	})

	h, err := toyvcr.NewPlaybackHandler(sc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/123", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	got := decodeThing(t, res.Body)
	if got.ID != "patched-123" {
		t.Fatalf("expected patched id, got %%q", got.ID)
	}
}

func TestPlayback_StackLayersOuterFirst(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	body := []byte("{\"id\":\"123\"}\n")
	if err := store.WriteStub("GetThing", vcrruntime.RequestSpec{URL: "http://example.com/things/123"}, vcrruntime.ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
	}, body); err != nil {
		t.Fatalf("write stub: %%v", err)
	}

	// Inner layer prefixes the id with "inner-".
	inner := func(next toy.Service) toy.Service {
		s := toyvcr.NewScenario(next)
		s.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
			r, err := next.GetThing(ctx, p)
			if err != nil {
				return nil, err
			}
			r.ID = "inner-" + r.ID
			return r, nil
		})
		return s
	}
	// Outer layer prefixes the id with "outer-" AFTER inner has run.
	outer := func(next toy.Service) toy.Service {
		s := toyvcr.NewScenario(next)
		s.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
			r, err := next.GetThing(ctx, p)
			if err != nil {
				return nil, err
			}
			r.ID = "outer-" + r.ID
			return r, nil
		})
		return s
	}

	bg := toyvcr.NewBackground(store)
	svc := toyvcr.Stack(bg, outer, inner)

	h, err := toyvcr.NewPlaybackHandler(svc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/123", nil)
	got := decodeThing(t, res.Body)
	// outer runs first (outer-), calls into inner (inner-), which calls bg (123).
	// Result bubbles back up, so final id is outer-inner-123.
	if got.ID != "outer-inner-123" {
		t.Fatalf("expected outer-inner-123, got %%q", got.ID)
	}
}

func TestPlayback_UnaryViewedResult_NoPanicAndRespectsView(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThingViewed\":{\"variant\":{\"query\":false}}}}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	// Store an "extended" view stub for GetThingViewed, including the goa-view header.
	body := []byte("{\"id\":\"123\",\"name\":\"widget\",\"secret\":\"s3cr3t\"}\n")
	if err := store.WriteStub("GetThingViewed", vcrruntime.RequestSpec{URL: "http://example.com/things/123/viewed?view=extended"}, vcrruntime.ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
		Headers:  map[string]string{"goa-view": "extended"},
	}, body); err != nil {
		t.Fatalf("write stub: %%v", err)
	}

	svc := toyvcr.NewBackground(store)
	h, err := toyvcr.NewPlaybackHandler(svc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/123/viewed?view=extended", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	if gotView := res.Header.Get("goa-view"); gotView != "extended" {
		t.Fatalf("unexpected goa-view: %%q", gotView)
	}
	got := decodeThingWithViews(t, res.Body)
	if got.ID != "123" || got.Name != "widget" || got.Secret == nil || *got.Secret != "s3cr3t" {
		t.Fatalf("unexpected viewed result: %%+v", got)
	}
}

func TestPlayback_StreamingWithoutHandlerErrors(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	bg := toyvcr.NewBackground(store)
	sc := toyvcr.NewScenario(bg)
	h, err := toyvcr.NewPlaybackHandler(sc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := mustGet(t, srv.URL+"/things/123/stream-sse", nil)
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !bytes.Contains(b, []byte("no scenario handler")) {
		t.Fatalf("expected missing scenario error, got: %%q", string(b))
	}

	// Install a handler on the scenario; expect non-500 and a body.
	sc.SetStreamThingsSse(func(ctx context.Context, p *toy.StreamThingsSsePayload, stream toy.StreamThingsSseServerStream) error {
		_ = stream.Send(&toytypes.ThingEvent{Type: "thing", ID: p.ID})
		return nil
	})

	res2 := mustGet(t, srv.URL+"/things/123/stream-sse", nil)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %%d", res2.StatusCode)
	}
	b2, _ := io.ReadAll(res2.Body)
	_ = res2.Body.Close()
	if len(b2) == 0 {
		t.Fatalf("expected SSE response body")
	}
}

func TestPlayback_WebSocketBidirectionalAndSendOnly(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	bg := toyvcr.NewBackground(store)
	sc := toyvcr.NewScenario(bg)
	h, err := toyvcr.NewPlaybackHandler(sc)
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Missing handler => ws dial should fail with an HTTP error response.
	{
		wsURL := mustWSURL(t, srv.URL, "/things/123/stream-ws")
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			t.Fatalf("expected ws dial to fail without scenario handler")
		}
		if resp == nil {
			t.Fatalf("expected HTTP response on ws handshake failure")
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !bytes.Contains(b, []byte("no scenario handler")) {
			t.Fatalf("expected missing scenario error, got: %%q", string(b))
		}
	}

	// Add scenario handlers and verify we can connect and receive at least one message.
	sc.SetStreamThingsWs(func(ctx context.Context, p *toy.StreamThingsWsPayload, stream toy.StreamThingsWsServerStream) error {
		_, _ = stream.RecvWithContext(ctx)
		_ = stream.SendWithContext(ctx, &toytypes.ThingEvent{Type: "thing", ID: p.ID})
		return nil
	})
	sc.SetStreamThingsWsSendOnly(func(ctx context.Context, p *toy.StreamThingsWsSendOnlyPayload, stream toy.StreamThingsWsSendOnlyServerStream) error {
		_ = stream.SendWithContext(ctx, &toytypes.ThingEvent{Type: "thing", ID: p.ID})
		return nil
	})

	// Bidirectional
	{
		wsURL := mustWSURL(t, srv.URL, "/things/123/stream-ws")
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("ws dial: %%v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]any{"msg": "hi"}); err != nil {
			t.Fatalf("ws write: %%v", err)
		}
		var evt struct {
			Type string
			ID   string
		}
		if err := conn.ReadJSON(&evt); err != nil {
			t.Fatalf("ws read: %%v", err)
		}
		if evt.ID != "123" {
			t.Fatalf("unexpected ws id: %%q", evt.ID)
		}
	}

	// Send-only
	{
		wsURL := mustWSURL(t, srv.URL, "/things/456/stream-ws-send-only")
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("ws dial: %%v", err)
		}
		defer conn.Close()

		var evt struct {
			Type string
			ID   string
		}
		if err := conn.ReadJSON(&evt); err != nil {
			t.Fatalf("ws read: %%v", err)
		}
		if evt.ID != "456" {
			t.Fatalf("unexpected ws id: %%q", evt.ID)
		}
	}
}

func mustGet(t *testing.T, url string, hdr http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %%v", err)
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %%v", err)
	}
	return res
}

func decodeThing(t *testing.T, r io.ReadCloser) *toy.Thing {
	t.Helper()
	defer r.Close()
	var out toy.Thing
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("decode: %%v", err)
	}
	return &out
}

func decodeThingWithViews(t *testing.T, r io.ReadCloser) *toy.Thingwithviews {
	t.Helper()
	defer r.Close()
	var out toy.Thingwithviews
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("decode: %%v", err)
	}
	return &out
}

func mustWSURL(t *testing.T, base string, path string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base url: %%v", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
`, mod))

	// Add a smoke test that imports and exercises the generated CLI core.
	writeFile(t, filepath.Join(tmp, "toy_cli_smoke_test.go"), fmt.Sprintf(`package toyint

import (
	"testing"

	toy "%[1]s/gen/toy"
	toyvcr "%[1]s/gen/http/toy/vcr"
	vcrruntime "github.com/xeger/goa-vcr/runtime"
)

func TestVCRCLI_Usage(t *testing.T) {
	code := toyvcr.RunCLI([]string{"help"}, toyvcr.CLIConfig{
		AppName: "toy-vcr",
		ScenarioRegistry: map[string]func(*vcrruntime.VCR) toy.Service{
			"Noop": func(store *vcrruntime.VCR) toy.Service {
				return toyvcr.NewBackground(store)
			},
		},
		DefaultPort:        8080,
		DefaultUpstream:    "https://example.com",
		DefaultScenario:    "Noop",
		DefaultMaxVariants: 5,
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %%d", code)
	}
}
`, mod))

	// Compile + run the generated + smoke tests.
	run(t, tmp, "go", "test", "./...")
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(dir, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at repo root %q: %v", root, err)
	}
	return root
}

func run(t *testing.T, dir string, exe string, args ...string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOFLAGS=-mod=mod",
		"GOWORK=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %s\n%s\n%v", exe, strings.Join(args, " "), string(out), err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
```

- [ ] **Step 2: Run the integration test**

Run:
```bash
go test ./plugin/vcr/ -run TestToyExample_GenerateAndRun -v
```

Expected: PASS. The test regenerates the toy example via `goa gen`, compiles the generated code with the smoke tests, and runs them. If something in the generated code doesn't compile (e.g., a template bug exposed by a real Goa service shape), fix the template in `render.go` and re-run.

Common debugging pattern if the integration test fails:
1. Read the compiler errors from the test output — they name the file and line inside the generated `gen/http/toy/vcr/vcr.go`.
2. Manually run `go run goa.design/goa/v3/cmd/goa gen github.com/xeger/goa-vcr/examples/toy/design -o examples/toy/` (per `AGENTS.md`) to get a local copy of the generated code for inspection.
3. Adjust the template in `render.go` to emit correct code. Re-run the integration test.

If the template changes are substantial, separate the template fix into its own commit with message `fix: generated code for toy service`.

- [ ] **Step 3: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add plugin/vcr/integration_toy_test.go
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
test: rewrite toy integration smoke tests for v2.0 API

Replaces loopback-bypass assertions with Scenario-as-Service coverage:
empty scenario delegates to background, handlers override unary methods,
handlers can post-process background results, Stack applies layers
outer-first, streaming still errors without a handler.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Delete `runtime/loopback.go` and its test

**Files:**
- Delete: `runtime/loopback.go`
- Delete: `runtime/loopback_test.go`

- [ ] **Step 1: Verify no remaining references**

Run:
```bash
git -C /Users/tony/Code/xeger/goa-vcr grep -l 'LoopbackHeader\|LoopbackMiddleware\|IsLoopback\|WithLoopback' -- ':!docs' ':!runtime/loopback.go' ':!runtime/loopback_test.go'
```

Expected: no output. If any file outside `docs/` still references the loopback symbols, stop and fix those references first — they indicate an incomplete update from earlier tasks.

- [ ] **Step 2: Delete the files**

Run:
```bash
rm /Users/tony/Code/xeger/goa-vcr/runtime/loopback.go /Users/tony/Code/xeger/goa-vcr/runtime/loopback_test.go
```

- [ ] **Step 3: Verify the runtime package still builds and tests pass**

Run:
```bash
go build ./runtime/...
go test ./runtime/...
```

Expected: both succeed. The runtime package has no other dependency on `loopback.go`.

- [ ] **Step 4: Run the full test suite as a sanity check**

Run:
```bash
go test ./...
```

Expected: all tests pass. The integration test will regenerate the toy example and run its smoke tests.

- [ ] **Step 5: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add -A runtime/
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
refactor: delete loopback apparatus

Scenario-as-Service uses typed next references for cross-layer calls,
obsoleting the X-Vcr-Loopback header that prevented HTTP-round-trip
recursion. All generated consumers have been migrated.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Rewrite README.md

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Replace the file contents**

Replace `README.md` with the content below. The outer fence uses four backticks so the inner triple-backtick Go/JSON blocks render correctly; write literal triple-backticks into the README file.

````markdown
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
- **`endpoints.<name>.variant.path`** (optional): Controls whether route params participate in stub variants. Defaults to `false` if not specified.

**Authorization gate:** the `authorization.claims` policy only affects **recording** (via `RecordingTransport`). Playback does not enforce authorization claims; it serves stubs based on endpoint matching and diversifiers only.
````

- [ ] **Step 2: Render-check the README**

Open the file, verify the markdown looks right (no unterminated code fences, headings make sense). Running a local markdown renderer is not required; visual inspection is enough.

- [ ] **Step 3: Commit**

```bash
git -C /Users/tony/Code/xeger/goa-vcr add README.md
git -C /Users/tony/Code/xeger/goa-vcr commit -m "$(cat <<'EOF'
docs: README for v2.0 Scenario-as-Service API

Drop the loopback-client guidance, add Scenario-as-Service and Stack
examples, document CLI scenario registry with the new builder type,
keep policy/auth docs intact.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Final sanity check

- [ ] **Step 1: Run the full test suite**

Run:
```bash
go test ./...
```

Expected: all tests pass. This includes the integration test which regenerates the toy example and exercises the full stacking API.

- [ ] **Step 2: Verify git log**

Run:
```bash
git -C /Users/tony/Code/xeger/goa-vcr log --oneline main..HEAD
```

Expected output shape (order may vary slightly):

```
<hash> docs: README for v2.0 Scenario-as-Service API
<hash> refactor: delete loopback apparatus
<hash> test: rewrite toy integration smoke tests for v2.0 API
<hash> feat: CLI scenario registry takes a store and returns a Service
<hash> test: new-API golden assertions for CLI codegen
<hash> feat: Scenario-as-Service codegen with Stack helper
<hash> test: new-API golden assertions for Scenario-as-Service codegen
<hash> docs: spec for stackable scenarios (v2.0)
```

- [ ] **Step 3: Report completion**

Report to the user that v2.0 implementation is complete on the `v2` branch. Do NOT tag `v2.0.0` — that's the user's call once they review the branch.

---

## Notes for the implementer

- **Template debugging.** The `render_test.go` tests use substring assertions; they're a smoke test, not a full compilation check. The integration test (`TestToyExample_GenerateAndRun`) is what actually verifies the generated code compiles and behaves. Expect to iterate on the template in Task 2 even after its unit tests pass, because the integration test in Task 5 may surface shape mismatches (e.g., Goa-generated types you didn't anticipate). When that happens, fix the template and rerun the integration test — it's OK to amend into Task 2's commit if it's a small fix, or to create a follow-up `fix: ...` commit.

- **Goa's viewed-result plumbing.** The viewed-result branch of the template assumes `{{ .ServicePkgName }}.{{ .ViewedResultInitName }}` is callable from outside the service package and returns the viewed wrapper type. This matches today's template behavior; if Goa v3.23.4 places `NewViewed*` helpers elsewhere, the template will need a qualifier fix. Evidence that today's path works: the existing integration test calls the viewed endpoint successfully.

- **Viewed-result `next` ergonomics.** A scenario handler for a viewed-result method returns the plain result (not the viewed wrapper); the Scenario method then wraps with `NewViewed*`. If such a handler calls `next.<Method>`, it gets the *viewed* type (because `next` is a `Service`). Unwrapping viewed → plain from inside a handler is awkward but possible (`viewed.Projected` usually gives an accessor). This limitation is documented in the spec's "Open items deferred to implementation" section; it is acceptable for v2.0.

- **No-result unary handlers.** These are rare (methods with neither a result nor a stream). The template returns `(any, error)` from the handler and discards the value to keep the "`next` is the last handler arg" invariant uniform. If a code reviewer strongly prefers `func(ctx, p, next) error` for this subset, changing the template is a few lines; just keep it consistent with the Scenario method's dispatch code.

- **Working tree hygiene.** After every task's commit, run `git -C /Users/tony/Code/xeger/goa-vcr status` and confirm the working tree is clean before moving on. Untracked files left over from a failed run (e.g., a partial `examples/toy/gen/` tree from running `goa gen` manually) should be cleaned up — that directory is gitignored, so a stray `rm -rf examples/toy/gen` is safe.
