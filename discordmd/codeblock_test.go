package discordmd

import (
	"bytes"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
)

const _fencedInline = "```thing"
const _fencedInlineHTML = `<p><pre><code>thing</code></pre>
</p>
`

const _fencedLanguage = "```go" + `
package main

func main() {
	fmt.Println("Hello, 世界！")
}
` + "```"
const _fencedLanguageHTML = `<p><pre><code class="language-go">package main

func main() {
	fmt.Println(&quot;Hello, 世界！&quot;)
}</code></pre>
</p>
`

const _fencedBroken = "`````go" + `
package main
` + "````"
const _fencedBrokenHTML = `<p><pre><code class="language-go">package main
` + "````" + `</code></pre>
</p>
`

func TestFenced(t *testing.T) {
	// Make a fenced only parser:
	p := parser.NewParser(
		parser.WithInlineParsers(InlineParsers()...),
		parser.WithBlockParsers(BlockParsers()...),
	)

	// Make a default new markdown renderer:
	md := goldmark.New(
		goldmark.WithParser(p),
	)

	var tests = []struct {
		md, html, name string
	}{
		{_fencedInline, _fencedInlineHTML, "inline"},
		{_fencedLanguage, _fencedLanguageHTML, "language"},
		{_fencedBroken, _fencedBrokenHTML, "broken"},
	}

	// Results
	var buf bytes.Buffer

	for _, test := range tests {
		if err := md.Convert([]byte(test.md), &buf); err != nil {
			t.Fatal("Failed to parse fenced "+test.name+":", err)
		}

		strcmp(t, "fenced "+test.name, buf.String(), test.html)
		buf.Reset()
	}
}
