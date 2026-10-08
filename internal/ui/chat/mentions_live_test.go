//go:build live

package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/keyring"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

// TestMentionsInboxLive opens the recent mentions popup with the @ button on
// the real account and closes it again. It never opens a channel, so no
// channel is marked as read.
//
//	go test -tags live -run TestMentionsInboxLive -v ./internal/ui/chat
func TestMentionsInboxLive(t *testing.T) {
	token, err := keyring.GetToken()
	if err != nil {
		t.Skip("no saved token:", err)
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mouse = true

	const width, height = 140, 40
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: width, Y: height}))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.EnableMouse()

	m := NewModel(cfg, token)
	tview.Styles = tview.Theme{}
	app := tview.NewApplication(m, tview.WithScreen(screen))
	go app.Run()
	defer func() { _ = closeState(m.state)() }()

	dump := func(name string) string {
		time.Sleep(700 * time.Millisecond)
		var b strings.Builder
		for y := range height {
			for x := 0; x < width; {
				str, _, w := screen.Get(x, y)
				if str == "" {
					str = " "
				}
				b.WriteString(str)
				x += max(w, 1)
			}
			b.WriteString("\n")
		}
		text := b.String()
		t.Logf("--- %s ---\n%s", name, text)
		return text
	}

	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(dump("ready"), "Direct Messages") {
		if time.Now().After(deadline) {
			t.Fatal("READY did not arrive")
		}
	}

	x, y, _, ok := mentionsButtonBounds(m.messagesList.Box)
	if !ok {
		t.Fatal("no room for the mentions button")
	}
	_, style, _ := screen.Get(x+1, y)
	t.Logf("button unread=%v bg=%v", m.mentionsUnread, style.GetBackground())

	screen.EventQ() <- tcell.NewEventMouse(x+1, y, tcell.Button1, tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	screen.EventQ() <- tcell.NewEventMouse(x+1, y, tcell.ButtonNone, tcell.ModNone)

	deadline = time.Now().Add(15 * time.Second)
	var text string
	for {
		text = dump("inbox")
		if strings.Contains(text, "Recent Mentions") && !strings.Contains(text, "Loading...") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recent mentions did not load")
		}
	}
	// Without a filter input the first entry sits right under the title.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Recent Mentions") {
			if i+1 >= len(lines) || strings.Contains(lines[i+1], "─") {
				t.Error("popup still shows a filter input")
			}
			break
		}
	}

	screen.EventQ() <- tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone)
	if strings.Contains(dump("closed"), "Recent Mentions") {
		t.Fatal("popup did not close")
	}
}
