package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type har struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version,omitempty"`
	Creator harCreator `json:"creator,omitempty"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type harEntry struct {
	Request  harRequest  `json:"request"`
	Response harResponse `json:"response"`
}

type harRequest struct {
	URL string `json:"url"`
}

type harResponse struct {
	Status     int            `json:"status"`
	StatusText string         `json:"statusText,omitempty"`
	Headers    []harNameValue `json:"headers,omitempty"`
	Content    harContent     `json:"content"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// stub represents a parsed VCR stub file and its blob on disk.
type stub struct {
	HARPath  string
	BlobPath string
	Request  RequestSpec
	Response ResponseMeta
}

// ResponseMeta describes the response metadata persisted in the HAR file.
type ResponseMeta struct {
	Status   int
	Headers  map[string]string
	MimeType string
	Size     int
}

// readStub loads a HAR file from disk and returns a parsed stub.
func readStub(harPath string) (*stub, error) {
	archive, err := readHAR(harPath)
	if err != nil {
		return nil, err
	}
	entry, err := singleEntry(archive)
	if err != nil {
		return nil, err
	}
	req := RequestSpec{
		URL: entry.Request.URL,
	}
	if u, err := url.Parse(entry.Request.URL); err == nil {
		req.Host = u.Host
	}
	return &stub{
		HARPath:  harPath,
		BlobPath: blobPathForHARPath(harPath),
		Request:  req,
		Response: responseMetaFromHAR(entry.Response),
	}, nil
}

// writeStub writes a HAR file with the provided request/response metadata.
func writeStub(harPath string, req RequestSpec, resp ResponseMeta) error {
	archive := &har{
		Log: harLog{
			Version: "1.2",
			Creator: harCreator{Name: "goa-vcr"},
			Entries: []harEntry{
				{
					Request: harRequest{
						URL: req.URL,
					},
					Response: harResponse{
						Status:     resp.Status,
						StatusText: httpStatusText(resp.Status),
						Headers:    headersToNameValues(resp.Headers),
						Content: harContent{
							MimeType: resp.MimeType,
							Size:     resp.Size,
						},
					},
				},
			},
		},
	}
	return writeHAR(harPath, archive)
}

func readHAR(path string) (*har, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var archive har
	if err := json.Unmarshal(data, &archive); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &archive, nil
}

func writeHAR(path string, archive *har) error {
	data, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func blobPathForHARPath(harPath string) string {
	if strings.HasSuffix(harPath, ".vcr.har") {
		return strings.TrimSuffix(harPath, ".vcr.har") + ".vcr.json"
	}
	return harPath + ".blob"
}

func responseMetaFromHAR(resp harResponse) ResponseMeta {
	headers := make(map[string]string, len(resp.Headers))
	for _, header := range resp.Headers {
		headers[header.Name] = header.Value
	}
	return ResponseMeta{
		Status:   resp.Status,
		Headers:  headers,
		MimeType: resp.Content.MimeType,
		Size:     resp.Content.Size,
	}
}

func headersToNameValues(headers map[string]string) []harNameValue {
	if len(headers) == 0 {
		return nil
	}
	pairs := make([]harNameValue, 0, len(headers))
	for name, value := range headers {
		pairs = append(pairs, harNameValue{Name: name, Value: value})
	}
	return pairs
}

func singleEntry(archive *har) (*harEntry, error) {
	if archive == nil {
		return nil, fmt.Errorf("har is nil")
	}
	if len(archive.Log.Entries) != 1 {
		return nil, fmt.Errorf("har must contain exactly one entry")
	}
	return &archive.Log.Entries[0], nil
}

func httpStatusText(code int) string {
	if code == 0 {
		return ""
	}
	return http.StatusText(code)
}

