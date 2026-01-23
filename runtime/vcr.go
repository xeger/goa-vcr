package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

// PolicyFileName is the name of the VCR policy file.
const PolicyFileName = "vcr.json"

type (
	// Policy represents the on-disk schema for vcr.json.
	Policy struct {
		// Upstream is the base URL of the upstream server, e.g. "https://atlaslive.io"
		Upstream string `json:"upstream"`
		// Endpoints holds per-endpoint policy options keyed by endpoint name.
		Endpoints map[string]EndpointPolicy `json:"endpoints,omitempty"`
	}

	// VCR is the runtime store for VCR stubs. It loads policy from a single root
	// directory and provides centralized stub I/O.
	VCR struct {
		// Root is the storage directory for policy and stubs.
		Root string
		// Policy is loaded from Root.
		Policy Policy
	}

	// Endpoint defines an API endpoint for VCR recording and playback.
	Endpoint struct {
		// Name is the endpoint identifier used for VCR stub filenames.
		Name string `json:"name"`
		// Method is the HTTP method.
		Method string `json:"method"`
		// Pattern is the URL path pattern with Goa-style wildcards.
		Pattern string `json:"pattern"`
	}

	// RequestSpec represents a parsed HTTP request from HAR metadata.
	RequestSpec struct {
		URL  string
		Host string // extracted from URL for validation
	}

	EndpointPolicy struct {
		Variant *VariantPolicy `json:"variant,omitempty"`
	}

	VariantPolicy struct {
		// Query controls whether query strings participate in stub variants.
		// If nil, query variants are enabled and may be auto-tuned by heuristics.
		Query *bool `json:"query,omitempty"`
		// Path controls whether route params participate in stub variants.
		// If nil, path variants are disabled.
		Path *bool `json:"path,omitempty"`
	}
)

// New creates a VCR store from a single root directory.
// The root must contain a vcr.json policy file.
func New(root string) (*VCR, error) {
	if root == "" {
		return nil, fmt.Errorf("empty root directory")
	}
	clean := filepath.Clean(root)
	info, err := os.Stat(clean)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", clean, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", clean)
	}

	policy, err := readPolicy(clean)
	if err != nil {
		return nil, err
	}

	return &VCR{
		Root:   clean,
		Policy: policy,
	}, nil
}

// Host returns the hostname of the upstream server.
func (p Policy) Host() string {
	u, err := url.Parse(p.Upstream)
	if err != nil {
		return ""
	}
	return u.Host
}

// readResult loads a HAR file and unmarshals the corresponding JSON stub into a
// response body type, validates it, then converts it to a service result type.
func readResult[Body any, Result any](
	path string,
	toResult func(*Body) Result,
	validate func(*Body) error,
) (Result, error) {
	var zero Result

	har, err := readHAR(path)
	if err != nil {
		return zero, fmt.Errorf("read har %s: %w", path, err)
	}
	if len(har.Log.Entries) != 1 {
		return zero, fmt.Errorf("har %s must contain exactly one entry", path)
	}

	jsonPath := blobPathForHARPath(path)
	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return zero, fmt.Errorf("open vcr json %s: %w", jsonPath, err)
	}
	defer jsonFile.Close()

	body, err := decodeJSON[Body](jsonFile)
	if err != nil {
		return zero, fmt.Errorf("decode vcr json %s: %w", jsonPath, err)
	}

	if validate != nil {
		if err := validate(&body); err != nil {
			return zero, fmt.Errorf("validate vcr json %s: %w", jsonPath, err)
		}
	}

	return toResult(&body), nil
}

func decodeJSON[Body any](r io.Reader) (Body, error) {
	var zero Body

	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	var body Body
	if err := decoder.Decode(&body); err != nil {
		return zero, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("unexpected extra JSON content")
	}
	return body, nil
}

func readPolicy(dir string) (Policy, error) {
	path := filepath.Join(dir, PolicyFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read %s: %w", path, err)
	}

	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return Policy{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return policy, nil
}

