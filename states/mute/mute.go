// Package mute implements a channel/guild muted state. It automatically
// updates.
package mute

import (
	"sync"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/state"
	"github.com/diamondburned/ningen/handlerrepo"
)

// State implements a queryable channel and guild mute state.
type State struct {
	mutex    sync.RWMutex
	store    state.Store
	settings []gateway.UserGuildSettings
	chMutes  map[discord.ChannelID]*gateway.SettingsChannelOverride // cache
}

func NewState(store state.Store, r handlerrepo.AddHandler) *State {
	mutestate := &State{store: store}

	r.AddHandler(func(r *gateway.ReadyEvent) {
		mutestate.mutex.Lock()
		defer mutestate.mutex.Unlock()

		mutestate.settings = r.UserGuildSettings
		mutestate.chMutes = map[discord.ChannelID]*gateway.SettingsChannelOverride{}
	})

	r.AddHandler(func(u *gateway.UserGuildSettingsUpdateEvent) {
		mutestate.mutex.Lock()
		defer mutestate.mutex.Unlock()

		for i, guild := range mutestate.settings {
			if guild.GuildID == u.GuildID {
				// Invalidate the channel cache first.
				for _, ch := range guild.ChannelOverrides {
					delete(mutestate.chMutes, ch.ChannelID)
				}

				mutestate.settings[i] = u.UserGuildSettings
			}
		}
	})

	return mutestate
}

// CategoryMuted returns whether or not the channel's category is muted.
func (m *State) Category(channelID discord.ChannelID) bool {
	c, err := m.store.Channel(channelID)
	if err != nil || !c.CategoryID.Valid() {
		return false
	}

	return m.Channel(c.CategoryID)
}

// Channel returns whether or not the channel is muted.
func (m *State) Channel(channelID discord.ChannelID) bool {
	if m := m.ChannelOverrides(channelID); m != nil {
		return m.Muted
	}
	return false
}

func (m *State) ChannelOverrides(channelID discord.ChannelID) *gateway.SettingsChannelOverride {
	m.mutex.RLock()
	if mute, ok := m.chMutes[channelID]; ok {
		m.mutex.RUnlock()
		return mute
	}
	m.mutex.RUnlock()

	for i, guild := range m.settings {
		for j, ch := range guild.ChannelOverrides {
			if ch.ChannelID == channelID {
				// cache
				m.mutex.Lock()
				m.chMutes[channelID] = &m.settings[i].ChannelOverrides[j]
				m.mutex.Unlock()

				return &ch
			}
		}
	}

	return nil
}

// Guild returns whether or not the ping should mention. It works with @everyone
// if everyone is true.
func (m *State) Guild(guildID discord.GuildID, everyone bool) bool {
	if m := m.GuildSettings(guildID); m != nil {
		return (!everyone && m.Muted) || (everyone && m.SupressEveryone)
	}
	return false
}

func (m *State) GuildSettings(guildID discord.GuildID) *gateway.UserGuildSettings {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for _, guild := range m.settings {
		if guild.GuildID == guildID {
			return &guild // copy
		}
	}

	return nil
}
