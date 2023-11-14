package discordmd

import "github.com/yuin/goldmark/parser"

type listItemParser struct {
	parser.BlockParser
}

func (b listItemParser) Trigger() []byte {
	return []byte{'-', '*', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}
}

func newListItemParser() parser.BlockParser {
	return listItemParser{parser.NewListItemParser()}
}
