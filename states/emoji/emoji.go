package emoji

import (
	"sort"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/state"
	"github.com/pkg/errors"
)

type State struct {
	store state.Store
}

type Guild struct {
	discord.Guild
	Emojis []discord.Emoji
}

func NewState(store state.Store) *State {
	return &State{
		store: store,
	}
}

// Get returns all emojis if the user has Nitro, else only images.
func (s *State) Get(guildID discord.Snowflake) ([]Guild, error) {
	u, err := s.store.Me()
	if err == nil && u.Nitro != discord.NoUserNitro {
		return s.allEmojis(guildID)
	}

	// User doesn't have Nitro, so only non-GIF guild emojis are available:

	// If we don't have a guildID, return nothing.
	if !guildID.Valid() {
		return nil, nil
	}

	g, err := s.store.Guild(guildID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get guild")
	}

	emojis, err := s.store.Emojis(guildID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get emojis")
	}

	filtered := emojis[:0]

	for _, e := range emojis {
		if e.Animated == false {
			filtered = append(filtered, e)
		}
	}

	if len(filtered) == 0 {
		// No emojis.
		return nil, nil
	}

	return []Guild{{
		Guild:  *g,
		Emojis: emojis,
	}}, nil
}

func (s *State) allEmojis(firstGuild discord.Snowflake) ([]Guild, error) {
	// User has Nitro, grab all emojis.
	guilds, err := s.store.Guilds()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get guilds")
	}

	var emojis = make([]Guild, 0, len(guilds))

	for _, g := range guilds {
		if e, err := s.store.Emojis(g.ID); err == nil {
			if len(e) == 0 {
				continue
			}

			emojis = append(emojis, Guild{
				Guild:  g,
				Emojis: e,
			})
		}
	}

	// Put the searched emoji in front.
	if firstGuild.Valid() {
		sort.SliceStable(emojis, func(i, j int) bool {
			return emojis[i].ID == firstGuild
		})
	}

	return emojis, nil
}
