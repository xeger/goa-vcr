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
	run(t, tmp, "go", "list", "-deps", "github.com/xeger/goa-vcr/cmd/goa-vcr-goa")

	// Generate code into tmp module using the wrapper tool that bakes the plugin in.
	run(t, tmp, "go", "run", "github.com/xeger/goa-vcr/cmd/goa-vcr-goa", "gen", "github.com/xeger/goa-vcr/examples/toy/design", "-o", ".")

	// Add a smoke test that imports and exercises the generated VCR glue.
	writeFile(t, filepath.Join(tmp, "toy_smoke_test.go"), fmt.Sprintf(`package toyint

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	toy "%[1]s/gen/toy"
	toyvcr "%[1]s/gen/http/toy/vcr"
	vcrruntime "github.com/xeger/goa-vcr/runtime"
)

func TestPlayback_UnaryFallbackAndLoopbackBypass(t *testing.T) {
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

	sc := toyvcr.NewScenario()

	h, err := toyvcr.NewPlaybackHandler(store, sc, toyvcr.PlaybackOptions{ScenarioName: "test"})
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// No scenario handler set => fallback to background stub.
	res1 := mustGet(t, srv.URL+"/things/123", nil)
	if res1.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res1.StatusCode)
	}
	got := decodeThing(t, res1.Body)
	if got.ID != "123" {
		t.Fatalf("unexpected id: %%q", got.ID)
	}

	// Scenario handler overrides normal requests.
	sc.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload) (*toy.Thing, error) {
		return &toy.Thing{ID: p.ID}, nil
	})
	res2 := mustGet(t, srv.URL+"/things/999", nil)
	got2 := decodeThing(t, res2.Body)
	if got2.ID != "999" {
		t.Fatalf("unexpected id from scenario: %%q", got2.ID)
	}

	// Loopback bypass forces background, even if scenario handler exists.
	hdr := http.Header{}
	hdr.Set(vcrruntime.LoopbackHeader, "1")
	res3 := mustGet(t, srv.URL+"/things/123", hdr)
	got3 := decodeThing(t, res3.Body)
	if got3.ID != "123" {
		t.Fatalf("unexpected id from loopback bypass: %%q", got3.ID)
	}
}

func TestPlayback_StreamingRequiresScenario(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	sc := toyvcr.NewScenario()
	h, err := toyvcr.NewPlaybackHandler(store, sc, toyvcr.PlaybackOptions{})
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

	// Add a minimal scenario handler; expect non-500 and some body.
	sc.SetStreamThingsSse(func(ctx context.Context, p *toy.StreamThingsSsePayload, stream toy.StreamThingsSseServerStream) error {
		_ = stream.Send(&toy.ThingEvent{Type: "thing", ID: p.ID})
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
	// file = <root>/plugin/vcr/integration_toy_test.go
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

