package member

import (
	"sync"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
)

type (
	OPCallback     = func(id string, l *List, guild discord.GuildID, op gateway.GuildMemberListOp)
	SyncCallback   = func(id string, l *List, guild discord.GuildID)
	MemberCallback = func(guild discord.GuildID, member discord.Member)
)

type callbacks struct {
	// I reckon it's a terrible idea to share these mutices. But whatever.
	cbMut sync.RWMutex

	onOP     []OPCallback
	onSync   []SyncCallback
	onMember []MemberCallback
}

// OnOP is called when Discord updates the member list.
func (c *callbacks) OnOP(fn OPCallback) {
	c.cbMut.Lock()
	c.onOP = append(c.onOP, fn)
	c.cbMut.Unlock()
}

// OnSync is called when Discord initializes any chunk of the member list.
func (c *callbacks) OnSync(fn SyncCallback) {
	c.cbMut.Lock()
	c.onSync = append(c.onSync, fn)
	c.cbMut.Unlock()
}

// OnMember is called when Discord replies with the requested member.
func (c *callbacks) OnMember(fn MemberCallback) {
	c.cbMut.Lock()
	c.onMember = append(c.onMember, fn)
	c.cbMut.Unlock()
}

func (c *callbacks) op(id string, l *List, guild discord.GuildID, op gateway.GuildMemberListOp) {
	c.cbMut.RLock()
	defer c.cbMut.RUnlock()

	for _, fn := range c.onOP {
		fn(id, l, guild, op)
	}
}
func (c *callbacks) sync(id string, l *List, guild discord.GuildID) {
	c.cbMut.RLock()
	defer c.cbMut.RUnlock()

	for _, fn := range c.onSync {
		fn(id, l, guild)
	}
}
func (c *callbacks) member(guild discord.GuildID, member discord.Member) {
	c.cbMut.RLock()
	defer c.cbMut.RUnlock()

	for _, fn := range c.onMember {
		fn(guild, member)
	}
}
