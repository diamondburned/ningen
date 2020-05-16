package member

import (
	"sync"

	"github.com/diamondburned/arikawa/gateway"
)

type List struct {
	mu sync.Mutex

	MemberCount uint64
	OnlineCount uint64

	Groups []gateway.GuildMemberListGroup
	Items  []*gateway.GuildMemberListOpItem
}

func (l *List) CountNil() (nils int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, item := range l.Items {
		if item == nil {
			nils++
		}
	}
	return nils
}

// MaxChunk returns the maximum complete chunk from Items.
func (l *List) MaxChunk() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.Items) == 0 {
		return 0
	}
	return ChunkFromIndex(len(l.Items) - 1)
}

// ChunkFromIndex calculates the chunk number from the index of Items in List.
func ChunkFromIndex(index int) int {
	return index / 100
}
