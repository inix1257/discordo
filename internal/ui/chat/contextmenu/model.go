package contextmenu

import (
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/ui"
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/list"
	"github.com/gdamore/tcell/v3"
	"github.com/rivo/uniseg"
)

type Model struct {
	*list.Model
	cfg   *config.Config
	items []string
}

func NewModel(cfg *config.Config) *Model {
	l := list.NewModel()
	ui.ConfigureBox(l.Box, &cfg.Theme)
	l.
		SetSelectedStyle(tcell.StyleDefault.Reverse(true)).
		SetSnapToItems(true).
		SetCenterCursor(false).
		SetTitle("Message")

	return &Model{Model: l, cfg: cfg}
}

func (m *Model) SetItems(items []string) {
	m.items = append(m.items[:0], items...)
	m.SetBuilder(func(index int) list.Item {
		if index < 0 || index >= len(m.items) {
			return nil
		}
		style := tcell.StyleDefault
		line := tview.NewLine(tview.NewSegment(m.items[index], style))
		return tview.NewTextView().
			SetScrollable(false).
			SetWrap(false).
			SetWordWrap(false).
			SetTextStyle(style).
			SetLines([]tview.Line{line})
	})
	if len(m.items) == 0 {
		m.SetCursor(-1)
		return
	}
	m.SetCursor(0)
}

func (m *Model) PreferredSize() (width, height int) {
	width = uniseg.StringWidth(m.Title())
	for _, item := range m.items {
		width = max(width, uniseg.StringWidth(item))
	}

	borders := 0
	if m.cfg.Theme.Border.Enabled {
		borders = 1
	}
	padding := m.cfg.Theme.Border.Padding
	width += borders*2 + padding[2] + padding[3]
	height = len(m.items) + borders*2 + padding[0] + padding[1]
	if height < 1 {
		height = 1
	}
	if width < 8 {
		width = 8
	}
	return width, height
}

func (m *Model) selectedText() (string, bool) {
	index := m.Cursor()
	if index < 0 || index >= len(m.items) {
		return "", false
	}
	return m.items[index], true
}

func (m *Model) hoverAt(x, y int) {
	_, innerY, _, innerH := m.InnerRect()
	index := y - innerY
	if index < 0 || index >= innerH || index >= len(m.items) {
		return
	}
	m.SetCursor(index)
}

func (m *Model) selectCurrent() tview.Cmd {
	text, ok := m.selectedText()
	if !ok {
		return nil
	}
	return func() tview.Msg { return SelectedMsg{Text: text} }
}

func (m *Model) Update(msg tview.Msg) tview.Cmd {
	ui.UpdateBoxFocus(m.Box, &m.cfg.Theme, msg)
	switch msg := msg.(type) {
	case tview.KeyMsg:
		switch msg.Key() {
		case tcell.KeyEnter:
			return m.selectCurrent()
		case tcell.KeyEscape:
			return func() tview.Msg { return CancelMsg{} }
		}
	case tview.MouseMsg:
		if !m.InRect(msg.Position()) {
			if msg.Action == tview.MouseLeftClick || msg.Action == tview.MouseRightClick {
				return func() tview.Msg { return CancelMsg{} }
			}
			return nil
		}
		switch msg.Action {
		case tview.MouseMove:
			m.hoverAt(msg.Position())
			return nil
		case tview.MouseLeftClick:
			m.hoverAt(msg.Position())
			return m.selectCurrent()
		}
	}
	return m.Model.Update(msg)
}

var _ tview.Model = (*Model)(nil)
