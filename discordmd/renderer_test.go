package discordmd

import (
	"strings"
	"testing"

	_ "embed"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

//go:embed renderer_test.txt
var message string

//go:embed renderer_test_want.txt
var messageWant string

func TestRenderer(t *testing.T) {
	p := parser.NewParser(
		parser.WithBlockParsers(BlockParsers()...),
		parser.WithInlineParsers(InlineParserWithLink()...),
	)

	node := p.Parse(text.NewReader([]byte(message)))
	buff := strings.Builder{}
	DefaultRenderer.Render(&buff, []byte(message), node)
	t.Log("\n" + buff.String())

	if strings.TrimSpace(buff.String()) != strings.TrimSpace(messageWant) {
		t.Error("renderer output does not match")
	}
}
