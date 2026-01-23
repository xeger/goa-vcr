package runtime

import "testing"

func TestPolicyQueryVariantDefaultEnabled(t *testing.T) {
	enabled, explicit := (Policy{}).QueryVariantEnabled("X")
	if !enabled || explicit {
		t.Fatalf("unexpected: enabled=%v explicit=%v", enabled, explicit)
	}
}

func TestPolicyPathVariantDefaultDisabled(t *testing.T) {
	enabled, explicit := (Policy{}).PathVariantEnabled("X")
	if enabled || explicit {
		t.Fatalf("unexpected: enabled=%v explicit=%v", enabled, explicit)
	}
}

func TestPolicySetAndClearVariantQuery(t *testing.T) {
	var p Policy
	p.SetVariantQuery("E", false)

	enabled, explicit := p.QueryVariantEnabled("E")
	if enabled || !explicit {
		t.Fatalf("unexpected after set: enabled=%v explicit=%v", enabled, explicit)
	}

	p.ClearVariantQuery("E")
	enabled, explicit = p.QueryVariantEnabled("E")
	if !enabled || explicit {
		t.Fatalf("unexpected after clear: enabled=%v explicit=%v", enabled, explicit)
	}
}

