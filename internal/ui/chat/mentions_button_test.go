package chat

import (
	"testing"

	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

func TestMentionsButton(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: 40, Y: 5}))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	box := tview.NewBox()
	box.SetRect(0, 0, 40, 5)
	x, y, _, ok := mentionsButtonBounds(box)
	if !ok {
		t.Fatal("no room for the button")
	}
	if !hitMentionsButton(box, x+1, y) || hitMentionsButton(box, x+1, y+1) {
		t.Error("hit test does not match the drawn cells")
	}

	for _, unread := range []bool{false, true} {
		drawMentionsButton(screen, box, cfg, false, unread)
		str, style, _ := screen.Get(x+1, y)
		if str != "@" {
			t.Fatalf("button cell = %q, want @", str)
		}
		if got := style.GetBackground() == tcell.ColorWhite; got != unread {
			t.Errorf("unread=%v: white background = %v", unread, got)
		}
	}
}
