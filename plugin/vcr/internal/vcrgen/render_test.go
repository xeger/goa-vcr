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

	// Record background is a Service implementation backed by the upstream
	// recorder transport, so record can share scenario dispatch with playback.
	assertContains(t, src, `type recordingBackgroundService struct {`)
	assertContains(t, src, `func NewRecordingBackground(ctx context.Context, store *vcrruntime.VCR, upstream *url.URL) toy.Service`)
	assertContains(t, src, `vcrruntime.NewRecordingTransport(ctx, store, Endpoints(), recordingBaseTransport{}, 0)`)
	assertContains(t, src, `func (b *recordingBackgroundService) GetThing(ctx context.Context, p *toy.GetThingPayload) (*toy.Thing, error)`)
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
				ReturnsViewName:      true,
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

	// Background returns plain result + view name; the Goa endpoint layer wraps.
	assertContains(t, src, `return res, viewFromPayload(p), nil`)

	// Scenario method returns (ResultRef, string, error); handler func returns plain.
	assertContains(t, src, `func (s *Scenario) GetThingViewed(ctx context.Context, p *toyviews.GetThingViewedPayload) (*toyviews.ThingWithViews, string, error)`)
	assertContains(t, src, `func (b *backgroundService) GetThingViewed(ctx context.Context, p *toyviews.GetThingViewedPayload) (*toyviews.ThingWithViews, string, error)`)
	assertContains(t, src, `type ServiceGetThingViewedFunc func(context.Context, *toyviews.GetThingViewedPayload, toyviews.Service) (*toyviews.ThingWithViews, error)`)
}

