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
	Mentioned bool

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

	// True if the ping actually mentions. This is always false for channels.
	var mentioned bool

	switch string(matches[1]) {
	case "#": // channel
		d := discord.ChannelID(d)
		c, err := state.Channel(d)
		if err != nil {
			c = &discord.Channel{
				ID:   d,
				Name: d.String(),
			}
		}

		return &Mention{
			BaseInline: ast.BaseInline{},
			Mentioned:  mentioned,
			Channel:    c,
		}

	case "@", "@!": // user/member
		d := discord.UserID(d)
		var target *discord.GuildUser
		for _, user := range msg.Mentions {
			if user.ID == d {
				target = &user
				mentioned = true // user is mentioned
				break
			}
		}

		switch {
		// If we can't find the user in mentions, then try and fetch the user
		// anyway, but leave mentioned at false.
		case target == nil:
			target = searchMember(state, msg.GuildID, msg.ChannelID, d)

		// If we don't have a member, then try and fetch it.
		case target.Member == nil && msg.GuildID.Valid():
			target.Member, _ = state.Member(msg.GuildID, d)
		}

		return &Mention{
			BaseInline: ast.BaseInline{},
			Mentioned:  mentioned,
			GuildUser:  target,
		}

	case "@&": // role
		d := discord.RoleID(d)
		// Check if the role is actually mentioned.
		for _, id := range msg.MentionRoleIDs {
			if id == d {
				mentioned = true
				break
			}
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
			Mentioned:  mentioned,
			GuildRole:  r,
		}
	}

	return nil
}

func searchMember(state state.Store, guild discord.GuildID, channel discord.ChannelID, user discord.UserID) *discord.GuildUser {
	// Fetch a member if the user is in a guild.
	if guild.Valid() {
		m, err := state.Member(guild, user)
		if err == nil {
			return &discord.GuildUser{
				User:   m.User,
				Member: m,
			}
		}
	} else {
		// Search the user if this isn't in a guild, as they might be in
		// a DM channel.
		c, err := state.Channel(channel)
		if err == nil {
			for _, u := range c.DMRecipients {
				if u.ID == user {
					return &discord.GuildUser{
						User: u,
					}
				}
			}
		}
	}

	// Maybe the Prensence search would give us some information?
	p, err := state.Presence(guild, user)
	if err == nil {
		return &discord.GuildUser{
			User: p.User,
		}
	}

	// Nothing was found. Make a new user to set to both fields inside GuildUser.
	var u = discord.User{ID: user, Username: user.String()}

	// Return the dummy user.
	return &discord.GuildUser{
		User:   u,
		Member: &discord.Member{User: u},
	}
}
