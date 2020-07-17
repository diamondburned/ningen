// Package read implements a read state with an event handler API.
package read

import (
	"sync"

	"github.com/diamondburned/arikawa/api"
	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/handler"
	"github.com/diamondburned/arikawa/state"
	"github.com/diamondburned/ningen/handlerrepo"
)

type UpdateEvent struct {
	gateway.ReadState
	Unread bool
}

type State struct {
	mutex  sync.Mutex
	state  *state.State
	states map[discord.Snowflake]*gateway.ReadState

	selfID    discord.Snowflake
	onUpdates *handler.Handler

	lastAck  api.Ack
	ackMutex sync.Mutex
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	readstate := &State{
		state:  state,
		states: make(map[discord.Snowflake]*gateway.ReadState),
	}

	readstate.onUpdates = handler.New()
	readstate.onUpdates.Synchronous = true

	u, err := state.Me()
	if err != nil {
		// TODO: remove panic?
		panic("Failed to get current user's ID: " + err.Error())
	}

	readstate.selfID = u.ID

	r.AddHandler(func(r *gateway.ReadyEvent) {
		readstate.mutex.Lock()
		defer readstate.mutex.Unlock()

		for i, rs := range r.ReadState {
			readstate.states[rs.ChannelID] = &r.ReadState[i]
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

func (r *State) FindLast(channelID discord.Snowflake) *gateway.ReadState {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if s, ok := r.states[channelID]; ok && s.LastMessageID.Valid() {
		return s
	}
	return nil
}

func (r *State) MarkUnread(chID, msgID discord.Snowflake, mentions int) {
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

	if ch, _ := r.state.Store.Channel(chID); ch != nil {
		ch.LastMessageID = msgID
		r.state.ChannelSet(ch)
	}

	if msg, _ := r.state.Store.Message(chID, msgID); msg != nil {
		if msg.Author.ID == r.selfID {
			// If the message is ours, we should marrk it as already read, since
			// it is registered like that on the Discord servers.
			rs.LastMessageID = msgID
		}
	}

	// Whether or not the message is read.
	unread := rs.LastMessageID != msgID
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

func (r *State) MarkRead(chID, msgID discord.Snowflake) {
	// send ack
	r.markRead(chID, msgID, true)
}

func (r *State) markRead(chID, msgID discord.Snowflake, sendack bool) {
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
	if rs.LastMessageID == msgID {
		return
	}

	// Update.
	rs.LastMessageID = msgID
	rs.MentionCount = 0

	// copy
	rscp := *rs

	// log.Println("MarkRead called at", string(debug.Stack()))

	// Send out Ack in the background, but only if we explicitly want to, that
	// is, if MarkRead is called. In the event that the gateway receives an Ack,
	// we don't want to send another one of the same.
	go r.ack(chID, msgID)

	go func() {
		// Announce that there is a change.
		update := &UpdateEvent{rscp, false}
		r.onUpdates.Call(update)
	}()
}

func (r *State) ack(chID, msgID discord.Snowflake) {
	m, err := r.state.Store.Message(chID, msgID)
	if err != nil {
		return
	}
	// Check if this is our message or not. If it is, don't ack.
	if m.Author.ID == r.selfID {
		return
	}

	// Atomically guard the last ack struct.
	r.ackMutex.Lock()
	defer r.ackMutex.Unlock()

	// Send over Ack.
	r.state.Ack(chID, msgID, &r.lastAck)
}
