package runtime

import (
	"net/url"
	"strings"
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

	// Default: query and path enabled — path part first, then query.
	div := RequestDiversifier(policy, "AnyEndpoint", q, map[string]string{"id": "123"})
	if div == "" {
		t.Fatalf("expected non-empty diversifier")
	}
	if !strings.HasPrefix(div, "p-") || !strings.Contains(div, "--q-") {
		t.Fatalf("expected path then query diversifier, got %q", div)
	}
}
