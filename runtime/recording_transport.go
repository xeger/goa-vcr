package runtime

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"goa.design/clue/log"
)

// RecordingTransport is an http.RoundTripper that proxies to an upstream
// RoundTripper and records JSON 200 OK GET responses into the VCR store, using
// Goa mount points to identify endpoint names.
type RecordingTransport struct {
	ctx     context.Context
	store   *VCR
	matcher *RouteMatcher
	base    http.RoundTripper
}

// NewRecordingTransport creates a recorder transport.
// The final argument is retained for API compatibility and is ignored.
func NewRecordingTransport(ctx context.Context, store *VCR, endpoints []Endpoint, base http.RoundTripper, _ int) *RecordingTransport {
	if ctx == nil {
		ctx = context.Background()
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &RecordingTransport{
		ctx:     ctx,
		store:   store,
		matcher: NewRouteMatcher(endpoints),
		base:    base,
	}
}

func (t *RecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.store == nil || t.matcher == nil {
		return t.base.RoundTrip(req)
	}

	endpointName, vars, ok := t.matcher.Match(req)
	div := ""
	if ok {
		div = RequestDiversifier(t.store.Policy, endpointName, req.URL.Query(), vars)
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	// Record only GET 200 responses for known endpoints.
	if !ok || req.Method != http.MethodGet || resp.StatusCode != http.StatusOK {
		return resp, err
	}

	// Check authorization policy: if claims don't match, skip recording.
	if !t.store.Policy.AllowRecord(req) {
		return resp, err
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, err
	}
	rawBody := body

	// Handle gzip if upstream returned it anyway.
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr == nil {
			body, _ = io.ReadAll(reader)
			_ = reader.Close()
		}
	}

	// Only record JSON bodies.
	if !json.Valid(body) {
		resp.Body = io.NopCloser(bytes.NewReader(rawBody))
		resp.ContentLength = int64(len(rawBody))
		return resp, err
	}

	pretty, mimeType := formatJSONBlob(body, resp.Header)
	resp.Body = io.NopCloser(bytes.NewReader(rawBody))
	resp.ContentLength = int64(len(rawBody))

	ctx := log.With(t.ctx, log.KV{K: "vcr.endpoint.name", V: endpointName})
	if div != "" {
		ctx = log.With(ctx, log.KV{K: "vcr.variant", V: div})
	}

	exists, existsErr := t.store.HasStub(endpointName, div)
	if existsErr != nil {
		log.Error(ctx, existsErr, log.KV{K: "msg", V: "stub exists check failed"})
		return resp, err
	}
	action := "create"
	if exists {
		action = "update"
	}

	if writeErr := t.store.WriteStub(endpointName, RequestSpec{URL: req.URL.String()}, ResponseMeta{
		Status:   resp.StatusCode,
		Headers:  firstHeaderValues(resp.Header),
		MimeType: mimeType,
		Size:     len(pretty),
	}, pretty, div); writeErr != nil {
		log.Error(ctx, writeErr, log.KV{K: "msg", V: "write failed"})
		return resp, err
	}

	log.Info(ctx, log.KV{K: "vcr.action", V: action})
	return resp, err
}

func formatJSONBlob(body []byte, headers http.Header) ([]byte, string) {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		return body, contentType
	}
	pretty.WriteByte('\n')
	return pretty.Bytes(), contentType
}

func firstHeaderValues(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for name, values := range headers {
		if len(values) == 0 {
			continue
		}
		out[name] = values[0]
	}
	return out
}
