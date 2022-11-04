// Package ningen contains a set of helpful functions and packages to aid in
// making a Discord client.
package ningen

import (
	"context"
	"sort"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/handler"
	"github.com/diamondburned/arikawa/v3/utils/httputil"
	"github.com/diamondburned/arikawa/v3/utils/ws"
	"github.com/diamondburned/ningen/v3/nstore"
	"github.com/diamondburned/ningen/v3/states/emoji"
	"github.com/diamondburned/ningen/v3/states/guild"
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

func init() {
	gateway.ReadyEventKeepRaw = true
}

// ConnectedEvent is an event that's sent on Ready or Resumed. The event arrives
// before all ningen's handlers are called.
type ConnectedEvent struct {
	gateway.Event
}

// DisconnectedEvent is an event that's sent when the websocket is disconnected.
type DisconnectedEvent struct {
	ws.CloseEvent
}

// IsLoggedOut returns true if the session that Discord gave is now outdated and
// that the user must login again.
func (ev *DisconnectedEvent) IsLoggedOut() bool {
	if ev.Code == -1 {
		return false
	}

	for _, code := range gateway.DefaultGatewayOpts.FatalCloseCodes {
		if code == ev.Code {
			return true
		}
	}

	return false
}

// IsGraceful returns true if the disconnection is done by the websocket and not
// by a connection drop.
func (ev *DisconnectedEvent) IsGraceful() bool {
	return ev.Code != -1
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
	GuildState        *guild.State
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
	state.GuildState = guild.NewState(prehandler)
	state.EmojiState = emoji.NewState(s.Cabinet)
	state.MemberState = member.NewState(s, prehandler)
	state.RelationshipState = relationship.NewState(prehandler)

	s.AddSyncHandler(func(v gateway.Event) {
		switch v := v.(type) {
		case *gateway.SessionsReplaceEvent:
			me, _ := s.Me()
			if me == nil {
				break
			}

			s.PresenceSet(0, joinSession(*me, v), true)

		case *gateway.UserSettingsUpdateEvent:
			me, _ := s.Me()
			if me == nil {
				break
			}

			p, _ := s.PresenceStore.Presence(0, me.ID)
			if p != nil {
				new := *p
				new.Status = v.Status

				if v.CustomStatus != nil {
					customActivity := discord.Activity{
						Name: v.CustomStatus.Text,
					}

					if v.CustomStatus.EmojiName != "" {
						customActivity.Emoji = &discord.Emoji{
							ID:   v.CustomStatus.EmojiID,
							Name: v.CustomStatus.EmojiName,
						}
					}

					new.Activities = append([]discord.Activity{}, new.Activities...)
					for i, activity := range new.Activities {
						if activity.Type == discord.CustomActivity {
							new.Activities[i] = customActivity
							goto found
						}
					}
					new.Activities = append(new.Activities, customActivity)
				found:
				}

				s.PresenceSet(p.GuildID, &new, true)
			}
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

		switch v := v.(type) {
		// Might be better to trigger this on a ReadySupplemental event, as
		// that's when things are truly done?
		case *gateway.ReadyEvent, *gateway.ResumedEvent:
			state.Handler.Call(&ConnectedEvent{v})
		case *ws.CloseEvent:
			state.Handler.Call(&DisconnectedEvent{*v})
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

// MessageMentionFlags is the resulting flag of a MessageMentions check. If it's
// 0, then absolutely no mentions are done, otherwise non-0 is returned.
type MessageMentionFlags uint8

const (
	// MessageMentions is when a message mentions the user either by tagging
	// that user or a role that the user is in.
	MessageMentions MessageMentionFlags = 1 << iota
	// MessageNotifies is when the message should also send a visible
	// notification.
	MessageNotifies
)

// Has returns true if other is in f.
func (f MessageMentionFlags) Has(other MessageMentionFlags) bool {
	return f&other == other
}

// Status returns the user's presence status. Use to check if notifications
// should be sent.
func (s *State) Status() discord.Status {
	me, _ := s.Cabinet.Me()
	if me == nil {
		return discord.OfflineStatus
	}

	if p, _ := s.PresenceStore.Presence(0, me.ID); p != nil {
		return p.Status
	}

	return discord.OfflineStatus
}

// MessageMentions returns true if the given message mentions the current user.
func (s *State) MessageMentions(msg *discord.Message) MessageMentionFlags {
	me, _ := s.Cabinet.Me()
	if me == nil {
		return 0
	}

	// Ignore own messages.
	if msg.Author.ID == me.ID {
		return 0
	}

	// Ignore messages from blocked users.
	if s.UserIsBlocked(msg.Author.ID) {
		return 0
	}

	var mutedGuild gateway.UserGuildSetting

	// If there's guild:
	if msg.GuildID.IsValid() {
		mutedGuild = s.MutedState.GuildSettings(msg.GuildID)

		// We're only checking mutes and suppressions, as channels don't
		// have these. Whatever channels have will override guilds.

		// @everyone mentions still work if the guild is muted and @everyone
		// is not suppressed.
		if msg.MentionEveryone && !mutedGuild.SuppressEveryone {
			return MessageMentions | MessageNotifies
		}

		// TODO: roles

		// If the guild is muted of all messages:
		if mutedGuild.Muted {
			return 0
		}
	}

	var flags MessageMentionFlags
	if messageMentions(msg, me.ID) {
		flags = MessageMentions
	}

	// Check channel settings. Channel settings override guilds.
	mutedCh := s.MutedState.ChannelOverrides(msg.ChannelID)

	switch mutedCh.Notifications {
	case gateway.NoNotifications:
		// No notifications are allowed whatsoever.
		return 0

	case gateway.AllNotifications:
		if mutedCh.Muted {
			return flags
		}

	case gateway.OnlyMentions:
		// If mentions are allowed. We return early because this overrides
		// the guild settings, even if Guild wants all messages.
		if flags != 0 {
			flags |= MessageNotifies
		}
		return flags
	}

	if msg.GuildID.IsValid() {
		switch mutedGuild.Notifications {
		case gateway.NoNotifications:
			// No notifications are allowed whatsoever.
			return 0

		case gateway.AllNotifications:
			if !mutedGuild.Muted {
				// All messages trigger notification if not muted.
				flags |= MessageNotifies
			}
			return flags

		case gateway.OnlyMentions:
			if flags != 0 {
				// If mentioned, will always notify.
				flags |= MessageNotifies
			}
			return flags
		}

	}

	// Is this from a DM? TODO: get a better check.
	if ch, err := s.Cabinet.Channel(msg.ChannelID); err == nil {
		// True if the message is from DM or group.
		if ch.Type == discord.DirectMessage || ch.Type == discord.GroupDM {
			return flags | MessageNotifies
		}
	}

	return flags
}

func messageMentions(msg *discord.Message, uID discord.UserID) bool {
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

	filtered := c[:0]
	for _, ch := range filtered {
		// This sometimes happens. It makes no sense for this to make it
		// through!
		if ch.Type == discord.GroupDM && len(ch.DMRecipients) == 0 {
			continue
		}

		filtered = append(filtered, ch)
	}
	c = filtered

	sort.SliceStable(c, func(i, j int) bool {
		return c[i].LastMessageID > c[j].LastMessageID
	})

	return c, nil
}

// Channels returns a list of visible channels. Empty categories are
// automatically filtered out.
func (s *State) Channels(guildID discord.GuildID, allowedTypes []discord.ChannelType) ([]discord.Channel, error) {
	// I have fully given up on life.
	var allowedMap [64]bool
	for _, t := range allowedTypes {
		allowedMap[t] = true
	}

	chs, err := s.State.Channels(guildID)
	if err != nil {
		return nil, err
	}

	filtered := chs[:0]

	// Filter out channels we can't see.
	for _, ch := range chs {
		if !allowedMap[ch.Type] {
			continue
		}

		// Only check if the channel is not a category, since we're filtering
		// out empty categories anyway.
		if ch.Type != discord.GuildCategory {
			if !s.HasPermissions(ch.ID, discord.PermissionViewChannel) {
				continue
			}
		}

		filtered = append(filtered, ch)
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

// LastMessage returns the last message ID in the given channel.
func (r *State) LastMessage(chID discord.ChannelID) discord.MessageID {
	msgs, _ := r.Cabinet.Messages(chID)
	if len(msgs) > 0 {
		return msgs[0].ID
	}

	ch, _ := r.Cabinet.Channel(chID)
	if ch != nil {
		return ch.LastMessageID
	}

	return 0
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
	state := r.ReadState.ReadState(chID)
	if state == nil || !state.LastMessageID.IsValid() {
		return ChannelRead
	}

	// Mentions override mutes.
	if state.MentionCount > 0 {
		return ChannelMentioned
	}

	if r.MutedState.Channel(chID) || r.MutedState.Category(chID) {
		return ChannelRead
	}

	lastMsgID := r.LastMessage(chID)
	if !lastMsgID.IsValid() {
		return ChannelRead
	}

	// This permission check isn't very important. We can do it just right
	// before the unread check.
	if !r.HasPermissions(chID, discord.PermissionViewChannel) {
		return ChannelRead
	}

	if state.LastMessageID < lastMsgID {
		return ChannelUnread
	}

	return ChannelRead
}

// GuildIsUnread returns true if the guild contains unread channels.
func (r *State) GuildIsUnread(guildID discord.GuildID, types []discord.ChannelType) UnreadIndication {
	chs, err := r.Cabinet.Channels(guildID)
	if err != nil {
		return ChannelRead
	}

	typeMap := [128]bool{}
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

	if isMuted := r.MutedState.Guild(guildID, false); isMuted {
		// Only show mentions for muted guilds.
		if ind != ChannelMentioned {
			return ChannelRead
		}
	}

	return ind
}

// ChanneCountUnreads returns the number of unread messages in the channel.
func (s *State) ChannelCountUnreads(chID discord.ChannelID) int {
	var unread int

	// Grab our known messages so we can count the unread ones.
	msgs, _ := s.Cabinet.Messages(chID)

	readState := s.ReadState.ReadState(chID)
	// We check if either the read state is not known at all or we're getting
	// neither a mention nor a last read message, indicating that we've never
	// read anything here.
	if readState == nil || !readState.LastMessageID.IsValid() {
		// We've never seen this channel before, so we might not have any
		// messages. If we do, then we'll count them, otherwise, we'll just
		// assume that there are 1 (one) unread message.
		if msgs != nil {
			unread = len(msgs)
		} else {
			unread = 1
		}
	} else if msgs != nil {
		// We've seen this channel before, so we'll count (if we can )the unread
		// messages from the last read message.
		for _, msg := range msgs {
			if msg.ID > readState.LastMessageID {
				unread++
			} else {
				// We've reached the last read message, so we can stop counting.
				break
			}
		}
	} else if unread == 0 && s.ChannelIsUnread(chID) != ChannelRead {
		unread = 1
	}

	return unread
}

// SetStatus sets the current user's status and presence.
func (r *State) SetStatus(status discord.Status, custom *gateway.CustomUserStatus, activities ...discord.Activity) error {
	me, _ := r.Me()

	cmd := gateway.UpdatePresenceCommand{
		Status:     status,
		Activities: activities,
	}

	if custom != nil {
		customActivity := discord.Activity{
			Name:  "Custom Status",
			Type:  discord.CustomActivity,
			State: custom.Text,
		}

		if custom.EmojiName != "" {
			customActivity.Emoji = &discord.Emoji{
				ID:   custom.EmojiID,
				Name: custom.EmojiName,
			}
		}

		activities = append([]discord.Activity{}, activities...)
		activities = append(activities, customActivity)
	}

	if p, _ := r.PresenceStore.Presence(0, me.ID); p != nil {
		if status == "" && p.Status != "" {
			cmd.Status = p.Status
		}
		if activities == nil && p.Activities != nil {
			cmd.Activities = p.Activities
		}
	}

	if err := r.Gateway().Send(r.Context(), &cmd); err != nil {
		return errors.Wrap(err, "cannot update gateway")
	}

	// Keep this the same as gateway.UserSettings.
	patchSettings := map[string]interface{}{"status": status}
	if custom != nil {
		patchSettings["custom_status"] = custom
	}

	err := r.FastRequest("PATCH", api.EndpointMe+"/settings", httputil.WithJSONBody(patchSettings))
	return errors.Wrap(err, "cannot update user settings API")
}

// UserIsBlocked returns true if the user with the given ID is blocked by the
// current user.
func (r *State) UserIsBlocked(uID discord.UserID) bool {
	return r.RelationshipState.IsBlocked(uID)
}

// ChannelIsMuted returns true if the channel with the given ID is muted or if
// it's in a category that's muted.
func (r *State) ChannelIsMuted(chID discord.ChannelID, category bool) bool {
	if r.MutedState.Channel(chID) {
		return true
	}

	if !category {
		return false
	}

	c, err := r.Cabinet.Channel(chID)
	if err != nil || !c.ParentID.IsValid() {
		return false
	}

	return r.MutedState.Channel(c.ParentID)
}
