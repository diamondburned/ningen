// Package mute implements a channel/guild muted state. It automatically
// updates.
package mute

import (
	"sync"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/arikawa/v2/state/store"
	"github.com/diamondburned/ningen/v2/handlerrepo"
)

// State implements a queryable channel and guild mute state.
type State struct {
	cab store.Cabinet

	mutex    sync.RWMutex
	settings []gateway.UserGuildSetting
	chMutes  map[discord.ChannelID]*gateway.UserChannelOverride // cache
}

func NewState(cab store.Cabinet, r handlerrepo.AddHandler) *State {
	mutestate := &State{cab: cab}

	r.AddHandler(func(r *gateway.ReadyEvent) {
		mutestate.mutex.Lock()
		defer mutestate.mutex.Unlock()

		mutestate.settings = r.UserGuildSettings
		mutestate.chMutes = map[discord.ChannelID]*gateway.UserChannelOverride{}
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

				mutestate.settings[i] = u.UserGuildSetting
			}
		}
	})

	return mutestate
}

// CategoryMuted returns whether or not the channel's category is muted.
func (m *State) Category(channelID discord.ChannelID) bool {
	c, err := m.cab.Channel(channelID)
	if err != nil || !c.CategoryID.IsValid() {
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

func (m *State) ChannelOverrides(channelID discord.ChannelID) *gateway.UserChannelOverride {
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
		return (!everyone && m.Muted) || (everyone && m.SuppressEveryone)
	}
	return false
}

func (m *State) GuildSettings(guildID discord.GuildID) *gateway.UserGuildSetting {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for _, guild := range m.settings {
		if guild.GuildID == guildID {
			return &guild // copy
		}
	}

	return nil
}
