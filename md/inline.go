package md

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type Inline struct {
	ast.BaseInline
	Attr Attribute
}

var KindInline = ast.NewNodeKind("Inline")

// Kind implements Node.Kind.
func (e *Inline) Kind() ast.NodeKind {
	return KindInline
}

// Dump implements Node.Dump
func (e *Inline) Dump(source []byte, level int) {
	ast.DumpHelper(e, source, level, nil, nil)
}

var inlineTriggers = []byte{'*', '_', '|', '~', '`'}

type inlineDelimiterProcessor struct {
	char byte
	attr Attribute
}

func (p *inlineDelimiterProcessor) IsDelimiter(b byte) bool {
	for _, t := range inlineTriggers {
		if t == b {
			p.char = b
			return true
		}
	}

	return false
}

func (p *inlineDelimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	var can = opener.Char == closer.Char
	if !can {
		return false
	}

	switch consumes := opener.Length; {
	case p.char == '_' && consumes == 2: // __
		p.attr = AttrUnderline
	case p.char == '_' && consumes == 1: // _
		fallthrough
	case p.char == '*' && consumes == 1: // *
		p.attr = AttrItalics
	case p.char == '*' && consumes == 2: // **
		p.attr = AttrBold
	case p.char == '|' && consumes == 2: // ||
		p.attr = AttrSpoiler
	case p.char == '~' && consumes == 2: // ~~
		p.attr = AttrStrikethrough
	case p.char == '`' && consumes == 1: // `
		p.attr = AttrMonospace
	default:
		return false
	}

	return true
}

func (p *inlineDelimiterProcessor) OnMatch(consumes int) ast.Node {
	return &Inline{
		BaseInline: ast.BaseInline{},
		Attr:       p.attr,
	}
}

type inline struct{}

func (inline) Trigger() []byte {
	return inlineTriggers
}

func (inline) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()

	processor := inlineDelimiterProcessor{}

	node := parser.ScanDelimiter(line, before, 1, &processor)
	if node == nil {
		return nil
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)

	block.Advance(node.Length)
	pc.PushDelimiter(node)

	return node
}
