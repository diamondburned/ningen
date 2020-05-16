package member

import (
	"fmt"
	"log"
	"os"

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
