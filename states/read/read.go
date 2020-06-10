// Package read implements a read state with an event handler API.
package read

import (
	"sync"

	"github.com/diamondburned/arikawa/api"
	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/state"
	"github.com/diamondburned/ningen/handler"
)

type OnChange = func(ch gateway.ReadState, unread bool)

type State struct {
	mutex  sync.Mutex
	state  *state.State
	states map[discord.Snowflake]*gateway.ReadState

	selfID    discord.Snowflake
	onChanges []OnChange

	lastAck  api.Ack
	ackMutex sync.Mutex
}

func NewState(state *state.State, r handler.AddHandler) *State {
	readstate := &State{
		state:  state,
		states: make(map[discord.Snowflake]*gateway.ReadState),
	}

	u, err := state.Me()
	if err != nil {
		// TODO: remove panic?
		panic("Failed to get current user's ID.")
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
		readstate.MarkRead(a.ChannelID, a.MessageID)
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

// OnReadChange adds a read change callback into the list. This function is not
// thread-safe.
func (r *State) OnChange(fn OnChange) {
	r.onChanges = append(r.onChanges, fn)
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
		for _, fn := range r.onChanges {
			fn(rscp, unread)
		}
	}()
}

func (r *State) MarkRead(chID, msgID discord.Snowflake) {
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

	// Send out Ack in the background.
	go r.ack(chID, msgID)

	go func() {
		// Announce.
		for _, fn := range r.onChanges {
			fn(rscp, false)
		}
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

	r.ackMutex.Lock()
	defer r.ackMutex.Unlock()

	// Send over Ack.
	r.state.Ack(chID, msgID, &r.lastAck)
}
