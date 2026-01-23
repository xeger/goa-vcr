package runtime

import (
	"bytes"
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

func TestRecordingTransportVariantHeuristicDisablesQuery(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte("{\"upstream\":\"https://example.com\"}\n"), 0600); err != nil {
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

	// maxVariants=1 means the 2nd distinct query diversifier triggers the heuristic.
	tr := NewRecordingTransport(nil, store, endpoints, base, 1)

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

	// Second request should trigger heuristic: policy persisted and stubs deleted.
	data, err := os.ReadFile(filepath.Join(tmp, PolicyFileName))
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	enabled, explicit := policy.QueryVariantEnabled("GetThing")
	if enabled || !explicit {
		t.Fatalf("expected QueryVariantEnabled=false explicit=true, got enabled=%v explicit=%v", enabled, explicit)
	}

	// All existing stubs for endpoint should have been deleted.
	if ok, _ := store.HasStub("GetThing", div1); ok {
		t.Fatalf("expected diversified stub deleted")
	}

	// Third request should record undiversified stub (div == "").
	req3 := mustRequest(t, http.MethodGet, "http://example.com/things/123?a=999")
	_, _ = tr.RoundTrip(req3)
	if ok, _ := store.HasStub("GetThing"); !ok {
		t.Fatalf("expected undiversified stub after heuristic trigger")
	}
}

