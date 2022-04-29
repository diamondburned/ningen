package guild

import (
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

// State contains additional guild states that are only available on join.
type State struct {
	mutex sync.RWMutex
	joins map[discord.GuildID]time.Time
}

func NewState(h handlerrepo.AddHandler) *State {
	s := &State{}

	h.AddSyncHandler(func(r *gateway.ReadyEvent) {
		s.mutex.Lock()
		defer s.mutex.Unlock()

		s.joins = make(map[discord.GuildID]time.Time, len(r.Guilds))
		for _, guild := range r.Guilds {
			s.joins[guild.ID] = guild.Joined.Time()
		}
	})

	return s
}

// JoinedAt returns the time that the user joined the guild or the zero-value if
// there's none.
func (s *State) JoinedAt(guildID discord.GuildID) (time.Time, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	t, ok := s.joins[guildID]
	return t, ok
}
