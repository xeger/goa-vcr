package runtime

import (
	"net/url"
	"testing"
)

func TestNormalizeValuesStable(t *testing.T) {
	v := url.Values{}
	v.Add("b", "2")
	v.Add("a", "1")
	v.Add("b", "1")

	got := NormalizeValues(v)
	if got != "a=1&b=1&b=2" {
		t.Fatalf("unexpected normalized values: %q", got)
	}
}

func TestRequestDiversifierRespectsPolicyDefaults(t *testing.T) {
	policy := Policy{}

	q := url.Values{}
	q.Add("x", "1")

	// Default: query enabled, path disabled.
	div := RequestDiversifier(policy, "AnyEndpoint", q, map[string]string{"id": "123"})
	if div == "" {
		t.Fatalf("expected non-empty diversifier (query default enabled)")
	}
	if len(div) < 2 || div[0:2] != "q-" {
		t.Fatalf("expected query diversifier prefix, got %q", div)
	}
}

