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
	assertContains(t, src, "BuildScenario(")
	assertContains(t, src, "NewPlaybackHandler(")
}
