package note

import (
	"sync"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/ningen/handler"
)

type State struct {
	mutex sync.RWMutex
	notes map[discord.Snowflake]string
}

func NewState(r handler.AddHandler) *State {
	state := &State{
		notes: map[discord.Snowflake]string{},
	}

	r.AddHandler(func(r *gateway.ReadyEvent) {
		state.mutex.Lock()
		defer state.mutex.Unlock()

		state.notes = r.Notes
	})

	r.AddHandler(func(r *gateway.UserNoteUpdateEvent) {
		state.mutex.Lock()
		defer state.mutex.Unlock()

		state.notes[r.ID] = r.Note
	})

	return state
}

// Note returns the note for the given user, or an empty string if none.
func (s *State) Note(userID discord.Snowflake) string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	note, _ := s.notes[userID]
	return note
}
