package note

import (
	"sync"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/arikawa/v2/state"
	"github.com/diamondburned/ningen/v2/handlerrepo"
)

type State struct {
	mutex    sync.Mutex
	state    *state.State
	notes    map[discord.UserID]string
	fetching map[discord.UserID]struct{}
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	noteState := &State{
		state:    state,
		notes:    map[discord.UserID]string{},
		fetching: map[discord.UserID]struct{}{},
	}

	r.AddHandler(func(u *gateway.UserNoteUpdateEvent) {
		noteState.mutex.Lock()
		defer noteState.mutex.Unlock()

		noteState.notes[u.ID] = u.Note
	})

	return noteState
}

// Note returns the note for the given user, or an empty string if none.
func (s *State) Note(userID discord.UserID) string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	note, ok := s.notes[userID]
	if ok {
		return note
	}

	if _, ok := s.fetching[userID]; ok {
		return ""
	}

	s.fetching[userID] = struct{}{}

	go func() {
		note, _ := s.state.Note(userID)

		s.mutex.Lock()
		defer s.mutex.Unlock()

		s.notes[userID] = note
		delete(s.fetching, userID)
	}()

	return ""
}
