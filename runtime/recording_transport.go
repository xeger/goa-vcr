package runtime

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

	mu           sync.Mutex
	maxVariants  int
	variantsSeen map[string]map[string]struct{}
}

func NewRecordingTransport(ctx context.Context, store *VCR, endpoints []Endpoint, base http.RoundTripper, maxVariants int) *RecordingTransport {
	if ctx == nil {
		ctx = context.Background()
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &RecordingTransport{
		ctx:          ctx,
		store:        store,
		matcher:      NewRouteMatcher(endpoints),
		base:         base,
		maxVariants:  maxVariants,
		variantsSeen: map[string]map[string]struct{}{},
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

	// If policy/query options are implicit and we exceed max variants, flip policy and delete stubs.
	if div != "" {
		if _, explicit := t.store.Policy.QueryVariantEnabled(endpointName); !explicit {
			if triggered := t.observeVariantAndMaybeDisableQuery(endpointName, div); triggered {
				log.Warn(log.With(t.ctx,
					log.KV{K: "vcr.endpoint.name", V: endpointName},
					log.KV{K: "vcr.variant", V: div},
				),
					log.KV{K: "vcr.action", V: "heuristic"},
					log.KV{K: "vcr.heuristic", V: "variant.query"},
					log.KV{K: "vcr.max_variants", V: t.maxVariants},
					log.KV{K: "msg", V: "too many query variants; auto-setting endpoints.<name>.variant.query=false and deleting existing stubs"},
				)
				return resp, err // wait for next call to record undiversified stub
			}
		}
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

func (t *RecordingTransport) observeVariantAndMaybeDisableQuery(endpointName, diversifier string) bool {
	if t.maxVariants <= 0 {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Ignore heuristic if user explicitly set variant.query (true or false).
	if _, explicit := t.store.Policy.QueryVariantEnabled(endpointName); explicit {
		return false
	}

	seen := t.variantsSeen[endpointName]
	if seen == nil {
		seen = map[string]struct{}{}
		t.variantsSeen[endpointName] = seen
	}
	seen[diversifier] = struct{}{}
	if len(seen) <= t.maxVariants {
		return false
	}

	t.store.Policy.SetVariantQuery(endpointName, false)
	if err := t.store.WritePolicy(); err != nil {
		log.Error(t.ctx, err, log.KV{K: "msg", V: "failed to persist policy update"})
		t.store.Policy.ClearVariantQuery(endpointName)
		return false
	}

	t.deleteEndpointStubs(endpointName)
	delete(t.variantsSeen, endpointName)
	return true
}

func (t *RecordingTransport) deleteEndpointStubs(endpointName string) {
	entries, err := os.ReadDir(t.store.Root)
	if err != nil {
		log.Error(t.ctx, err, log.KV{K: "msg", V: "failed to read stub dir for deletion"})
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == PolicyFileName {
			continue
		}
		if !strings.HasSuffix(name, ".vcr.har") && !strings.HasSuffix(name, ".vcr.json") {
			continue
		}
		if name == endpointName+".vcr.har" || name == endpointName+".vcr.json" || strings.HasPrefix(name, endpointName+"--") {
			_ = os.Remove(filepath.Join(t.store.Root, name))
		}
	}
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

