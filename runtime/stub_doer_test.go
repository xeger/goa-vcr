package runtime

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	req := mustRequest(t, http.MethodGet, "http://example.com/unknown")
	resp, err := d.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
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

func mustRequest(t *testing.T, method, rawurl string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawurl, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

