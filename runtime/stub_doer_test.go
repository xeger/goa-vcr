package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goa.design/clue/log"
)

func TestStubDoerUnknownRoute(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	store, err := New(tmp)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	d := NewStubDoer(store, []Endpoint{
		{Name: "Known", Method: http.MethodGet, Pattern: "/known"},
	})
	var out bytes.Buffer
	logCtx := log.Context(context.Background(),
		log.WithOutput(&out),
		log.WithFormat(log.FormatJSON),
		log.WithDisableBuffering(func(context.Context) bool { return true }),
	)

	req := mustRequest(t, http.MethodGet, "http://example.com/unknown").WithContext(logCtx)
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if !strings.Contains(out.String(), `"level":"error"`) {
		t.Fatalf("expected error log, got: %s", out.String())
	}
	if !strings.Contains(out.String(), `"vcr.action":"playback_route_miss"`) {
		t.Fatalf("expected playback_route_miss log action, got: %s", out.String())
	}
}

func TestStubDoerServesStub(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	store, err := New(tmp)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	body := []byte("{\"ok\":true}\n")
	if err := store.WriteStub("Known", RequestSpec{URL: "http://example.com/known"}, ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(body),
	}, body); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	d := NewStubDoer(store, []Endpoint{
		{Name: "Known", Method: http.MethodGet, Pattern: "/known"},
	})
	req := mustRequest(t, http.MethodGet, "http://example.com/known")
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(b) != string(body) {
		t.Fatalf("unexpected body: %q", string(b))
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unexpected content-type: %q", resp.Header.Get("Content-Type"))
	}
}

func TestStubDoerMissingStubLogsError(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	store, err := New(tmp)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	d := NewStubDoer(store, []Endpoint{
		{Name: "Known", Method: http.MethodGet, Pattern: "/known/{id}"},
	})

	var out bytes.Buffer
	logCtx := log.Context(context.Background(),
		log.WithOutput(&out),
		log.WithFormat(log.FormatJSON),
		log.WithDisableBuffering(func(context.Context) bool { return true }),
	)

	req := mustRequest(t, http.MethodGet, "http://example.com/known/123").WithContext(logCtx)
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if !strings.Contains(out.String(), `"level":"error"`) {
		t.Fatalf("expected error log, got: %s", out.String())
	}
	if !strings.Contains(out.String(), `"vcr.action":"playback_stub_miss"`) {
		t.Fatalf("expected playback_stub_miss log action, got: %s", out.String())
	}
}

func TestStubDoerNonGetReturns405(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	store, err := New(tmp)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	d := NewStubDoer(store, []Endpoint{
		{Name: "UpdateSettings", Method: http.MethodPatch, Pattern: "/settings"},
	})

	var out bytes.Buffer
	logCtx := log.Context(context.Background(),
		log.WithOutput(&out),
		log.WithFormat(log.FormatJSON),
		log.WithDisableBuffering(func(context.Context) bool { return true }),
	)

	req := mustRequest(t, http.MethodPatch, "http://example.com/settings").WithContext(logCtx)
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "scenario handler") {
		t.Fatalf("body should mention scenario handler, got: %q", string(body))
	}
	if !strings.Contains(string(body), "UpdateSettings") {
		t.Fatalf("body should mention endpoint name, got: %q", string(body))
	}
	logged := out.String()
	for _, want := range []string{
		`"level":"error"`,
		`"vcr.action":"playback_method_not_allowed"`,
		`"vcr.endpoint.name":"UpdateSettings"`,
		`"http.method":"PATCH"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q: %s", want, logged)
		}
	}
}

func mustRequest(t *testing.T, method, rawurl string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawurl, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}
