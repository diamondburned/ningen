package md

import (
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// ^ because we already parsed to the start of the URL.
var autolinkRegex = regexp.MustCompile(`^https?:\/\S+(?:\.|:)[^>\s]+`)

type autolink struct{}

func (s autolink) Trigger() []byte {
	return []byte{' ', '<'}
}

func (s autolink) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()

	before := line[0]

	switch before {
	case '<':
		// If there's an opener, consume it:
		line = line[1:]
		segment.Start++

		// We defer once now for the opener and once after segmenting for the
		// closure.
		block.Advance(1)
		defer block.Advance(1)

	case ' ':
		// Consume a space so FindURLIndex doesn't break:
		line = line[1:]
		block.Advance(1)
	}

	locs := autolinkRegex.FindIndex(line)
	if len(locs) == 0 {
		return nil
	}

	stop := locs[1]

	// If we've consumed a space, we should restore the space before the
	// URL as well.
	if before == ' ' {
		// Space? Fine, I'll do it myself. Prepend a space:
		s := segment.WithStop(segment.Start + 1)
		ast.MergeOrAppendTextSegment(parent, s)

		// Consume a space in segment so NewTextSegment works properly:
		segment.Start++
	}

	value := ast.NewTextSegment(text.NewSegment(segment.Start, segment.Start+stop))
	block.Advance(stop)

	return ast.NewAutoLink(ast.AutoLinkURL, value)
}
