package member

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/state"
	"github.com/diamondburned/ningen/handlerrepo"
	"github.com/pkg/errors"
	"github.com/twmb/murmur3"
)

var (
	// ErrListNotFound is returned if GetMemberList can't find the list.
	ErrListNotFound = errors.New("List not found.")
)

// State handles members and the member list.
//
// Members
//
// Discord wants all clients to request member information over the gateway
// instead of using the usual member API endpoint. This makes sense, as it
// reduces the load onto the server, but it also makes it a lot more painful to
// efficiently request members.
//
// The state helps abstract this away by allowing the caller to request multiple
// times the same member. If the gateway has yet to reply or if the state
// already has the member, the function will not send a command over.
//
// Member List
//
// Discord also wants all clients to not use the members (plural) endpoint. In
// fact, calling this endpoint will immediately unverify the user's email.
//
// The state helps abstract this by keeping a local state of all member lists as
// well as providing APIs to query the member list. Keep in mind that since most
// channels typically have its own member list, this might be pretty hefty on
// memory.
//
// For reference, go to
// https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html.
type State struct {
	state  *state.State
	guilds sync.Map // snowflake -> *Guild

	hashCache sync.Map // channelID -> string

	maxFetchMu sync.Mutex
	maxFetched map[discord.Snowflake]int

	callbacks // OnOP and OnSync

	OnError func(error)

	// RequestFrequency is the duration before the next SearchMember is allowed
	// to do anything else. Default is 1s.
	SearchFrequency time.Duration
	SearchLimit     uint // 50
}

func NewState(state *state.State, h handlerrepo.AddHandler) *State {
	s := &State{
		state:      state,
		maxFetched: make(map[discord.Snowflake]int),
		OnError: func(err error) {
			log.Println("Members list error:", err)
		},
		SearchFrequency: time.Second,
		SearchLimit:     50,
	}
	h.AddHandler(s.onListUpdateState)
	h.AddHandler(s.onListUpdate)
	h.AddHandler(s.onMembers)
	return s
}

type Guild struct {
	mut sync.Mutex

	// map to keep track of members being requested, which allows duplicate
	// calls.
	reqing map[discord.Snowflake]struct{}

	// last SearchMember call.
	lastreq time.Time

	// whether or not the guild is subscribed.
	subscribed bool

	// not mutex guarded
	lists sync.Map // string -> *List
}

// Subscribe subscribes the guild to typing events and activities. Callers cal
// call this multiple times concurrently. The state will ensure that only one
// command is sent to the gateway.
//
// The gateway command will be sent asynchronously.
func (m *State) Subscribe(guildID discord.Snowflake) {
	v, _ := m.guilds.LoadOrStore(guildID, &Guild{})
	gd := v.(*Guild)

	gd.mut.Lock()
	defer gd.mut.Unlock()

	// Exit if already subscribed.
	if gd.subscribed {
		return
	}

	gd.subscribed = true

	go func() {
		// Subscribe.
		err := m.state.Gateway.GuildSubscribe(gateway.GuildSubscribeData{
			GuildID:    guildID,
			Typing:     true,
			Activities: true,
		})

		if err != nil {
			m.OnError(errors.Wrap(err, "Failed to subscribe guild"))
			return
		}
	}()
}

// SearchMember queries Discord for a list of members with the given query
// string.
func (m *State) SearchMember(guildID discord.Snowflake, query string) {
	v, _ := m.guilds.LoadOrStore(guildID, &Guild{})
	gd := v.(*Guild)

	gd.mut.Lock()
	defer gd.mut.Unlock()

	if gd.lastreq.Add(m.SearchFrequency).After(time.Now()) {
		return
	}

	gd.lastreq = time.Now()

	go func() {
		err := m.state.Gateway.RequestGuildMembers(gateway.RequestGuildMembersData{
			GuildID:   []discord.Snowflake{guildID},
			Query:     query,
			Presences: true,
			Limit:     m.SearchLimit,
		})

		if err != nil {
			m.OnError(errors.Wrap(err, "Failed to search guild members"))
		}
	}()
}

