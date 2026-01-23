package runtime

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// StubDoer serves HTTP responses from VCR stubs by matching requests against a
// set of Goa mount points.
type StubDoer struct {
	Store   *VCR
	Matcher *RouteMatcher
}

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
		return vcrErrorResponse(req, http.StatusNotImplemented, "vcr: unstubbed endpoint"), nil
	}

	div := RequestDiversifier(d.Store.Policy, endpointName, req.URL.Query(), vars)
	meta, body, err := d.Store.ReadResponse(endpointName, div)
	if err != nil {
		if os.IsNotExist(err) {
			return vcrErrorResponse(req, http.StatusNotImplemented, "vcr: unstubbed endpoint"), nil
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

