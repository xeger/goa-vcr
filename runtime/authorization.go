package runtime

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
)

// AllowRecord checks if a request should be recorded based on authorization policy.
// Returns true if recording should proceed, false if it should be skipped.
// If policy has no authorization.claims configured, returns true.
// If no Authorization: Bearer header is present, returns true (Option A behavior).
// If Authorization: Bearer header is present but token is malformed/unparseable, returns false.
// If claims don't match, returns false.
func (p Policy) AllowRecord(req *http.Request) bool {
	if p.Authorization == nil || len(p.Authorization.Claims) == 0 {
		return true
	}

	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		return true // Option A: no header means allow recording
	}

	// Extract bearer token
	bearerPrefix := "Bearer "
	if !strings.HasPrefix(strings.ToLower(authHeader), strings.ToLower(bearerPrefix)) {
		return true // Not a bearer token, allow recording
	}

	token := strings.TrimSpace(authHeader[len(bearerPrefix):])
	if token == "" {
		return true // Empty bearer token, allow recording
	}

	// Decode JWT payload (2nd segment)
	claims, err := decodeJWTClaims(token)
	if err != nil {
		return false // Malformed token, skip recording
	}

	// Check that all required claims match
	for claimName, requiredValue := range p.Authorization.Claims {
		actualValue, ok := claims[claimName]
		if !ok {
			return false // Required claim missing
		}

		// Normalize values for comparison (handle JSON number -> float64 conversion)
		if !claimsMatch(requiredValue, actualValue) {
			return false // Claim value mismatch
		}
	}

	return true
}

// decodeJWTClaims extracts and decodes the JWT payload (claims) from a JWT token.
// Returns the claims as a map[string]any, or an error if the token is malformed.
// No signature verification is performed.
func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, &jwtError{msg: "JWT must have at least header and payload segments"}
	}

	// Decode payload (2nd segment) using base64url raw encoding
	payload := parts[1]
	// Add padding if needed for base64 decoding
	if pad := len(payload) % 4; pad != 0 {
		payload += strings.Repeat("=", 4-pad)
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, &jwtError{msg: "failed to base64url decode payload", cause: err}
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, &jwtError{msg: "failed to unmarshal JWT payload as JSON", cause: err}
	}

	return claims, nil
}

// claimsMatch compares a required claim value with an actual claim value.
// Handles JSON number type conversions (e.g., int -> float64).
func claimsMatch(required, actual any) bool {
	// Direct equality check first
	if reflect.DeepEqual(required, actual) {
		return true
	}

	// Handle JSON number conversion: JSON unmarshals numbers as float64,
	// but policy might have int values. Compare numerically.
	if reqNum, ok := numberValue(required); ok {
		if actNum, ok := numberValue(actual); ok {
			return reqNum == actNum
		}
	}

	return false
}

// numberValue extracts a float64 from a numeric value, handling int/float conversions.
func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

type jwtError struct {
	msg   string
	cause error
}

func (e *jwtError) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

func (e *jwtError) Unwrap() error {
	return e.cause
}
