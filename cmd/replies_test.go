package cmd

import (
	"testing"

	"github.com/piotrwolkowski/tcli/internal/graph"
)

func TestRenderBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		content     string
		want        string
	}{
		{"entities", "html", "a &amp; b &lt;c&gt; d&nbsp;e", "a & b <c> d e"},
		{"paragraphs", "html", "<p>a</p><p>b</p>", "a\nb"},
		{"line break", "html", "line1<br>line2", "line1\nline2"},
		{"self-closing break", "html", "line1<BR/>line2<br />line3", "line1\nline2\nline3"},
		{"divs", "html", "<div>one</div><DIV>two</DIV>", "one\ntwo"},
		{"list", "html", "<ul><li>x</li><li class=\"a\">y</li></ul>", "- x\n- y"},
		{"blank runs collapse", "html", "a<br><br><br><br>b", "a\n\nb"},
		{"inline tags stripped", "html", "<b>bold</b> <a href=\"u\">link</a>", "bold link"},
		{"trimmed", "html", "  <p> hi </p>  ", "hi"},
		{"html content type case", "HTML", "x<br>y", "x\ny"},
		{"plain passthrough", "text", "  <b>not html</b> &amp;  ", "<b>not html</b> &amp;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderBody(graph.MessageBodyFull{ContentType: tt.contentType, Content: tt.content})
			if got != tt.want {
				t.Errorf("renderBody(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}
