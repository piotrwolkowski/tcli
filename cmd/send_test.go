package cmd

import "testing"

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
