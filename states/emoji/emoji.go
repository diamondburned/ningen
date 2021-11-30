package emoji

import (
	"sort"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/state/store"
	"github.com/pkg/errors"
)

type State struct {
	cab        *store.Cabinet
	emojiStore store.EmojiStore
}

type Guild struct {
	discord.Guild
	Emojis []discord.Emoji
}

func NewState(cab *store.Cabinet) *State {
	return &State{
		cab: cab,
	}
}

// Get returns all emojis if the user has Nitro, else only images.
func (s *State) Get(guildID discord.GuildID) ([]Guild, error) {
	u, err := s.cab.Me()
	if err == nil && u.Nitro != discord.NoUserNitro {
		return s.allEmojis(guildID)
	}

	// User doesn't have Nitro, so only non-GIF guild emojis are available:

	// If we don't have a guildID, return nothing.
	if !guildID.IsValid() {
		return nil, nil
	}

	g, err := s.cab.Guild(guildID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get guild")
	}

	emojis, err := s.cab.Emojis(guildID)
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

func (s *State) allEmojis(firstGuild discord.GuildID) ([]Guild, error) {
	// User has Nitro, grab all emojis.
	guilds, err := s.cab.Guilds()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get guilds")
	}

	var emojis = make([]Guild, 0, len(guilds))

	for _, g := range guilds {
		if e, err := s.cab.Emojis(g.ID); err == nil {
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
	if firstGuild.IsValid() {
		sort.SliceStable(emojis, func(i, j int) bool {
			return emojis[i].ID == firstGuild
		})
	}

	return emojis, nil
}
