package member

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/diamondburned/arikawa/state"
)

type mockNingen struct {
	*state.State
	MemberState *State
}

func ningenFromState(s *state.State) (*mockNingen, error) {
	return &mockNingen{s, NewState(s, s)}, nil
}

const (
	GuildID   = 0
	ChannelID = 0
)

func ExampleState_RequestMemberList() {
	s, err := state.New(os.Getenv("TOKEN"))
	if err != nil {
		log.Fatalln("Failed to create a state:", err)
	}

	// Replace with the actual ningen.FromState function.
	n, err := ningenFromState(s)
	if err != nil {
		log.Fatalln("Failed to create a ningen state:", err)
	}

	updates := make(chan *gateway.GuildMemberListUpdate, 1)
	n.AddHandler(updates)

	if err := n.Open(); err != nil {
		panic(err)
	}

	defer n.Close()

	for i := 0; ; i++ {
		c := n.MemberState.RequestMemberList(GuildID, ChannelID, i)
		if c == nil {
			break
		}

		<-updates
		log.Println("Received", i)
	}

	l, err := n.MemberState.GetMemberList(GuildID, ChannelID)
	if err != nil {
		panic(err)
	}

	l.ViewGroups(func(groups []gateway.GuildMemberListGroup) {
		for _, group := range groups {
			var name = group.ID
			if p, err := discord.ParseSnowflake(name); err == nil {
				r, err := s.Role(GuildID, discord.RoleID(p))
				if err != nil {
					log.Fatalln("Failed to get role:", err)
				}

				name = r.Name
			}

			fmt.Println("Group:", name, group.Count)
		}
	})

	l.ViewItems(func(items []gateway.GuildMemberListOpItem) {
		for i := 0; i < len(items); i += 100 {
			for j := 0; j < 99 && i+j < len(items); j++ {
				if ListItemIsNil(items[i+j]) {
					fmt.Print(" ")
				} else {
					fmt.Print("O")
				}
			}

			fmt.Println("|")
		}

		var firstNonNil = ListItemSeek(items, 100)
		fmt.Println("First non-nil past 100:", firstNonNil)
		fmt.Println("Above member:", items[firstNonNil].Member)

		fmt.Println("Last member:", items[len(items)-1].Member.User.Username)
	})

}

func TestComputeListID(t *testing.T) {
	perms := []discord.Overwrite{
		{Type: discord.OverwriteRole, ID: 361910177961738242, Deny: 1024, Allow: 0},
		{Type: discord.OverwriteRole, ID: 361919857836425217, Deny: 0, Allow: 117760},
		{Type: discord.OverwriteRole, ID: 532359766694035457, Deny: 0, Allow: 10240},
		{Type: discord.OverwriteRole, ID: 564702909519101952, Deny: 93184, Allow: 0},
		{Type: discord.OverwriteRole, ID: 578035907232530432, Deny: 2112, Allow: 0},
		{Type: discord.OverwriteRole, ID: 697931217521082455, Deny: 0, Allow: 1024},
	}

	if id := ComputeListID(perms); id != "3720633681" {
		t.Fatal("Unexpected ID:", id, "expected", "3720633681")
	}
}
