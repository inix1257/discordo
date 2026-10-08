package mentionsinbox

import (
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
)

func TestPreview(t *testing.T) {
	message := discord.Message{
		Content:  "hey <@1> and <@!2>,\nlook   here <@&3> <@&4>",
		Mentions: []discord.GuildUser{{User: discord.User{ID: 1, Username: "alice"}}, {User: discord.User{ID: 2, Username: "bob", DisplayName: "Bob"}}},
	}
	roleName := func(id discord.RoleID) string {
		if id == 3 {
			return "mods"
		}
		return ""
	}
	if got, want := preview(message, roleName), "hey @alice and @Bob, look here @mods @role"; got != want {
		t.Errorf("preview() = %q, want %q", got, want)
	}

	attachmentOnly := discord.Message{Attachments: []discord.Attachment{{Filename: "a.png"}}}
	if got, want := preview(attachmentOnly, roleName), "[attachment]"; got != want {
		t.Errorf("preview() = %q, want %q", got, want)
	}
}