// RequestMember tries to ask the gateway for a member from the ID. This method
// will not send the command if the member is already in the state or it's
// already being requested.
func (m *State) RequestMember(guildID, memberID discord.Snowflake) {
	// TODO: Maybe this is a bit excessive? Probably won't need this in most
	// cases.

	// Check if we already have the member in the state.
	_, err := m.state.Store.Member(guildID, memberID)
	if err == nil {
		return
	}

	v, _ := m.guilds.LoadOrStore(guildID, &Guild{})
	guild := v.(*Guild)

	guild.mut.Lock()
	defer guild.mut.Unlock()

	if guild.reqing == nil {
		guild.reqing = make(map[discord.Snowflake]struct{})
	} else {
		// Check if the member is already being requested.
		if _, ok := guild.reqing[memberID]; ok {
			return
		}
	}

	guild.reqing[memberID] = struct{}{}

	go func() {
		err := m.state.Gateway.RequestGuildMembers(gateway.RequestGuildMembersData{
			GuildID:   []discord.Snowflake{guildID},
			UserIDs:   []discord.Snowflake{memberID},
			Presences: true,
		})

		if err != nil {
			guild.mut.Lock()
			delete(guild.reqing, memberID)
			guild.mut.Unlock()

			m.OnError(errors.Wrap(err, "Failed to request guild members"))
			return
		}

		// Wait for Discord to deliver their events then delete them in the
		// callback.
	}()
}

// onMembers is called a bit after RequestGuildMembers if the UserIDs field is
// filled.
func (m *State) onMembers(c *gateway.GuildMembersChunkEvent) {
	v, _ := m.guilds.LoadOrStore(c.GuildID, &Guild{})
	guild := v.(*Guild)

	guild.mut.Lock()

	// Add all members to state first.
	for _, member := range c.Members {
		delete(guild.reqing, member.User.ID)

		// copy the member variable.
		mcpy := member
		m.state.MemberSet(mcpy.User.ID, &mcpy)
	}

	// Release the lock early so callbacks wouldn't affect it.
	guild.mut.Unlock()

	for _, member := range c.Members {
		m.callbacks.member(c.GuildID, member)
	}
}

// RequestMemberList tries to ask the gateway for a chunk (or many) of the
// members list. Chunk is an integer (0, 1, ...), which indicates the maximum
// number of chunks from 0 that the API should return.
func (m *State) RequestMemberList(guildID, channelID discord.Snowflake, chunk int) {
	// TODO: This won't be synchronized with the actual members list if we
	// remove any of them from the list. Maybe remove the map state if possible.
	m.maxFetchMu.Lock()
	defer m.maxFetchMu.Unlock()

	// Chunk to start.
	start, ok := m.maxFetched[channelID]
	// If we've already had this chunk.
	if ok && start >= chunk {
		return
	}

	// Increment chunk by one, similar to how we add 1 into index for the
	// length.
	chunk++

	// If not, update:
	m.maxFetched[channelID] = chunk

	go func() {
		// If not, start from the last one fetched.
		var chunks = make([][2]int, 0, chunk-start)
		for i := start; i < chunk; i++ {
			chunks = append(chunks, [2]int{
				(i * 100),      // start: 100
				(i * 100) + 99, // end:   199
			})
		}

		// Subscribe.
		err := m.state.Gateway.GuildSubscribe(gateway.GuildSubscribeData{
			GuildID: guildID,
			Channels: map[discord.Snowflake][][2]int{
				channelID: chunks,
			},
		})

		if err != nil {
			m.OnError(errors.Wrap(err, "Failed to subscribe to member list"))
		}
	}()
}

// GetMemberList looks up for the member list. It returns an error if no list
// is found. The callback will be called with the mutex locked to prevent race
// conditions. The function can be used to check if the list is there or not
// with a nil callback.
//
// Reference: https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html
func (m *State) GetMemberList(guildID, channelID discord.Snowflake, fn func(*List)) error {
	gv, ok := m.guilds.Load(guildID)
	if !ok {
		return ErrListNotFound
	}
	guild := gv.(*Guild)

	// Compute Discord's magical member list ID thing.
	hv, ok := m.hashCache.Load(channelID)
	if !ok {
		c, err := m.state.Channel(channelID)
		if err != nil {
			return errors.Wrap(err, "Failed to get channel permissions")
		}

		hv = computeListID(c.Permissions)
		m.hashCache.Store(channelID, hv)
	}

	// Query for the *List.
	ls, ok := guild.lists.Load(hv)
	if !ok {
		return errors.New("List not found.")
	}

	// Allow nil callback.
	if fn == nil {
		return nil
	}

	list := ls.(*List)

	list.mu.Lock()
	fn(list)
	list.mu.Unlock()

	return nil
}

