package member

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/ningen/v3/handlerrepo"
	"github.com/pkg/errors"
	"github.com/twmb/murmur3"
)

var (
	// ErrListNotFound is returned if GetMemberList can't find the list.
	ErrListNotFound = errors.New("List not found.")
)

// RequestPresences, when true, will make RequestMember ask for the presences as
// well.
var RequestPresences = true

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
	state   *state.State
	guildMu sync.Mutex
	guilds  map[discord.GuildID]*Guild // snowflake -> *Guild

	minFetchMu sync.Mutex
	minFetched map[discord.ChannelID]int

	OnError func(error)

	// RequestFrequency is the duration before the next SearchMember is allowed
	// to do anything else. Default is 600ms.
	SearchFrequency time.Duration
	SearchLimit     uint // 50
}

func NewState(state *state.State, h handlerrepo.AddHandler) *State {
	s := &State{
		state:      state,
		guilds:     map[discord.GuildID]*Guild{},
		minFetched: map[discord.ChannelID]int{},
		OnError: func(err error) {
			log.Println("Members list error:", err)
		},
		SearchFrequency: 600 * time.Millisecond,
		SearchLimit:     50,
	}
	h.AddSyncHandler(s.onListUpdateState)
	h.AddSyncHandler(s.onListUpdate)
	h.AddSyncHandler(s.onMembers)
	h.AddSyncHandler(func(*gateway.ReadyEvent) {
		s.guildMu.Lock()
		s.minFetchMu.Lock()

		// Invalidate everything.
		s.guilds = map[discord.GuildID]*Guild{}
		s.minFetched = map[discord.ChannelID]int{}

		s.minFetchMu.Unlock()
		s.guildMu.Unlock()
	})
	return s
}

type Guild struct {
	mut sync.Mutex
	id  discord.GuildID

	// map to keep track of members being requested, which allows duplicate
	// calls.
	requested  map[discord.UserID]bool
	requesting bool

	// last SearchMember call.
	lastSearch time.Time

	// whether or not the guild is subscribed.
	subscribed bool

	listMu sync.Mutex
	lists  map[string]*List

	// different mutex
	subMutex sync.Mutex

	// map to keep track of subscribed channels
	subChannels map[discord.ChannelID][][2]int
}

func (g *Guild) list(listID string, create bool) *List {
	g.listMu.Lock()
	defer g.listMu.Unlock()

	list, ok := g.lists[listID]
	if !ok && create {
		list = NewList(listID, g.id)
		g.lists[listID] = list
	}

	return list
}

func (m *State) guildState(guildID discord.GuildID, create bool) *Guild {
	m.guildMu.Lock()
	defer m.guildMu.Unlock()

	guild, ok := m.guilds[guildID]
	if !ok && create {
		guild = &Guild{
			id:          guildID,
			lists:       map[string]*List{},
			requested:   map[discord.UserID]bool{},
			subChannels: map[discord.ChannelID][][2]int{},
		}
		m.guilds[guildID] = guild
	}

	return guild
}

// Subscribe subscribes the guild to typing events and activities. Callers cal
// call this multiple times concurrently. The state will ensure that only one
// command is sent to the gateway.
//
// The gateway command will be sent asynchronously.
func (m *State) Subscribe(guildID discord.GuildID) {
	gd := m.guildState(guildID, true)
	gd.mut.Lock()
	defer gd.mut.Unlock()

	// Exit if already subscribed.
	if gd.subscribed {
		return
	}

	gd.subscribed = true

	go func() {
		// Subscribe.
		err := m.state.Gateway().Send(context.Background(), &gateway.GuildSubscribeCommand{
			GuildID:    guildID,
			Typing:     true,
			Threads:    true,
			Activities: true,
		})

		if err != nil {
			m.OnError(errors.Wrap(err, "Failed to subscribe guild"))

			gd.mut.Lock()
			gd.subscribed = false
			gd.mut.Unlock()

			return
		}
	}()
}

