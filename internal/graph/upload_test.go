package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func setChunkSize(t *testing.T, n int64) {
	t.Helper()
	old := uploadChunkSize
	uploadChunkSize = n
	t.Cleanup(func() { uploadChunkSize = old })
}

type putReq struct {
	rng, body, auth, contentType string
}

// uploadServer serves createUploadSession and chunk PUTs. respond decides the
// reply to the i-th (0-based) PUT; a nil respond uses 202 then final(201).
type uploadServer struct {
	mu          sync.Mutex
	createPath  string
	createBody  string
	createAuth  string
	puts        []putReq
	getQuery    string
	getPath     string
	getCalls    int
	createCalls int
}

func newUploadServer(t *testing.T, respond func(i int, w http.ResponseWriter)) (*Client, *uploadServer) {
	t.Helper()
	us := &uploadServer{}
	var srvURL string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		us.mu.Lock()
		defer us.mu.Unlock()
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.EscapedPath(), "/createUploadSession"):
			us.createCalls++
			us.createPath = r.URL.EscapedPath()
			us.createAuth = r.Header.Get("Authorization")
			b, _ := io.ReadAll(r.Body)
			us.createBody = string(b)
			writeJSON(t, w, map[string]any{"uploadUrl": srvURL + "/upload/session1"})
		case r.Method == "PUT" && r.URL.Path == "/upload/session1":
			b, _ := io.ReadAll(r.Body)
			i := len(us.puts)
			us.puts = append(us.puts, putReq{
				rng:         r.Header.Get("Content-Range"),
				body:        string(b),
				auth:        r.Header.Get("Authorization"),
				contentType: r.Header.Get("Content-Type"),
			})
			if respond != nil {
				respond(i, w)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		case r.Method == "GET":
			us.getCalls++
			us.getPath = r.URL.EscapedPath()
			us.getQuery = r.URL.Query().Get("$select")
			writeJSON(t, w, map[string]any{
				"id": "item1", "name": "q3 report#1.txt", "eTag": `"{AAAA}"`, "webDavUrl": "https://dav/x",
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	srvURL = srv.URL
	return c, us
}

func finalCreated(t *testing.T, n int) func(int, http.ResponseWriter) {
	return func(i int, w http.ResponseWriter) {
		if i < n-1 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]any{"id": "item1"})
	}
}

