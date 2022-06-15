package nstore

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/state/store"
)

// PresenceStore is a presence store that allows searching for a user presence
// regardless of the guild they're from.
type PresenceStore struct {
	mut       sync.RWMutex
	presences map[discord.UserID][]discord.Presence
}

func NewPresenceStore() *PresenceStore {
	return &PresenceStore{
		presences: make(map[discord.UserID][]discord.Presence, 100),
	}
}

func (pres *PresenceStore) Reset() error {
	pres.mut.Lock()
	defer pres.mut.Unlock()

	pres.presences = make(map[discord.UserID][]discord.Presence, 100)

	return nil
}

func (pres *PresenceStore) Presence(
	guild discord.GuildID, user discord.UserID) (*discord.Presence, error) {

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
func (pres *PresenceStore) Each(g discord.GuildID, fn func(*discord.Presence) (stop bool)) {
	pres.mut.RLock()
	defer pres.mut.RUnlock()

	for _, presences := range pres.presences {
		if fn(&presences[len(presences)-1]) {
			break
		}
	}
}

// presence gets the presence without locking the mutex.
func (pres *PresenceStore) presence(guild discord.GuildID, user discord.UserID) *discord.Presence {
	presences, ok := pres.presences[user]
	if !ok {
		return nil
	}

	// Prioritize presences from users that are in multiple guilds.
	if guild.IsValid() {
		for _, presence := range presences {
			if presence.GuildID == guild {
				return &presence
			}
		}
	}

	// last is latest
	return &presences[len(presences)-1]
}

// Presences creates a copy of all known presences. It is a fairly costly copy,
// so Each should be preferred over Presences. The only reason this method does
// what is intended is for compatibility with internal state functions that may
// rely on this for some reason.
func (pres *PresenceStore) Presences(guild discord.GuildID) ([]discord.Presence, error) {
	pres.mut.RLock()
	defer pres.mut.RUnlock()

	latestPresences := make([]discord.Presence, 0, len(pres.presences))
	for _, presences := range pres.presences {
		latestPresences = append(latestPresences, presences[len(presences)-1])
	}

	return latestPresences, nil
}

func (pres *PresenceStore) PresenceSet(guild discord.GuildID, p *discord.Presence, update bool) error {
	cpy := *p
	cpy.GuildID = guild

	pres.mut.Lock()
	defer pres.mut.Unlock()

	presences, ok := pres.presences[p.User.ID]
	if !ok {
		pres.presences[p.User.ID] = []discord.Presence{cpy}
		return nil
	}

	for i, presence := range presences {
		if presence.GuildID == guild {
			// Delete this entry and break out of the loop. Add this one to the
			// end of the list always.
			presences = append(presences[:i], presences[i+1:]...)
			break
		}
	}

	presences = append(presences, cpy)
	pres.presences[p.User.ID] = presences

	return nil
}

func (pres *PresenceStore) PresenceRemove(guild discord.GuildID, user discord.UserID) error {
	pres.mut.Lock()
	defer pres.mut.Unlock()

	presences, ok := pres.presences[user]
	if !ok {
		return nil
	}

	for i, presence := range presences {
		if presence.GuildID == guild {
			if len(presences) == 1 {
				delete(pres.presences, user)
				return nil
			}

			presences = append(presences[:i], presences[i+1:]...)
			pres.presences[user] = presences
			return nil
		}
	}

	return nil
}
