package chat

import (
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
)

func TestMessageCopyText(t *testing.T) {
	attachmentURL := "https://cdn.discordapp.com/attachments/1/2/file.png"
	embedURL := "https://example.com/embed"

	tests := []struct {
		name string
		msg  discord.Message
		want string
	}{
		{
			name: "content only",
			msg:  discord.Message{Content: "hello"},
			want: "hello",
		},
		{
			name: "content already includes its link",
			msg:  discord.Message{Content: "see https://example.com"},
			want: "see https://example.com",
		},
		{
			name: "content plus attachment url",
			msg: discord.Message{
				Content:     "photo",
				Attachments: []discord.Attachment{{URL: attachmentURL}},
			},
			want: "photo\n" + attachmentURL,
		},
		{
			name: "attachment only",
			msg: discord.Message{
				Attachments: []discord.Attachment{{URL: attachmentURL}},
			},
			want: attachmentURL,
		},
		{
			name: "content plus embed url",
			msg: discord.Message{
				Content: "title",
				Embeds:  []discord.Embed{{URL: embedURL}},
			},
			want: "title\n" + embedURL,
		},
		{
			name: "skips attachment url already in content",
			msg: discord.Message{
				Content:     attachmentURL,
				Attachments: []discord.Attachment{{URL: attachmentURL}},
			},
			want: attachmentURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageCopyText(tt.msg); got != tt.want {
				t.Fatalf("messageCopyText() = %q, want %q", got, tt.want)
			}
		})
	}
}
