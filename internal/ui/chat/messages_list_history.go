package chat

import (
	"log/slog"
	"slices"

	"github.com/ayn2op/tview"
)

// resetOlderHistory forgets any in-flight or finished older-history request.
// Call it whenever the loaded message window is replaced.
func (ml *messagesList) resetOlderHistory() {
	ml.olderLoading = 0
	ml.olderExhausted = false
}

// fetchOlderMessages loads the page before the oldest loaded message. It is
// used when the selection moves above the first message.
func (ml *messagesList) fetchOlderMessages() tview.Cmd {
	return ml.requestOlderMessages(false)
}

// loadOlderOnScrollTop loads one older page when a mouse-wheel scroll leaves
// the viewport at the very top of the loaded messages.
func (ml *messagesList) loadOlderOnScrollTop(mouse tview.MouseMsg) tview.Cmd {
	if !ml.InRect(mouse.Position()) || !ml.canRequestOlderMessages() {
		return nil
	}
	if pos, ok := ml.viewportPosition(); !ok || pos.lines != 0 {
		return nil
	}
	return ml.requestOlderMessages(true)
}

func (ml *messagesList) canRequestOlderMessages() bool {
	return len(ml.messages) > 0 && !ml.olderLoading.IsValid() && !ml.olderExhausted
}

func (ml *messagesList) requestOlderMessages(fromScroll bool) tview.Cmd {
	if !ml.canRequestOlderMessages() {
		return nil
	}
	selectedChannel, ok := ml.chat.SelectedChannel()
	if !ok {
		return nil
	}

	channelID := selectedChannel.ID
	before := ml.messages[0].ID
	limit := uint(ml.cfg.MessagesLimit)
	ml.olderLoading = channelID
	return func() tview.Msg {
		msg := olderMessagesLoadedMsg{ChannelID: channelID, Before: before, FromScroll: fromScroll}
		messages, err := ml.chat.state.MessagesBefore(channelID, before, limit)
		if err != nil {
			slog.Error("failed to fetch older messages", "err", err)
			return msg
		}

		msg.Exhausted = uint(len(messages)) < limit
		msg.Older = slices.Clone(messages)
		slices.Reverse(msg.Older)
		return msg
	}
}

func (ml *messagesList) onOlderMessagesLoaded(msg olderMessagesLoadedMsg) tview.Cmd {
	if ml.olderLoading != msg.ChannelID {
		return nil
	}
	ml.olderLoading = 0

	selectedChannel, ok := ml.chat.SelectedChannel()
	if !ok || selectedChannel.ID != msg.ChannelID {
		return nil
	}
	// Drop stale pages if the message window changed while the request ran.
	if len(ml.messages) == 0 || ml.messages[0].ID != msg.Before {
		return nil
	}
	if msg.Exhausted {
		ml.olderExhausted = true
	}
	if len(msg.Older) == 0 {
		return nil
	}

	// Measure the current viewport before the rows shift so a scroll-driven
	// load can keep the same messages on screen.
	pos, keepViewport := viewportPosition{}, false
	if msg.FromScroll {
		pos, keepViewport = ml.viewportPosition()
	}
	oldAnchor := ml.contentHeight(0, ml.messageToRowIndex(0), pos.width)
	prevCursor := ml.Cursor()

	// Defensive invalidation if Discord returns overlapping windows.
	boundary := ml.messages[0].ID
	for _, message := range msg.Older {
		delete(ml.itemByID, message.ID)
	}
	ml.messages = slices.Concat(msg.Older, ml.messages)
	delete(ml.itemByID, boundary)
	ml.rebuildRows()

	switch {
	case msg.FromScroll:
		if prevCursor >= 0 {
			// Keep selection on the same message after prepend shifts indexes.
			ml.Model.SetCursor(ml.messageToRowIndex(prevCursor + len(msg.Older)))
		}
		if keepViewport {
			newAnchor := ml.contentHeight(0, ml.messageToRowIndex(len(msg.Older)), pos.width)
			ml.ScrollTop()
			ml.SetPendingScroll(pos.lines + newAnchor - oldAnchor)
		}
	case prevCursor == 0:
		// Preserve "SelectUp at top" semantics: move to the next older message.
		ml.SetCursor(len(msg.Older) - 1)
	case prevCursor > 0:
		// Keep selection on the same message after prepend shifts indexes.
		ml.SetCursor(prevCursor + len(msg.Older))
	default:
		ml.SetCursor(prevCursor)
	}

	if selectedChannel.GuildID.IsValid() {
		return ml.requestGuildMembers(selectedChannel.GuildID, msg.Older)
	}
	return nil
}

// viewportPosition describes where the messages list is scrolled to.
type viewportPosition struct {
	// lines is the number of content lines above the viewport.
	lines int
	// width is the width the list lays its items out with.
	width int
}

// viewportPosition lays the list out immediately, applying any pending
// scroll, and reports how far the viewport is from the top of the content.
func (ml *messagesList) viewportPosition() (viewportPosition, bool) {
	x, y, width, height := ml.Rect()
	if width <= 0 || height <= 0 || len(ml.rows) == 0 {
		return viewportPosition{}, false
	}

	// The list only positions items it draws, so clear stale positions first
	// and treat any message item with a width as drawn by this layout pass.
	for _, item := range ml.itemByID {
		if _, _, w, _ := item.Rect(); w > 0 {
			item.SetRect(0, 0, 0, 0)
		}
	}
	ml.SetBuilder(ml.buildItem) // Force the next SetRect to lay out again.
	ml.Model.SetRect(x, y, width, height)

	_, innerY, _, _ := ml.InnerRect()
	for rowIndex, row := range ml.rows {
		if row.kind != messagesListRowMessage {
			continue
		}
		item, ok := ml.itemByID[ml.messages[row.messageIndex].ID]
		if !ok {
			continue
		}
		_, itemY, itemWidth, _ := item.Rect()
		if itemWidth <= 0 {
			continue
		}
		above := ml.contentHeight(0, rowIndex, itemWidth)
		return viewportPosition{lines: max(above-(itemY-innerY), 0), width: itemWidth}, true
	}
	return viewportPosition{}, false
}

// contentHeight returns the laid-out height of rows [start, end).
func (ml *messagesList) contentHeight(start, end, width int) int {
	if width <= 0 {
		return 0
	}
	height := 0
	for i := max(start, 0); i < end && i < len(ml.rows); i++ {
		if item := ml.buildItem(i); item != nil {
			height += max(item.Height(width), 1)
		}
	}
	return height
}
