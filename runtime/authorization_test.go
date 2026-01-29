package runtime

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyAllowRecord_NoAuthorizationPolicy(t *testing.T) {
	policy := Policy{}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when no authorization policy")
	}
}

func TestPolicyAllowRecord_NoAuthorizationHeader(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when no Authorization header (Option A)")
	}
}

func TestPolicyAllowRecord_BearerTokenClaimsMatch(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"sub": "deadbeef"}))
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when claims match")
	}
}

func TestPolicyAllowRecord_BearerTokenClaimsMismatch(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"sub": "different"}))
	if policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=false when claims mismatch")
	}
}

func TestPolicyAllowRecord_BearerTokenMissingClaim(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"aud": "something"}))
	if policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=false when required claim missing")
	}
}

func TestPolicyAllowRecord_BearerTokenMalformed(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	if policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=false when token is malformed")
	}
}

func TestPolicyAllowRecord_BearerTokenInvalidBase64(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer header.invalid-base64.signature")
	if policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=false when token has invalid base64")
	}
}

func TestPolicyAllowRecord_BearerTokenInvalidJSON(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	// Create a token with valid base64 but invalid JSON payload
	invalidPayload := base64.URLEncoding.EncodeToString([]byte("not json"))
	req.Header.Set("Authorization", "Bearer header."+invalidPayload+".signature")
	if policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=false when token payload is invalid JSON")
	}
}

func TestPolicyAllowRecord_MultipleClaims(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{
				"sub": "deadbeef",
				"aud": "myapp",
			},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{
		"sub": "deadbeef",
		"aud": "myapp",
	}))
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when all claims match")
	}
}

func TestPolicyAllowRecord_MultipleClaimsOneMismatch(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{
				"sub": "deadbeef",
				"aud": "myapp",
			},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{
		"sub": "deadbeef",
		"aud": "wrongapp",
	}))
	if policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=false when one claim mismatches")
	}
}

func TestPolicyAllowRecord_NumberClaim(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"exp": float64(1234567890)},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"exp": float64(1234567890)}))
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when numeric claim matches")
	}
}

func TestPolicyAllowRecord_BoolClaim(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"admin": true},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"admin": true}))
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when bool claim matches")
	}
}

func TestPolicyAllowRecord_NotBearerToken(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when Authorization header is not Bearer")
	}
}

func TestPolicyAllowRecord_CaseInsensitiveBearer(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{"sub": "deadbeef"},
		},
	}
	req := mustRequest(t, http.MethodGet, "http://example.com/test")
	req.Header.Set("Authorization", "bearer "+makeJWT(t, map[string]any{"sub": "deadbeef"}))
	if !policy.AllowRecord(req) {
		t.Fatalf("expected AllowRecord=true when Bearer prefix is lowercase")
	}
}

func TestPolicyValidate_ValidScalarClaims(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{
				"sub": "deadbeef",
				"exp": 1234567890,
				"admin": true,
				"nullval": nil,
			},
		},
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestPolicyValidate_InvalidNonScalarClaim(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{
				"sub": "deadbeef",
				"roles": []string{"admin", "user"}, // array is not a scalar
			},
		},
	}
	if err := policy.Validate(); err == nil {
		t.Fatalf("expected validation error for non-scalar claim")
	}
}

func TestPolicyValidate_InvalidObjectClaim(t *testing.T) {
	policy := Policy{
		Authorization: &AuthorizationPolicy{
			Claims: map[string]any{
				"sub": "deadbeef",
				"metadata": map[string]any{"key": "value"}, // object is not a scalar
			},
		},
	}
	if err := policy.Validate(); err == nil {
		t.Fatalf("expected validation error for object claim")
	}
}

func TestRecordingTransport_AuthorizationGate(t *testing.T) {
	tmp := t.TempDir()
	policyJSON := `{
		"upstream": "https://example.com",
		"authorization": {
			"claims": {
				"sub": "deadbeef"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmp, PolicyFileName), []byte(policyJSON), 0600); err != nil {
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

	tr := NewRecordingTransport(nil, store, endpoints, base, 5)

	// Test 1: No Authorization header -> should record (Option A)
	req1 := mustRequest(t, http.MethodGet, "http://example.com/things/123")
	_, _ = tr.RoundTrip(req1)
	if ok, _ := store.HasStub("GetThing"); !ok {
		t.Fatalf("expected stub recorded when no Authorization header")
	}

	// Clean up
	_ = os.Remove(filepath.Join(tmp, "GetThing.vcr.har"))
	_ = os.Remove(filepath.Join(tmp, "GetThing.vcr.json"))

	// Test 2: Bearer token with matching claims -> should record
	req2 := mustRequest(t, http.MethodGet, "http://example.com/things/123")
	req2.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"sub": "deadbeef"}))
	_, _ = tr.RoundTrip(req2)
	if ok, _ := store.HasStub("GetThing"); !ok {
		t.Fatalf("expected stub recorded when claims match")
	}

	// Clean up
	_ = os.Remove(filepath.Join(tmp, "GetThing.vcr.har"))
	_ = os.Remove(filepath.Join(tmp, "GetThing.vcr.json"))

	// Test 3: Bearer token with mismatched claims -> should NOT record
	req3 := mustRequest(t, http.MethodGet, "http://example.com/things/123")
	req3.Header.Set("Authorization", "Bearer "+makeJWT(t, map[string]any{"sub": "different"}))
	_, _ = tr.RoundTrip(req3)
	if ok, _ := store.HasStub("GetThing"); ok {
		t.Fatalf("expected stub NOT recorded when claims mismatch")
	}
}

// makeJWT creates an unsigned JWT token with the given claims.
// Format: base64url(header).base64url(payload).empty
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "none", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	return headerB64 + "." + claimsB64 + "."
}
