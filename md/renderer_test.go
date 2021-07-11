package md

import (
	"fmt"
	"strings"
	"testing"

	_ "embed"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

//go:embed renderer_test.txt
var message string

func TestRenderer(t *testing.T) {
	p := parser.NewParser(
		parser.WithBlockParsers(BlockParsers()...),
		parser.WithInlineParsers(InlineParserWithLink()...),
	)

	node := p.Parse(text.NewReader([]byte(message)))
	buff := strings.Builder{}
	DefaultRenderer.Render(&buff, []byte(message), node)

	fmt.Println(buff.String())
}
