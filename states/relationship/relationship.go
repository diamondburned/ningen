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
	rela := &State{
		relationshipIDs: map[discord.UserID]*discord.Relationship{},
	}

	r.AddHandler(func(r *gateway.ReadyEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		rela.relationships = make([]*discord.Relationship, len(r.Relationships))
		rela.relationshipIDs = make(map[discord.UserID]*discord.Relationship, len(r.Relationships))

		for i, rl := range r.Relationships {
			rl := rl
			rela.relationships[i] = &rl
			rela.relationshipIDs[rl.UserID] = &rl
		}
	})

	r.AddHandler(func(add *gateway.RelationshipAddEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		if r, ok := rela.relationshipIDs[add.UserID]; ok {
			*r = add.Relationship
			return
		}

		rela.relationshipIDs[add.UserID] = &add.Relationship
		rela.relationships = append(rela.relationships, &add.Relationship)
	})

	r.AddHandler(func(rem *gateway.RelationshipRemoveEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		for i, r := range rela.relationships {
			if r.UserID == rem.UserID {
				relationships := rela.relationships

				relationships[i] = relationships[len(relationships)-1]
				relationships[len(relationships)-1] = nil
				rela.relationships = relationships[:len(relationships)-1]

				delete(rela.relationshipIDs, rem.UserID)

				return
			}
		}
	})

	return rela
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
