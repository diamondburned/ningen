package discordmd

import (
	"testing"

	_ "embed"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const _fencedInline = "hi ```thing```"
const _fencedInlineHTML = `
Document {
    Paragraph {
        RawText: "hi ` + "```thing```" + `"
        HasBlankPreviousLines: true
        Text: "hi "
        FencedCodeBlock {
            RawText: "thing"
            HasBlankPreviousLines: false
        }
    }
}`

const _fencedLanguage = "hi ```go" + `
package main

func main() {
	fmt.Println("Hello, 世界！")
}
` + "```"
const _fencedLanguageHTML = `<p>hi <pre><code class="language-go">package main

func main() {
	fmt.Println(&quot;Hello, 世界！&quot;)
}</code></pre>
</p>
`

const _fencedBroken = "hi `````go" + `
package main
` + "````"
const _fencedBrokenHTML = `<p>hi <pre><code class="language-go">package main
` + "````" + `</code></pre>
</p>
`

const _fencedMixed = "hii ```go" + `
package main
asdas
# bruh
ased
# hi
` + "```"
const _fencedMixedHTML = `<p>hii <pre><code class="language-go">package main
# hi
</code></pre>
</p>`

func TestFenced(t *testing.T) {
	// Make a fenced only parser:
	p := parser.NewParser(
		parser.WithInlineParsers(InlineParsers()...),
		parser.WithBlockParsers(BlockParsers()...),
	)

	// // Make a default new markdown renderer:
	// md := goldmark.New(
	// 	goldmark.WithParser(p),
	// )
	//
	var tests = []struct {
		md, html, name string
	}{
		{_fencedInline, _fencedInlineHTML, "inline"},
		{_fencedLanguage, _fencedLanguageHTML, "language"},
		{_fencedBroken, _fencedBrokenHTML, "broken"},
		{_fencedMixed, _fencedMixedHTML, "mixed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := p.Parse(text.NewReader([]byte(test.md)))
			dump := dump(node, []byte(test.md))
			t.Log("node:\n", dump)

			// var buf bytes.Buffer
			// if err := md.Convert([]byte(test.md), &buf); err != nil {
			// 	t.Fatal("Failed to parse fenced "+test.name+":", err)
			// }
			// strcmp(t, "fenced "+test.name, buf.String(), test.html)
		})
	}
}
