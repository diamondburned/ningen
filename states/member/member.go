package member

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/state"
	"github.com/diamondburned/ningen/handler"
	"github.com/pkg/errors"
	"github.com/twmb/murmur3"
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

	lists     sync.Map // string -> *List
	hashCache sync.Map // channelID -> string

	maxFetchMu sync.Mutex
	maxFetched map[discord.Snowflake]int

	callbacks // OnOP and OnSync

	OnError func(error)
}

func NewState(state *state.State, h handler.AddHandler) *State {
	s := &State{
		state:      state,
		maxFetched: make(map[discord.Snowflake]int),
		OnError: func(err error) {
			log.Println("Members list error:", err)
		},
	}
	h.AddHandler(s.onListUpdate)
	h.AddHandler(s.onMembers)
	return s
}

type Guild struct {
	reqmut sync.Mutex
	// map to keep track of members being requested, which allows duplicate
	// calls.
	reqing map[discord.Snowflake]struct{}
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

	v, _ := m.guilds.LoadOrStore(guildID, &Guild{
		reqing: make(map[discord.Snowflake]struct{}),
	})
	guild := v.(*Guild)

	guild.reqmut.Lock()
	defer guild.reqmut.Unlock()

	// Check if the member is already being requested.
	if _, ok := guild.reqing[memberID]; ok {
		return
	}

	go func() {
		err := m.state.Gateway.RequestGuildMembers(gateway.RequestGuildMembersData{
			GuildID:   []discord.Snowflake{guildID},
			UserIDs:   []discord.Snowflake{memberID},
			Presences: true,
		})

		if err != nil {
			guild.reqmut.Lock()
			delete(guild.reqing, memberID)
			guild.reqmut.Unlock()

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

	guild.reqmut.Lock()

	// Add all members to state first.
	for _, member := range c.Members {
		delete(guild.reqing, member.User.ID)

		// copy the member variable.
		mcpy := member
		m.state.MemberSet(mcpy.User.ID, &mcpy)
	}

	// Release the lock early so callbacks wouldn't affect it.
	guild.reqmut.Unlock()

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

		log.Println("Chunks:", chunks)

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
// conditions.
//
// Reference: https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html
func (m *State) GetMemberList(guildID, channelID discord.Snowflake, fn func(*List)) error {
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
	ls, ok := m.lists.Load(hv)
	if !ok {
		return errors.New("List not found.")
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
	v, _ := m.lists.LoadOrStore(ev.ID, &List{})
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

func growItems(items *[]*gateway.GuildMemberListOpItem, maxLen int) {
	if len(*items) >= maxLen {
		return
	}
	delta := maxLen - len(*items)
	*items = append(*items, make([]*gateway.GuildMemberListOpItem, delta)...)
}

func computeListID(overrides []discord.Overwrite) string {
	var hashInput = make([]string, 0, len(overrides))

	for _, perm := range overrides {
		switch {
		case perm.Allow.Has(discord.PermissionReadMessageHistory):
			hashInput = append(hashInput, "allow:"+perm.ID.String())
		case perm.Deny.Has(discord.PermissionReadMessageHistory):
			hashInput = append(hashInput, "deny:"+perm.ID.String())
		}
	}

	var mm3Input = strings.Join(hashInput, ",")
	// TODO: confirm.
	return strconv.FormatUint(murmur3.Sum64([]byte(mm3Input)), 64)
}
