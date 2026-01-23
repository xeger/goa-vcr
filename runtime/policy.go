package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

