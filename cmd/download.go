package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/piotrwolkowski/tcli/internal/graph"
)

// downloadAttachments saves the files and inline images of msgs into dir.
// It does nothing when dir is empty. Progress goes to stderr so --json output
// stays parseable. A failed download is reported and skipped; an error is
// returned at the end if any download failed.
func downloadAttachments(ctx context.Context, client *graph.Client, chatID string, msgs []graph.Message, dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating download directory: %w", err)
	}

	failed := 0
	save := func(name string, guessExt bool, open func() (*http.Response, error)) {
		resp, err := open()
		if err == nil {
			var path string
			path, err = saveResponse(dir, name, guessExt, resp)
			if err == nil {
				fmt.Fprintf(os.Stderr, "saved %s\n", path)
				return
			}
		}
		failed++
		fmt.Fprintf(os.Stderr, "failed to download %s: %v\n", name, err)
	}

	for _, m := range msgs {
		for _, a := range m.Attachments {
			if !a.IsFile() {
				continue
			}
			save(a.Name, false, func() (*http.Response, error) { return client.DownloadFile(ctx, a) })
		}
		for i, id := range m.HostedContentIDs() {
			name := fmt.Sprintf("image-%s-%d", m.ID, i+1)
			save(name, true, func() (*http.Response, error) { return client.DownloadHostedContent(ctx, chatID, m.ID, id) })
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d attachment(s) could not be downloaded", failed)
	}
	return nil
}

// saveResponse writes resp's body to a new file in dir and closes the body.
// With guessExt (inline images, whose names tcli makes up) the name gets an
// extension from the Content-Type; sender-chosen file names are kept as is.
func saveResponse(dir, name string, guessExt bool, resp *http.Response) (string, error) {
	defer resp.Body.Close()

	name = safeFileName(name)
	if guessExt {
		name += extensionFor(resp.Header.Get("Content-Type"))
	}

	f, err := createUnique(dir, name)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	return path, f.Close()
}

// maxNameLen keeps saved names, with a "-N" suffix, under the 255-byte name
// limit of common filesystems.
const maxNameLen = 200

// safeFileName strips directory components so a sender-chosen name cannot
// escape the download directory, and shortens overlong names.
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == ".." || name == "" {
		return "attachment"
	}
	return shortenName(name, maxNameLen)
}

// shortenName cuts name to at most n bytes, keeping its extension and whole
// UTF-8 characters.
func shortenName(name string, n int) string {
	if len(name) <= n {
		return name
	}
	stem, ext := splitExt(name)
	if len(ext) > n/2 {
		stem, ext = name, ""
	}
	keep := n - len(ext)
	for keep > 0 && !utf8.RuneStart(stem[keep]) {
		keep--
	}
	return stem[:keep] + ext
}

// splitExt splits name into stem and extension. A dotfile such as .bashrc
// has no extension.
func splitExt(name string) (stem, ext string) {
	ext = filepath.Ext(name)
	if ext == name {
		ext = ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// createUnique creates dir/name, or dir/name-N.ext if that is taken, never
// overwriting anything. Taking the name in the create call itself (O_EXCL)
// leaves no gap for another process to claim it first.
func createUnique(dir, name string) (*os.File, error) {
	stem, ext := splitExt(name)
	candidate := name
	for i := 1; i <= 10000; i++ {
		f, err := os.OpenFile(filepath.Join(dir, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}
	return nil, fmt.Errorf("too many files named like %s in %s", name, dir)
}

func extensionFor(contentType string) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	}
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}
