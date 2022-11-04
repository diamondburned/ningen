package ningen

import "github.com/diamondburned/arikawa/v3/discord"

type readyEventExtras struct {
	Users             []discord.User `json:"users,omitempty"`
	PrivateChannelsV2 []struct {
		discord.Channel
		RecipientIDs []discord.UserID `json:"recipient_ids,omitempty"`
	} `json:"private_channels,omitempty"`
}
