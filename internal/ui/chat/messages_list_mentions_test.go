package chat

import (
	"strings"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

func TestMessagesListOffscreenMentionBars(t *testing.T) {
	ml, _ := newHistoryTestList(t, 20)
	me := discord.User{ID: 99, Username: "me"}
	if err := ml.chat.state.Cabinet.MyselfSet(me, false); err != nil {
		t.Fatal(err)
	}
	// Mention the user in an old message that is scrolled out of view.
	target := discord.MessageID(1002)
	for i := range ml.messages {
		if ml.messages[i].ID == target {
			ml.messages[i].Mentions = []discord.GuildUser{{User: me}}
		}
	}

	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 40, Y: 12}))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	row := func(y int) string {
		var b strings.Builder
		for x := range 40 {
			str, _, _ := screen.Get(x, y)
			b.WriteString(str)
		}
		return b.String()
	}

	ml.View(screen)
	_, innerY, _, innerH := ml.InnerRect()
	if !strings.Contains(row(innerY), "▲") {
		t.Fatalf("top row = %q, want a ▲ bar", row(innerY))
	}
	if strings.Contains(row(innerY+innerH-1), "▼") {
		t.Fatalf("bottom row = %q, want no ▼ bar", row(innerY+innerH-1))
	}

	ml.Update(tview.MouseMsg{
		EventMouse: tcell.NewEventMouse(5, innerY, tcell.Button1, tcell.ModNone),
		Action:     tview.MouseLeftClick,
	})
	if got := ml.messages[ml.Cursor()].ID; got != target {
		t.Fatalf("cursor on message %d after clicking the bar, want %d", got, target)
	}

	// The app lays the list out again before every frame.
	screen.Clear()
	ml.SetRect(0, 0, 40, 12)
	ml.View(screen)
	if strings.Contains(row(innerY), "▲") {
		t.Fatalf("top row = %q, want the bar gone once the mention is shown", row(innerY))
	}
}

func TestMessagesListMentionBarBelow(t *testing.T) {
	ml, _ := newHistoryTestList(t, 20)
	me := discord.User{ID: 99, Username: "me"}
	if err := ml.chat.state.Cabinet.MyselfSet(me, false); err != nil {
		t.Fatal(err)
	}
	last := len(ml.messages) - 1
	ml.messages[last].MentionEveryone = true
	ml.ScrollTop()
	ml.SetRect(0, 0, 40, 12)

	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 40, Y: 12}))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	ml.View(screen)
	_, innerY, _, innerH := ml.InnerRect()
	var b strings.Builder
	for x := range 40 {
		str, _, _ := screen.Get(x, innerY+innerH-1)
		b.WriteString(str)
	}
	if !strings.Contains(b.String(), "▼") {
		t.Fatalf("bottom row = %q, want a ▼ bar", b.String())
	}
	if ml.bottomBar != last {
		t.Fatalf("bottom bar jumps to %d, want %d", ml.bottomBar, last)
	}
}