func TestUploadChatFileMultiChunk(t *testing.T) {
	setChunkSize(t, 4)
	c, us := newUploadServer(t, finalCreated(t, 3))

	item, err := c.UploadChatFile(context.Background(), "q3 report#1.txt", strings.NewReader("0123456789"), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if us.createPath != "/me/drive/root:/Microsoft%20Teams%20Chat%20Files/q3%20report%231.txt:/createUploadSession" {
		t.Errorf("create path = %q", us.createPath)
	}
	if us.createAuth != "Bearer test-token" {
		t.Errorf("create Authorization = %q", us.createAuth)
	}
	var body, want any
	if err := json.Unmarshal([]byte(us.createBody), &body); err != nil {
		t.Fatalf("create body: %v", err)
	}
	json.Unmarshal([]byte(`{"item":{"@microsoft.graph.conflictBehavior":"rename"}}`), &want)
	if !jsonEqual(body, want) {
		t.Errorf("create body = %s", us.createBody)
	}

	wantPuts := []putReq{
		{rng: "bytes 0-3/10", body: "0123"},
		{rng: "bytes 4-7/10", body: "4567"},
		{rng: "bytes 8-9/10", body: "89"},
	}
	if len(us.puts) != len(wantPuts) {
		t.Fatalf("PUTs = %d, want %d", len(us.puts), len(wantPuts))
	}
	for i, p := range us.puts {
		if p.rng != wantPuts[i].rng || p.body != wantPuts[i].body {
			t.Errorf("PUT %d = %+v, want %+v", i, p, wantPuts[i])
		}
		if p.auth != "" {
			t.Errorf("PUT %d has Authorization %q", i, p.auth)
		}
		if p.contentType == "application/json" {
			t.Errorf("PUT %d has Content-Type application/json", i)
		}
	}

	if us.getPath != "/me/drive/items/item1" || us.getQuery != "id,name,eTag,webDavUrl" {
		t.Errorf("GET = %q ?$select=%q", us.getPath, us.getQuery)
	}
	if *item != (DriveItem{ID: "item1", Name: "q3 report#1.txt", ETag: `"{AAAA}"`, WebDavURL: "https://dav/x"}) {
		t.Errorf("item = %+v", *item)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestUploadChatFileExactMultiple(t *testing.T) {
	setChunkSize(t, 4)
	c, us := newUploadServer(t, func(i int, w http.ResponseWriter) {
		if i == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(t, w, map[string]any{"id": "item1"}) // 200 OK
	})
	if _, err := c.UploadChatFile(context.Background(), "a.txt", strings.NewReader("01234567"), 8); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(us.puts) != 2 {
		t.Fatalf("PUTs = %d, want 2", len(us.puts))
	}
	if us.puts[1].rng != "bytes 4-7/8" {
		t.Errorf("last range = %q", us.puts[1].rng)
	}
}

func TestUploadChatFileSanitisesName(t *testing.T) {
	c, us := newUploadServer(t, finalCreated(t, 1))
	if _, err := c.UploadChatFile(context.Background(), "log 12:30?.txt.", strings.NewReader("x"), 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/me/drive/root:/Microsoft%20Teams%20Chat%20Files/log%2012_30_.txt:/createUploadSession"
	if us.createPath != want {
		t.Errorf("path = %q, want %q", us.createPath, want)
	}
}

func TestOneDriveName(t *testing.T) {
	tests := map[string]string{
		"a:b":    "a_b",
		"x. ":    "x",
		"  y":    "y",
		"...":    "file",
		"ok.pdf": "ok.pdf",
		"":       "file",
		`a/b\c`:  "a_b_c",
	}
	for in, want := range tests {
		if got := oneDriveName(in); got != want {
			t.Errorf("oneDriveName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUploadChatFileErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		size    int64
		respond func(i int, w http.ResponseWriter)
		wantErr string
		noHTTP  bool
	}{
		{name: "empty file", content: "", size: 0, wantErr: "empty", noHTTP: true},
		{name: "reader shorter than size", content: "0123", size: 10, respond: finalCreated(t, 3), wantErr: "did it change"},
		{name: "last chunk answered 202", content: "01234567", size: 8, wantErr: "not completed"},
		{
			name: "non-final chunk answered 200", content: "01234567", size: 8,
			respond: func(i int, w http.ResponseWriter) { writeJSON(t, w, map[string]any{"id": "x"}) },
			wantErr: "ended early",
		},
		{
			name: "final 201 with empty id", content: "01234567", size: 8,
			respond: func(i int, w http.ResponseWriter) {
				if i == 0 {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				w.WriteHeader(http.StatusCreated)
				writeJSON(t, w, map[string]any{"id": ""})
			},
			wantErr: "no item ID",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setChunkSize(t, 4)
			c, us := newUploadServer(t, tt.respond)
			_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader(tt.content), tt.size)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if tt.noHTTP && (us.createCalls != 0 || len(us.puts) != 0 || us.getCalls != 0) {
				t.Errorf("unexpected HTTP calls: %+v", us)
			}
		})
	}
}

func TestUploadChunk429Replayed(t *testing.T) {
	setChunkSize(t, 4)
	c, us := newUploadServer(t, func(i int, w http.ResponseWriter) {
		switch i {
		case 0:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case 1:
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, map[string]any{"id": "item1"})
		}
	})
	if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("01234567"), 8); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(us.puts) != 3 {
		t.Fatalf("PUTs = %d, want 3", len(us.puts))
	}
	if us.puts[0] != us.puts[1] {
		t.Errorf("retry differs: %+v vs %+v", us.puts[0], us.puts[1])
	}
	if us.puts[1].body != "0123" || us.puts[1].rng != "bytes 0-3/8" || us.puts[1].auth != "" {
		t.Errorf("retry = %+v", us.puts[1])
	}
}

func TestChatAttachment(t *testing.T) {
	tests := []struct {
		name    string
		item    DriveItem
		want    Attachment
		wantErr bool
	}{
		{
			name: "braced GUID lowercased",
			item: DriveItem{Name: "f.txt", ETag: "\"{153FA47D-18C9-4179-BE08-9879815A9F90},2\"", WebDavURL: "https://dav/f.txt"},
			want: Attachment{ID: "153fa47d-18c9-4179-be08-9879815a9f90", ContentType: "reference", ContentURL: "https://dav/f.txt", Name: "f.txt"},
		},
		{name: "no braced GUID", item: DriveItem{Name: "f", ETag: `"abc,2"`, WebDavURL: "https://d"}, wantErr: true},
		{name: "empty webDavUrl", item: DriveItem{Name: "f", ETag: "\"{153FA47D-18C9-4179-BE08-9879815A9F90},2\""}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.item.ChatAttachment()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

type inviteBody struct {
	Recipients     []map[string]string `json:"recipients"`
	Roles          []string            `json:"roles"`
	RequireSignIn  bool                `json:"requireSignIn"`
	SendInvitation bool                `json:"sendInvitation"`
}

func TestShareFile(t *testing.T) {
	a := ChatMember{Email: "a@x"}
	b := ChatMember{UserID: "u2"}
	c3 := ChatMember{Email: "c@x"}
	none := ChatMember{DisplayName: "Ghost"}

	tests := []struct {
		name       string
		members    []ChatMember
		status     func(req inviteBody) int // nil => 200
		wantFailed []ChatMember
		wantErr    bool
		wantCalls  int
	}{
		{name: "batch success", members: []ChatMember{a, b}, wantCalls: 1},
		{name: "no members", members: nil, wantCalls: 0},
		{name: "only unshareable", members: []ChatMember{none}, wantFailed: []ChatMember{none}, wantErr: true, wantCalls: 0},
		{
			name: "batch fails, one individual fails", members: []ChatMember{a, b},
			status: func(r inviteBody) int {
				if len(r.Recipients) > 1 || r.Recipients[0]["objectId"] == "u2" {
					return 400
				}
				return 200
			},
			wantFailed: []ChatMember{b}, wantCalls: 3,
		},
		{
			name: "unshareable member plus success", members: []ChatMember{none, a},
			wantFailed: []ChatMember{none}, wantCalls: 1,
		},
		{
			name: "all individual fail", members: []ChatMember{a, b, c3},
			status:     func(inviteBody) int { return 400 },
			wantFailed: []ChatMember{a, b, c3}, wantErr: true, wantCalls: 4,
		},
		{
			name: "207 is success", members: []ChatMember{a, b},
			status:    func(inviteBody) int { return 207 },
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqs []inviteBody
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.EscapedPath() != "/me/drive/items/item1/invite" {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				var req inviteBody
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decode: %v", err)
				}
				reqs = append(reqs, req)
				status := 200
				if tt.status != nil {
					status = tt.status(req)
				}
				w.WriteHeader(status)
				if status < 400 {
					w.Write([]byte(`{}`))
				} else {
					w.Write([]byte(`{"error":{"code":"accessDenied","message":"no"}}`))
				}
			})
			failed, err := c.ShareFile(context.Background(), "item1", tt.members)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if len(failed) != len(tt.wantFailed) {
				t.Fatalf("failed = %+v, want %+v", failed, tt.wantFailed)
			}
			for i := range failed {
				if failed[i] != tt.wantFailed[i] {
					t.Errorf("failed[%d] = %+v, want %+v", i, failed[i], tt.wantFailed[i])
				}
			}
			if len(reqs) != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", len(reqs), tt.wantCalls)
			}
			for _, req := range reqs {
				if len(req.Roles) != 1 || req.Roles[0] != "read" || !req.RequireSignIn || req.SendInvitation {
					t.Errorf("request flags = %+v", req)
				}
				for _, rc := range req.Recipients {
					if len(rc) != 1 {
						t.Errorf("recipient %v must have exactly one key", rc)
					}
				}
			}
		})
	}
}

func TestShareFileRecipientBody(t *testing.T) {
	var got inviteBody
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{}`))
	})
	_, err := c.ShareFile(context.Background(), "item1", []ChatMember{{Email: "a@x"}, {UserID: "u2"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{{"email": "a@x"}, {"objectId": "u2"}}
	if !jsonEqual(got.Recipients, want) {
		t.Errorf("recipients = %v, want %v", got.Recipients, want)
	}
}

func TestListChatMembers(t *testing.T) {
	var srvURL string
	var paths []string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Query().Get("page") == "" {
			if r.URL.EscapedPath() != "/chats/19:abc@thread.v2/members" {
				t.Errorf("path = %q", r.URL.EscapedPath())
			}
			writeJSON(t, w, map[string]any{
				"value":           []map[string]any{{"userId": "u1", "email": "a@x", "displayName": "Alice"}},
				"@odata.nextLink": srvURL + "/chats/19:abc@thread.v2/members?page=2",
			})
			return
		}
		writeJSON(t, w, map[string]any{"value": []map[string]any{{"userId": "u2", "displayName": "Bob"}}})
	})
	srvURL = srv.URL

	got, err := c.ListChatMembers(context.Background(), "19:abc@thread.v2")
	if err != nil {
		t.Fatal(err)
	}
	want := []ChatMember{{UserID: "u1", Email: "a@x", DisplayName: "Alice"}, {UserID: "u2", DisplayName: "Bob"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("members = %+v, want %+v", got, want)
	}
	if len(paths) != 2 {
		t.Errorf("requests = %v", paths)
	}
}
