package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// QueryVariantEnabled returns (enabled, explicit) for endpoints[name].variant.query.
// If explicit is false, enabled defaults to true.
func (p Policy) QueryVariantEnabled(endpointName string) (bool, bool) {
	if p.Endpoints == nil {
		return true, false
	}
	ep, ok := p.Endpoints[endpointName]
	if !ok || ep.Variant == nil || ep.Variant.Query == nil {
		return true, false
	}
	return *ep.Variant.Query, true
}

// PathVariantEnabled returns (enabled, explicit) for endpoints[name].variant.path.
// If explicit is false, enabled defaults to false.
func (p Policy) PathVariantEnabled(endpointName string) (bool, bool) {
	if p.Endpoints == nil {
		return false, false
	}
	ep, ok := p.Endpoints[endpointName]
	if !ok || ep.Variant == nil || ep.Variant.Path == nil {
		return false, false
	}
	return *ep.Variant.Path, true
}

func (p *Policy) SetVariantQuery(endpointName string, enabled bool) {
	if p.Endpoints == nil {
		p.Endpoints = map[string]EndpointPolicy{}
	}
	ep := p.Endpoints[endpointName]
	if ep.Variant == nil {
		ep.Variant = &VariantPolicy{}
	}
	ep.Variant.Query = &enabled
	p.Endpoints[endpointName] = ep
}

func (p *Policy) SetVariantPath(endpointName string, enabled bool) {
	if p.Endpoints == nil {
		p.Endpoints = map[string]EndpointPolicy{}
	}
	ep := p.Endpoints[endpointName]
	if ep.Variant == nil {
		ep.Variant = &VariantPolicy{}
	}
	ep.Variant.Path = &enabled
	p.Endpoints[endpointName] = ep
}

func (p *Policy) ClearVariantQuery(endpointName string) {
	if p.Endpoints == nil {
		return
	}
	ep, ok := p.Endpoints[endpointName]
	if !ok {
		return
	}
	if ep.Variant != nil {
		ep.Variant.Query = nil
	}
	// If nothing remains in the policy for this endpoint, remove it.
	if ep.Variant == nil || (ep.Variant.Query == nil && ep.Variant.Path == nil) {
		delete(p.Endpoints, endpointName)
		if len(p.Endpoints) == 0 {
			p.Endpoints = nil
		}
		return
	}
	p.Endpoints[endpointName] = ep
}

// Validate checks that the policy is valid.
// It ensures that authorization.claims values are JSON scalars only.
func (p Policy) Validate() error {
	if p.Authorization == nil || len(p.Authorization.Claims) == 0 {
		return nil
	}
	for claimName, claimValue := range p.Authorization.Claims {
		if !isJSONScalar(claimValue) {
			return fmt.Errorf("authorization.claims.%s: value must be a JSON scalar (string, number, bool, null), got %T", claimName, claimValue)
		}
	}
	return nil
}

// isJSONScalar returns true if v is a JSON scalar (string, number, bool, null).
func isJSONScalar(v any) bool {
	if v == nil {
		return true
	}
	kind := reflect.TypeOf(v).Kind()
	return kind == reflect.String ||
		kind == reflect.Bool ||
		kind == reflect.Int || kind == reflect.Int8 || kind == reflect.Int16 || kind == reflect.Int32 || kind == reflect.Int64 ||
		kind == reflect.Uint || kind == reflect.Uint8 || kind == reflect.Uint16 || kind == reflect.Uint32 || kind == reflect.Uint64 ||
		kind == reflect.Float32 || kind == reflect.Float64
}

// WritePolicy persists the current policy to Root/vcr.json.
func (v *VCR) WritePolicy() error {
	if v.Root == "" {
		return fmt.Errorf("empty root")
	}
	path := filepath.Join(v.Root, PolicyFileName)
	data, err := json.MarshalIndent(v.Policy, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

