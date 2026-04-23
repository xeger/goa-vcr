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

	// Background returns plain result + view name; the Goa endpoint layer wraps.
	assertContains(t, src, `return res, viewFromPayload(p), nil`)

	// Scenario method returns (ResultRef, string, error); handler func returns plain.
	assertContains(t, src, `func (s *Scenario) GetThingViewed(ctx context.Context, p *toyviews.GetThingViewedPayload) (*toyviews.ThingWithViews, string, error)`)
	assertContains(t, src, `func (b *backgroundService) GetThingViewed(ctx context.Context, p *toyviews.GetThingViewedPayload) (*toyviews.ThingWithViews, string, error)`)
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
