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
// With ningen's default handler,
//
// For reference, go to
// https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html.
type State struct {
	state  *state.State
	guilds sync.Map // snowflake -> *Guild

	minFetchMu sync.Mutex
	minFetched map[discord.ChannelID]int

	OnError func(error)

	// RequestFrequency is the duration before the next SearchMember is allowed
	// to do anything else. Default is 1s.
	SearchFrequency time.Duration
	SearchLimit     uint // 50
}

func NewState(state *state.State, h handlerrepo.AddHandler) *State {
	s := &State{
		state:      state,
		minFetched: make(map[discord.ChannelID]int),
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
	reqing map[discord.UserID]struct{}

	// last SearchMember call.
	lastreq time.Time

	// whether or not the guild is subscribed.
	subscribed bool

	// not mutex guarded
	lists sync.Map // string -> *List

	// different mutex
	subMutex sync.Mutex

	// map to keep track of subscribed channels
	subChannels map[discord.ChannelID][][2]int
}

// Subscribe subscribes the guild to typing events and activities. Callers cal
// call this multiple times concurrently. The state will ensure that only one
// command is sent to the gateway.
//
// The gateway command will be sent asynchronously.
func (m *State) Subscribe(guildID discord.GuildID) {
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
func (m *State) SearchMember(guildID discord.GuildID, query string) {
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
			GuildID:   []discord.GuildID{guildID},
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
func (m *State) RequestMember(guildID discord.GuildID, memberID discord.UserID) {
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
		guild.reqing = make(map[discord.UserID]struct{})
	} else {
		// Check if the member is already being requested.
		if _, ok := guild.reqing[memberID]; ok {
			return
		}
	}

	guild.reqing[memberID] = struct{}{}

	go func() {
		err := m.state.Gateway.RequestGuildMembers(gateway.RequestGuildMembersData{
			GuildID:   []discord.GuildID{guildID},
			UserIDs:   []discord.UserID{memberID},
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
	defer guild.mut.Unlock()

	// Add all members to state first.
	for _, member := range c.Members {
		delete(guild.reqing, member.User.ID)
		m.state.MemberSet(c.GuildID, member)
	}
}

// // InvalidateMemberList invalides the member list for the given channel ID.
// func (m *State) InvalidateMemberList(guildID discord.GuildID, channelID discord.ChannelID) {
// 	m.maxFetchMu.Lock()
// 	delete(m.maxFetched, channelID)
// 	m.maxFetchMu.Unlock()

// 	go func() {
// 		err := m.state.Gateway.GuildSubscribe(gateway.GuildSubscribeData{
// 			GuildID: guildID,
// 			Channels: map[discord.ChannelID][][2]int{
// 				channelID: {{0, 0}},
// 			},
// 		})

// 		if err != nil {
// 			m.OnError(errors.Wrap(err, "Failed to subscribe to member list"))
// 		}
// 	}()
// }

// MaxMemberChunk indicates the number of chunks the member list might have
// active at once. The 3 means that there can be 300 simultaneous active users.
// 1 is subtracted to always keep the first chunk alive.
const MaxMemberChunk = 3 - 1

// GetMemberListChunk returns the current member list chunk. It returns -1 if
// there is none.
func (m *State) GetMemberListChunk(guildID discord.GuildID, channelID discord.ChannelID) int {
	m.minFetchMu.Lock()
	defer m.minFetchMu.Unlock()

	ck, ok := m.minFetched[channelID]
	if !ok {
		return -1
	}

	return ck
}

var firstChunk = [][2]int{{0, 99}}

// chunkEq compares the two chunks.
func chunkEq(chk1, chk2 [][2]int) bool {
	if len(chk1) != len(chk2) {
		return false
	}

	for i := 0; i < 2; i++ {
		for j := range chk1 {
			if chk1[j][i] != chk2[j][i] {
				return false
			}
		}
	}

	return true
}

// RequestMemberList tries to ask the gateway for a chunk (or many) of the
// members list. Chunk is an integer (0, 1, ...), which indicates the maximum
// number of chunks from 0 that the API should return. The function returns the
// chunks to be fetched.
//
// Specifically, this method guarantees that the current chunk and the next
// chunk will always be alive, as well as the first chunk.
func (m *State) RequestMemberList(
	guildID discord.GuildID, channelID discord.ChannelID, chunk int) [][2]int {

	// Use the total to stop on max chunk.
	var total = -1

	// Get the list so we could calculate the total.
	l, err := m.GetMemberList(guildID, channelID)
	if err == nil {
		total = l.TotalVisible()

		// Round the total up with ceiling.
		total = (total) / 100
	}

	// TODO: This won't be synchronized with the actual members list if we
	// remove any of them from the list. Maybe remove the map state if possible.
	m.minFetchMu.Lock()
	defer m.minFetchMu.Unlock()

	// Chunk to start.
	start, ok := m.minFetched[channelID]
	// Check if we've already had this chunk.
	if ok && chunk == start {
		// We should always keep the current chunk and next chunk alive. As
		// such, we need an equal check.
		return nil
	}

	// Update the current chunks.
	m.minFetched[channelID] = chunk

	// Increment chunk by one, similar to how we add 1 into index for the
	// length.
	chunk++

	// Cap chunk if we have a total.
	if total > -1 && chunk > total {
		chunk = total
	}
	// If we've reached the point where the chunks to be fetched go beyond the
	// total, then we don't fetch anything.
	if chunk < start {
		return nil
	}

	// Derive the minimum chunk, if any. Skip the first chunk (0th), because
	// we're adding that ourselves.
	start = chunk - MaxMemberChunk
	if start < 1 {
		start = 1
	}

	// Always keep the first chunk alive.
	chunks := make([][2]int, 1, chunk-start+1)
	chunks[0] = [2]int{0, 99}

	// Start from the last one fetched.
	for i := start; i < chunk; i++ {
		chunks = append(chunks, [2]int{
			(i * 100),      // start: 100
			(i * 100) + 99, // end:   199
		})
	}

	go func() {
		gv, _ := m.guilds.LoadOrStore(guildID, &Guild{})
		guild := gv.(*Guild)

		guild.subMutex.Lock()
		defer guild.subMutex.Unlock()

		if guild.subChannels == nil {
			guild.subChannels = make(map[discord.ChannelID][][2]int, 1)
		}

		// If we're already in the chunk and have fetched everything already,
		// then we can exit.
		if fetched, ok := guild.subChannels[channelID]; ok && chunkEq(fetched, chunks) {
			return
		}

		for id := range guild.subChannels {
			// Reset the chunks.
			guild.subChannels[id] = firstChunk
		}

		// Set this channel's chunk to be different.
		guild.subChannels[channelID] = chunks

		// Subscribe.
		err := m.state.Gateway.GuildSubscribe(gateway.GuildSubscribeData{
			GuildID:  guildID,
			Channels: guild.subChannels,
		})

		if err != nil {
			m.OnError(errors.Wrap(err, "Failed to subscribe to member list"))
		}
	}()

	return chunks
}

// GetMemberList looks up for the member list. It returns an error if no list
// is found.
//
// Reference: https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html
func (m *State) GetMemberList(guildID discord.GuildID, channelID discord.ChannelID) (*List, error) {
	// Compute Discord's magical member list ID thing.
	c, err := m.state.Channel(channelID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get channel permissions")
	}

	hv := ComputeListID(c.Permissions)

	return m.GetMemberListDirect(guildID, hv)
}

// GetMemberListDirect gets the guild's member list directly from the list's ID.
func (m *State) GetMemberListDirect(guildID discord.GuildID, id string) (*List, error) {
	gv, ok := m.guilds.Load(guildID)
	if !ok {
		return nil, ErrListNotFound
	}
	guild := gv.(*Guild)

	// Query for the *List.
	ls, ok := guild.lists.Load(id)
	if !ok {
		return nil, ErrListNotFound
	}

	return ls.(*List), nil
}

// onListUpdate is called a bit after RequestGuildMembers if the Channels field
// is filled. It handles updating the local members list state.
func (m *State) onListUpdate(ev *gateway.GuildMemberListUpdate) {
	gv, _ := m.guilds.LoadOrStore(ev.GuildID, &Guild{})
	guild := gv.(*Guild)

	var ml *List

	if v, ok := guild.lists.Load(ev.ID); ok {
		ml = v.(*List)
	} else {
		ml = NewList(ev.ID, ev.GuildID)
		guild.lists.Store(ev.ID, ml)
	}

	ml.mu.Lock()
	defer ml.mu.Unlock()

	ml.memberCount = int(ev.MemberCount)
	ml.onlineCount = int(ev.OnlineCount)
	ml.groups = ev.Groups

	for i, op := range ev.Ops {
		switch op.Op {
		case "SYNC":
			start, end := op.Range[0], op.Range[1]
			growItems(&ml.items, end+1)

			for i := 0; i < len(op.Items); i++ {
				ml.items[start+i] = op.Items[i]
			}

			continue

		case "INVALIDATE":
			start, end := op.Range[0], op.Range[1]
			// Copy the old items into the Items field for future uses in other
			// handlers.
			op.Items = append([]gateway.GuildMemberListOpItem{}, ml.items[start:end]...)
			ev.Ops[i] = op

			// Nullify the to-be-invalidated chunks.
			for i := start; i < end && i < len(ml.items); i++ {
				ml.items[i] = gateway.GuildMemberListOpItem{}
			}

			continue
		}

		// https://github.com/golang/go/wiki/SliceTricks
		oi := op.Index

		// Bounds check
		var length = len(ml.items)
		if op.Op == "INSERT" {
			length++
		}

		if length == 0 || length <= oi {
			m.OnError(fmt.Errorf(
				"Member %s: index out of range: len(ml.Items)=%d <= op.Index=%d\n",
				op.Op, len(ml.items), oi,
			))
			continue
		}

		// https://luna.gitlab.io/discord-unofficial-docs/lazy_guilds.html#operator
		switch op.Op {
		case "INSERT":
			ml.items = append(ml.items, gateway.GuildMemberListOpItem{})
			copy(ml.items[oi+1:], ml.items[oi:])
			ml.items[oi] = op.Item

		case "UPDATE":
			ml.items[oi] = op.Item

		case "DELETE":
			// Copy the old item into the Items field for future uses.
			op.Item = ml.items[i]
			ev.Ops[i] = op
			// Actually delete the item.
			ml.items = append(ml.items[:oi], ml.items[oi+1:]...)
		}
	}

	// Clean up.
	var filledLen = len(ml.items)
	// Iterate until we reach the end of slice or ListItemIsNil returns false.
	for i := filledLen - 1; i >= 0 && ListItemIsNil(ml.items[i]); i-- {
		filledLen = i
	}

	ml.items = ml.items[:filledLen]
}

// onListUpdateState is called when onListUpdate is called, but this one updates
// the local member/presence state instead.
func (m *State) onListUpdateState(ev *gateway.GuildMemberListUpdate) {
	for _, op := range ev.Ops {
		switch op.Op {
		case "SYNC", "INSERT", "UPDATE":
			for _, item := range append(op.Items, op.Item) {
				if item.Member != nil {
					m.state.MemberSet(ev.GuildID, item.Member.Member)
					m.state.PresenceSet(ev.GuildID, item.Member.Presence)
				}
			}
		}
	}
}

func growItems(items *[]gateway.GuildMemberListOpItem, maxLen int) {
	cpy := *items
	if len(cpy) >= maxLen {
		return
	}
	delta := maxLen - len(cpy)
	*items = append(cpy, make([]gateway.GuildMemberListOpItem, delta)...)
}

func ComputeListID(overrides []discord.Overwrite) string {
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
