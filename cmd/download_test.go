package cmd

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
	"unicode/utf8"
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

	p1, err := saveResponse(dir, "a.txt", false, resp("one", "text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := saveResponse(dir, "a.txt", false, resp("two", "text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if p1 != filepath.Join(dir, "a.txt") || p2 != filepath.Join(dir, "a-1.txt") {
		t.Errorf("paths = %s, %s", p1, p2)
	}
	if b, _ := os.ReadFile(p1); string(b) != "one" {
		t.Errorf("first file overwritten: %q", b)
	}

	p3, err := saveResponse(dir, "image-1-1", true, resp("png", "image/png"))
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

// createdName creates a file with createUnique, closes it and returns its
// base name.
func createdName(t *testing.T, dir, name string) string {
	t.Helper()
	f, err := createUnique(dir, name)
	if err != nil {
		t.Fatalf("createUnique(%q): %v", name, err)
	}
	base := filepath.Base(f.Name())
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestCreateUniqueEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		existing []string // regular files
		dirs     []string // directories
		in       string
		want     string
	}{
		{"free name unchanged", nil, nil, "a.txt", "a.txt"},
		{"no extension", []string{"README"}, nil, "README", "README-1"},
		{"no extension several", []string{"README", "README-1"}, nil, "README", "README-2"},
		{"several collisions", []string{"a.txt", "a-1.txt", "a-2.txt"}, nil, "a.txt", "a-3.txt"},
		{"first gap is used", []string{"a.txt", "a-2.txt"}, nil, "a.txt", "a-1.txt"},
		{"double extension splits at last dot", []string{"archive.tar.gz"}, nil, "archive.tar.gz", "archive.tar-1.gz"},
		{"directory counts as collision", nil, []string{"d"}, "d", "d-1"},
		{"unicode", []string{"отчёт.pdf"}, nil, "отчёт.pdf", "отчёт-1.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			existingFiles(t, dir, tt.existing...)
			for _, d := range tt.dirs {
				if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := createdName(t, dir, tt.in); got != tt.want {
				t.Errorf("createUnique(%q) created %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A dotfile has no extension: the counter must go after the whole name.
func TestCreateUniqueDotfile(t *testing.T) {
	dir := t.TempDir()
	existingFiles(t, dir, ".bashrc")
	if got := createdName(t, dir, ".bashrc"); got != ".bashrc-1" {
		t.Errorf("createUnique(.bashrc) created %q, want %q", got, ".bashrc-1")
	}
}

// The file is new and empty, writable, and a name clash leaves the existing
// file's content alone.
func TestCreateUniqueNewEmptyFileKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(orig, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := createUnique(dir, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if path != filepath.Join(dir, "a-1.txt") {
		t.Errorf("created %q, want a-1.txt", filepath.Base(path))
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() != 0 {
		t.Errorf("new file: %v, %v; want empty", fi, err)
	}
	if _, err := f.WriteString("new"); err != nil {
		t.Errorf("new file not writable: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if b, _ := os.ReadFile(orig); string(b) != "original" {
		t.Errorf("existing file changed: %q", b)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("new file content = %q, want %q", b, "new")
	}
}

// Only an existing name is retried; any other error must come back at once.
func TestCreateUniqueOtherErrorReturnedAtOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	f, err := createUnique(dir, "a.txt")
	if err == nil {
		f.Close()
		t.Fatal("want error for missing directory")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

// A name over the filesystem limit makes every create fail with the same
// non-exist error; it used to be retried forever.
func TestCreateUniqueTooLongNameFailsPromptly(t *testing.T) {
	dir := t.TempDir()
	name := strings.Repeat("報", 90) + ".pdf" // 274 bytes

	type result struct {
		f   *os.File
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := createUnique(dir, name)
		done <- result{f, err}
	}()

	select {
	case r := <-done:
		if r.err == nil {
			r.f.Close()
			t.Skip("filesystem accepts names over 255 bytes")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("createUnique did not return: too-long name is retried forever")
	}
}

func TestCreateUniqueGivesUp(t *testing.T) {
	if testing.Short() {
		t.Skip("creates 10000 files")
	}
	dir := t.TempDir()
	for i := 0; i < 10000; i++ {
		name := "a.txt"
		if i > 0 {
			name = "a-" + strconv.Itoa(i) + ".txt"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	f, err := createUnique(dir, "a.txt")
	if err == nil {
		f.Close()
		t.Fatalf("want error after 10000 collisions, created %q", f.Name())
	}
	if errors.Is(err, fs.ErrExist) || !strings.Contains(err.Error(), "too many files") {
		t.Errorf("err = %v, want a \"too many files\" error", err)
	}
}

func TestSplitExt(t *testing.T) {
	tests := []struct{ in, stem, ext string }{
		{"a.txt", "a", ".txt"},
		{".bashrc", ".bashrc", ""},
		{".hidden.txt", ".hidden", ".txt"},
		{"archive.tar.gz", "archive.tar", ".gz"},
		{"noext", "noext", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		stem, ext := splitExt(tt.in)
		if stem != tt.stem || ext != tt.ext {
			t.Errorf("splitExt(%q) = (%q, %q), want (%q, %q)", tt.in, stem, ext, tt.stem, tt.ext)
		}
	}
}

func TestShortenName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short unchanged", "a.pdf", 200, "a.pdf"},
		{"exactly n unchanged", "abcdefghij", 10, "abcdefghij"},
		{"ascii cut keeps extension", strings.Repeat("a", 300) + ".pdf", 200, strings.Repeat("a", 196) + ".pdf"},
		{"multibyte stem cut on a character boundary", strings.Repeat("報", 90) + ".pdf", 200, strings.Repeat("報", 65) + ".pdf"},
		{"multibyte without extension", strings.Repeat("報", 100), 200, strings.Repeat("報", 66)},
		{"extension of n/2 is kept", strings.Repeat("a", 300) + "." + strings.Repeat("b", 99), 200, strings.Repeat("a", 100) + "." + strings.Repeat("b", 99)},
		{"extension over n/2 cuts the whole name", strings.Repeat("a", 10) + "." + strings.Repeat("b", 300), 200, strings.Repeat("a", 10) + "." + strings.Repeat("b", 189)},
		{"dotfile without extension", "." + strings.Repeat("a", 300), 200, "." + strings.Repeat("a", 199)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortenName(tt.in, tt.n)
			if got != tt.want {
				t.Errorf("shortenName(%d bytes, %d) = %q (%d bytes), want %q (%d bytes)",
					len(tt.in), tt.n, got, len(got), tt.want, len(tt.want))
			}
			if len(got) > tt.n || !utf8.ValidString(got) {
				t.Errorf("result has %d bytes (limit %d), valid UTF-8 = %v", len(got), tt.n, utf8.ValidString(got))
			}
		})
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
		{"long name shortened keeping extension", strings.Repeat("a", 300) + ".pdf", strings.Repeat("a", 196) + ".pdf"},
		{"long name after path stripped", "dir/" + strings.Repeat("a", 300) + ".pdf", strings.Repeat("a", 196) + ".pdf"},
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
		name     string
		in, ct   string
		guessExt bool
		want     string
	}{
		// Inline images: the name is invented, the extension comes from the type.
		{"png from content type", "pic", "image/png", true, "pic.png"},
		{"jpeg with params", "pic", "image/jpeg; charset=x", true, "pic.jpg"},
		{"gif", "pic", "image/gif", true, "pic.gif"},
		{"unknown type adds nothing", "blob", "application/x-tcli-no-such-type", true, "blob"},
		{"no content type adds nothing", "blob", "", true, "blob"},
		{"malformed content type adds nothing", "blob", "///", true, "blob"},
		// File attachments: the sender's name is kept as is.
		{"file: text/plain gets no extension", "README", "text/plain", false, "README"},
		{"file: octet-stream gets no extension", "Makefile", "application/octet-stream", false, "Makefile"},
		{"file: existing extension untouched", "pic.dat", "image/png", false, "pic.dat"},
		{"file: dotfile kept", ".hidden", "image/png", false, ".hidden"},
		{"file: path stripped before saving", "../../x.txt", "text/plain", false, "x.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p, err := saveResponse(dir, tt.in, tt.guessExt, testResp(io.NopCloser(strings.NewReader("d")), tt.ct))
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
	p, err := saveResponse(dir, "", true, testResp(io.NopCloser(strings.NewReader("d")), "image/png"))
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(p); got != "attachment.png" {
		t.Errorf("empty name saved as %q, want %q", got, "attachment.png")
	}
}

// A name over the filesystem limit is shortened (keeping its extension and
// whole characters) instead of failing.
func TestSaveResponseLongName(t *testing.T) {
	dir := t.TempDir()
	name := strings.Repeat("報", 90) + ".pdf" // 274 bytes
	p, err := saveResponse(dir, name, false, testResp(io.NopCloser(strings.NewReader("d")), "application/pdf"))
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(p)
	if len(base) > maxNameLen {
		t.Errorf("saved name has %d bytes, want at most %d", len(base), maxNameLen)
	}
	if !strings.HasSuffix(base, ".pdf") {
		t.Errorf("saved name %q lost its .pdf extension", base)
	}
	if !utf8.ValidString(base) {
		t.Errorf("saved name %q is not valid UTF-8", base)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "d" {
		t.Errorf("content = %q, %v", b, err)
	}
}

func TestSaveResponseContentAndClose(t *testing.T) {
	body := make([]byte, 200_000)
	for i := range body {
		body[i] = byte(i * 7)
	}
	tr := &closeTracker{Reader: bytes.NewReader(body)}
	dir := t.TempDir()
	p, err := saveResponse(dir, "bin.dat", false, testResp(tr, "application/octet-stream"))
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
	p, err = saveResponse(dir, "empty.txt", false, testResp(io.NopCloser(strings.NewReader("")), ""))
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
		if _, err := saveResponse(dir, "a.txt", false, testResp(tr, "")); err == nil {
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
		if _, err := saveResponse(filepath.Join(t.TempDir(), "nope"), "a.txt", false, testResp(tr, "")); err == nil {
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
