package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/piotrwolkowski/tcli/config"
)

func TestLooksLikeChatID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"19:abc123@thread.v2", true},
		{"19:meeting_xyz@thread.v2", true},
		{"19:abc@unq.gbl.spaces", true},
		{"standup", false},
		{"19:noat", false},
		{"abc@thread.v2", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeChatID(tt.in); got != tt.want {
			t.Errorf("looksLikeChatID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestResolveChat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.SaveAliases(config.Aliases{"standup": "19:standup@thread.v2"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: "standup", want: "19:standup@thread.v2"},
		{in: "19:raw@thread.v2", want: "19:raw@thread.v2"},
		{in: "standpu", wantErr: `unknown alias "standpu"`},
	}
	for _, tt := range tests {
		got, err := resolveChat(tt.in)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("resolveChat(%q) err = %v, want containing %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveChat(%q) unexpected err: %v", tt.in, err)
		} else if got != tt.want {
			t.Errorf("resolveChat(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveChatNoAliasesFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := resolveChat("19:raw@thread.v2")
	if err != nil || got != "19:raw@thread.v2" {
		t.Errorf("resolveChat = %q, %v; want raw ID, nil", got, err)
	}
}

func TestResolveChatCorruptAliases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aliases.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveChat("19:raw@thread.v2"); err == nil || !strings.Contains(err.Error(), "aliases.json") {
		t.Errorf("resolveChat with corrupt aliases err = %v, want parse error", err)
	}
}

func TestLooksLikeChatIDEdgeCases(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		// Documented: the check is a loose prefix + "@" test.
		{"19:@", true},
		{"19:abc", false},
		{"19:", false},
		{"@", false},
		{"19@:", false},
		{"20:abc@thread.v2", false},
		{" 19:abc@thread.v2", false},
		{"19:abc@thread.v2 ", true},
		{"19:ABC@THREAD.V2", true},
		{"19:a@b@c", true},
	}
	for _, tt := range tests {
		if got := looksLikeChatID(tt.in); got != tt.want {
			t.Errorf("looksLikeChatID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestResolveChatEdgeCases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.SaveAliases(config.Aliases{
		"19:shadow@thread.v2": "19:real@thread.v2",
		"standup":             "19:standup@thread.v2",
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "alias that looks like a chat id wins", in: "19:shadow@thread.v2", want: "19:real@thread.v2"},
		{name: "empty argument", in: "", wantErr: `unknown alias ""`},
		{name: "loose chat id accepted", in: "19:@", want: "19:@"},
		{name: "no at sign rejected", in: "19:abc", wantErr: `unknown alias "19:abc"`},
		{name: "alias lookup is case sensitive", in: "Standup", wantErr: `unknown alias "Standup"`},
		{name: "alias lookup does not trim", in: " standup", wantErr: "unknown alias"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveChat(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("got (%q, %v), want (%q, nil)", got, err, tt.want)
			}
		})
	}
}