// SearchMember queries Discord for a list of members with the given query
// string.
func (m *State) SearchMember(guildID discord.GuildID, query string) {
	gd := m.guildState(guildID, true)
	gd.mut.Lock()
	defer gd.mut.Unlock()

	if gd.lastSearch.Add(m.SearchFrequency).After(time.Now()) {
		return
	}

	gd.lastSearch = time.Now()

	go func() {
		err := m.state.Gateway().Send(context.Background(), &gateway.RequestGuildMembersCommand{
			GuildIDs:  []discord.GuildID{guildID},
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
	_, err := m.state.Cabinet.Member(guildID, memberID)
	if err == nil {
		return
	}

	guild := m.guildState(guildID, true)
	guild.mut.Lock()
	defer guild.mut.Unlock()

	if guild.requested == nil {
		guild.requested = make(map[discord.UserID]bool)
	} else {
		// Check if the member is already being requested.
		if _, ok := guild.requested[memberID]; ok {
			return
		}
	}

	guild.requested[memberID] = false

	if guild.requesting {
		return
	}
	guild.requesting = true

	go func() {
		// Wait for 500ms.
		time.Sleep(500 * time.Millisecond)

		// Re-check the guild for the member list.
		guild.mut.Lock()

		memberIDs := make([]discord.UserID, 0, 10)
		for id, requested := range guild.requested {
			if !requested {
				memberIDs = append(memberIDs, id)
				guild.requested[id] = true
			}
		}

		guild.requesting = false
		guild.mut.Unlock()

		// Fetch everything that wasn't requested.
		err := m.state.Gateway().Send(context.Background(), &gateway.RequestGuildMembersCommand{
			GuildIDs:  []discord.GuildID{guildID},
			UserIDs:   memberIDs,
			Presences: RequestPresences,
		})

		log.Println("guild", guildID, "requested", len(memberIDs), "members")

		if err != nil {
			guild.mut.Lock()
			// Add back the member IDs that we failed to request.
			for _, id := range memberIDs {
				guild.requested[id] = false
			}
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
	guild := m.guildState(c.GuildID, true)
	guild.mut.Lock()
	defer guild.mut.Unlock()

	for _, member := range c.Members {
		delete(guild.requested, member.User.ID)
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
//
// If the given guild is not subscribed already, then it will subscribe
// automatically.
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
		guild := m.guildState(guildID, true)
		guild.subMutex.Lock()

		// If we're already in the chunk and have fetched everything already,
		// then we can exit.
		if fetched, ok := guild.subChannels[channelID]; ok && chunkEq(fetched, chunks) {
			guild.subMutex.Unlock()
			return
		}

		for id := range guild.subChannels {
			// Reset the chunks.
			guild.subChannels[id] = firstChunk
		}

		// Set this channel's chunk to be different.
		guild.subChannels[channelID] = chunks

		guild.subscribed = true
		guild.subMutex.Unlock() // Do not block IO.

		// Subscribe.
		err := m.state.Gateway().Send(context.Background(), &gateway.GuildSubscribeCommand{
			GuildID:    guildID,
			Channels:   guild.subChannels,
			Typing:     true,
			Activities: true,
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

	hv := ComputeListID(c.Overwrites)

	return m.GetMemberListDirect(guildID, hv)
}

// GetMemberListDirect gets the guild's member list directly from the list's ID.
func (m *State) GetMemberListDirect(guildID discord.GuildID, id string) (*List, error) {
	guild := m.guildState(guildID, false)
	if guild == nil {
		return nil, ErrListNotFound
	}

	list := guild.list(id, false)
	if list == nil {
		return nil, ErrListNotFound
	}

	return list, nil
}

// onListUpdate is called a bit after RequestGuildMembers if the Channels field
// is filled. It handles updating the local members list state.
func (m *State) onListUpdate(ev *gateway.GuildMemberListUpdate) {
	guild := m.guildState(ev.GuildID, true)

	ml := guild.list(ev.ID, true)
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
			items := append(op.Items, op.Item)
			for i, item := range items {
				if item.Member != nil {
					update := op.Op == "UPDATE"
					m.state.MemberSet(ev.GuildID, &items[i].Member.Member, update)
					m.state.PresenceSet(ev.GuildID, &items[i].Member.Presence, update)
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
