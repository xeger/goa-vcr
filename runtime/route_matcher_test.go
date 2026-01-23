package runtime

import (
	"net/http"
	"testing"
)

func TestRouteMatcherMatch(t *testing.T) {
	rm := NewRouteMatcher([]Endpoint{
		{Name: "GetThing", Method: http.MethodGet, Pattern: "/things/{id}"},
	})
	req, err := http.NewRequest(http.MethodGet, "http://example.com/things/123", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	name, vars, ok := rm.Match(req)
	if !ok {
		t.Fatalf("expected match")
	}
	if name != "GetThing" {
		t.Fatalf("unexpected name: %q", name)
	}
	if vars["id"] != "123" {
		t.Fatalf("unexpected vars: %#v", vars)
	}
}

