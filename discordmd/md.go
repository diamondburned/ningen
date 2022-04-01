package discordmd

import (
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/state/store"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	messageCtx = parser.NewContextKey()
	sessionCtx = parser.NewContextKey()
)

// ParseWithMessage parses the given byte slice with the Discord state and the
// Message as source for the ast nodes. If msg is false, then links will also be
// parsed (accordingly to embeds and webhooks, normal messages don't have
// links).
func ParseWithMessage(b []byte, s store.Cabinet, m *discord.Message, msg bool) ast.Node {
	// Context to pass down messages:
	ctx := parser.NewContext()
	ctx.Set(messageCtx, m)
	ctx.Set(sessionCtx, &s)

	var inlineParsers []util.PrioritizedValue
	if msg {
		inlineParsers = InlineParsers()
	} else {
		inlineParsers = InlineParserWithLink()
	}

	p := parser.NewParser(
		parser.WithBlockParsers(BlockParsers()...),
		parser.WithInlineParsers(inlineParsers...),
	)

	return p.Parse(text.NewReader(b), parser.WithContext(ctx))
}

// Parse parses the given byte slice with extra options. It does not parse
// links.
func Parse(content []byte, opts ...parser.ParseOption) ast.Node {
	p := parser.NewParser(
		parser.WithBlockParsers(BlockParsers()...),
		parser.WithInlineParsers(InlineParsers()...),
	)

	return p.Parse(text.NewReader(content), opts...)
}

func getMessage(pc parser.Context) *discord.Message {
	if v := pc.Get(messageCtx); v != nil {
		return v.(*discord.Message)
	}
	return nil
}
func getSession(pc parser.Context) *store.Cabinet {
	if v := pc.Get(sessionCtx); v != nil {
		return v.(*store.Cabinet)
	}
	return nil
}
