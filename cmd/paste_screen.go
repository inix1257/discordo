package cmd

import "github.com/gdamore/tcell/v3"

// pasteScreen turns an empty bracketed paste into Ctrl+V.
// Windows Terminal consumes Ctrl+V. When the clipboard holds only an image, it
// sends paste start and end with nothing between them, and the key never arrives.
type pasteScreen struct {
	tcell.Screen
	events chan tcell.Event
}

func wrapPasteScreen(screen tcell.Screen) tcell.Screen {
	events := make(chan tcell.Event, cap(screen.EventQ()))
	go forwardPasteEvents(screen.EventQ(), events)
	return &pasteScreen{Screen: screen, events: events}
}

func (s *pasteScreen) EventQ() chan tcell.Event {
	return s.events
}

func forwardPasteEvents(in <-chan tcell.Event, out chan<- tcell.Event) {
	defer close(out)

	var (
		pasting bool
		start   *tcell.EventPaste
		pending []tcell.Event
	)
	for ev := range in {
		paste, ok := ev.(*tcell.EventPaste)
		if !ok {
			if pasting {
				pending = append(pending, ev)
				continue
			}
			out <- ev
			continue
		}
		if paste.Start() {
			pasting = true
			start = paste
			pending = nil
			continue
		}
		if !pasting {
			out <- ev
			continue
		}
		pasting = false
		if len(pending) == 0 {
			out <- tcell.NewEventKey(tcell.KeyCtrlV, "", tcell.ModCtrl)
			continue
		}
		if start != nil {
			out <- start
		}
		for _, pendingEv := range pending {
			out <- pendingEv
		}
		out <- paste
		pending = nil
		start = nil
	}
}
