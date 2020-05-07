package md

import (
	"regexp"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/state"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type Mention struct {
	ast.BaseInline

	// both could be nil
	Channel   *discord.Channel
	GuildUser *discord.GuildUser
	GuildRole *discord.Role // might not have anything else but ID
}

var KindMention = ast.NewNodeKind("Mention")

// Kind implements Node.Kind.
func (m *Mention) Kind() ast.NodeKind {
	return KindMention
}

// Dump implements Node.Dump
func (m *Mention) Dump(source []byte, level int) {
	ast.DumpHelper(m, source, level, nil, nil)
}

type mention struct{}

var mentionRegex = regexp.MustCompile(`<(@!?|@&|#)(\d+)>`)

func (mention) Trigger() []byte {
	return []byte{'<'}
}

func (mention) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	// Don't parse if no messages are given:
	msg := getMessage(pc)
	if msg == nil {
		return nil
	}

	// Also don't parse if there's no Discord state:
	state := getSession(pc)
	if state == nil {
		return nil
	}

	match := matchInline(block, '<', '>')
	if match == nil {
		return nil
	}

	var matches = mentionRegex.FindSubmatch(match)
	if len(matches) != 3 {
		return nil
	}

	// All of the mentions should have a valid ID:
	d, err := discord.ParseSnowflake(string(matches[2]))
	if err != nil {
		return nil
	}

	switch string(matches[1]) {
	case "#": // channel
		c, err := state.Channel(d)
		if err != nil {
			c = &discord.Channel{
				ID:   d,
				Name: d.String(),
			}
		}

		return &Mention{
			BaseInline: ast.BaseInline{},
			Channel:    c,
		}

	case "@", "@!": // user/member
		var target *discord.GuildUser
		for _, user := range msg.Mentions {
			if user.ID == d {
				target = &user
				break
			}
		}

		// Don't try, the user is probably not mentioned.
		if target == nil {
			return nil
		}

		return &Mention{
			BaseInline: ast.BaseInline{},
			GuildUser:  target,
		}

	case "@&": // role
		var target discord.Snowflake
		for _, id := range msg.MentionRoleIDs {
			if id == d {
				target = id
				break
			}
		}
		if !target.Valid() {
			return nil
		}

		r, err := state.Role(msg.GuildID, d)
		if err != nil {
			// Allow fallback.
			r = &discord.Role{
				ID:   d,
				Name: d.String(),
			}
		}

		return &Mention{
			BaseInline: ast.BaseInline{},
			GuildRole:  r,
		}
	}

	return nil
}

func searchMember(state state.Store, guild, user discord.Snowflake) *discord.GuildUser {
	m, err := state.Member(guild, user)
	if err == nil {
		return &discord.GuildUser{
			User:   m.User,
			Member: m,
		}
	}

	// Maybe?
	p, err := state.Presence(guild, user)
	if err == nil {
		return &discord.GuildUser{
			User: p.User,
		}
	}

	return nil
}
