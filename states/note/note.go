package note

import (
	"sync"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/ningen/handlerrepo"
)

type State struct {
	mutex sync.RWMutex
	notes map[discord.UserID]string
}

func NewState(r handlerrepo.AddHandler) *State {
	state := &State{
		notes: map[discord.UserID]string{},
	}

	// r.AddHandler(func(r *gateway.ReadyEvent) {
	// 	state.mutex.Lock()
	// 	defer state.mutex.Unlock()

	// 	state.notes = r.Notes
	// })

	// r.AddHandler(func(r *gateway.UserNoteUpdateEvent) {
	// 	state.mutex.Lock()
	// 	defer state.mutex.Unlock()

	// 	state.notes[r.ID] = r.Note
	// })

	return state
}

// TODO

// Note returns the note for the given user, or an empty string if none.
func (s *State) Note(userID discord.UserID) string {
	return ""
}
