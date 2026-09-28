package chat

import (
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/rivo/uniseg"
)

const (
	guildsCollapseGlyph = "◀"
	guildsExpandGlyph   = "▶"
)

func guildsToggleGlyph(collapsed bool) string {
	if collapsed {
		return guildsExpandGlyph
	}
	return guildsCollapseGlyph
}

func guildsToggleStyle(cfg *config.Config, focused bool) tcell.Style {
	style := cfg.Theme.Title.NormalStyle.Style
	if focused {
		style = cfg.Theme.Title.ActiveStyle.Style
	}
	return style.Bold(true).Reverse(true)
}

// guildsToggleBounds is the title-border cell of the pane button.
// Expanded: right end of the guilds pane. Collapsed: left of the channel title.
func guildsToggleBounds(box *tview.Box, collapsed bool) (x, y, w int, ok bool) {
	bx, by, width, height := box.Rect()
	w = uniseg.StringWidth(guildsToggleGlyph(collapsed))
	if height < 1 || w < 1 || width < w+3 {
		return 0, 0, 0, false
	}
	if collapsed {
		return bx + 1, by, w, true
	}
	return bx + width - 1 - w, by, w, true
}

func hitGuildsToggle(box *tview.Box, collapsed bool, px, py int) bool {
	x, y, w, ok := guildsToggleBounds(box, collapsed)
	return ok && py == y && px >= x && px < x+w
}

func drawExpandedGuildsToggle(screen tcell.Screen, box *tview.Box, cfg *config.Config, focused bool) {
	x, y, w, ok := guildsToggleBounds(box, false)
	if !ok {
		return
	}
	tview.PrintStyled(screen, guildsCollapseGlyph, x, y, 0, w, tview.AlignmentLeft, guildsToggleStyle(cfg, focused), false)
}

func drawCollapsedGuildsToggle(screen tcell.Screen, box *tview.Box, cfg *config.Config, focused bool) {
	x, y, w, ok := guildsToggleBounds(box, true)
	if !ok {
		return
	}

	bx, _, width, _ := box.Rect()
	borderStyle := cfg.Theme.Border.NormalStyle.Style
	titleStyle := cfg.Theme.Title.NormalStyle.Style
	if focused {
		borderStyle = cfg.Theme.Border.ActiveStyle.Style
		titleStyle = cfg.Theme.Title.ActiveStyle.Style
	}

	fill := " "
	fillStyle := tcell.StyleDefault
	if box.Borders().Has(tview.BordersTop) {
		fillStyle = borderStyle
		if top := box.BorderSet().Top; top != "" {
			fill = top
		}
	}
	for cx := bx + 1; cx < bx+width-1; cx++ {
		screen.Put(cx, y, fill, fillStyle)
	}

	tview.PrintStyled(screen, guildsExpandGlyph, x, y, 0, w, tview.AlignmentLeft, guildsToggleStyle(cfg, focused), false)
	screen.Put(x+w, y, " ", titleStyle)

	title := box.Title()
	titleX := x + w + 1
	remain := bx + width - 1 - titleX
	if title == "" || remain <= 0 {
		return
	}

	align := cfg.Theme.Title.Alignment.Alignment
	start, end, _ := tview.PrintStyled(screen, title, titleX, y, 0, remain, align, titleStyle, true)
	if len(title)-(end-start) > 0 && end > start {
		ellipsisX := titleX + remain - 1
		if align == tview.AlignmentRight {
			ellipsisX = titleX
		}
		_, style, _ := screen.Get(ellipsisX, y)
		tview.Print(screen, string(tview.SemigraphicsHorizontalEllipsis), ellipsisX, y, 1, tview.AlignmentLeft, style.GetForeground())
	}
}
