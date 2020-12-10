// Package ningen contains a set of helpful functions and packages to aid in
// making a Discord client.
package ningen

import (
	"sort"
	"sync"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/arikawa/v2/state"
	"github.com/diamondburned/arikawa/v2/utils/handler"
	"github.com/diamondburned/ningen/states/emoji"
	"github.com/diamondburned/ningen/states/member"
	"github.com/diamondburned/ningen/states/mute"
	"github.com/diamondburned/ningen/states/note"
	"github.com/diamondburned/ningen/states/read"
	"github.com/diamondburned/ningen/states/relationship"
)

// Connected is an event that's sent on Ready or Resumed. The event arrives
// before all ningen's handlers are called.
type Connected struct {
	Event gateway.Event
}

type State struct {
	*state.State
	*handler.Handler

	// handler that is given to states; always synchronous
	prehandler *handler.Handler

	// handler that users can call

	initd chan struct{} // nil after Open().
	initm sync.Mutex

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
		State:      s,
		Handler:    handler.New(),
		prehandler: handler.New(),
	}

	// This is required to avoid data race with future handlers.
	state.prehandler.Synchronous = true

	// Give our local states the synchronous prehandler.
	state.NoteState = note.NewState(state.prehandler)
	state.ReadState = read.NewState(s, state.prehandler)
	state.MutedState = mute.NewState(s.Cabinet, state.prehandler)
	state.EmojiState = emoji.NewState(s.Cabinet)
	state.MemberState = member.NewState(s, state.prehandler)
	state.RelationshipState = relationship.NewState(state.prehandler)

	s.AddHandler(func(v interface{}) {
		switch v := v.(type) {
		case *gateway.SessionsReplaceEvent:
			if u, err := s.Me(); err == nil {
				s.PresenceSet(0, joinSession(*u, v))
			}
		}

		// Synchronously run the handlers that our states use.
		state.prehandler.Call(v)

		switch v.(type) {
		case *gateway.ReadyEvent, *gateway.ResumedEvent:
			state.Handler.Call(&Connected{v})
		}

		// Call the external handler after we're done. This handler is
		// asynchronuos, or at least it should be.
		state.Handler.Call(v)

		state.initm.Lock()
		initd := state.initd
		state.initm.Unlock()

		// Send to channel that unblocks Open() so applications don't access nil
		// states and avoid data race.
		if initd != nil {
			initd <- struct{}{}
		}
	})

	return state, nil
}

func (s *State) Open() error {
	// Make the channel so the ready handler can use it. This channel is
	// 1-buffered in case the handler is faster than us.
	s.initm.Lock()
	s.initd = make(chan struct{}, 1)
	s.initm.Unlock()

	if err := s.State.Open(); err != nil {
		return err
	}

	s.initm.Lock()
	initd := s.initd
	s.initm.Unlock()

	<-initd

	s.initm.Lock()
	s.initd = nil // so future Ready events will never use this ch
	s.initm.Unlock()

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

	var mutedGuild *gateway.UserGuildSetting

	// If there's guild:
	if msg.GuildID.IsValid() {
		if mutedGuild = s.MutedState.GuildSettings(msg.GuildID); mutedGuild != nil {
			// We're only checking mutes and suppressions, as channels don't
			// have these. Whatever channels have will override guilds.

			// @everyone mentions still work if the guild is muted and @everyone
			// is not suppressed.
			if msg.MentionEveryone && !mutedGuild.SuppressEveryone {
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
	var mentioned = messageMentions(msg, s.Ready().User.ID)

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

func messageMentions(msg discord.Message, uID discord.UserID) bool {
	for _, user := range msg.Mentions {
		if user.ID == uID {
			return true
		}
	}
	return false
}

func joinSession(me discord.User, r *gateway.SessionsReplaceEvent) gateway.Presence {
	ses := *r

	var status gateway.Status
	var activities []discord.Activity

	for i := len(ses) - 1; i >= 0; i-- {
		presence := ses[i]

		if presence.Status != "" {
			status = presence.Status
		}

		activities = append(activities, presence.Activities...)
	}

	return gateway.Presence{
		User:       me,
		Status:     status,
		Activities: activities,
	}
}
