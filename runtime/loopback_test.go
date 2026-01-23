package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackMiddlewareMarksContext(t *testing.T) {
	var got bool
	h := LoopbackMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = IsLoopback(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set(LoopbackHeader, "1")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if !got {
		t.Fatalf("expected loopback context")
	}
}

