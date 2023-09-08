package discordmd

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
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
	ast.DumpHelper(e, source, level, map[string]string{
		"Attributes": e.Attr.String(),
	}, nil)
}

var inlineTriggers = []byte{'*', '_', '|', '~'}

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
	case p.char == '*' && consumes == 3: // ***
		p.attr = AttrBold | AttrItalics
	case p.char == '|' && consumes == 2: // ||
		p.attr = AttrSpoiler
	case p.char == '~' && consumes == 2: // ~~
		p.attr = AttrStrikethrough
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

	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)

	return node
}

type inlineCodeSpan struct{}

var _ parser.InlineParser = inlineCodeSpan{}

func (p inlineCodeSpan) Trigger() []byte {
	return []byte{'`'}
}

func (p inlineCodeSpan) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, startSegment := block.PeekLine()
	opener := 0
	for ; opener < len(line) && line[opener] == '`'; opener++ {
	}
	block.Advance(opener)
	l, pos := block.Position()
	node := &Inline{Attr: AttrMonospace}
	for {
		line, segment := block.PeekLine()
		if line == nil {
			block.SetPosition(l, pos)
			return ast.NewTextSegment(startSegment.WithStop(startSegment.Start + opener))
		}
		for i := 0; i < len(line); i++ {
			c := line[i]
			if c == '`' {
				oldi := i
				for ; i < len(line) && line[i] == '`'; i++ {
				}
				closure := i - oldi
				if closure == opener && (i >= len(line) || line[i] != '`') {
					segment = segment.WithStop(segment.Start + i - closure)
					if !segment.IsEmpty() {
						node.AppendChild(node, ast.NewRawTextSegment(segment))
					}
					block.Advance(i)
					goto end
				}
			}
		}
		if !util.IsBlank(line) {
			node.AppendChild(node, ast.NewRawTextSegment(segment))
		}
		block.AdvanceLine()
	}
end:
	return node
}