func TestRenderServiceVCR_ViewedResultSingleViewOmitsViewReturn(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toyviewdefault",
		ServicePkgName:  "toyviewdefault",
		HasWebSocket:    false,
		Endpoints: []EndpointSpec{
			{
				MethodVarName:        "GetThingViewedDefaultOnly",
				PayloadRef:           "*toyviewdefault.GetThingViewedDefaultOnlyPayload",
				ResultRef:            "*toyviewdefault.ThingWithViews",
				IsStreaming:          false,
				ViewedResultInitName: "NewViewedThingWithViews",
				ViewedResultViewName: "default",
				Routes:               []RouteSpec{{Verb: "GET", Path: "/things/{id}/viewed-default"}},
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

	assertNotContains(t, src, `"reflect"`)
	assertNotContains(t, src, `func viewFromPayload`)

	assertContains(t, src, `func (s *Scenario) GetThingViewedDefaultOnly(ctx context.Context, p *toyviewdefault.GetThingViewedDefaultOnlyPayload) (*toyviewdefault.ThingWithViews, error)`)
	assertContains(t, src, `func (b *backgroundService) GetThingViewedDefaultOnly(ctx context.Context, p *toyviewdefault.GetThingViewedDefaultOnlyPayload) (*toyviewdefault.ThingWithViews, error)`)
	assertNotContains(t, src, `func (s *Scenario) GetThingViewedDefaultOnly(ctx context.Context, p *toyviewdefault.GetThingViewedDefaultOnlyPayload) (*toyviewdefault.ThingWithViews, string, error)`)
}

func TestRenderServiceVCR_ResultErrorPathsUseTypedZeroValue(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toyvalue",
		ServicePkgName:  "toyvalue",
		Endpoints: []EndpointSpec{
			{
				MethodVarName: "GetID",
				PayloadRef:    "*toyvalue.GetIDPayload",
				ResultRef:     "toyvalue.UUID",
				Routes:        []RouteSpec{{Verb: "GET", Path: "/id"}},
			},
			{
				MethodVarName:        "GetViewedID",
				PayloadRef:           "*toyvalue.GetViewedIDPayload",
				ResultRef:            "toyvalue.UUID",
				ViewedResultInitName: "NewViewedUUID",
				ReturnsViewName:      true,
				Routes:               []RouteSpec{{Verb: "GET", Path: "/id/viewed"}},
			},
		},
		HasViewedResult: true,
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

	assertContains(t, src, `func zeroValue[T any]() T`)
	assertContains(t, src, `return zeroValue[toyvalue.UUID](), fmt.Errorf("vcr: scenario handler for GetID has unexpected type %T", h)`)
	assertContains(t, src, `return zeroValue[toyvalue.UUID](), "", fmt.Errorf("vcr: scenario handler for GetViewedID has unexpected type %T", h)`)
	assertContains(t, src, `return zeroValue[toyvalue.UUID](), "", err`)
	assertNotContains(t, src, `return nil, fmt.Errorf("vcr: scenario handler for GetID has unexpected type %T", h)`)
	assertNotContains(t, src, `return nil, "", fmt.Errorf("vcr: scenario handler for GetViewedID has unexpected type %T", h)`)
}

func TestRenderServiceVCR_SkipResponseBodyEncodeDecodeShapes(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toyraw",
		ServicePkgName:  "toyraw",
		HasRawResponse:  true,
		Endpoints: []EndpointSpec{
			{
				MethodVarName:  "GetRawOnly",
				PayloadRef:     "*toyraw.GetRawOnlyPayload",
				HasRawResponse: true,
				Routes:         []RouteSpec{{Verb: "GET", Path: "/raw-only"}},
			},
			{
				MethodVarName:  "GetRawWithResult",
				PayloadRef:     "*toyraw.GetRawWithResultPayload",
				ResultRef:      "*toyraw.Thing",
				HasRawResponse: true,
				Routes:         []RouteSpec{{Verb: "GET", Path: "/raw-with-result"}},
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

	assertContains(t, src, `"io"`)

	assertContains(t, src, `type ServiceGetRawOnlyFunc func(context.Context, *toyraw.GetRawOnlyPayload, toyraw.Service) (io.ReadCloser, error)`)
	assertContains(t, src, `func (s *Scenario) GetRawOnly(ctx context.Context, p *toyraw.GetRawOnlyPayload) (io.ReadCloser, error)`)
	assertContains(t, src, `func (b *backgroundService) GetRawOnly(ctx context.Context, p *toyraw.GetRawOnlyPayload) (io.ReadCloser, error)`)

	assertContains(t, src, `type ServiceGetRawWithResultFunc func(context.Context, *toyraw.GetRawWithResultPayload, toyraw.Service) (*toyraw.Thing, io.ReadCloser, error)`)
	assertContains(t, src, `func (s *Scenario) GetRawWithResult(ctx context.Context, p *toyraw.GetRawWithResultPayload) (*toyraw.Thing, io.ReadCloser, error)`)
	assertContains(t, src, `func (b *backgroundService) GetRawWithResult(ctx context.Context, p *toyraw.GetRawWithResultPayload) (*toyraw.Thing, io.ReadCloser, error)`)
	assertContains(t, src, `return zeroValue[*toyraw.Thing](), nil, fmt.Errorf("vcr: scenario handler for GetRawWithResult has unexpected type %T", h)`)
}

func TestRenderServiceVCR_NonGetBackgroundShortCircuits(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toymut",
		ServicePkgName:  "toymut",
		Endpoints: []EndpointSpec{
			{
				MethodVarName: "GetThing",
				PayloadRef:    "*toymut.GetThingPayload",
				ResultRef:     "*toymut.Thing",
				Routes:        []RouteSpec{{Verb: "GET", Path: "/things/{id}"}},
			},
			{
				MethodVarName: "UpdateSettings",
				PayloadRef:    "*toymut.UpdateSettingsPayload",
				ResultRef:     "*toymut.Settings",
				Routes:        []RouteSpec{{Verb: "PATCH", Path: "/settings"}},
			},
			{
				MethodVarName: "DeleteThing",
				PayloadRef:    "*toymut.DeleteThingPayload",
				Routes:        []RouteSpec{{Verb: "DELETE", Path: "/things/{id}"}},
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

	// Recordable GET still dials the stub-backed client.
	assertContains(t, src, `return b.hc.GetThing(ctx, p)`)

	// Non-recordable PATCH short-circuits with the runtime helper.
	assertContains(t, src, `return zeroValue[*toymut.Settings](), vcrruntime.NoScenarioHandler(ctx, "UpdateSettings", "PATCH")`)
	assertNotContains(t, src, `return b.hc.UpdateSettings(ctx, p)`)
	assertContains(t, src, `return zeroValue[*toymut.Settings](), vcrruntime.RecordNoScenarioHandler(ctx, "UpdateSettings", "PATCH")`)
	assertNotContains(t, src, `return b.hc.UpdateSettings(ctx, p)`)

	// Non-recordable DELETE with no result type returns just the error.
	assertContains(t, src, `return vcrruntime.NoScenarioHandler(ctx, "DeleteThing", "DELETE")`)
	assertNotContains(t, src, `return b.hc.DeleteThing(ctx, p)`)
	assertContains(t, src, `return vcrruntime.RecordNoScenarioHandler(ctx, "DeleteThing", "DELETE")`)
	assertNotContains(t, src, `return b.hc.DeleteThing(ctx, p)`)
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
