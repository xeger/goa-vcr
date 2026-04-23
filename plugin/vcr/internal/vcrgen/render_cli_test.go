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
	assertContains(t, src, "ScenarioRegistry")
	assertContains(t, src, "map[string]func(toy.Service) toy.Service")
	assertContains(t, src, "DefaultScenarios []string")
	assertContains(t, src, "scenarioFlag := scenarioListFlag{}")
	assertContains(t, src, `fs.Var(&scenarioFlag, "scenario", "Scenario name (repeat for outer-to-inner stack)")`)
	assertContains(t, src, "vcrruntime.NewActiveScenarios(")
	assertContains(t, src, `mux.HandleFunc("/__vcr__/scenarios", controller.HandleScenarios)`)
	assertContains(t, src, `mux.HandleFunc("/__vcr__/scenarios/active", controller.HandleActiveScenarios)`)

	// Removed: loopback plumbing.
	assertNotContains(t, src, "BuildScenario(")
	assertNotContains(t, src, "loopbackDoer")
	assertNotContains(t, src, "LoopbackHeader")
	assertNotContains(t, src, "IsLoopback")
	assertNotContains(t, src, "PlaybackOptions")
	assertNotContains(t, src, "ScenarioFactory")
}
