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

func TestReactionSummary(t *testing.T) {
	mixed := []graph.Reaction{
		{ReactionType: "👍"},
		{ReactionType: "like"},
		{ReactionType: "❤️"},
		{ReactionType: "custom", DisplayName: "partyparrot"},
		{ReactionType: "👍"},
	}
	if got, want := reactionSummary(mixed), "👍 3  ❤️ 1  partyparrot 1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := reactionSummary(nil); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

func TestHTMLToTextEdgeCases(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"whitespace only", " \n\t ", ""},
		{"decimal entity", "it&#39;s", "it's"},
		{"hex entity emoji", "&#x1F44D;", "👍"},
		{"numeric nbsp becomes space", "a&#160;b", "a b"},
		{"double escaped nbsp stays literal", "a&amp;nbsp;b", "a&nbsp;b"},
		{"escaped tags are text not stripped", "&lt;b&gt;x&lt;/b&gt;", "<b>x</b>"},
		{"br inside p", "<p>a<br>b</p>", "a\nb"},
		{"p with attributes", `<p style="margin:0" class="x">hi</p>`, "hi"},
		{"uppercase list", "<UL><LI>x</LI><LI>y</LI></UL>", "- x\n- y"},
		{"attachment tag stripped", `see <attachment id="abc-123"></attachment>`, "see"},
		{"attachment only", `<attachment id="1"></attachment>`, ""},
		{"text then attachment", `<p>report</p><attachment id="1"></attachment>`, "report"},
		{"two paragraphs with blank between", "<p>a</p><p></p><p>b</p>", "a\n\nb"},
		{"nbsp paragraph is a blank line", "<p>a</p><p>&nbsp;</p><p>b</p>", "a\n \nb"},
		{"list with source newlines", "<ul>\n<li>x</li>\n<li>y</li>\n</ul>", "- x\n- y"},
		{"ordered list after paragraph", "<p>Steps:</p><ol><li>one</li><li>two</li></ol>", "Steps:\n- one\n- two"},
		{"text after list", "<ul><li>x</li></ul>done", "- x\ndone"},
		{"div after text", "a<div>b</div>c", "a\nb\nc"},
		{"pre is not p", "<pre>code</pre>", "code"},
		{"NUL in input is dropped", "a\x00b", "ab"},
		{"unclosed tag text kept", "1 < 2", "1 < 2"},
		{"unicode", "<p>привет 日本語</p>", "привет 日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := htmlToText(tt.in); got != tt.want {
				t.Errorf("htmlToText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Text directly before a block element must not run into it.
func TestHTMLToTextTextBeforeParagraph(t *testing.T) {
	if got, want := htmlToText("intro<p>para</p>"), "intro\npara"; got != want {
		t.Errorf("htmlToText = %q, want %q", got, want)
	}
}

// A nested list item must start on its own line, not glue onto its parent.
func TestHTMLToTextNestedList(t *testing.T) {
	got := htmlToText("<ul><li>a<ul><li>b</li></ul></li><li>c</li></ul>")
	if want := "- a\n- b\n- c"; got != want {
		t.Errorf("htmlToText nested list = %q, want %q", got, want)
	}
}

// A list straight after text must not glue "- " onto the text.
func TestHTMLToTextListAfterText(t *testing.T) {
	got := htmlToText("Todo:<ul><li>x</li></ul>")
	if want := "Todo:\n- x"; got != want {
		t.Errorf("htmlToText = %q, want %q", got, want)
	}
}

func TestRenderBodyEdgeCases(t *testing.T) {
	tests := []struct{ name, ct, content, want string }{
		{"empty html", "html", "", ""},
		{"empty text", "text", "", ""},
		{"empty content type is plain", "", " <p>x</p> ", "<p>x</p>"},
		{"plain keeps interior newlines", "text", "a\n\n\n\nb", "a\n\n\n\nb"},
		{"html attachment tag", "html", `<p>hi</p><attachment id="1"></attachment>`, "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderBody(graph.MessageBodyFull{ContentType: tt.ct, Content: tt.content})
			if got != tt.want {
				t.Errorf("renderBody = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReactionSummaryEdgeCases(t *testing.T) {
	r := func(typ, name string) graph.Reaction { return graph.Reaction{ReactionType: typ, DisplayName: name} }
	tests := []struct {
		name string
		in   []graph.Reaction
		want string
	}{
		{"single", []graph.Reaction{r("❤️", "")}, "❤️ 1"},
		{"custom without display name", []graph.Reaction{r("custom", "")}, "custom 1"},
		{"custom counted per display name", []graph.Reaction{r("custom", "parrot"), r("custom", "cat"), r("custom", "parrot")}, "parrot 2  cat 1"},
		{"display name ignored for non-custom", []graph.Reaction{r("like", "ignored")}, "👍 1"},
		{"legacy name then emoji", []graph.Reaction{r("like", ""), r("👍", "")}, "👍 2"},
		{"emoji then legacy name", []graph.Reaction{r("👍", ""), r("like", "")}, "👍 2"},
		{"merge keeps first-seen position", []graph.Reaction{r("like", ""), r("❤️", ""), r("👍", "")}, "👍 2  ❤️ 1"},
		// Documented: an empty reactionType yields an empty label, i.e. " 1".
		{"empty reaction type", []graph.Reaction{r("", "")}, " 1"},
		{"empty slice", []graph.Reaction{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reactionSummary(tt.in); got != tt.want {
				t.Errorf("reactionSummary = %q, want %q", got, tt.want)
			}
		})
	}
}
