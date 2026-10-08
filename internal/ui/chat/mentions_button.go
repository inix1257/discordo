package chat

import (
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/rivo/uniseg"
)

const mentionsButtonLabel = " @ "

// mentionsButtonBounds is the cell range of the recent mentions button on the
// right end of the messages pane title border.
func mentionsButtonBounds(box *tview.Box) (x, y, w int, ok bool) {
	bx, by, width, height := box.Rect()
	w = uniseg.StringWidth(mentionsButtonLabel)
	if height < 1 || width < w+4 {
		return 0, 0, 0, false
	}
	return bx + width - 2 - w, by, w, true
}

func hitMentionsButton(box *tview.Box, px, py int) bool {
	x, y, w, ok := mentionsButtonBounds(box)
	return ok && py == y && px >= x && px < x+w
}

// drawMentionsButton draws the button black on white while unread mentions
// are waiting, like a mentioned channel in the guilds tree.
func drawMentionsButton(screen tcell.Screen, box *tview.Box, cfg *config.Config, focused, unread bool) {
	x, y, w, ok := mentionsButtonBounds(box)
	if !ok {
		return
	}
	style := cfg.Theme.Title.NormalStyle.Style
	if focused {
		style = cfg.Theme.Title.ActiveStyle.Style
	}
	style = style.Bold(true)
	if unread {
		style = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite).Bold(true)
	}
	tview.PrintStyled(screen, mentionsButtonLabel, x, y, 0, w, tview.AlignmentLeft, style, false)
}

// refreshMentionsUnread recomputes whether any guild channel holds unread
// mentions. DMs are left out because the recent mentions list does not show
// them.
func (m *Model) refreshMentionsUnread() {
	total := m.state.ReadState.TotalMentionCount()
	if total > 0 {
		if channels, err := m.state.Cabinet.PrivateChannels(); err == nil {
			for _, channel := range channels {
				if rs := m.state.ReadState.ReadState(channel.ID); rs != nil {
					total -= rs.MentionCount
				}
			}
		}
	}
	m.mentionsUnread = total > 0
}
