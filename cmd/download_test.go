package cmd

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeFileName(t *testing.T) {
	tests := map[string]string{
		"report.pdf":          "report.pdf",
		"../../etc/passwd":    "passwd",
		`..\..\evil.exe`:      "evil.exe",
		"/abs/path/notes.txt": "notes.txt",
		"..":                  "attachment",
		"":                    "attachment",
	}
	for in, want := range tests {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveResponseAvoidsOverwrite(t *testing.T) {
	dir := t.TempDir()
	resp := func(body, ct string) *http.Response {
		r := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
		r.Header.Set("Content-Type", ct)
		return r
	}

	p1, err := saveResponse(dir, "a.txt", resp("one", "text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := saveResponse(dir, "a.txt", resp("two", "text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if p1 != filepath.Join(dir, "a.txt") || p2 != filepath.Join(dir, "a-1.txt") {
		t.Errorf("paths = %s, %s", p1, p2)
	}
	if b, _ := os.ReadFile(p1); string(b) != "one" {
		t.Errorf("first file overwritten: %q", b)
	}

	p3, err := saveResponse(dir, "image-1-1", resp("png", "image/png"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p3) != "image-1-1.png" {
		t.Errorf("inline image saved as %s, want image-1-1.png", filepath.Base(p3))
	}
}
