// Package ningen contains a set of helpful functions and packages to aid in
// making a Discord client.
package ningen

import (
	"sort"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/state"
	"github.com/diamondburned/ningen/handler"
	"github.com/diamondburned/ningen/states/emoji"
	"github.com/diamondburned/ningen/states/member"
	"github.com/diamondburned/ningen/states/mute"
	"github.com/diamondburned/ningen/states/note"
	"github.com/diamondburned/ningen/states/read"
	"github.com/diamondburned/ningen/states/relationship"
)

type State struct {
	*state.State

	initd chan struct{}

	// nil before Open().
	NoteState         *note.State
	ReadState         *read.State
	MutedState        *mute.State
	EmojiState        *emoji.State
	MemberState       *member.State
	RelationshipState *relationship.State
}

// FromState wraps a normal state.
func FromState(s *state.State) (*State, error) {
	state := &State{
		State: s,
		initd: make(chan struct{}, 1),
	}

	s.AddHandler(func(r *gateway.ReadyEvent) {
		inj := handler.NewReadyInjector(s, r)

		state.NoteState = note.NewState(inj)
		state.ReadState = read.NewState(s, inj)
		state.MutedState = mute.NewState(s, inj)
		state.EmojiState = emoji.NewState(s)
		state.MemberState = member.NewState(s, inj)
		state.RelationshipState = relationship.NewState(inj)

		// Send to channel that unblocks Open() so applications don't access nil
		// states and avoid data race.
		state.initd <- struct{}{}
	})

	s.AddHandler(func(r *gateway.SessionsReplaceEvent) {
		if u, _ := s.Me(); u != nil {
			s.PresenceSet(0, joinSession(*u, r))
		}
	})

	return state, nil
}

func (s *State) Open() error {
	if err := s.State.Open(); err != nil {
		return err
	}

	s.initd <- struct{}{}
	return nil
}

func (s *State) PrivateChannels() ([]discord.Channel, error) {
	c, err := s.State.PrivateChannels()
	if err != nil {
		return nil, err
	}

	sort.SliceStable(c, func(i, j int) bool {
		return c[i].LastMessageID > c[j].LastMessageID
	})

	return c, nil
}

func (s *State) MessageMentions(msg discord.Message) bool {
	// Ignore own messages.
	if u, err := s.Me(); err == nil && msg.Author.ID == u.ID {
		return false
	}

	var mutedGuild *gateway.UserGuildSettings

	// If there's guild:
	if msg.GuildID.Valid() {
		if mutedGuild = s.MutedState.GuildSettings(msg.GuildID); mutedGuild != nil {
			// We're only checking mutes and suppressions, as channels don't
			// have these. Whatever channels have will override guilds.

			// @everyone mentions still work if the guild is muted and @everyone
			// is not suppressed.
			if msg.MentionEveryone && !mutedGuild.SupressEveryone {
				return true
			}

			// TODO: roles

			// If the guild is muted of all messages:
			if mutedGuild.Muted {
				return false
			}
		}
	}

	// Boolean on whether the message contains a self mention or not:
	var mentioned = messageMentions(msg, s.Ready.User.ID)

	// Check channel settings. Channel settings override guilds.
	if mutedCh := s.MutedState.ChannelOverrides(msg.ChannelID); mutedCh != nil {
		switch mutedCh.MessageNotifications {
		case gateway.AllNotifications:
			if mutedCh.Muted {
				return false
			}

		case gateway.NoNotifications:
			// If no notifications are allowed, not even mentions.
			return false

		case gateway.OnlyMentions:
			// If mentions are allowed. We return early because this overrides
			// the guild settings, even if Guild wants all messages.
			return mentioned
		}
	}

	if mutedGuild != nil {
		switch mutedGuild.MessageNotifications {
		case gateway.AllNotifications:
			// If the guild is muted, but we can return early here. If we allow
			// all notifications, we can return the opposite of muted.
			//   - If we're muted, we don't want a mention.
			//   - If we're not muted, we want a mention.
			return !mutedGuild.Muted

		case gateway.NoNotifications:
			// If no notifications are allowed whatsoever.
			return false

		case gateway.OnlyMentions:
			// We can return early here.
			return mentioned
		}
	}

	// Is this from a DM? TODO: get a better check.
	if ch, err := s.Channel(msg.ChannelID); err == nil {
		// True if the message is from DM or group.
		return ch.Type == discord.DirectMessage || ch.Type == discord.GroupDM
	}

	return false
}

func messageMentions(msg discord.Message, uID discord.Snowflake) bool {
	for _, user := range msg.Mentions {
		if user.ID == uID {
			return true
		}
	}
	return false
}

func joinSession(me discord.User, r *gateway.SessionsReplaceEvent) *discord.Presence {
	ses := *r

	var game *discord.Activity
	var status discord.Status
	var activities []discord.Activity

	for i := len(ses) - 1; i >= 0; i-- {
		presence := ses[i]

		if presence.Game != nil {
			game = presence.Game
		}
		if presence.Status != "" {
			status = presence.Status
		}

		activities = append(activities, presence.Activities...)
	}

	if game == nil && len(activities) > 0 {
		game = &activities[len(activities)-1]
	}

	return &discord.Presence{
		User:       me,
		Game:       game,
		Status:     status,
		Activities: activities,
	}
}
