package member

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
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

// ListItemSeek seeks to the first non-nil item. -1 is returned if there are no
// non-nil items.
func ListItemSeek(items []gateway.GuildMemberListOpItem, offset int) int {
	for i := offset; i < len(items); i++ {
		if !ListItemIsNil(items[i]) {
			return i
		}
	}
	return -1
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

// TotalVisible returns the total number of members visible.
func (l *List) TotalVisible() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if we have an offline group.
	for _, group := range l.groups {
		if group.ID == "offline" {
			return l.memberCount
		}
	}
	// Else, we should only show the onlines.
	return l.onlineCount
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
