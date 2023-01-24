package thread

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/state/store"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

// State contains additional thread states that are not in the built-in state
// cache.
type State struct {
	state   *state.State
	cabinet *store.Cabinet

	joinedMu sync.RWMutex
	joined   map[discord.ChannelID]struct{}
}

func NewState(state *state.State, h handlerrepo.AddHandler) *State {
	s := &State{
		state:   state,
		cabinet: state.Cabinet,
		joined:  make(map[discord.ChannelID]struct{}),
	}

	var userID discord.UserID

	h.AddSyncHandler(func(ev *gateway.ReadyEvent) {
		userID = ev.User.ID

		s.joinedMu.Lock()
		defer s.joinedMu.Unlock()

		for _, guild := range ev.Guilds {
			for _, thread := range guild.Threads {
				s.joined[thread.ID] = struct{}{}
			}
		}
	})

	h.AddSyncHandler(func(ev *gateway.GuildCreateEvent) {
		s.joinedMu.Lock()
		defer s.joinedMu.Unlock()

		for _, thread := range ev.Threads {
			s.joined[thread.ID] = struct{}{}
		}
	})

	h.AddSyncHandler(func(ev *gateway.ThreadMembersUpdateEvent) {
		for _, member := range ev.AddedMembers {
			if member.UserID == userID {
				// We joined a thread.
				s.joinedMu.Lock()
				s.joined[ev.ID] = struct{}{}
				s.joinedMu.Unlock()
				return
			}
		}

		for _, memberID := range ev.RemovedMemberIDs {
			if memberID == userID {
				// We left a thread.
				s.joinedMu.Lock()
				delete(s.joined, ev.ID)
				s.joinedMu.Unlock()
				return
			}
		}
	})

	h.AddSyncHandler(func(ev *gateway.ThreadMemberUpdateEvent) {
		if ev.UserID != userID {
			return
		}

		s.joinedMu.Lock()
		s.joined[ev.ID] = struct{}{}
		s.joinedMu.Unlock()
	})

	return s
}

// ThreadIsJoined returns whether the current user is joined to the given
// thread.
func (s *State) ThreadIsJoined(id discord.ChannelID) bool {
	s.joinedMu.RLock()
	defer s.joinedMu.RUnlock()

	_, ok := s.joined[id]
	return ok
}
