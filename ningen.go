// Package ningen contains a set of helpful functions and packages to aid in
// making a Discord client.
package ningen

import (
	"context"
	"sort"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/handler"
	"github.com/diamondburned/ningen/v3/nstore"
	"github.com/diamondburned/ningen/v3/states/emoji"
	"github.com/diamondburned/ningen/v3/states/member"
	"github.com/diamondburned/ningen/v3/states/mute"
	"github.com/diamondburned/ningen/v3/states/note"
	"github.com/diamondburned/ningen/v3/states/read"
	"github.com/diamondburned/ningen/v3/states/relationship"
	"github.com/pkg/errors"
)

var cancelledCtx context.Context

func init() {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledCtx = c
}

// Connected is an event that's sent on Ready or Resumed. The event arrives
// before all ningen's handlers are called.
type Connected struct {
	Event gateway.Event
}

type State struct {
	*state.State
	*handler.Handler

	// Custom Cabinet values.
	MemberStore   *nstore.MemberStore
	PresenceStore *nstore.PresenceStore

	// Custom State values.
	NoteState         *note.State
	ReadState         *read.State
	MutedState        *mute.State
	EmojiState        *emoji.State
	MemberState       *member.State
	RelationshipState *relationship.State

	initd  chan struct{} // nil after Open().
	oldCtx context.Context
}

// New creates a new ningen state from the given token and the default
// identifier.
func New(token string) *State {
	id := gateway.DefaultIdentifier(token)
	id.Capabilities = 253 // magic constant from reverse-engineering
	return NewWithIdentifier(id)
}

// NewWithIdentifier creates a new ningen state from the given identifier.
func NewWithIdentifier(id gateway.Identifier) *State {
	return FromState(state.NewWithIdentifier(id))
}

// FromState wraps a normal state.
func FromState(s *state.State) *State {
	state := &State{
		initd:   make(chan struct{}, 1),
		State:   s,
		Handler: handler.New(),
	}

	state.MemberStore = nstore.NewMemberStore()
	state.PresenceStore = nstore.NewPresenceStore()

	state.Cabinet.MemberStore = state.MemberStore
	state.Cabinet.PresenceStore = state.PresenceStore

	prehandler := s.Handler
	// Give our local states the synchronous prehandler.
	state.NoteState = note.NewState(s, prehandler)
	state.ReadState = read.NewState(s, prehandler)
	state.MutedState = mute.NewState(s.Cabinet, prehandler)
	state.EmojiState = emoji.NewState(s.Cabinet)
	state.MemberState = member.NewState(s, prehandler)
	state.RelationshipState = relationship.NewState(prehandler)

	s.AddSyncHandler(func(v interface{}) {
		switch v := v.(type) {
		case *gateway.SessionsReplaceEvent:
			if u, err := s.Me(); err == nil {
				s.PresenceSet(0, joinSession(*u, v), true)
			}
		}

		switch v.(type) {
		// Might be better to trigger this on a ReadySupplemental event, as
		// that's when things are truly done?
		case *gateway.ReadyEvent, *gateway.ResumedEvent:
			state.Handler.Call(&Connected{v.(gateway.Event)})
		}

		// Only unblock if we have a ReadySupplemental to ensure that we have
		// everything in the state.
		if _, ok := v.(*gateway.ReadyEvent); ok {
			// Send to channel that unblocks Open() so applications don't access
			// nil states and avoid data race.
			select {
			case state.initd <- struct{}{}:
				// Since this channel is one-buffered, we can do this.
			default:
			}
		}

		// Call the external handler after we're done. This handler is
		// asynchronuos, or at least it should be.
		state.Handler.Call(v)
	})

	return state
}

func (s *State) Open(ctx context.Context) error {
	// Ensure the channel is free.
	select {
	case <-s.initd:
	default:
	}

	if err := s.State.Open(ctx); err != nil {
		return err
	}

	// Wait until ReadySupplementalEvent.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.initd:
		return nil
	}
}

// WithContext returns State with the given context.
func (s *State) WithContext(ctx context.Context) *State {
	cpy := *s
	cpy.State = cpy.State.WithContext(ctx)
	return &cpy
}

// Offline returns an offline version of the state.
func (s *State) Offline() *State {
	oldCtx := s.Context()
	cpy := s.WithContext(cancelledCtx)
	cpy.oldCtx = oldCtx
	return cpy
}

// Online returns an online state. If the state is already online, then it
// returns itself.
func (s *State) Online() *State {
	if s.oldCtx == nil {
		return s
	}
	online := s.WithContext(s.oldCtx)
	online.oldCtx = nil
	return online
}

