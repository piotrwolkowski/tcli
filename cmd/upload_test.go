package cmd

import (
	"os"
	"path/filepath"
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
