package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/piotrwolkowski/tcli/internal/graph"
)

func TestCheckFiles(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.txt")
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(ok, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		paths   []string
		wantErr bool
	}{
		{"regular file", []string{ok}, false},
		{"none", nil, false},
		{"missing", []string{filepath.Join(dir, "nope")}, true},
		{"directory", []string{dir}, true},
		{"empty file", []string{empty}, true},
		{"one bad among good", []string{ok, empty}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkFiles(tt.paths); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOtherMembers(t *testing.T) {
	members := []graph.ChatMember{{UserID: "me"}, {UserID: "u2"}, {UserID: "u3"}}
	got := otherMembers(members, "me")
	if len(got) != 2 || got[0].UserID != "u2" || got[1].UserID != "u3" {
		t.Errorf("got %+v", got)
	}
}

func TestMemberNames(t *testing.T) {
	got := memberNames([]graph.ChatMember{
		{DisplayName: "Alice", Email: "a@x"},
		{Email: "b@x"},
		{},
	})
	if want := "Alice, b@x, (unnamed member)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCheckFilesEdgeCases(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	file := filepath.Join(dir, "f.txt")
	emptyFile := filepath.Join(dir, "empty.txt")
	for p, data := range map[string][]byte{file: []byte("x"), emptyFile: nil} {
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := func(name, target string) string {
		p := filepath.Join(dir, name)
		if err := os.Symlink(target, p); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		return p
	}
	fileLink := link("link-file", file)
	dirLink := link("link-dir", sub)
	danglingLink := link("link-dangling", filepath.Join(dir, "gone"))
	emptyLink := link("link-empty", emptyFile)

	tests := []struct {
		name    string
		paths   []string
		wantErr bool
		inErr   string // must appear in the error message
	}{
		{"empty list", []string{}, false, ""},
		{"symlink to regular file", []string{fileLink}, false, ""},
		{"same file twice", []string{file, file}, false, ""},
		{"symlink to directory", []string{dirLink}, true, dirLink},
		{"dangling symlink", []string{danglingLink}, true, danglingLink},
		{"symlink to empty file", []string{emptyLink}, true, emptyLink},
		{"missing file names path", []string{filepath.Join(dir, "nope.txt")}, true, "nope.txt"},
		{"directory names path", []string{sub}, true, sub},
		{"empty file names path", []string{emptyFile}, true, emptyFile},
		{"empty path", []string{""}, true, ""},
		{"error names the offending one", []string{file, fileLink, sub}, true, sub},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkFiles(tt.paths)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), tt.inErr) {
				t.Errorf("err = %q, want it to contain %q", err, tt.inErr)
			}
		})
	}
}

func TestOtherMembersEdgeCases(t *testing.T) {
	ids := func(ms []graph.ChatMember) []string {
		out := []string{}
		for _, m := range ms {
			out = append(out, m.UserID)
		}
		return out
	}
	m := func(ids ...string) []graph.ChatMember {
		var out []graph.ChatMember
		for _, id := range ids {
			out = append(out, graph.ChatMember{UserID: id})
		}
		return out
	}
	tests := []struct {
		name    string
		members []graph.ChatMember
		meID    string
		want    []string
	}{
		{"nil input", nil, "me", []string{}},
		{"empty input", []graph.ChatMember{}, "me", []string{}},
		{"all members are me", m("me", "me"), "me", []string{}},
		{"me not present", m("a", "b"), "me", []string{"a", "b"}},
		{"order preserved", m("z", "me", "a", "m"), "me", []string{"z", "a", "m"}},
		{"duplicates of others kept", m("a", "a"), "me", []string{"a", "a"}},
		// Documented: with an empty meID, members with an empty UserID are dropped.
		{"empty meID drops members without user id", m("a", "", "b"), "", []string{"a", "b"}},
		{"empty meID with only blank ids", m("", ""), "", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(otherMembers(tt.members, tt.meID))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOtherMembersKeepsFields(t *testing.T) {
	in := []graph.ChatMember{{UserID: "me"}, {UserID: "u", DisplayName: "U", Email: "u@x"}}
	got := otherMembers(in, "me")
	if len(got) != 1 || got[0] != in[1] {
		t.Errorf("got %+v", got)
	}
}

func TestMemberNamesEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		in   []graph.ChatMember
		want string
	}{
		{"nil", nil, ""},
		{"empty", []graph.ChatMember{}, ""},
		{"single has no separator", []graph.ChatMember{{DisplayName: "Alice"}}, "Alice"},
		{"single unnamed", []graph.ChatMember{{}}, "(unnamed member)"},
		{"display name wins over email", []graph.ChatMember{{DisplayName: "A", Email: "a@x"}}, "A"},
		{"two", []graph.ChatMember{{DisplayName: "A"}, {Email: "b@x"}}, "A, b@x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := memberNames(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
