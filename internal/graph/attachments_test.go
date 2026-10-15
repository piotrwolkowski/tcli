package graph

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEncodeShareID(t *testing.T) {
	// Example from the Graph "Accessing shared DriveItems" documentation.
	got := encodeShareID("https://onedrive.live.com/redir?resid=1231244193912!12&authKey=1201919!12921!1")
	want := "u!aHR0cHM6Ly9vbmVkcml2ZS5saXZlLmNvbS9yZWRpcj9yZXNpZD0xMjMxMjQ0MTkzOTEyITEyJmF1dGhLZXk9MTIwMTkxOSExMjkyMSEx"
	if got != want {
		t.Errorf("encodeShareID = %q, want %q", got, want)
	}
}

func TestHostedContentIDs(t *testing.T) {
	m := Message{Body: MessageBodyFull{
		ContentType: "html",
		Content: `<p>look</p><img src="https://graph.microsoft.com/v1.0/chats/19:abc@thread.v2/messages/123/hostedContents/aWQ9eF8x/$value" alt="image">` +
			`<img src="https://graph.microsoft.com/v1.0/chats/19:abc@thread.v2/messages/123/hostedContents/aWQ9eF8y/$value">` +
			`<img src="https://graph.microsoft.com/v1.0/chats/19:abc@thread.v2/messages/123/hostedContents/aWQ9eF8x/$value">`,
	}}
	got := m.HostedContentIDs()
	want := []string{"aWQ9eF8x", "aWQ9eF8y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HostedContentIDs = %v, want %v", got, want)
	}

	if ids := (Message{Body: MessageBodyFull{Content: "plain"}}).HostedContentIDs(); ids != nil {
		t.Errorf("expected no IDs, got %v", ids)
	}
}

func TestAttachmentsParsed(t *testing.T) {
	raw := `{"id":"1","body":{"contentType":"html","content":"x"},"attachments":[
		{"id":"a","contentType":"reference","contentUrl":"https://contoso.sharepoint.com/f.pdf","name":"f.pdf"},
		{"id":"b","contentType":"messageReference","content":"{}"}]}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(m.Attachments))
	}
	if !m.Attachments[0].IsFile() || m.Attachments[0].Name != "f.pdf" {
		t.Errorf("first attachment should be file f.pdf: %+v", m.Attachments[0])
	}
	if m.Attachments[1].IsFile() {
		t.Errorf("messageReference should not be a file")
	}
}
