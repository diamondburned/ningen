package md

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const message = `**this is a test.** https://google.com strictly URL.
> be me
> wacky **blockquote**
> fml
> >>> bruh
` + "```" + `go
package main

func main() {
	fmt.Println("Bruh moment.")
}
` + "```" + `
[test](https://google.com)

**bold and *italics***
`

func TestRenderer(t *testing.T) {
	p := parser.NewParser(
		parser.WithBlockParsers(BlockParsers()...),
		parser.WithInlineParsers(InlineParserWithLink()...),
	)

	node := p.Parse(text.NewReader([]byte(message)))
	buff := bytes.Buffer{}
	DefaultRenderer.Render(&buff, []byte(message), node)

	fmt.Println(buff.String())
}