// onListUpdate is called a bit after RequestGuildMembers if the Channels field
// is filled. It handles updating the local members list state.
func (m *State) onListUpdate(ev *gateway.GuildMemberListUpdate) {
	gv, _ := m.guilds.LoadOrStore(ev.GuildID, &Guild{})
	guild := gv.(*Guild)

	v, _ := guild.lists.LoadOrStore(ev.ID, &List{})
	ml := v.(*List)

	ml.mu.Lock()
	defer ml.mu.Unlock()

	ml.MemberCount = ev.MemberCount
	ml.OnlineCount = ev.OnlineCount
	ml.Groups = ev.Groups

	synced := false

	for _, op := range ev.Ops {
		switch op.Op {
		case "SYNC":
			start, end := op.Range[0], op.Range[1]
			length := end + 1
			growItems(&ml.Items, length)

			for i := 0; i < length-start && i < len(op.Items); i++ {
				ml.Items[start+i] = &op.Items[i]
			}
			synced = true
			continue

		case "INVALIDATE":
			start, end := op.Range[0], op.Range[1]
			for i := start; i < end && i < len(ml.Items); i++ {
				ml.Items[i] = nil
			}
			continue
		}

		// https://github.com/golang/go/wiki/SliceTricks
		i := op.Index

		// Bounds check
		if len(ml.Items) > 0 && i != 0 {
			var length = len(ml.Items)
			if op.Op == "INSERT" {
				length++
			}

			if length <= i {
				m.OnError(fmt.Errorf(
					"Member %s: index out of range: len(ml.Items)=%d <= op.Index=%d\n",
					op.Op, len(ml.Items), i,
				))
				continue
			}
		}

		// https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html#operator
		switch op.Op {
		case "INSERT":
			ml.Items = append(ml.Items, nil)
			copy(ml.Items[i+1:], ml.Items[i:])
			ml.Items[i] = &op.Item

		case "UPDATE":
			ml.Items[i] = &op.Item

		case "DELETE":
			if i < len(ml.Items)-1 {
				copy(ml.Items[i:], ml.Items[i+1:])
			}
			ml.Items[len(ml.Items)-1] = nil
			ml.Items = ml.Items[:len(ml.Items)-1]
		}

		m.op(ev.ID, ml, ev.GuildID, op)
	}

	if synced {
		m.sync(ev.ID, ml, ev.GuildID)
	}
}

// onListUpdateState is called when onListUpdate is called, but this one updates
// the local member/presence state instead.
func (m *State) onListUpdateState(ev *gateway.GuildMemberListUpdate) {
	for _, op := range ev.Ops {
		switch op.Op {
		case "SYNC", "INSERT", "UPDATE":
			for _, item := range append(op.Items, op.Item) {
				if item.Member != nil {
					m.state.MemberSet(ev.GuildID, &item.Member.Member)
					m.state.PresenceSet(ev.GuildID, &item.Member.Presence)
				}
			}
		case "INVALIDATE", "DELETE":
			for _, item := range append(op.Items, op.Item) {
				if item.Member != nil {
					m.state.MemberRemove(ev.GuildID, item.Member.Member.User.ID)
					m.state.PresenceRemove(ev.GuildID, item.Member.Member.User.ID)
				}
			}
		}
	}
}

func growItems(items *[]*gateway.GuildMemberListOpItem, maxLen int) {
	if len(*items) >= maxLen {
		return
	}
	delta := maxLen - len(*items)
	*items = append(*items, make([]*gateway.GuildMemberListOpItem, delta)...)
}

func computeListID(overrides []discord.Overwrite) string {
	var allows, denies []discord.Snowflake

	for _, perm := range overrides {
		switch {
		case perm.Allow.Has(discord.PermissionViewChannel):
			allows = append(allows, perm.ID)
		case perm.Deny.Has(discord.PermissionViewChannel):
			denies = append(denies, perm.ID)
		}
	}

	if len(allows) == 0 && len(denies) == 0 {
		return "everyone"
	}

	var input = make([]string, 0, len(allows)+len(denies))
	for _, a := range allows {
		input = append(input, "allow:"+a.String())
	}
	for _, b := range denies {
		input = append(input, "deny:"+b.String())
	}

	mm3Input := strings.Join(input, ",")
	return strconv.FormatUint(uint64(murmur3.StringSum32(mm3Input)), 10)
}
