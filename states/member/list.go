package member

import (
	"sync"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
)

type ListOP struct {
	gateway.GuildMemberListOp
	List *List
}

// ListItemIsNil returns true if the item has nothing in it. This might be an
// uninitialized item.
func ListItemIsNil(it gateway.GuildMemberListOpItem) bool {
	return it.Member == nil && it.Group == nil
}

// List is the local state of the member list. The function is safe to be used
// thread-safe.
type List struct {
	mu sync.Mutex

	id    string
	guild discord.GuildID

	memberCount int
	onlineCount int

	groups []gateway.GuildMemberListGroup
	items  []gateway.GuildMemberListOpItem
}

func NewList(id string, guild discord.GuildID) *List {
	return &List{
		id:    id,
		guild: guild,
	}
}

// ID returns the list's ID. The ID is made by hashing roles. The list's ID is
// constant.
func (l *List) ID() string {
	return l.id
}

// GuildID returns the list's guild ID. This ID is constant.
func (l *List) GuildID() discord.GuildID {
	return l.guild
}

// ViewItems acquires the list's mutex and views the current items. The function
// must not mutate nor reference the slice nor any of its items. The given
// callback must not call any other method except for ID and GuildID.
func (l *List) ViewItems(fn func(items []gateway.GuildMemberListOpItem)) {
	l.mu.Lock()
	fn(l.items)
	l.mu.Unlock()
}

// ViewGroups acquires the list's mutex and views the current groups. The
// function must not mutate nor reference the slice nor any of its items. The
// given callback must not call any other method except for ID and GuildID.
func (l *List) ViewGroups(fn func(gruops []gateway.GuildMemberListGroup)) {
	l.mu.Lock()
	fn(l.groups)
	l.mu.Unlock()
}

// MemberCount returns the total number of members.
func (l *List) MemberCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.memberCount
}

// OnlineCount returns the total number of online users.
func (l *List) OnlineCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.onlineCount
}

// CountNils returns the number of nil items.
func (l *List) CountNil() (nils int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, item := range l.items {
		if ListItemIsNil(item) {
			nils++
		}
	}
	return nils
}

// MaxChunk returns the maximum complete chunk from Items.
func (l *List) MaxChunk() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.items) == 0 {
		return 0
	}
	return ChunkFromIndex(len(l.items) - 1)
}

// ChunkFromIndex calculates the chunk number from the index of Items in List.
func ChunkFromIndex(index int) int {
	return index / 100
}
