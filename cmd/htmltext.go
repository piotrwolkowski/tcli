package cmd

import (
	"html"
	"regexp"
	"strings"
)

var (
	// htmlTokenRe splits HTML into comments and declarations (group 1), tags
	// (closing slash in group 2, name in group 3) and runs of text. A "<" that
	// does not start a tag is text.
	htmlTokenRe = regexp.MustCompile(`(?s)(<!--.*?-->|<![^>]*>)|<(/?)([a-zA-Z][a-zA-Z0-9]*)[^>]*>|<|[^<]+`)
	blankRunRe  = regexp.MustCompile(`\n{3,}`)
)

// htmlBlocks are the elements laid out on lines of their own.
var htmlBlocks = map[string]bool{
	"p": true, "div": true, "ul": true, "ol": true, "li": true, "pre": true,
	"blockquote": true, "table": true, "tr": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// htmlToText renders a Teams HTML message body as readable plain text, laid
// out roughly as a browser would: source whitespace collapses (except inside
// <pre>), block elements start on a new line, <br> breaks the line, list
// items get "- ", table cells are separated by a space, and an empty <p> is
// kept as a blank line.
func htmlToText(s string) string {
	var w textWriter
	for _, m := range htmlTokenRe.FindAllStringSubmatch(s, -1) {
		switch {
		case m[1] != "": // comment or declaration
		case m[3] != "":
			w.tag(strings.ToLower(m[3]), m[2] == "/")
		default:
			w.text(html.UnescapeString(m[0]))
		}
	}
	out := strings.ReplaceAll(w.b.String(), "\u00a0", " ")
	return strings.TrimSpace(blankRunRe.ReplaceAllString(out, "\n\n"))
}

// textWriter accumulates the output of htmlToText.
type textWriter struct {
	b       strings.Builder
	midLine bool // the current line has content
	bullet  bool // ...and that content is only a list bullet
	brk     bool // block boundary: start a new line before more content
	space   bool // collapsed whitespace: add a space before more content
	emptyP  bool // a <p> was opened and nothing written since
	pre     int  // depth of <pre> elements
}

func (w *textWriter) text(s string) {
	if w.pre > 0 {
		w.write(strings.ReplaceAll(s, "\r\n", "\n"))
		return
	}
	for s != "" {
		i := strings.IndexFunc(s, isHTMLSpace)
		if i < 0 {
			w.write(s)
			return
		}
		w.write(s[:i])
		w.space = true
		s = strings.TrimLeftFunc(s[i:], isHTMLSpace)
	}
}

// isHTMLSpace reports whether r is whitespace that HTML collapses. Unlike
// unicode.IsSpace it excludes the non-breaking space, which is content.
func isHTMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

// write appends content, first resolving a pending line break or space. A
// line holding only a bullet takes neither, so "<li><p>x" reads "- x".
func (w *textWriter) write(s string) {
	if s == "" {
		return
	}
	switch {
	case w.brk && w.midLine && !w.bullet:
		w.newline()
	case w.space && w.midLine && !w.bullet:
		w.b.WriteByte(' ')
	}
	w.b.WriteString(s)
	w.midLine = !strings.HasSuffix(s, "\n")
	w.brk, w.space, w.bullet, w.emptyP = false, false, false, false
}

func (w *textWriter) newline() {
	w.b.WriteByte('\n')
	w.midLine, w.bullet, w.brk, w.space = false, false, false, false
}

func (w *textWriter) tag(name string, closing bool) {
	switch {
	case name == "br":
		if w.brk && w.midLine && !w.bullet {
			w.newline()
		}
		w.newline()
		w.emptyP = false
	case name == "li" && !closing:
		if w.midLine {
			w.newline()
		}
		w.b.WriteString("- ")
		w.midLine, w.bullet, w.brk, w.space = true, true, false, false
	case name == "td" || name == "th":
		w.space = true
	case htmlBlocks[name]:
		if name == "p" && closing && w.emptyP {
			// Teams shows an empty paragraph as a blank line.
			if w.midLine && !w.bullet {
				w.newline()
			}
			w.newline()
		}
		if name == "pre" {
			if closing {
				w.pre = max(w.pre-1, 0)
			} else {
				w.pre++
			}
		}
		w.brk = true
		w.emptyP = name == "p" && !closing
	}
}
