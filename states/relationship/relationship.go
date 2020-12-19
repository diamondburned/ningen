package relationship

import (
	"sync"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/ningen/v2/handlerrepo"
)

type State struct {
	mutex           sync.RWMutex
	relationships   []*discord.Relationship
	relationshipIDs map[discord.UserID]*discord.Relationship
}

func NewState(r handlerrepo.AddHandler) *State {
	relastate := &State{
		relationshipIDs: map[discord.UserID]*discord.Relationship{},
	}

	r.AddHandler(func(r *gateway.ReadyEvent) {
		relastate.mutex.Lock()
		defer relastate.mutex.Unlock()

		relastate.relationships = make([]*discord.Relationship, len(r.Relationships))

		for i, rl := range r.Relationships {
			rl := rl
			relastate.relationships[i] = &rl
			relastate.relationships[rl.UserID] = &rl
		}
	})

	r.AddHandler(func(add *gateway.RelationshipAddEvent) {
		relastate.mutex.Lock()
		defer relastate.mutex.Unlock()

		if r, ok := relastate.relationshipIDs[add.UserID]; ok {
			*r = add.Relationship
			return
		}

		relastate.relationships[add.UserID] = &add.Relationship
		relastate.relationships = append(relastate.relationships, &add.Relationship)
	})

	r.AddHandler(func(rem *gateway.RelationshipRemoveEvent) {
		relastate.mutex.Lock()
		defer relastate.mutex.Unlock()

		for i, rela := range relastate.relationships {
			if rela.UserID == rem.UserID {
				relationships := relastate.relationships

				relationships[i] = relationships[len(relationships)-1]
				relationships[len(relationships)-1] = nil
				relastate.relationships = relationships[:len(relationships)-1]

				delete(relastate.relationshipIDs, rem.UserID)

				return
			}
		}
	})

	return relastate
}

func (r *State) Each(fn func(*discord.Relationship) (stop bool)) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, rela := range r.relationships {
		if fn(rela) {
			return
		}
	}
}

// Relationship returns the relationship for the given user, or 0 if there is
// none.
func (r *State) Relationship(userID discord.UserID) discord.RelationshipType {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if t, ok := r.relationshipIDs[userID]; ok {
		return t.Type
	}

	return 0
}

// Blocked returns if the user is blocked.
func (r *State) Blocked(userID discord.UserID) bool {
	return r.Relationship(userID) == discord.BlockedRelationship
}
