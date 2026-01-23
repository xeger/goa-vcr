package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadHARBlob(t *testing.T) {
	tmpDir := t.TempDir()
	harPath := filepath.Join(tmpDir, "Example.vcr.har")
	blobPath := blobPathForHARPath(harPath)

	blob := []byte(`{"foo":"bar"}`)
	if err := os.WriteFile(blobPath, blob, 0600); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	if err := writeStub(harPath, RequestSpec{URL: "https://example.com/api"}, ResponseMeta{
		Status:   200,
		MimeType: "application/json",
		Size:     len(blob),
	}); err != nil {
		t.Fatalf("write har: %v", err)
	}

	type Body struct {
		Foo string `json:"foo"`
	}

	result, err := readResult(harPath, func(b *Body) *Body { return b }, nil)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if result.Foo != "bar" {
		t.Fatalf("unexpected result: %q", result.Foo)
	}
}

