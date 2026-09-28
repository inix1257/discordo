package cmd

import (
	"testing"

	"github.com/gdamore/tcell/v3"
)

func TestEmptyPasteBecomesCtrlV(t *testing.T) {
	in := make(chan tcell.Event)
	out := make(chan tcell.Event, 2)
	go forwardPasteEvents(in, out)

	in <- tcell.NewEventPaste(true)
	in <- tcell.NewEventPaste(false)
	close(in)

	ev, ok := (<-out).(*tcell.EventKey)
	if !ok || ev.Key() != tcell.KeyCtrlV || ev.Modifiers() != tcell.ModCtrl {
		t.Fatalf("event = %#v", ev)
	}
	if _, ok := <-out; ok {
		t.Fatal("extra event")
	}
}

func TestTextPasteIsUnchanged(t *testing.T) {
	in := make(chan tcell.Event)
	out := make(chan tcell.Event, 4)
	go forwardPasteEvents(in, out)

	in <- tcell.NewEventPaste(true)
	in <- tcell.NewEventKey(tcell.KeyRune, "a", tcell.ModNone)
	in <- tcell.NewEventPaste(false)
	close(in)

	if _, ok := (<-out).(*tcell.EventPaste); !ok {
		t.Fatal("missing paste start")
	}
	key, ok := (<-out).(*tcell.EventKey)
	if !ok || key.Str() != "a" {
		t.Fatalf("key = %#v", key)
	}
	end, ok := (<-out).(*tcell.EventPaste)
	if !ok || !end.End() {
		t.Fatal("missing paste end")
	}
	if _, ok := <-out; ok {
		t.Fatal("extra event")
	}
}
