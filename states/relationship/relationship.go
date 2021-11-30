package relationship

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

type State struct {
	mutex         sync.RWMutex
	relationships map[discord.UserID]discord.Relationship
}

func NewState(r handlerrepo.AddHandler) *State {
	rela := &State{
		relationships: map[discord.UserID]discord.Relationship{},
	}

	r.AddSyncHandler(func(r *gateway.ReadyEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		rela.relationships = make(map[discord.UserID]discord.Relationship, len(r.Relationships))

		for _, rl := range r.Relationships {
			rela.relationships[rl.UserID] = rl
		}
	})

	r.AddSyncHandler(func(add *gateway.RelationshipAddEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		rela.relationships[add.UserID] = add.Relationship
	})

	r.AddSyncHandler(func(rem *gateway.RelationshipRemoveEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		delete(rela.relationships, rem.UserID)
	})

	return rela
}

func (r *State) Each(fn func(*discord.Relationship) (stop bool)) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for userID, rela := range r.relationships {
		stop := fn(&rela)
		r.relationships[userID] = rela

		if stop {
			return
		}
	}
}

// Relationship returns the relationship for the given user, or 0 if there is
// none.
func (r *State) Relationship(userID discord.UserID) discord.RelationshipType {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if t, ok := r.relationships[userID]; ok {
		return t.Type
	}

	return 0
}

// Blocked returns if the user is blocked.
func (r *State) Blocked(userID discord.UserID) bool {
	return r.Relationship(userID) == discord.BlockedRelationship
}
