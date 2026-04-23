package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"goa.design/clue/log"
)

// StubDoer serves HTTP responses from VCR stubs by matching requests against a
// set of Goa mount points.
type StubDoer struct {
	Store   *VCR
	Matcher *RouteMatcher
}

var (
	errPlaybackRouteMiss = errors.New("vcr: playback route miss")
	errPlaybackStubMiss  = errors.New("vcr: playback stub miss")
)

func NewStubDoer(store *VCR, endpoints []Endpoint) *StubDoer {
	return &StubDoer{
		Store:   store,
		Matcher: NewRouteMatcher(endpoints),
	}
}

func (d *StubDoer) Do(req *http.Request) (*http.Response, error) {
	if d == nil || d.Store == nil || d.Matcher == nil || req == nil {
		return vcrErrorResponse(req, http.StatusInternalServerError, "vcr: invalid stub doer"), nil
	}

	endpointName, vars, ok := d.Matcher.Match(req)
	if !ok {
		log.Error(playbackLogContext(req), errPlaybackRouteMiss,
			log.KV{K: "vcr.action", V: "playback_route_miss"},
			log.KV{K: "http.method", V: req.Method},
			log.KV{K: "http.path", V: req.URL.Path},
			log.KV{K: "msg", V: "playback request did not match any configured route"},
		)
		return vcrErrorResponse(req, http.StatusNotImplemented, fmt.Sprintf("vcr: unrecognized route: %s %s", req.Method, req.URL.Path)), nil
	}

	div := RequestDiversifier(d.Store.Policy, endpointName, req.URL.Query(), vars)
	meta, body, err := d.Store.ReadResponse(endpointName, div)
	if err != nil {
		if os.IsNotExist(err) {
			stubFile := StubHARFileName(endpointName, div)
			log.Error(playbackLogContext(req,
				log.KV{K: "vcr.endpoint.name", V: endpointName},
				log.KV{K: "vcr.stub.file", V: stubFile},
			), errPlaybackStubMiss,
				log.KV{K: "vcr.action", V: "playback_stub_miss"},
				log.KV{K: "msg", V: "playback request matched route but stub file was missing"},
			)
			return vcrErrorResponse(req, http.StatusNotImplemented, fmt.Sprintf("vcr: unstubbed endpoint: missing %s", stubFile)), nil
		}
		return vcrErrorResponse(req, http.StatusInternalServerError, "vcr: failed to read stub"), nil
	}

	status := meta.Status
	if status == 0 {
		status = http.StatusOK
	}

	h := make(http.Header, len(meta.Headers)+2)
	for k, v := range meta.Headers {
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		h.Set(k, v)
	}
	if h.Get("Content-Type") == "" && meta.MimeType != "" {
		h.Set("Content-Type", meta.MimeType)
	}

	h.Set("Content-Length", strconv.Itoa(len(body)))
	return &http.Response{
		StatusCode:    status,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}

func playbackLogContext(req *http.Request, keyvals ...log.Fielder) context.Context {
	ctx := context.Background()
	if req != nil {
		ctx = req.Context()
	}
	if len(keyvals) == 0 {
		return ctx
	}
	return log.With(ctx, keyvals...)
}

func vcrErrorResponse(req *http.Request, status int, msg string) *http.Response {
	b := []byte(msg)
	h := make(http.Header, 2)
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	return &http.Response{
		StatusCode:    status,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
		Request:       req,
	}
}
