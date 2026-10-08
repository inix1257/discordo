package profilecard

import (
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/ui"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/rivo/uniseg"
)

const (
	minWidth = 24
	maxWidth = 48
)

// Model is a small read-only popup that shows a user's profile.
type Model struct {
	*tview.TextView
	cfg   *config.Config
	lines []tview.Line
}

func NewModel(cfg *config.Config) *Model {
	tv := tview.NewTextView().
		SetScrollable(false).
		SetWrap(true).
		SetWordWrap(true)
	ui.ConfigureBox(tv.Box, &cfg.Theme)
	tv.SetTitle("Profile")
	return &Model{TextView: tv, cfg: cfg}
}

func (m *Model) SetLines(lines []tview.Line) {
	m.lines = lines
	m.TextView.SetLines(lines)
}

func (m *Model) PreferredSize() (width, height int) {
	for _, line := range m.lines {
		w := 0
		for _, segment := range line {
			w += uniseg.StringWidth(segment.Text)
		}
		width = max(width, w)
	}
	width = min(max(width, minWidth), maxWidth)

	borders := 0
	if m.cfg.Theme.Border.Enabled {
		borders = 1
	}
	padding := m.cfg.Theme.Border.Padding
	height = m.TextView.Height(width) + borders*2 + padding[0] + padding[1]
	width += borders*2 + padding[2] + padding[3]
	return width, height
}

func (m *Model) Update(msg tview.Msg) tview.Cmd {
	ui.UpdateBoxFocus(m.Box, &m.cfg.Theme, msg)
	switch msg := msg.(type) {
	case tview.KeyMsg:
		switch msg.Key() {
		case tcell.KeyEscape, tcell.KeyEnter:
			return func() tview.Msg { return CloseMsg{} }
		}
		if msg.Key() == tcell.KeyRune && msg.Str() == "q" {
			return func() tview.Msg { return CloseMsg{} }
		}
		return nil
	case tview.MouseMsg:
		if !m.InRect(msg.Position()) && (msg.Action == tview.MouseLeftClick || msg.Action == tview.MouseRightClick) {
			return func() tview.Msg { return CloseMsg{} }
		}
		return nil
	}
	return m.TextView.Update(msg)
}

var _ tview.Model = (*Model)(nil)
