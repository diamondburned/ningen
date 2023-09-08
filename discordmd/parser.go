package discordmd

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Heading is a Discord-specific heading.
type Heading = ast.Heading

// BlockParsers returns a list of block parsers.
func BlockParsers() []util.PrioritizedValue {
	return []util.PrioritizedValue{
		util.Prioritized(parser.NewATXHeadingParser(), 100),
		util.Prioritized(parser.NewListParser(), 300),
		util.Prioritized(parser.NewListItemParser(), 400),
		util.Prioritized(blockquote{}, 500),
		util.Prioritized(paragraph{}, 1000),
	}
}

// InlineParsers returns a list of inline parsers.
func InlineParsers() []util.PrioritizedValue {
	return []util.PrioritizedValue{
		util.Prioritized(fenced{}, 100), // code blocks, prioritized
		util.Prioritized(&emoji{}, 200), // (*emoji).Parse()
		util.Prioritized(inlineCodeSpan{}, 300),
		// util.Prioritized(parser.NewCodeSpanParser(), 300),
		util.Prioritized(inline{}, 350),
		util.Prioritized(mention{}, 400),
		util.Prioritized(autolink{}, 500),
	}
}

// InlineParserWithLink returns a list of inline parsers, including the link
// parser.
func InlineParserWithLink() []util.PrioritizedValue {
	return append(InlineParsers(), util.Prioritized(parser.NewLinkParser(), 600))
}

// matchInline function to parse a pair of bytes (chars)
func matchInline(r text.Reader, open, close byte) []byte {
	line, _ := r.PeekLine()

	start := 0
	for ; start < len(line) && line[start] != open; start++ {
	}

	stop := start
	for ; stop < len(line) && line[stop] != close; stop++ {
	}

	// This would be true if there's no closure.
	if stop >= len(line) || line[stop] != close {
		return nil
	}

	stop++ // add the '>'

	// Advance total distance:
	r.Advance(stop)

	return line[start:stop]
}
