package nstore

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/state/store"
)

type MemberStore struct {
	mut    sync.Mutex
	guilds map[discord.GuildID]*guildMembers
}

type guildMembers struct {
	mut     sync.RWMutex
	members map[discord.UserID]discord.Member
}

func newGuildMembers() *guildMembers {
	return &guildMembers{
		members: make(map[discord.UserID]discord.Member, 1),
	}
}

var _ store.MemberStore = (*MemberStore)(nil)

func NewMemberStore() *MemberStore {
	return &MemberStore{
		guilds: map[discord.GuildID]*guildMembers{},
	}
}

func (s *MemberStore) Reset() error {
	s.mut.Lock()
	s.guilds = map[discord.GuildID]*guildMembers{}
	s.mut.Unlock()

	return nil
}

func (s *MemberStore) Member(guildID discord.GuildID, userID discord.UserID) (*discord.Member, error) {
	s.mut.Lock()
	gm, ok := s.guilds[guildID]
	s.mut.Unlock()

	if !ok {
		return nil, store.ErrNotFound
	}

	gm.mut.RLock()
	defer gm.mut.RUnlock()

	m, ok := gm.members[userID]
	if ok {
		return &m, nil
	}

	return nil, store.ErrNotFound
}

func (s *MemberStore) Members(guildID discord.GuildID) ([]discord.Member, error) {
	s.mut.Lock()
	gm, ok := s.guilds[guildID]
	s.mut.Unlock()

	if !ok {
		return nil, store.ErrNotFound
	}

	gm.mut.RLock()
	defer gm.mut.RUnlock()

	var members = make([]discord.Member, 0, len(gm.members))
	for _, m := range gm.members {
		members = append(members, m)
	}

	return members, nil
}

func (s *MemberStore) MemberSet(guildID discord.GuildID, member *discord.Member, update bool) error {
	s.mut.Lock()
	gm, ok := s.guilds[guildID]
	if !ok {
		gm = newGuildMembers()
		s.guilds[guildID] = gm
	}
	s.mut.Unlock()

	// TODO: update
	gm.mut.Lock()
	gm.members[member.User.ID] = *member
	gm.mut.Unlock()

	return nil
}

func (s *MemberStore) MemberRemove(guildID discord.GuildID, userID discord.UserID) error {
	s.mut.Lock()
	gm, ok := s.guilds[guildID]
	s.mut.Unlock()

	if !ok {
		return nil
	}

	gm.mut.Lock()
	delete(gm.members, userID)
	gm.mut.Unlock()

	return nil
}

// Each iterates over all members of the guild in undefined order. The given
// callback must not store the pointer outside of the callback; it must do so
// after making its own copy.
func (s *MemberStore) Each(g discord.GuildID, fn func(*discord.Member) (stop bool)) error {
	s.mut.Lock()
	gm, ok := s.guilds[g]
	s.mut.Unlock()

	if !ok {
		return store.ErrNotFound
	}

	gm.mut.RLock()
	defer gm.mut.RUnlock()

	for _, m := range gm.members {
		if fn(&m) {
			break
		}
	}

	return nil
}
