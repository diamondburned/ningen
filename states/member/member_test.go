package member

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/state"
)

func ExampleState_RequestMemberList() {
	s, err := state.New(os.Getenv("TOKEN"))
	if err != nil {
		log.Fatalln("Failed to create state:", err)
	}

	done := make(chan struct{}, 1)

	// This would normally be ningen.FromState().
	n := NewState(s, s)
	n.OnSync(func(id string, l *List, guild discord.Snowflake) {
		for _, member := range l.Items {
			switch {
			case member.Group != nil:
				fmt.Println(member.Group.ID, member.Group.Count)
			case member.Member != nil:
				fmt.Println("\t" + member.Member.User.Username)
			}
		}
		done <- struct{}{}
	})

	if err := s.Open(); err != nil {
		log.Fatalln("Failed to open:", err)
	}

	log.Println("Connected")

	// Request the member list. This function sends the command asynchronously.
	n.RequestMemberList(361910177961738242, 361920025051004939, 0)

	<-done
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

	if id := computeListID(perms); id != "3720633681" {
		t.Fatal("Unexpected ID:", id, "expected", "3720633681")
	}
}