// MessageMentions returns true if the given message mentions the current user.
func (s *State) MessageMentions(msg discord.Message) bool {
	// Ignore own messages.
	if u, err := s.Me(); err == nil && msg.Author.ID == u.ID {
		return false
	}

	var mutedGuild *gateway.UserGuildSetting

	// If there's guild:
	if msg.GuildID.IsValid() {
		mutedGuild := s.MutedState.GuildSettings(msg.GuildID)

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

	// Boolean on whether the message contains a self mention or not:
	var mentioned = messageMentions(msg, s.Ready().User.ID)

	// Check channel settings. Channel settings override guilds.
	mutedCh := s.MutedState.ChannelOverrides(msg.ChannelID)

	switch mutedCh.Notifications {
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

	if mutedGuild != nil {
		switch mutedGuild.Notifications {
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

func joinSession(me discord.User, r *gateway.SessionsReplaceEvent) *discord.Presence {
	ses := *r

	var status discord.Status
	var activities []discord.Activity

	for i := len(ses) - 1; i >= 0; i-- {
		presence := ses[i]

		if presence.Status != "" {
			status = presence.Status
		}

		activities = append(activities, presence.Activities...)
	}

	return &discord.Presence{
		User:       me,
		Status:     status,
		Activities: activities,
	}
}

// PrivateChannels returns the sorted list of private channels from the state.
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

// Channels returns a list of visible channels. Empty categories are
// automatically filtered out.
func (s *State) Channels(guildID discord.GuildID) ([]discord.Channel, error) {
	chs, err := s.State.Channels(guildID)
	if err != nil {
		return nil, err
	}

	filtered := chs[:0]

	// Filter out channels we can't see.
	for _, ch := range chs {
		if s.HasPermissions(ch.ID, discord.PermissionViewChannel) {
			filtered = append(filtered, ch)
		}
	}

	chs = filtered

	categories := make(map[discord.ChannelID]int, 10)
	// Initialize the category map.
	for _, ch := range chs {
		if ch.Type == discord.GuildCategory {
			categories[ch.ID] = 0
		}
	}

	// Count all channels within categories.
	for _, ch := range chs {
		_, ok := categories[ch.ParentID]
		if ok {
			categories[ch.ParentID]++
		}
	}

	filtered = chs[:0]

	// Filter again but exclude all categories with no channels.
	for _, ch := range chs {
		if count, ok := categories[ch.ID]; ok && count == 0 {
			continue
		}
		filtered = append(filtered, ch)
	}

	return filtered, nil
}

// NoPermissionError is returned by AssertPermissions if the user lacks
// the requested permissions.
type NoPermissionError struct {
	Has    discord.Permissions
	Wanted discord.Permissions
}

// Error implemenets error.
func (err *NoPermissionError) Error() string {
	return "user is missing permission"
}

// HasPermissions returns true if AssertPermissions returns a nil error.
func (s *State) HasPermissions(chID discord.ChannelID, perms discord.Permissions) bool {
	return s.AssertPermissions(chID, perms) == nil
}

// AssertPermissions asserts that the current user has the given permissions in
// the given channel. If the assertion fails, a NoPermissionError might be
// returned.
func (s *State) AssertPermissions(chID discord.ChannelID, perms discord.Permissions) error {
	me, err := s.Me()
	if err != nil {
		return errors.Wrap(err, "cannot get current user information")
	}

	p, err := s.Permissions(chID, me.ID)
	if err != nil {
		return errors.Wrap(err, "cannot get permissions")
	}

	if !p.Has(perms) {
		return &NoPermissionError{
			Has:    p,
			Wanted: perms,
		}
	}

	return nil
}

// UnreadIndication indicates the channel as either unread, mentioned (which
// implies unread) or neither.
type UnreadIndication uint8

const (
	ChannelRead UnreadIndication = iota
	ChannelUnread
	ChannelMentioned
)

// ChannelIsUnread returns true if the channel with the given ID has unread
// messages.
func (r *State) ChannelIsUnread(chID discord.ChannelID) UnreadIndication {
	if r.MutedState.Channel(chID) || r.MutedState.Category(chID) {
		return ChannelRead
	}

	if !r.HasPermissions(chID, discord.PermissionViewChannel) {
		return ChannelRead
	}

	ch, err := r.Cabinet.Channel(chID)
	if err != nil || !ch.LastMessageID.IsValid() {
		return ChannelRead
	}

	state := r.ReadState.ReadState(chID)
	if state == nil || !state.LastMessageID.IsValid() {
		return ChannelRead
	}

	if state.MentionCount > 0 {
		return ChannelMentioned
	}

	if state.LastMessageID < ch.LastMessageID {
		return ChannelUnread
	}

	return ChannelRead
}

// GuildIsUnread returns true if the guild contains unread channels.
func (r *State) GuildIsUnread(guildID discord.GuildID, types []discord.ChannelType) UnreadIndication {
	if r.MutedState.Guild(guildID, false) {
		return ChannelRead
	}

	chs, err := r.Cabinet.Channels(guildID)
	if err != nil {
		return ChannelRead
	}

	typeMap := [255]bool{}
	for _, typ := range types {
		typeMap[typ] = true
	}

	ind := ChannelRead

	for _, ch := range chs {
		if !typeMap[ch.Type] {
			continue
		}

		if s := r.ChannelIsUnread(ch.ID); s > ind {
			ind = s
		}
	}

	return ind
}
