package md

import (
	"bytes"
	"strconv"
	"strings"
)

type Attribute uint16

const (
	AttrBold Attribute = 1 << iota
	AttrItalics
	AttrUnderline
	AttrStrikethrough
	AttrSpoiler
	AttrMonospace
	AttrQuoted
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

func (a Attribute) Has(attr Attribute) bool {
	return a&attr == attr
}

func (a *Attribute) Add(attr Attribute) {
	*a |= attr
}
func (a *Attribute) Remove(attr Attribute) {
	*a &= ^attr
}

func (a Attribute) Markup() string {
	var attrs = make([]string, 0, 1)

	if a.Has(AttrBold) {
		attrs = append(attrs, `weight="bold"`)
	}
	if a.Has(AttrItalics) {
		attrs = append(attrs, `style="italic"`)
	}
	if a.Has(AttrUnderline) {
		attrs = append(attrs, `underline="single"`)
	}
	if a.Has(AttrStrikethrough) {
		attrs = append(attrs, `strikethrough="true"`)
	}
	if a.Has(AttrSpoiler) {
		attrs = append(attrs, `foreground="#808080"`) // no fancy click here
	}
	if a.Has(AttrMonospace) {
		attrs = append(attrs, `font_family="monospace"`)
	}

	// only append this if not spoiler to avoid duplicate tags
	if a.Has(AttrQuoted) && !a.Has(AttrStrikethrough) {
		attrs = append(attrs, `foreground="#789922"`)
	}

	return strings.Join(attrs, " ")
}

func (a Attribute) StringInt() string {
	return strconv.FormatUint(uint64(a), 10)
}

func (a Attribute) String() string {
	var attrs = make([]string, 0, 1)
	if a.Has(AttrBold) {
		attrs = append(attrs, "bold")
	}
	if a.Has(AttrItalics) {
		attrs = append(attrs, "italics")
	}
	if a.Has(AttrUnderline) {
		attrs = append(attrs, "underline")
	}
	if a.Has(AttrStrikethrough) {
		attrs = append(attrs, "strikethrough")
	}
	if a.Has(AttrSpoiler) {
		attrs = append(attrs, "spoiler")
	}
	if a.Has(AttrMonospace) {
		attrs = append(attrs, "monospace")
	}
	if a.Has(AttrQuoted) {
		attrs = append(attrs, "quoted")
	}
	return strings.Join(attrs, ", ")
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
