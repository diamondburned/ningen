// Package read implements a read state with an event handler API.
package read

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/ws"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

type UpdateEvent struct {
	gateway.ReadState
	GuildID discord.GuildID
	Unread  bool
}

var _ gateway.Event = (*UpdateEvent)(nil)

func (ev UpdateEvent) Op() ws.OpCode           { return -1 }
func (ev UpdateEvent) EventType() ws.EventType { return "__read.UpdateEvent" }

type State struct {
	mutex  sync.Mutex
	state  *state.State
	states map[discord.ChannelID]*gateway.ReadState

	selfID discord.UserID
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	readstate := &State{
		state:  state,
		states: make(map[discord.ChannelID]*gateway.ReadState),
	}

	r.AddSyncHandler(func(r *gateway.ReadyEvent) {
		// Discord sucks massive fucking balls.
		// They sometimes do this. Probably because of the .capabilities field.
		// Not sure why.
		var undocumentedWeirdness struct {
			ReadStates struct {
				Entries []gateway.ReadState `json:"entries"`
			} `json:"read_state"`
		}
		json.Unmarshal(r.RawEventBody, &undocumentedWeirdness)

		readstate.mutex.Lock()
		defer readstate.mutex.Unlock()

		readstate.selfID = r.User.ID

		for i, rs := range r.ReadStates {
			readstate.states[rs.ChannelID] = &r.ReadStates[i]
		}
		for i, rs := range undocumentedWeirdness.ReadStates.Entries {
			readstate.states[rs.ChannelID] = &undocumentedWeirdness.ReadStates.Entries[i]
		}
	})

	r.AddSyncHandler(func(a *gateway.MessageAckEvent) {
		// do not send a duplicate ack
		readstate.markRead(a.ChannelID, a.MessageID, false)
	})

	r.AddSyncHandler(func(c *gateway.MessageCreateEvent) {
		ch, _ := state.Cabinet.Channel(c.ChannelID)
		if ch != nil {
			cpy := *ch
			cpy.LastMessageID = c.ID
			state.ChannelSet(&cpy, true)
		}

		selfID := readstate.SelfID()
		if c.Author.ID == selfID {
			readstate.markRead(c.ChannelID, c.ID, false)
			return
		}

		var mentions int
		for _, u := range c.Mentions {
			if u.ID == selfID {
				mentions++
			}
		}

		readstate.MarkUnread(c.ChannelID, c.ID, mentions)
	})

	return readstate
}

func (r *State) SelfID() discord.UserID {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.selfID
}

// ReadState gets the read state for a channel.
func (r *State) ReadState(channelID discord.ChannelID) *gateway.ReadState {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if s, ok := r.states[channelID]; ok && s.LastMessageID.IsValid() {
		return s
	}
	return nil
}

func (r *State) MarkUnread(chID discord.ChannelID, msgID discord.MessageID, mentions int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	rs, ok := r.states[chID]
	if !ok {
		rs = &gateway.ReadState{
			ChannelID: chID,
		}
		r.states[chID] = rs
	}

	rs.MentionCount += mentions

	ch, _ := r.state.Cabinet.Channel(chID)
	if ch == nil {
		return
	}

	if ch.LastMessageID < msgID {
		ch.LastMessageID = msgID
		r.state.ChannelSet(ch, false)
	}

	if msg, _ := r.state.Cabinet.Message(chID, msgID); msg != nil {
		if msg.Author.ID == r.selfID {
			// If the message is ours, we should marrk it as already read, since
			// it is registered like that on the Discord servers.
			rs.LastMessageID = msgID
			// Reset the mentions as well.
			rs.MentionCount = 0
		}
	}

	// The message is not read if the last read message's ID is less than the
	// latest message's ID. We don't check for inequality since the latest
	// message may have been deleted, leading to us trying to mark a deleted
	// message.
	unread := rs.LastMessageID < msgID
	rscp := *rs

	// Force callbacks to run in a goroutine. This is because MarkRead and
	// MarkUnread may be called by the user in their main thread, which means
	// these callbacks may occupy the main loop. It may also run in any other
	// goroutine, making it impossible to properly synchronize these callbacks.
	// Doing this helps making a consistent synchronizing behavior.
	go func() {
		// Announce that there is a change.
		r.state.Call(&UpdateEvent{
			ReadState: rscp,
			GuildID:   ch.GuildID,
			Unread:    unread,
		})
	}()
}

func (r *State) MarkRead(chID discord.ChannelID, msgID discord.MessageID) {
	// send ack
	r.markRead(chID, msgID, true)
}

func (r *State) markRead(chID discord.ChannelID, msgID discord.MessageID, sendack bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	rs, ok := r.states[chID]
	if !ok {
		rs = &gateway.ReadState{
			ChannelID: chID,
		}
		r.states[chID] = rs
	}

	// If we've already marked the message as read.
	if rs.LastMessageID == msgID && rs.MentionCount == 0 {
		// log.Println("ningen: NOT acking", chID, "for message", msgID)
		return
	}

	// Update.
	// prevMessageID := rs.LastMessageID
	rs.LastMessageID = msgID
	rs.MentionCount = 0

	// Send out Ack in the background, but only if we explicitly want to, that
	// is, if MarkRead is called and sendAck is true. In the event that the
	// gateway receives an Ack, we don't want to send another one of the same.
	if sendack {
		m, err := r.state.Cabinet.Message(chID, msgID)
		if err != nil {
			// log.Println("ningen: trying to ack unknown message", msgID, "in channel", chID)
			return
		}

		// If there is an error or there is none and we know this message isn't
		// ours, then ack.
		if m.Author.ID != r.selfID {
			// log.Println("ningen: actually acking", chID, "for message", msgID, "was", prevMessageID)
			go r.ack(chID, msgID)
		}
	}

	// copy
	rscp := *rs

	go func() {
		ch, _ := r.state.Cabinet.Channel(chID)
		if ch == nil {
			return
		}

		// Announce that there is a change.
		r.state.Call(&UpdateEvent{
			ReadState: rscp,
			GuildID:   ch.GuildID,
			Unread:    false,
		})
	}()
}

func (r *State) ack(chID discord.ChannelID, msgID discord.MessageID) {
	if err := r.state.Ack(chID, msgID, &api.Ack{}); err != nil {
		log.Println("Discord: message ack failed:", err)
	}
}
