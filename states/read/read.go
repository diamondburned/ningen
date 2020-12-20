// Package read implements a read state with an event handler API.
package read

import (
	"sync"

	"github.com/diamondburned/arikawa/v2/api"
	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/arikawa/v2/state"
	"github.com/diamondburned/arikawa/v2/utils/handler"
	"github.com/diamondburned/ningen/v2/handlerrepo"
)

type UpdateEvent struct {
	gateway.ReadState
	Unread bool
}

type State struct {
	mutex  sync.Mutex
	state  *state.State
	states map[discord.ChannelID]*gateway.ReadState

	selfID    discord.UserID
	onUpdates *handler.Handler

	lastAck  api.Ack
	ackMutex sync.Mutex
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	readstate := &State{
		state:  state,
		states: make(map[discord.ChannelID]*gateway.ReadState),
	}

	readstate.onUpdates = handler.New()
	readstate.onUpdates.Synchronous = true

	r.AddHandler(func(r *gateway.ReadyEvent) {
		readstate.mutex.Lock()
		defer readstate.mutex.Unlock()

		readstate.selfID = r.User.ID

		for i, rs := range r.ReadStates {
			readstate.states[rs.ChannelID] = &r.ReadStates[i]
		}
	})

	r.AddHandler(func(a *gateway.MessageAckEvent) {
		// do not send a duplicate ack
		readstate.markRead(a.ChannelID, a.MessageID, false)
	})

	r.AddHandler(func(c *gateway.MessageCreateEvent) {
		var mentions int

		for _, u := range c.Mentions {
			if u.ID == readstate.selfID {
				mentions++
			}
		}

		readstate.MarkUnread(c.ChannelID, c.ID, mentions)
	})

	return readstate
}

// OnUpdate adds a read update callback into the list. This function is
// thread-safe. It is synchronous by default.
func (r *State) OnUpdate(fn func(*UpdateEvent)) (rm func()) {
	return r.onUpdates.AddHandler(fn)
}

func (r *State) FindLast(channelID discord.ChannelID) *gateway.ReadState {
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

	if ch, _ := r.state.Cabinet.Channel(chID); ch != nil {
		ch.LastMessageID = msgID
		r.state.ChannelSet(*ch)
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
		update := &UpdateEvent{rscp, unread}
		r.onUpdates.Call(update)
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
	if rs.LastMessageID >= msgID {
		return
	}

	// Update.
	rs.LastMessageID = msgID
	rs.MentionCount = 0

	// log.Println("MarkRead called at", string(debug.Stack()))

	// Send out Ack in the background, but only if we explicitly want to, that
	// is, if MarkRead is called and sendAck is true. In the event that the
	// gateway receives an Ack, we don't want to send another one of the same.
	if sendack {
		m, err := r.state.Cabinet.Message(chID, msgID)
		// If there is an error or there is none and we know this message isn't
		// ours, then ack.
		if err != nil || m.Author.ID != r.selfID {
			go r.ack(chID, msgID)
		}
	}

	// copy
	rscp := *rs

	go func() {
		// Announce that there is a change.
		update := &UpdateEvent{rscp, false}
		r.onUpdates.Call(update)
	}()
}

func (r *State) ack(chID discord.ChannelID, msgID discord.MessageID) {
	// Atomically guard the last ack struct.
	r.ackMutex.Lock()
	defer r.ackMutex.Unlock()

	// Send over Ack.
	r.state.Ack(chID, msgID, &r.lastAck)
}
