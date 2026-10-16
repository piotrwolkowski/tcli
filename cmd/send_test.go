package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestMessageText(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		files bool
		want  string
	}{
		{"no message with files skips stdin", nil, true, ""},
		{"argument", []string{"hi"}, false, "hi"},
		{"argument with files", []string{"hi"}, true, "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := messageText(tt.args, tt.files)
			if err != nil || got != tt.want {
				t.Errorf("got (%q, %v), want (%q, nil)", got, err, tt.want)
			}
		})
	}
}

// withStdin replaces os.Stdin with a pipe holding content until cleanup.
func withStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
}

func TestMessageTextStdin(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		args  []string
		files bool
		want  string
	}{
		{"dash reads stdin", "hello", []string{"-"}, false, "hello"},
		{"trailing newlines trimmed", "a\n\n", []string{"-"}, false, "a"},
		{"omitted message reads stdin", "hi\n", nil, false, "hi"},
		{"dash with files still reads stdin", "hi", []string{"-"}, true, "hi"},
		{"empty stdin", "", []string{"-"}, false, ""},
		{"only newlines", "\n\n\n", []string{"-"}, false, ""},
		{"interior newlines preserved", "a\n\nb\nc\n", []string{"-"}, false, "a\n\nb\nc"},
		{"leading and trailing spaces preserved", "  a  \n", []string{"-"}, false, "  a  "},
		{"unicode", "привет 👍\n", []string{"-"}, false, "привет 👍"},
		{"argument ignores stdin", "ignored", []string{"arg"}, false, "arg"},
		{"empty argument is not stdin", "ignored", []string{""}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withStdin(t, tt.stdin)
			got, err := messageText(tt.args, tt.files)
			if err != nil || got != tt.want {
				t.Errorf("got (%q, %v), want (%q, nil)", got, err, tt.want)
			}
		})
	}
}

// Piped input from Windows tools ends in CRLF; the line ending is a newline
// and must not leave a stray carriage return in the sent message.
func TestMessageTextCRLF(t *testing.T) {
	withStdin(t, "a\r\n")
	got, err := messageText([]string{"-"}, false)
	if err != nil || got != "a" {
		t.Errorf("got (%q, %v), want (%q, nil)", got, err, "a")
	}
}

func TestStdinIsTerminal(t *testing.T) {
	withStdin(t, "x")
	if stdinIsTerminal() {
		t.Error("pipe reported as terminal")
	}
}

// Documented: any character device counts as a terminal, including /dev/null
// (e.g. cron), so a missing message there gives the "no message" error
// instead of an empty-message error.
func TestMessageTextCharDeviceStdin(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; f.Close() })

	if !stdinIsTerminal() {
		t.Skip("/dev/null is not a character device here")
	}
	if _, err := messageText(nil, false); err == nil || !strings.Contains(err.Error(), "no message given") {
		t.Errorf("err = %v, want no message given", err)
	}
	// With files attached stdin is never touched.
	if got, err := messageText(nil, true); err != nil || got != "" {
		t.Errorf("got (%q, %v), want empty", got, err)
	}
}
