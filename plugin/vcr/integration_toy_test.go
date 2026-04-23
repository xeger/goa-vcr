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
	run(t, tmp, "go", "run", "goa.design/goa/v3/cmd/goa@v3.23.4", "gen", "github.com/xeger/goa-vcr/examples/toy/design", "-o", ".")

	// Add a smoke test that imports and exercises the generated VCR glue.
	writeFile(t, filepath.Join(tmp, "toy_smoke_test.go"), fmt.Sprintf(`package toyint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	policyJSON := "{\"upstream\":\"https://example.com\",\"authorization\":{\"claims\":{\"sub\":\"deadbeef\"}},\"endpoints\":{\"GetThing\":{\"variant\":{\"path\":false}}}}"
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
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThing\":{\"variant\":{\"path\":false}}}}\n"), 0600); err != nil {
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
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThing\":{\"variant\":{\"path\":false}}}}\n"), 0600); err != nil {
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
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThing\":{\"variant\":{\"path\":false}}}}\n"), 0600); err != nil {
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
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThing\":{\"variant\":{\"path\":false}}}}\n"), 0600); err != nil {
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
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThingViewed\":{\"variant\":{\"query\":false,\"path\":false}}}}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

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

	sc.SetStreamThingsWs(func(ctx context.Context, p *toy.StreamThingsWsPayload, stream toy.StreamThingsWsServerStream) error {
		_, _ = stream.RecvWithContext(ctx)
		_ = stream.SendWithContext(ctx, &toytypes.ThingEvent{Type: "thing", ID: p.ID})
		return nil
	})
	sc.SetStreamThingsWsSendOnly(func(ctx context.Context, p *toy.StreamThingsWsSendOnlyPayload, stream toy.StreamThingsWsSendOnlyServerStream) error {
		_ = stream.SendWithContext(ctx, &toytypes.ThingEvent{Type: "thing", ID: p.ID})
		return nil
	})

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

func TestPlayback_DynamicScenarioStackEndpoints(t *testing.T) {
	stubRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubRoot, vcrruntime.PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %%v", err)
	}
	store, err := vcrruntime.New(stubRoot)
	if err != nil {
		t.Fatalf("new store: %%v", err)
	}

	body := []byte("{\"id\":\"123\"}\n")
	div := vcrruntime.PathDiversifier(url.Values{"id": []string{"123"}})
	if err := store.WriteStub("GetThing", vcrruntime.RequestSpec{URL: "http://example.com/things/123"}, vcrruntime.ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
	}, body, div); err != nil {
		t.Fatalf("write stub: %%v", err)
	}

	happy := func(next toy.Service) toy.Service {
		s := toyvcr.NewScenario(next)
		s.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
			r, err := next.GetThing(ctx, p)
			if err != nil {
				return nil, err
			}
			r.ID = "happy-" + r.ID
			return r, nil
		})
		return s
	}
	sad := func(next toy.Service) toy.Service {
		s := toyvcr.NewScenario(next)
		s.SetGetThing(func(ctx context.Context, p *toy.GetThingPayload, next toy.Service) (*toy.Thing, error) {
			r, err := next.GetThing(ctx, p)
			if err != nil {
				return nil, err
			}
			r.ID = "sad-" + r.ID
			return r, nil
		})
		return s
	}
	registry := map[string]func(toy.Service) toy.Service{
		"Happy": happy,
		"Sad":   sad,
		"Noop": func(next toy.Service) toy.Service {
			return next
		},
		"Broken": func(next toy.Service) toy.Service {
			return next
		},
	}

	buildPlayback := func(specs []vcrruntime.ScenarioSpec) (http.Handler, error) {
		bg := toyvcr.NewBackground(store)
		layers := make([]func(toy.Service) toy.Service, len(specs))
		for i := range specs {
			if specs[i].Name == "Broken" {
				return nil, fmt.Errorf("broken scenario build")
			}
			layer, ok := registry[specs[i].Name]
			if !ok {
				return nil, fmt.Errorf("unknown scenario %%q", specs[i].Name)
			}
			layers[i] = layer
		}
		svc := toyvcr.Stack(bg, layers...)
		return toyvcr.NewPlaybackHandler(svc)
	}
	ctrl, err := vcrruntime.NewActiveScenarios(
		[]string{"Happy", "Sad", "Noop", "Broken"},
		[]vcrruntime.ScenarioSpec{{Name: "Happy"}, {Name: "Sad"}},
		buildPlayback,
	)
	if err != nil {
		t.Fatalf("new active scenarios: %%v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", ctrl)
	mux.HandleFunc("/__vcr__/scenarios", ctrl.HandleScenarios)
	mux.HandleFunc("/__vcr__/scenarios/active", ctrl.HandleActiveScenarios)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	expectThingID := func(want string) {
		t.Helper()
		res := mustGet(t, srv.URL+"/things/123", nil)
		if res.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			t.Fatalf("unexpected status: %%d body=%%q", res.StatusCode, string(b))
		}
		got := decodeThing(t, res.Body)
		if got.ID != want {
			t.Fatalf("expected id %%q, got %%q", want, got.ID)
		}
	}
	expectThingID("happy-sad-123")

	res := mustGet(t, srv.URL+"/__vcr__/scenarios", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %%d", res.StatusCode)
	}
	var state struct {
		Active []vcrruntime.ScenarioSpec
	}
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatalf("decode scenarios: %%v", err)
	}
	_ = res.Body.Close()
	if len(state.Active) != 2 || state.Active[0].Name != "Happy" || state.Active[1].Name != "Sad" {
		t.Fatalf("unexpected active stack: %%+v", state.Active)
	}

	putRes := mustJSONRequest(t, http.MethodPut, srv.URL+"/__vcr__/scenarios/active", "[{\"name\":\"Sad\"},{\"name\":\"Happy\"}]")
	if putRes.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %%d", putRes.StatusCode)
	}
	_ = putRes.Body.Close()
	expectThingID("sad-happy-123")

	putBgRes := mustJSONRequest(t, http.MethodPut, srv.URL+"/__vcr__/scenarios/active", "[]")
	if putBgRes.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %%d", putBgRes.StatusCode)
	}
	_ = putBgRes.Body.Close()
	expectThingID("123")

	putFailRes := mustJSONRequest(t, http.MethodPut, srv.URL+"/__vcr__/scenarios/active", "[{\"name\":\"Broken\"}]")
	if putFailRes.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: %%d", putFailRes.StatusCode)
	}
	_ = putFailRes.Body.Close()
	expectThingID("123")

	delReq, err := http.NewRequest(http.MethodDelete, srv.URL+"/__vcr__/scenarios/active", nil)
	if err != nil {
		t.Fatalf("new request: %%v", err)
	}
	delRes, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete request: %%v", err)
	}
	if delRes.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %%d", delRes.StatusCode)
	}
	_ = delRes.Body.Close()
	expectThingID("happy-sad-123")
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

func mustJSONRequest(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("new request: %%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
)

func TestVCRCLI_Usage(t *testing.T) {
	code := toyvcr.RunCLI([]string{"help"}, toyvcr.CLIConfig{
		AppName: "toy-vcr",
		ScenarioRegistry: map[string]func(toy.Service) toy.Service{
			"Noop": func(next toy.Service) toy.Service {
				return next
			},
		},
		DefaultPort:      8080,
		DefaultUpstream:  "https://example.com",
		DefaultScenarios: []string{"Noop"},
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
