package chat

import (
	"slices"
	"strings"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
)

func TestAuthorAtHitsOnlyTheName(t *testing.T) {
	ml, _ := newHistoryTestList(t, 3)
	last := len(ml.messages) - 1
	ml.SetCursor(last)
	item := ml.itemByID[ml.messages[last].ID]
	x, y, _, _ := item.Rect()

	if _, ok := ml.authorAt(x, y); !ok {
		t.Fatal("click on the first cell of the name should hit")
	}
	if _, ok := ml.authorAt(x+len("user")-1, y); !ok {
		t.Fatal("click on the last cell of the name should hit")
	}
	if _, ok := ml.authorAt(x+len("user"), y); ok {
		t.Fatal("click right after the name should miss")
	}
	if _, ok := ml.authorAt(x, y+1); ok {
		t.Fatal("click below the name should miss")
	}
}

func TestAuthorSpansReplyFollowsWrappedPreview(t *testing.T) {
	ml, _ := newHistoryTestList(t, 1)
	ref := discord.Message{Author: discord.User{ID: 7, Username: "orig"}, Content: strings.Repeat("long ", 20)}
	reply := discord.Message{
		Type:              discord.InlinedReplyMessage,
		Author:            discord.User{ID: 8, Username: "replier"},
		Content:           "ok",
		ReferencedMessage: &ref,
	}

	const width = 30
	spans := ml.authorSpans(reply, ml.renderMessage(reply, ml.cfg.Theme.MessagesList.MessageStyle.Style), width)
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	if spans[0].row != 0 || spans[0].col == 0 || spans[0].message.Author.ID != 7 {
		t.Fatalf("reply preview span = %+v", spans[0])
	}
	if spans[1].row < 2 || spans[1].col != 0 || spans[1].message.Author.ID != 8 {
		t.Fatalf("reply author span = %+v, want row past the wrapped preview", spans[1])
	}
}

func TestClickReplyPreviewJumpsToOriginal(t *testing.T) {
	ml, _ := newHistoryTestList(t, 3)
	messages := slices.Clone(ml.messages)
	ref := messages[0]
	messages = append(messages, discord.Message{
		ID:                2000,
		ChannelID:         ref.ChannelID,
		Type:              discord.InlinedReplyMessage,
		Author:            discord.User{ID: 9, Username: "replier"},
		Content:           "ok",
		ReferencedMessage: &ref,
	})
	slices.Reverse(messages)
	ml.setMessages(messages)
	ml.ScrollBottom()
	ml.SetRect(0, 0, 40, 12)

	last := len(ml.messages) - 1
	ml.SetCursor(last)
	_, y, _, _ := ml.itemByID[ml.messages[last].ID].Rect()

	if _, ok := ml.replyPreviewAt(y + 1); ok {
		t.Fatal("click on the reply body should not hit the preview")
	}
	got, ok := ml.replyPreviewAt(y)
	if !ok || got.ID != ref.ID {
		t.Fatalf("replyPreviewAt = %v, %v, want message %v", got.ID, ok, ref.ID)
	}
	if cmd := ml.jumpToMessage(got); cmd != nil {
		t.Fatal("loaded message should not need a reload")
	}
	if ml.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", ml.Cursor())
	}
}
