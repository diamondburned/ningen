package md

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var fencedCodeBlockInfoKey = parser.NewContextKey()

type fenced struct{}
type fenceData struct {
	indent int
	length int
	node   ast.Node
}

func (b fenced) Trigger() []byte {
	return []byte{'`'}
}

func (b fenced) Parse(p ast.Node, r text.Reader, pc parser.Context) ast.Node {
	n, _ := b._open(p, r, pc)
	if n == nil {
		return nil
	}

	// Crawl until b.Continue is done:
	for state := parser.Continue; state != parser.Close; state = b._continue(n, r, pc) {
	}

	// Close:
	b._close(n, r, pc)

	return n
}

func (b fenced) _open(p ast.Node, r text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := r.PeekLine()
	pos := pc.BlockOffset()

	if pos < 0 || (line[pos] != '`') {
		return nil, parser.NoChildren
	}

	findent := pos
	i := pos

	for ; i < len(line) && line[i] == '`'; i++ {
	}

	oFenceLength := i - pos

	// If there are less than 3 backticks:
	if oFenceLength < 3 {
		return nil, parser.NoChildren
	}

	// Advance through the backticks
	r.Advance(oFenceLength)

	var node = ast.NewFencedCodeBlock(nil)

	// If this isn't the last thing in the line: (```<language>)
	if i < len(line)-1 {
		rest := line[i:]
		infoStart, infoStop := segment.Start-segment.Padding+i, segment.Stop

		if len(rest) > 0 && infoStart < infoStop && bytes.IndexByte(rest, '\n') > -1 {
			// Trim trailing whitespaces:
			left := util.TrimLeftSpaceLength(rest)
			right := util.TrimRightSpaceLength(rest)

			// If there is no space:
			if left < right && bytes.IndexByte(rest, ' ') == -1 {
				seg := text.NewSegment(infoStart+left, infoStop-right)
				node.Info = ast.NewTextSegment(seg)
				r.Advance(infoStop - infoStart)
			}
		}
	}

	pc.Set(fencedCodeBlockInfoKey, &fenceData{findent, oFenceLength, node})
	return node, parser.NoChildren
}

func (b fenced) _continue(node ast.Node, r text.Reader, pc parser.Context) parser.State {
	line, segment := r.PeekLine()
	if len(line) == 0 {
		return parser.Close
	}

	fdata := pc.Get(fencedCodeBlockInfoKey).(*fenceData)
	_, pos := util.IndentWidth(line, r.LineOffset())

	// Crawl i to ```
	i := pos
	for ; i < len(line) && line[i] != '`'; i++ {
	}

	// Is there a string literal? Write it.
	pos, padding := util.DedentPositionPadding(line, r.LineOffset(), segment.Padding, fdata.indent)

	// start+i accounts for everything before end (```)
	var start, stop = segment.Start + pos, segment.Start + i

	// Since we're assigning this segment a Start, IsEmpty() would fail if
	// seg.End is not touched.
	var seg = text.Segment{
		Start:   start,
		Stop:    stop,
		Padding: padding,
	}
	r.AdvanceAndSetPadding(stop-start, padding)

	defer func() {
		// Append this at the end of the function, as the block below might
		// reuse our text segment.
		node.Lines().Append(seg)
	}()

	// If found:
	if i != len(line) {
		// Update the starting position:
		pos = i

		// Iterate until we're out of backticks:
		for ; i < len(line) && line[i] == '`'; i++ {
		}

		// Do we have enough (3 or more) backticks?
		// If yes, end the codeblock properly.
		if length := i - pos; length >= fdata.length {
			r.Advance(length)
			return parser.Close
		} else {
			// No, treat the rest as text:
			seg.Stop = segment.Stop
			r.Advance(segment.Stop - stop)
		}
	}

	return parser.Continue | parser.NoChildren
}

func (b fenced) _close(node ast.Node, r text.Reader, pc parser.Context) {
	fdata := pc.Get(fencedCodeBlockInfoKey).(*fenceData)
	if fdata.node == node {
		pc.Set(fencedCodeBlockInfoKey, nil)
	}

	lines := node.Lines()

	if length := lines.Len(); length > 0 {
		// Trim first whitespace
		first := lines.At(0)
		lines.Set(0, first.TrimLeftSpace(r.Source()))

		// Trim last new line
		last := lines.At(length - 1)
		if last.Len() == 0 {
			lines.SetSliced(0, length-1)
			length--
		}

		// Trim the new last line's trailing whitespace
		last = lines.At(length - 1)
		lines.Set(length-1, last.TrimRightSpace(r.Source()))
	}
}

func (b fenced) CanInterruptParagraph() bool {
	return true
}

func (b fenced) CanAcceptIndentedLine() bool {
	return false
}
