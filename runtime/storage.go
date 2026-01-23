package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HasStub reports whether a stub exists for the endpoint and optional diversifier.
func (v *VCR) HasStub(endpointName string, diversifier ...string) (bool, error) {
	div, err := diversifierFromArgs(diversifier)
	if err != nil {
		return false, err
	}
	_, err = v.findStub(endpointName, div)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// ReadRequest resolves a stub by endpoint name and optional diversifier and returns the request metadata.
func (v *VCR) ReadRequest(endpointName string, diversifier ...string) (RequestSpec, error) {
	div, err := diversifierFromArgs(diversifier)
	if err != nil {
		return RequestSpec{}, err
	}
	stub, err := v.findStub(endpointName, div)
	if err != nil {
		return RequestSpec{}, err
	}
	return stub.Request, nil
}

// ReadResponse resolves a stub by endpoint name and optional diversifier and returns the response metadata and JSON body.
func (v *VCR) ReadResponse(endpointName string, diversifier ...string) (ResponseMeta, []byte, error) {
	div, err := diversifierFromArgs(diversifier)
	if err != nil {
		return ResponseMeta{}, nil, err
	}
	stub, err := v.findStub(endpointName, div)
	if err != nil {
		return ResponseMeta{}, nil, err
	}
	body, err := v.readStubBody(stub)
	if err != nil {
		return ResponseMeta{}, nil, err
	}
	return stub.Response, body, nil
}

// WriteStub writes a stub (HAR + JSON) into the root with an optional diversifier.
func (v *VCR) WriteStub(endpointName string, req RequestSpec, resp ResponseMeta, body []byte, diversifier ...string) error {
	div, err := diversifierFromArgs(diversifier)
	if err != nil {
		return err
	}
	root := v.writeRoot()
	if root == "" {
		return fmt.Errorf("no write root configured")
	}

	harPath := filepath.Join(root, stubKey(endpointName, div)+".vcr.har")
	jsonPath := blobPathForHARPath(harPath)

	if err := os.WriteFile(jsonPath, body, 0600); err != nil {
		return fmt.Errorf("write %s: %w", jsonPath, err)
	}
	if err := writeStub(harPath, req, resp); err != nil {
		return err
	}
	return nil
}

func (v *VCR) findStub(endpointName string, diversifier string) (*stub, error) {
	if v.Root == "" {
		return nil, os.ErrNotExist
	}
	harPath := filepath.Join(v.Root, stubKey(endpointName, diversifier)+".vcr.har")
	stub, err := readStub(harPath)
	if err == nil {
		return stub, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	return nil, err
}

func (v *VCR) readStubBody(stub *stub) ([]byte, error) {
	return os.ReadFile(stub.BlobPath)
}

func (v *VCR) writeRoot() string {
	return v.Root
}

func diversifierFromArgs(args []string) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("expected 0 or 1 diversifiers, got %d", len(args))
	}
}

func stubKey(endpointName, diversifier string) string {
	if diversifier == "" {
		return endpointName
	}
	return strings.Join([]string{endpointName, diversifier}, "--")
}

