package summary

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

var maxSummaries int64 = 10

// SetMaxSummaries sets the maximum number of summaries to keep in memory.
func SetMaxSummaries(max int) {
	atomic.StoreInt64(&maxSummaries, int64(max))
}

type State struct {
	mutex     sync.RWMutex
	state     *state.State
	summaries map[discord.ChannelID][]gateway.ConversationSummary
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	s := &State{
		state:     state,
		summaries: make(map[discord.ChannelID][]gateway.ConversationSummary),
	}

	r.AddSyncHandler(func(u *gateway.ConversationSummaryUpdateEvent) {
		s.mutex.Lock()
		defer s.mutex.Unlock()

		summaries := s.summaries[u.ChannelID]
		for _, summary := range u.Summaries {
			ix, ok := slices.BinarySearchFunc(summaries, summary.ID,
				func(s gateway.ConversationSummary, id discord.Snowflake) int {
					if s.ID > id {
						return 1
					}
					if s.ID < id {
						return -1
					}
					return 0
				},
			)
			if ok {
				summaries[ix] = summary
			} else {
				summaries = slices.Insert(summaries, ix, summary)
			}
		}

		if len(summaries) > int(maxSummaries) {
			summaries = slices.Delete(summaries, 0, len(summaries)-int(maxSummaries))
		}

		s.summaries[u.ChannelID] = summaries
	})

	return s
}

// Summaries returns the summaries for the given channel.
func (s *State) Summaries(channelID discord.ChannelID) []gateway.ConversationSummary {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.summaries[channelID]
}

// LastSummary returns the last summary for the given channel.
func (s *State) LastSummary(channelID discord.ChannelID) *gateway.ConversationSummary {
	summaries := s.Summaries(channelID)
	if len(summaries) == 0 {
		return nil
	}
	return &summaries[len(summaries)-1]
}
