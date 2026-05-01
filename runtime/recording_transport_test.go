package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type staticRoundTripper struct {
	status  int
	headers http.Header
	body    []byte
}

func (rt staticRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h := make(http.Header, len(rt.headers))
	for k, vs := range rt.headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	return &http.Response{
		StatusCode:    rt.status,
		Header:        h,
		Body:          ioNopCloser{r: bytes.NewReader(rt.body)},
		ContentLength: int64(len(rt.body)),
		Request:       req,
	}, nil
}

// ioNopCloser avoids importing io in this file.
type ioNopCloser struct{ r *bytes.Reader }

func (c ioNopCloser) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c ioNopCloser) Close() error               { return nil }

type countingRoundTripper struct {
	calls int
}

func (rt *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls++
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          ioNopCloser{r: bytes.NewReader([]byte(`{"mutated":true}`))},
		ContentLength: int64(len(`{"mutated":true}`)),
		Request:       req,
	}, nil
}

func TestRecordingTransportBlocksNonGetBeforeUpstream(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	store, err := New(tmp)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	base := &countingRoundTripper{}
	tr := NewRecordingTransport(context.Background(), store, []Endpoint{
		{Name: "UpdateSettings", Method: http.MethodPatch, Pattern: "/settings"},
	}, base, 0)

	resp, err := tr.RoundTrip(mustRequest(t, http.MethodPatch, "http://example.com/settings"))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if base.calls != 0 {
		t.Fatalf("non-GET should not reach upstream, got %d calls", base.calls)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestRecordingTransportDoesNotAutoDisableQueryVariants(t *testing.T) {
	tmp := t.TempDir()
	// Path variant off so this test only exercises query diversifiers.
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\",\"endpoints\":{\"GetThing\":{\"variant\":{\"path\":false}}}}\n"), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	store, err := New(tmp)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	endpoints := []Endpoint{
		{Name: "GetThing", Method: http.MethodGet, Pattern: "/things/{id}"},
	}

	base := staticRoundTripper{
		status:  http.StatusOK,
		headers: http.Header{"Content-Type": []string{"application/json"}},
		body:    []byte(`{"ok":true}`),
	}

	// maxVariants is ignored; recording should keep query variants.
	tr := NewRecordingTransport(context.Background(), store, endpoints, base, 1)

	req1 := mustRequest(t, http.MethodGet, "http://example.com/things/123?a=1")
	_, _ = tr.RoundTrip(req1)

	// After first request, diversified stub should exist.
	div1 := RequestDiversifier(store.Policy, "GetThing", req1.URL.Query(), map[string]string{"id": "123"})
	if div1 == "" {
		t.Fatalf("expected diversifier")
	}
	if ok, _ := store.HasStub("GetThing", div1); !ok {
		t.Fatalf("expected diversified stub after first record")
	}

	req2 := mustRequest(t, http.MethodGet, "http://example.com/things/123?a=2")
	_, _ = tr.RoundTrip(req2)

	// Second request should keep policy unchanged and preserve first diversified stub.
	data, err := os.ReadFile(filepath.Join(tmp, PolicyFileName))
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	enabled, explicit := policy.QueryVariantEnabled("GetThing")
	if !enabled || explicit {
		t.Fatalf("expected QueryVariantEnabled=true explicit=false, got enabled=%v explicit=%v", enabled, explicit)
	}

	if ok, _ := store.HasStub("GetThing", div1); !ok {
		t.Fatalf("expected first diversified stub to remain")
	}

	div2 := RequestDiversifier(store.Policy, "GetThing", req2.URL.Query(), map[string]string{"id": "123"})
	if div2 == "" || div2 == div1 {
		t.Fatalf("expected a distinct second diversifier, got %q", div2)
	}
	if ok, _ := store.HasStub("GetThing", div2); !ok {
		t.Fatalf("expected second diversified stub")
	}

	// Third request should record a third diversified stub as well.
	req3 := mustRequest(t, http.MethodGet, "http://example.com/things/123?a=999")
	_, _ = tr.RoundTrip(req3)
	div3 := RequestDiversifier(store.Policy, "GetThing", req3.URL.Query(), map[string]string{"id": "123"})
	if div3 == "" || div3 == div1 || div3 == div2 {
		t.Fatalf("expected a distinct third diversifier, got %q", div3)
	}
	if ok, _ := store.HasStub("GetThing", div3); !ok {
		t.Fatalf("expected third diversified stub")
	}
}
