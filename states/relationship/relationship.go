package relationship

import (
	"sort"
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

type State struct {
	mutex         sync.RWMutex
	relationships map[discord.UserID]discord.RelationshipType
}

func NewState(r handlerrepo.AddHandler) *State {
	rela := &State{
		relationships: map[discord.UserID]discord.RelationshipType{},
	}

	r.AddSyncHandler(func(r *gateway.ReadyEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		rela.relationships = make(map[discord.UserID]discord.RelationshipType, len(r.Relationships))

		for _, rl := range r.Relationships {
			rela.relationships[rl.UserID] = rl.Type
		}
	})

	r.AddSyncHandler(func(add *gateway.RelationshipAddEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		rela.relationships[add.UserID] = add.Type
	})

	r.AddSyncHandler(func(rem *gateway.RelationshipRemoveEvent) {
		rela.mutex.Lock()
		defer rela.mutex.Unlock()

		delete(rela.relationships, rem.UserID)
	})

	return rela
}

func (r *State) Each(fn func(discord.UserID, discord.RelationshipType) (stop bool)) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for userID, rela := range r.relationships {
		if fn(userID, rela) {
			return
		}
	}
}

// Relationship returns the relationship for the given user, or 0 if there is
// none.
func (r *State) Relationship(userID discord.UserID) discord.RelationshipType {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.relationships[userID]
}

// IsBlocked returns if the user is blocked.
func (r *State) IsBlocked(userID discord.UserID) bool {
	return r.Relationship(userID) == discord.BlockedRelationship
}

// BlockedUserIDs returns all blocked users.
func (r *State) BlockedUserIDs() []discord.UserID {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	userIDs := make([]discord.UserID, 0, len(r.relationships))
	for uID, relationship := range r.relationships {
		if relationship != discord.BlockedRelationship {
			continue
		}
		userIDs = append(userIDs, uID)
	}

	sort.Slice(userIDs, func(i, j int) bool {
		return userIDs[i] < userIDs[j]
	})

	return userIDs
}
