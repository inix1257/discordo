package ui

import (
	"testing"

	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

func TestClearWideLeftEdge(t *testing.T) {
	term := vt.NewMockTerm(vt.MockOptSize{X: 14, Y: 3})
	screen, err := tcell.NewTerminfoScreenFromTty(term)
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	for y := range 3 {
		tview.Print(screen, "가나다라마바사", 0, y, 14, tview.AlignmentLeft, tcell.ColorDefault)
	}
	screen.Show()

	// x=3 is the right half of "나", so its glyph would cover the border.
	box := tview.NewBox().SetBorders(tview.BordersAll)
	box.SetRect(3, 0, 6, 3)
	ClearWideLeftEdge(screen, box)
	box.View(screen)
	screen.Show()

	for y, want := range []string{"┌", "│", "└"} {
		if got := term.GetCell(vt.Coord{X: 3, Y: vt.Row(y)}).C; got != want {
			t.Errorf("row %d: left border = %q, want %q", y, got, want)
		}
	}
}
