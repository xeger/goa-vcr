package vcrgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderServiceVCR_UnaryDispatchIncludesLoopbackBypassAndFallback(t *testing.T) {
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

	assertContains(t, src, `func makeEndpointGetThing`)
	assertContains(t, src, `if vcrruntime.IsLoopback(ctx)`)
	assertContains(t, src, `handler := scenario.Next("GetThing")`)
	assertContains(t, src, `return bg.GetThing(ctx, p)`)
	assertContains(t, src, `type ServiceGetThingFunc`)
	assertContains(t, src, `func (s *Scenario) SetGetThing`)
	assertContains(t, src, `func (s *Scenario) AddGetThing`)
}

func TestRenderServiceVCR_WebSocketUsesUpgrader(t *testing.T) {
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

	assertContains(t, src, `upgrader := &websocket.Upgrader{`)
	assertContains(t, src, `server.Mount(mux)`)
	assertContains(t, src, `v.(*toyws.StreamThingsEndpointInput)`)
	assertContains(t, src, `return nil, f(ctx, in.Payload, in.Stream)`)
}

func TestRenderServiceVCR_UnaryViewedResultWrapsWithNewViewed(t *testing.T) {
	spec := ServiceSpec{
		GenPkg:          "github.com/example/proj/gen",
		ServicePathName: "toyviews",
		ServicePkgName:  "toyviews",
		HasWebSocket:    false,
		HasViewedResult: true,
		Endpoints: []EndpointSpec{
			{
				MethodVarName:         "GetThingViewed",
				PayloadRef:            "*toyviews.GetThingViewedPayload",
				ResultRef:             "*toyviews.ThingWithViews",
				IsStreaming:           false,
				ViewedResultInitName:  "NewViewedThingWithViews",
				ViewedResultViewName:  "",
				Routes:                []RouteSpec{{Verb: "GET", Path: "/things/{id}/viewed"}},
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

	assertContains(t, src, `import (`) // sanity
	assertContains(t, src, `"reflect"`)
	assertContains(t, src, `func viewFromPayload`)
	assertContains(t, src, `return toyviews.NewViewedThingWithViews(res, viewFromPayload(p)), nil`)
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q", needle)
	}
}

