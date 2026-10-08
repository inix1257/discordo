package mentionsinbox

import "github.com/ayn2op/arikawa/v3/discord"

type SelectedMsg struct {
	ChannelID discord.ChannelID
	MessageID discord.MessageID
}

type CancelMsg struct{}
