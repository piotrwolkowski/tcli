package graph

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
)

// Attachment is a chatMessageAttachment. Files shared in a chat have
// ContentType "reference" and a ContentURL pointing at the sender's
// OneDrive/SharePoint; other types (adaptive cards, quoted messages) carry
// their payload in Content.
type Attachment struct {
	ID           string `json:"id"`
	ContentType  string `json:"contentType"`
	ContentURL   string `json:"contentUrl,omitempty"`
	Content      string `json:"content,omitempty"`
	Name         string `json:"name,omitempty"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}

// IsFile reports whether the attachment is a shared file that can be downloaded.
func (a Attachment) IsFile() bool {
	return a.ContentType == "reference" && a.ContentURL != ""
}

var hostedContentRe = regexp.MustCompile(`/hostedContents/([^/"'\s]+)/\$value`)

// HostedContentIDs returns the IDs of inline images (hosted contents)
// referenced from the message body, in order of appearance.
func (m Message) HostedContentIDs() []string {
	var ids []string
	seen := map[string]bool{}
	for _, match := range hostedContentRe.FindAllStringSubmatch(m.Body.Content, -1) {
		if id := match[1]; !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// encodeShareID turns a sharing URL into a Graph share ID, as described in
// https://learn.microsoft.com/graph/api/shares-get#encoding-sharing-urls.
func encodeShareID(url string) string {
	return "u!" + base64.RawURLEncoding.EncodeToString([]byte(url))
}

// DownloadFile opens the content of a "reference" attachment via the
// /shares endpoint. Requires the Files.Read.All scope. The caller must close
// the returned response body.
func (c *Client) DownloadFile(ctx context.Context, a Attachment) (*http.Response, error) {
	if !a.IsFile() {
		return nil, fmt.Errorf("attachment %q is not a file (type %s)", a.Name, a.ContentType)
	}
	return c.do(ctx, "GET", "/shares/"+encodeShareID(a.ContentURL)+"/driveItem/content", nil)
}

// DownloadHostedContent opens an inline image of a chat message. The caller
// must close the returned response body.
func (c *Client) DownloadHostedContent(ctx context.Context, chatID, messageID, hostedContentID string) (*http.Response, error) {
	path := fmt.Sprintf("/me/chats/%s/messages/%s/hostedContents/%s/$value", chatID, messageID, hostedContentID)
	return c.do(ctx, "GET", path, nil)
}
