package chat

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/ningen/v3"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
)

func newHistoryTestList(t *testing.T, count int) (*messagesList, discord.ChannelID) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}

	channelID := discord.ChannelID(1)
	chat := &Model{state: ningen.New("test-token")}
	chat.SetSelectedChannel(&discord.Channel{ID: channelID, Type: discord.DirectMessage})

	ml := newMessagesList(cfg, chat)
	ml.SetRect(0, 0, 40, 12)
	// Discord returns newest first.
	ml.setMessages(historyTestMessages(1000, count))
	ml.ScrollBottom()
	ml.SetRect(0, 0, 40, 12)
	return ml, channelID
}

// historyTestMessages returns count messages with IDs [firstID, firstID+count), newest first.
func historyTestMessages(firstID, count int) []discord.Message {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local)
	messages := make([]discord.Message, 0, count)
	for i := count - 1; i >= 0; i-- {
		id := firstID + i
		messages = append(messages, discord.Message{
			ID:        discord.MessageID(id),
			ChannelID: 1,
			Author:    discord.User{ID: discord.UserID(id%3 + 1), Username: "user"},
			Content:   fmt.Sprintf("message %d", id),
			Timestamp: discord.NewTimestamp(base.Add(time.Duration(id) * time.Hour)),
		})
	}
	return messages
}

func wheelUp(ml *messagesList) tview.Cmd {
	return ml.Update(tview.MouseMsg{
		EventMouse: tcell.NewEventMouse(5, 5, tcell.WheelUp, tcell.ModNone),
		Action:     tview.MouseScrollUp,
	})
}

func messageScreenY(t *testing.T, ml *messagesList, id discord.MessageID) int {
	t.Helper()
	item, ok := ml.itemByID[id]
	if !ok {
		t.Fatalf("message %d is not rendered", id)
	}
	_, y, _, _ := item.Rect()
	return y
}

func TestMessagesListWheelLoadsOlderOnceAtTop(t *testing.T) {
	ml, channelID := newHistoryTestList(t, 20)

	if cmd := wheelUp(ml); cmd != nil {
		t.Fatal("requested older history before reaching the top")
	}
	requests := 0
	for range 200 {
		if cmd := wheelUp(ml); cmd != nil {
			requests++
		}
	}
	if requests != 1 {
		t.Fatalf("older-history requests = %d, want 1", requests)
	}
	if ml.olderLoading != channelID {
		t.Fatalf("olderLoading = %v, want %v", ml.olderLoading, channelID)
	}
	if pos, ok := ml.viewportPosition(); !ok || pos.lines != 0 {
		t.Fatalf("viewport = %+v (ok=%v), want top", pos, ok)
	}

	oldFirst := ml.messages[0].ID
	oldY := messageScreenY(t, ml, oldFirst)

	ml.Update(olderMessagesLoadedMsg{
		ChannelID:  channelID,
		Before:     oldFirst,
		Older:      reversedHistory(historyTestMessages(900, 15)),
		FromScroll: true,
	})
	ml.SetRect(0, 0, 40, 12)

	if got := len(ml.messages); got != 35 {
		t.Fatalf("messages = %d, want 35", got)
	}
	if ml.olderLoading.IsValid() {
		t.Fatal("request is still marked as loading")
	}
	if got := messageScreenY(t, ml, oldFirst); got != oldY {
		t.Fatalf("previous first message moved from row %d to %d", oldY, got)
	}
	if pos, ok := ml.viewportPosition(); !ok || pos.lines == 0 {
		t.Fatalf("viewport = %+v (ok=%v), want below the new top", pos, ok)
	}

	// Scrolling up through the new page reaches the top again and loads once more.
	requests = 0
	for range 200 {
		if cmd := wheelUp(ml); cmd != nil {
			requests++
		}
	}
	if requests != 1 {
		t.Fatalf("second older-history requests = %d, want 1", requests)
	}
}

func TestMessagesListWheelStopsAfterHistoryStart(t *testing.T) {
	ml, channelID := newHistoryTestList(t, 5)

	if cmd := wheelUp(ml); cmd == nil {
		t.Fatal("short channel did not request older history")
	}
	ml.Update(olderMessagesLoadedMsg{ChannelID: channelID, Before: ml.messages[0].ID, Exhausted: true, FromScroll: true})

	for range 5 {
		if cmd := wheelUp(ml); cmd != nil {
			t.Fatal("requested older history after reaching the channel start")
		}
	}
}

func TestMessagesListIgnoresStaleOlderPage(t *testing.T) {
	ml, channelID := newHistoryTestList(t, 5)
	if cmd := wheelUp(ml); cmd == nil {
		t.Fatal("did not request older history")
	}

	ml.Update(olderMessagesLoadedMsg{
		ChannelID:  channelID,
		Before:     discord.MessageID(1),
		Older:      reversedHistory(historyTestMessages(900, 3)),
		FromScroll: true,
	})
	if got := len(ml.messages); got != 5 {
		t.Fatalf("messages = %d, want 5", got)
	}
	if ml.olderLoading.IsValid() {
		t.Fatal("stale response left the request marked as loading")
	}
}

func reversedHistory(messages []discord.Message) []discord.Message {
	out := make([]discord.Message, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		out = append(out, messages[i])
	}
	return out
}

// The response arrives asynchronously, usually while the composer (not the
// messages list) has focus, so the chat model must route it explicitly.
func TestChatRoutesOlderMessagesWithoutListFocus(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(cfg, "test-token")
	channelID := discord.ChannelID(1)
	m.SetSelectedChannel(&discord.Channel{ID: channelID, Type: discord.DirectMessage})
	m.messagesList.setMessages(historyTestMessages(1000, 5))
	m.messagesList.olderLoading = channelID

	m.Update(olderMessagesLoadedMsg{
		ChannelID: channelID,
		Before:    m.messagesList.messages[0].ID,
		Older:     reversedHistory(historyTestMessages(900, 3)),
	})

	if got := len(m.messagesList.messages); got != 8 {
		t.Fatalf("messages = %d, want 8", got)
	}
	if m.messagesList.olderLoading.IsValid() {
		t.Fatal("request is still marked as loading")
	}
}
