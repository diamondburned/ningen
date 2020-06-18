package md

import (
	"bytes"
	"strconv"
)

type Attribute uint8

const (
	AttrBold Attribute = iota
	AttrItalics
	AttrUnderline
	AttrStrikethrough
	AttrSpoiler
	AttrMonospace
)

func TagAttribute(tag []byte) Attribute {
	switch {
	case bytes.Equal(tag, []byte("**")):
		return AttrBold
	case bytes.Equal(tag, []byte("__")):
		return AttrUnderline
	case bytes.Equal(tag, []byte("*")), bytes.Equal(tag, []byte("_")):
		return AttrItalics
	case bytes.Equal(tag, []byte("***")):
		return AttrBold | AttrItalics
	case bytes.Equal(tag, []byte("~~")):
		return AttrStrikethrough
	case bytes.Equal(tag, []byte("||")):
		return AttrSpoiler
	case bytes.Equal(tag, []byte("`")):
		return AttrMonospace
	}
	return 0
}

func (a Attribute) Is(attr Attribute) bool {
	return a == attr
}

func (a *Attribute) Add(attr Attribute) {
	*a |= attr
}
func (a *Attribute) Remove(attr Attribute) {
	*a &= ^attr
}

func (a Attribute) StringInt() string {
	return strconv.FormatUint(uint64(a), 10)
}

func (a Attribute) String() (attrs string) {
	switch a {
	case AttrBold:
		attrs = "bold"
	case AttrItalics:
		attrs = "italics"
	case AttrUnderline:
		attrs = "underline"
	case AttrStrikethrough:
		attrs = "strikethrough"
	case AttrSpoiler:
		attrs = "spoiler"
	case AttrMonospace:
		attrs = "monospace"
	}

	return
}

var EmptyTag = Tag{}

type Tag struct {
	Attr  Attribute
	Color string
}

func (t Tag) Combine(tag Tag) Tag {
	attr := t.Attr | tag.Attr
	if tag.Color != "" {
		return Tag{attr, tag.Color}
	}

	return Tag{attr, t.Color}
}
