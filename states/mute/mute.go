// Package mute implements a channel/guild muted state. It automatically
// updates.
package mute

import (
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v2/discord"
	"github.com/diamondburned/arikawa/v2/gateway"
	"github.com/diamondburned/arikawa/v2/state/store"
	"github.com/diamondburned/ningen/v2/handlerrepo"
)

// State implements a queryable channel and guild mute state.
type State struct {
	cab store.Cabinet

	mutex    sync.RWMutex
	guilds   map[discord.GuildID]gateway.UserGuildSetting
	channels map[discord.ChannelID]gateway.UserChannelOverride
}

func NewState(cab store.Cabinet, r handlerrepo.AddHandler) *State {
	mute := &State{cab: cab}

	r.AddHandler(func(r *gateway.ReadyEvent) {
		mute.mutex.Lock()
		defer mute.mutex.Unlock()

		mute.guilds = make(map[discord.GuildID]gateway.UserGuildSetting, len(r.UserGuildSettings))
		mute.channels = map[discord.ChannelID]gateway.UserChannelOverride{}

		for i, guild := range r.UserGuildSettings {
			mute.guilds[guild.GuildID] = r.UserGuildSettings[i]

			for i, ch := range guild.ChannelOverrides {
				mute.channels[ch.ChannelID] = guild.ChannelOverrides[i]
			}
		}
	})

	r.AddHandler(func(u *gateway.UserGuildSettingsUpdateEvent) {
		mute.mutex.Lock()
		defer mute.mutex.Unlock()

		mute.guilds[u.GuildID] = u.UserGuildSetting

		for i, ch := range u.ChannelOverrides {
			mute.channels[ch.ChannelID] = u.ChannelOverrides[i]
		}
	})

	return mute
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
	m.mutex.RLock()
	mute, ok := m.channels[channelID]
	m.mutex.RUnlock()

	if !ok || muteConfigInvalid(mute.MuteConfig) {
		return false
	}
	return mute.Muted
}

func (m *State) ChannelOverrides(channelID discord.ChannelID) gateway.UserChannelOverride {
	m.mutex.RLock()
	override, ok := m.channels[channelID]
	m.mutex.RUnlock()

	if ok {
		return override
	}

	noti := gateway.GuildDefaults

	if ch, err := m.cab.Channel(channelID); err == nil {
		noti = m.GuildSettings(ch.GuildID).Notifications
	}

	return gateway.UserChannelOverride{
		Notifications: noti,
		ChannelID:     channelID,
	}
}

// Guild returns whether or not the ping should mention. It works with @everyone
// if everyone is true.
func (m *State) Guild(guildID discord.GuildID, everyone bool) bool {
	m.mutex.RLock()
	mute, ok := m.guilds[guildID]
	m.mutex.RUnlock()

	if !ok || muteConfigInvalid(mute.MuteConfig) {
		return false
	}
	return (!everyone && mute.Muted) || (everyone && mute.SuppressEveryone)
}

func (m *State) GuildSettings(guildID discord.GuildID) gateway.UserGuildSetting {
	m.mutex.RLock()
	setting, ok := m.guilds[guildID]
	m.mutex.RUnlock()

	if ok {
		return setting
	}

	var noti = gateway.AllNotifications

	if guild, _ := m.cab.Guild(guildID); guild != nil {
		switch guild.Notification {
		case discord.AllMessages:
			noti = gateway.AllNotifications
		case discord.OnlyMentions:
			noti = gateway.OnlyMentions
		}
	}

	return gateway.UserGuildSetting{
		GuildID:       guildID,
		Notifications: noti,
	}
}

func muteConfigInvalid(mute *gateway.UserMuteConfig) bool {
	// If there is no config, then it's a permanent mute.
	if mute == nil {
		return false
	}
	// Else, if the time is before now, then it's an expired mute, therefore
	// invalid. Return true.
	now := time.Now()
	return mute.EndTime.Time().Before(now)
}
