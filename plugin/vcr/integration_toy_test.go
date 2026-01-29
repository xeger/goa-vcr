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

func TestPlayback_PolicyWithAuthorizationClaims(t *testing.T) {
	stubRoot := t.TempDir()
	// Test that policy with authorization.claims loads correctly and doesn't affect playback
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

	sc := toyvcr.NewScenario()
	h, err := toyvcr.NewPlaybackHandler(store, sc, toyvcr.PlaybackOptions{ScenarioName: "test"})
	if err != nil {
		t.Fatalf("handler: %%v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Playback should work normally even with authorization.claims in policy
	res := mustGet(t, srv.URL+"/things/123", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	got := decodeThing(t, res.Body)
	if got.ID != "123" {
		t.Fatalf("unexpected id: %%q", got.ID)
	}
}

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

	sc := toyvcr.NewScenario()
	h, err := toyvcr.NewPlaybackHandler(store, sc, toyvcr.PlaybackOptions{})
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

	sc := toyvcr.NewScenario()
	h, err := toyvcr.NewPlaybackHandler(store, sc, toyvcr.PlaybackOptions{})
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
		// Expect one client message, then reply once.
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
