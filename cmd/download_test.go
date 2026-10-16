package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
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

func existingFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUniquePathEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		in       string
		want     string
	}{
		{"free name unchanged", nil, "a.txt", "a.txt"},
		{"no extension", []string{"README"}, "README", "README-1"},
		{"no extension several", []string{"README", "README-1"}, "README", "README-2"},
		{"several collisions", []string{"a.txt", "a-1.txt", "a-2.txt"}, "a.txt", "a-3.txt"},
		{"first gap is used", []string{"a.txt", "a-2.txt"}, "a.txt", "a-1.txt"},
		{"double extension splits at last dot", []string{"archive.tar.gz"}, "archive.tar.gz", "archive.tar-1.gz"},
		{"directory counts as collision", []string{"d"}, "d", "d-1"},
		{"unicode", []string{"отчёт.pdf"}, "отчёт.pdf", "отчёт-1.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			existingFiles(t, dir, tt.existing...)
			if got := uniquePath(dir, tt.in); got != filepath.Join(dir, tt.want) {
				t.Errorf("uniquePath(%q) = %q, want %q", tt.in, filepath.Base(got), tt.want)
			}
		})
	}
}

// A dotfile has no extension: the counter must go after the whole name.
func TestUniquePathDotfile(t *testing.T) {
	dir := t.TempDir()
	existingFiles(t, dir, ".bashrc")
	got := filepath.Base(uniquePath(dir, ".bashrc"))
	if got != ".bashrc-1" {
		t.Errorf("uniquePath(.bashrc) = %q, want %q", got, ".bashrc-1")
	}
}

func TestSafeFileNameEdgeCases(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"windows drive path", `C:\Users\x\report.pdf`, "report.pdf"},
		{"mixed separators", `a/b\c/d.txt`, "d.txt"},
		{"trailing slash", "dir/", "dir"},
		{"trailing backslash", `dir\`, "dir"},
		{"dot", ".", "attachment"},
		{"dot slash", "./", "attachment"},
		{"slash only", "/", "attachment"},
		{"double slash", "//", "attachment"},
		{"dotdot slash", "../", "attachment"},
		{"three dots is a normal name", "...", "..."},
		{"dotfile kept", ".bashrc", ".bashrc"},
		// Documented: a name of only spaces is not rejected.
		{"only spaces kept", "   ", "   "},
		{"unicode unchanged", "отчёт 📎 日本語.pdf", "отчёт 📎 日本語.pdf"},
		{"spaces inside kept", "my file (1).txt", "my file (1).txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeFileName(tt.in); got != tt.want {
				t.Errorf("safeFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error { c.closed = true; return nil }

func testResp(body io.ReadCloser, ct string) *http.Response {
	r := &http.Response{Header: make(http.Header), Body: body}
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	return r
}

func TestSaveResponseNaming(t *testing.T) {
	tests := []struct {
		name, in, ct, want string
	}{
		{"png from content type", "pic", "image/png", "pic.png"},
		{"jpeg with params", "pic", "image/jpeg; charset=x", "pic.jpg"},
		{"gif", "pic", "image/gif", "pic.gif"},
		{"unknown type adds nothing", "blob", "application/x-tcli-no-such-type", "blob"},
		{"no content type adds nothing", "blob", "", "blob"},
		{"malformed content type adds nothing", "blob", "///", "blob"},
		{"existing extension untouched", "pic.dat", "image/png", "pic.dat"},
		{"dotfile counts as having an extension", ".hidden", "image/png", ".hidden"},
		{"path stripped before saving", "../../x.txt", "text/plain", "x.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p, err := saveResponse(dir, tt.in, testResp(io.NopCloser(strings.NewReader("d")), tt.ct))
			if err != nil {
				t.Fatal(err)
			}
			if p != filepath.Join(dir, tt.want) {
				t.Errorf("saved as %q, want %q", filepath.Base(p), tt.want)
			}
		})
	}
}

// An attachment without a name falls back to "attachment"; the extension from
// the Content-Type should not turn it into a hidden file named ".png".
func TestSaveResponseEmptyNameFallsBack(t *testing.T) {
	dir := t.TempDir()
	p, err := saveResponse(dir, "", testResp(io.NopCloser(strings.NewReader("d")), "image/png"))
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(p); got != "attachment.png" {
		t.Errorf("empty name saved as %q, want %q", got, "attachment.png")
	}
}

func TestSaveResponseContentAndClose(t *testing.T) {
	body := make([]byte, 200_000)
	for i := range body {
		body[i] = byte(i * 7)
	}
	tr := &closeTracker{Reader: bytes.NewReader(body)}
	dir := t.TempDir()
	p, err := saveResponse(dir, "bin.dat", testResp(tr, "application/octet-stream"))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.closed {
		t.Error("body not closed after success")
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, body) {
		t.Errorf("content differs: got %d bytes, want %d", len(got), len(body))
	}

	// Empty body still creates an empty file.
	p, err = saveResponse(dir, "empty.txt", testResp(io.NopCloser(strings.NewReader("")), ""))
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Size() != 0 {
		t.Errorf("empty file: %v, %v", fi, err)
	}
}

func TestSaveResponseErrors(t *testing.T) {
	t.Run("read error removes partial file and closes body", func(t *testing.T) {
		dir := t.TempDir()
		tr := &closeTracker{Reader: io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(errors.New("boom")))}
		if _, err := saveResponse(dir, "a.txt", testResp(tr, "")); err == nil {
			t.Fatal("want error")
		}
		if !tr.closed {
			t.Error("body not closed after error")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("partial file left behind: %v", entries)
		}
	})
	t.Run("missing directory closes body", func(t *testing.T) {
		tr := &closeTracker{Reader: strings.NewReader("x")}
		if _, err := saveResponse(filepath.Join(t.TempDir(), "nope"), "a.txt", testResp(tr, "")); err == nil {
			t.Fatal("want error")
		}
		if !tr.closed {
			t.Error("body not closed after error")
		}
	})
}

func TestExtensionFor(t *testing.T) {
	// Only hard-coded types and invalid input: the rest depends on the
	// system mime database.
	tests := map[string]string{
		"image/jpeg":                ".jpg",
		"image/png":                 ".png",
		"image/gif":                 ".gif",
		"IMAGE/PNG":                 ".png",
		"image/jpeg; charset=x":     ".jpg",
		`image/png; name="a b.png"`: ".png",
		"":                          "",
		"///":                       "",
		"not a media type":          "",
		"application/x-tcli-nope":   "",
	}
	for in, want := range tests {
		if got := extensionFor(in); got != want {
			t.Errorf("extensionFor(%q) = %q, want %q", in, got, want)
		}
	}
}
