package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// chatFilesFolder is the OneDrive folder Teams itself keeps chat files in.
const chatFilesFolder = "Microsoft Teams Chat Files"

// uploadChunkSize is the upload-session fragment size (10 MiB); Graph requires
// a multiple of 320 KiB. A var so tests can use small fragments.
var uploadChunkSize int64 = 32 * 320 << 10

// DriveItem is the subset of a OneDrive driveItem needed to attach it to a
// chat message.
type DriveItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ETag      string `json:"eTag"`
	WebDavURL string `json:"webDavUrl"`
}

var eTagGUIDRe = regexp.MustCompile(`\{([0-9A-Fa-f-]{36})\}`)

// ChatAttachment returns the "reference" attachment that links d in a chat
// message: Graph wants the GUID in the item's eTag as the attachment ID and
// its webDavUrl as the content URL.
func (d DriveItem) ChatAttachment() (Attachment, error) {
	m := eTagGUIDRe.FindStringSubmatch(d.ETag)
	if m == nil {
		return Attachment{}, fmt.Errorf("unexpected eTag %q for %s", d.ETag, d.Name)
	}
	if d.WebDavURL == "" {
		return Attachment{}, fmt.Errorf("no webDavUrl for %s", d.Name)
	}
	return Attachment{
		ID:          strings.ToLower(m[1]),
		ContentType: "reference",
		ContentURL:  d.WebDavURL,
		Name:        d.Name,
	}, nil
}

var oneDriveNameReplacer = strings.NewReplacer(
	`"`, "_", "*", "_", ":", "_", "<", "_", ">", "_", "?", "_", "/", "_", `\`, "_", "|", "_")

// oneDriveName makes name acceptable to OneDrive, which rejects some
// characters and leading/trailing spaces or trailing dots. Replacing ":"
// also keeps the name from ending the root:/path: segment early.
func oneDriveName(name string) string {
	name = strings.TrimLeft(strings.TrimRight(oneDriveNameReplacer.Replace(name), ". "), " ")
	if name == "" {
		return "file"
	}
	return name
}

// UploadChatFile uploads size bytes from r to the chat files folder of the
// signed-in user's OneDrive, renaming it if the name is taken, and returns
// the new item. Requires Files.ReadWrite.
func (c *Client) UploadChatFile(ctx context.Context, name string, r io.Reader, size int64) (*DriveItem, error) {
	if size <= 0 {
		// Upload sessions cannot create empty files.
		return nil, fmt.Errorf("%s is empty", name)
	}

	path := fmt.Sprintf("/me/drive/root:/%s/%s:/createUploadSession",
		url.PathEscape(chatFilesFolder), url.PathEscape(oneDriveName(name)))
	resp, err := c.do(ctx, "POST", path, strings.NewReader(`{"item":{"@microsoft.graph.conflictBehavior":"rename"}}`))
	if err != nil {
		return nil, err
	}
	var session struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := decodeJSON(resp, &session); err != nil {
		return nil, err
	}
	if session.UploadURL == "" {
		return nil, fmt.Errorf("Graph returned no upload URL")
	}

	id, err := c.uploadChunks(ctx, session.UploadURL, r, size)
	if err != nil {
		return nil, err
	}

	// webDavUrl, which chat attachments link to, is only returned when selected.
	var item DriveItem
	if err := c.getJSON(ctx, "/me/drive/items/"+url.PathEscape(id)+"?$select=id,name,eTag,webDavUrl", &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// uploadChunks PUTs size bytes from r to an upload session in
// uploadChunkSize fragments and returns the ID of the created item.
func (c *Client) uploadChunks(ctx context.Context, uploadURL string, r io.Reader, size int64) (string, error) {
	buf := make([]byte, min(uploadChunkSize, size))
	for off := int64(0); off < size; {
		n := min(uploadChunkSize, size-off)
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return "", fmt.Errorf("reading file (did it change during the upload?): %w", err)
		}
		// The upload URL is pre-authenticated; a bearer token can get a 401.
		resp, err := c.send(ctx, "PUT", uploadURL, buf[:n], http.Header{
			"Content-Range": {fmt.Sprintf("bytes %d-%d/%d", off, off+n-1, size)},
		})
		if err != nil {
			return "", err
		}
		off += n

		if off < size {
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				return "", fmt.Errorf("upload ended early (HTTP %d)", resp.StatusCode)
			}
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			resp.Body.Close()
			return "", fmt.Errorf("upload not completed (HTTP %d)", resp.StatusCode)
		}
		var item DriveItem
		if err := decodeJSON(resp, &item); err != nil {
			return "", err
		}
		if item.ID == "" {
			return "", fmt.Errorf("upload response has no item ID")
		}
		return item.ID, nil
	}
	return "", fmt.Errorf("nothing to upload")
}

type driveRecipient struct {
	Email    string `json:"email,omitempty"`
	ObjectID string `json:"objectId,omitempty"`
}

type inviteRequest struct {
	Recipients     []driveRecipient `json:"recipients"`
	Roles          []string         `json:"roles"`
	RequireSignIn  bool             `json:"requireSignIn"`
	SendInvitation bool             `json:"sendInvitation"`
}

// ShareFile gives members read-only access to the item without emailing
// them, as Teams does for files shared in a chat. Members are invited in one
// request; if that fails (e.g. the tenant won't share with an external
// member), each is retried alone so one member can't block the rest. It
// returns the members who could not be given access, and an error only if
// nobody could. Requires Files.ReadWrite.
func (c *Client) ShareFile(ctx context.Context, itemID string, members []ChatMember) ([]ChatMember, error) {
	var shareable, failed []ChatMember
	for _, m := range members {
		if m.Email == "" && m.UserID == "" {
			failed = append(failed, m)
		} else {
			shareable = append(shareable, m)
		}
	}
	if len(shareable) == 0 {
		if len(failed) > 0 {
			return failed, fmt.Errorf("no chat member has an email address or user ID to share with")
		}
		return nil, nil
	}

	err := c.invite(ctx, itemID, shareable)
	if err == nil {
		return failed, nil
	}
	if len(shareable) == 1 {
		return append(failed, shareable...), err
	}

	granted := 0
	for _, m := range shareable {
		if err = c.invite(ctx, itemID, []ChatMember{m}); err != nil {
			failed = append(failed, m)
		} else {
			granted++
		}
	}
	if granted == 0 {
		return failed, err
	}
	return failed, nil
}

func (c *Client) invite(ctx context.Context, itemID string, members []ChatMember) error {
	payload := inviteRequest{
		Recipients:     make([]driveRecipient, len(members)),
		Roles:          []string{"read"},
		RequireSignIn:  true,
		SendInvitation: false,
	}
	for i, m := range members {
		// Graph accepts only one identifier per recipient.
		if m.Email != "" {
			payload.Recipients[i].Email = m.Email
		} else {
			payload.Recipients[i].ObjectID = m.UserID
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling invite: %w", err)
	}
	resp, err := c.do(ctx, "POST", "/me/drive/items/"+url.PathEscape(itemID)+"/invite", bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
