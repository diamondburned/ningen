package nstore

import (
	"sync"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/arikawa/v2/state/store"
)

// PresenceStore is a presence store that allows searching for a user presence
// regardless of the guild they're from. A zero-value instance is a valid
// instance, as long as it is given before a Ready event arrives.
type PresenceStore struct {
	mut        sync.RWMutex
	presences  map[discord.UserID]*gateway.Presence
	userGuilds map[discord.UserID]map[discord.GuildID]*gateway.Presence
}

func NewPresenceStore() *PresenceStore {
	return &PresenceStore{}
}

func (pres *PresenceStore) Reset() error {
	pres.mut.Lock()
	defer pres.mut.Unlock()

	pres.presences = map[discord.UserID]*gateway.Presence{}
	pres.userGuilds = map[discord.UserID]map[discord.GuildID]*gateway.Presence{}

	return nil
}

func (pres *PresenceStore) Presence(
	guild discord.GuildID, user discord.UserID) (*gateway.Presence, error) {

	pres.mut.RLock()
	defer pres.mut.RUnlock()

	presence := pres.presence(guild, user)
	if presence == nil {
		return nil, store.ErrNotFound
	}

	return presence, nil
}

// Each iterates over all presences in undefined order. If a valid guild ID is
// given, then the presence passed into the callback will be from that guild if
// available. Otherwise, a fallback is given.
//
// A read mutex is acquired, so the given callback is allowed to call
// PresenceStore's Presence and Presences methods. The given callback must not
// store the pointer outside of the callback; it must do so after making its own
// copy.
func (pres *PresenceStore) Each(g discord.GuildID, fn func(*gateway.Presence) (stop bool)) error {
	pres.mut.RLock()
	defer pres.mut.RUnlock()

	for userID := range pres.presences {
		// Use the getter instead of our own copy from range, which is a bit
		// slower, but should be trivial.
		presence := pres.presence(g, userID)

		if fn(presence) {
			break
		}
	}

	return nil
}

// presence gets the presence without locking the mutex.
func (pres *PresenceStore) presence(guild discord.GuildID, user discord.UserID) *gateway.Presence {
	// Prioritize presences from users that are in multiple guilds.
	if guild.IsValid() {
		guilds, ok := pres.userGuilds[user]
		if ok {
			presence, ok := guilds[guild]
			if ok {
				return presence
			}
		}
	}

	presence, ok := pres.presences[user]
	if ok {
		return presence
	}

	return nil
}

// Presences creates a copy of all known presences. It is a fairly costly copy,
// so Each should be preferred over Presences. The only reason this method does
// what is intended is for compatibility with internal state functions that may
// rely on this for some reason.
func (pres *PresenceStore) Presences(guild discord.GuildID) ([]gateway.Presence, error) {
	pres.mut.RLock()
	defer pres.mut.RUnlock()

	var presences = make([]gateway.Presence, 0, len(pres.presences))
	for userID := range pres.presences {
		presence := pres.presence(guild, userID)
		presences = append(presences, *presence)
	}

	return presences, nil
}

func (pres *PresenceStore) PresenceSet(guild discord.GuildID, p gateway.Presence) error {
	pres.mut.Lock()
	defer pres.mut.Unlock()

	// Do we already have this presence?
	presence, ok := pres.presences[p.User.ID]
	if !ok {
		// If not, then don't bother adding a guild record.
		pres.presences[p.User.ID] = &p
		return nil
	}

	// We already have it, so we'll both update it and add a new guild record.
	// We'll set the guild presence to this copy instead of the fallback.

	guilds, ok := pres.userGuilds[p.User.ID]
	if ok {
		guilds[guild] = &p
		return nil
	}

	guilds = map[discord.GuildID]*gateway.Presence{}
	pres.userGuilds[p.User.ID] = guilds

	// If we're inserting a presence with the same guild ID, then we should
	// replace the fallback one as well.
	if presence.GuildID == guild {
		*presence = p
		guilds[guild] = presence
	} else {
		guilds[guild] = &p
	}

	return nil
}

func (pres *PresenceStore) PresenceRemove(guild discord.GuildID, user discord.UserID) error {
	pres.mut.Lock()
	defer pres.mut.Unlock()

	if _, ok := pres.presences[user]; !ok {
		return nil
	}

	// Check if we have the presence in a guild record. If we do, then we
	// shouldn't wipe the user from the fallback map just yet. Else, we can
	// safely wipe the user off.
	guilds, ok := pres.userGuilds[user]
	if !ok {
		delete(pres.presences, user)
		return nil
	}

	// Delete the given guild off of the user's record.
	delete(guilds, guild)

	// If we no longer have any guilds, then cleanup.
	if len(guilds) == 0 {
		delete(pres.presences, user)
		delete(pres.userGuilds, user)
	}

	return nil
}
