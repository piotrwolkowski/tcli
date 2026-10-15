package cmd

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	save := func(name string, open func() (*http.Response, error)) {
		resp, err := open()
		if err == nil {
			var path string
			path, err = saveResponse(dir, name, resp)
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
			save(a.Name, func() (*http.Response, error) { return client.DownloadFile(ctx, a) })
		}
		for i, id := range m.HostedContentIDs() {
			name := fmt.Sprintf("image-%s-%d", m.ID, i+1)
			save(name, func() (*http.Response, error) { return client.DownloadHostedContent(ctx, chatID, m.ID, id) })
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d attachment(s) could not be downloaded", failed)
	}
	return nil
}

// saveResponse writes resp's body to a new file in dir and closes the body.
// Names without an extension (inline images) get one from the Content-Type.
func saveResponse(dir, name string, resp *http.Response) (string, error) {
	defer resp.Body.Close()

	if filepath.Ext(name) == "" {
		name += extensionFor(resp.Header.Get("Content-Type"))
	}
	path := uniquePath(dir, safeFileName(name))

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	return path, f.Close()
}

// safeFileName strips directory components so a sender-chosen name cannot
// escape the download directory.
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == ".." || name == "" {
		return "attachment"
	}
	return name
}

// uniquePath returns dir/name, or dir/name-N.ext if that already exists.
func uniquePath(dir, name string) string {
	path := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	}
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
