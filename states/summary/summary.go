package summary

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

type State struct {
	state     *state.State
	summaries sync.Map
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	s := &State{
		state: state,
	}

	r.AddSyncHandler(func(u *gateway.ConversationSummaryUpdateEvent) {
		s.summaries.Store(u.ChannelID, u.Summaries)
	})

	return s
}

// Summaries returns the summaries for the given channel.
func (s *State) Summaries(channelID discord.ChannelID) []gateway.ConversationSummary {
	if summaries, ok := s.summaries.Load(channelID); ok {
		return summaries.([]gateway.ConversationSummary)
	}
	return nil
}

// LastSummary returns the last summary for the given channel.
func (s *State) LastSummary(channelID discord.ChannelID) *gateway.ConversationSummary {
	summaries := s.Summaries(channelID)
	if len(summaries) == 0 {
		return nil
	}
	var last int
	for i, summary := range summaries {
		if summary.EndID > summaries[i].EndID {
			last = i
		}
	}
	return &summaries[last]
}
