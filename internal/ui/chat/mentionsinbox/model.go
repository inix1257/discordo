// Package mentionsinbox shows the messages that recently mentioned the user,
// like the inbox of the official client.
package mentionsinbox

import (
	"regexp"
	"strings"

	"github.com/ayn2op/arikawa/v3/api"
	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/utils/httputil"
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/ui"
	"github.com/ayn2op/ningen/v3"
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/keybind"
	"github.com/ayn2op/tview/list"
	"github.com/gdamore/tcell/v3"
)

const fetchLimit = 50

const unreadMarker = "● "

var (
	timeStyle    = tcell.StyleDefault.Dim(true)
	channelStyle = tcell.StyleDefault.Foreground(tcell.ColorTeal).Bold(true)
	guildStyle   = tcell.StyleDefault.Foreground(tcell.ColorTeal).Italic(true)
	authorStyle  = tcell.StyleDefault.Bold(true)
	unreadStyle  = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite).Bold(true)
)

type entry struct {
	line tview.Line
	ref  *SelectedMsg
}

type Model struct {
	*list.Model
	cfg     *config.Config
	entries []entry
	rows    []*row
}

func NewModel(cfg *config.Config) *Model {
	l := list.NewModel()
	ui.ConfigureBox(l.Box, &cfg.Theme)
	l.
		SetSelectedStyle(tcell.StyleDefault.Reverse(true)).
		SetSnapToItems(true).
		SetCenterCursor(false).
		SetTitle("Recent Mentions")
	l.SetScrollBarVisibility(cfg.Theme.ScrollBar.Visibility.ScrollBarVisibility)
	l.SetScrollBar(tview.NewScrollBar().
		SetTrackStyle(cfg.Theme.ScrollBar.TrackStyle.Style).
		SetThumbStyle(cfg.Theme.ScrollBar.ThumbStyle.Style).
		SetGlyphSet(cfg.Theme.ScrollBar.GlyphSet.GlyphSet))
	keybinds := cfg.Keybinds.Picker
	l.SetKeybinds(list.Keybinds{
		SelectUp:     keybinds.SelectUp.Keybind,
		SelectDown:   keybinds.SelectDown.Keybind,
		SelectTop:    keybinds.SelectTop.Keybind,
		SelectBottom: keybinds.SelectBottom.Keybind,
	})
	return &Model{Model: l, cfg: cfg}
}

var _ tview.Model = (*Model)(nil)

func (m *Model) Update(msg tview.Msg) tview.Cmd {
	ui.UpdateBoxFocus(m.Box, &m.cfg.Theme, msg)
	switch msg := msg.(type) {
	case tview.KeyMsg:
		switch {
		case keybind.Matches(msg, m.cfg.Keybinds.Picker.Select.Keybind):
			return m.selectCurrent()
		case keybind.Matches(msg, m.cfg.Keybinds.Picker.Cancel.Keybind):
			return func() tview.Msg { return CancelMsg{} }
		}
	case tview.MouseMsg:
		if msg.Action == tview.MouseLeftClick {
			x, y := msg.Position()
			cmd := m.Model.Update(msg)
			// The list moved the cursor onto the clicked row, unless the click
			// hit the scroll bar or empty space.
			if i := m.Cursor(); i >= 0 && i < len(m.rows) && m.rows[i].contains(x, y) {
				return m.selectCurrent()
			}
			return cmd
		}
	}
	return m.Model.Update(msg)
}

func (m *Model) View(screen tcell.Screen) {
	ui.ClearWideLeftEdge(screen, m.Box)
	m.Model.View(screen)
}

func (m *Model) selectCurrent() tview.Cmd {
	i := m.Cursor()
	if i < 0 || i >= len(m.entries) || m.entries[i].ref == nil {
		return nil
	}
	ref := *m.entries[i].ref
	return func() tview.Msg { return ref }
}

func (m *Model) ShortHelp() []keybind.Keybind {
	cfg := m.cfg.Keybinds.Picker
	return []keybind.Keybind{cfg.SelectUp.Keybind, cfg.SelectDown.Keybind, cfg.Select.Keybind, cfg.Cancel.Keybind}
}

func (m *Model) FullHelp() [][]keybind.Keybind {
	cfg := m.cfg.Keybinds.Picker
	return [][]keybind.Keybind{
		{cfg.SelectUp.Keybind, cfg.SelectDown.Keybind, cfg.SelectTop.Keybind, cfg.SelectBottom.Keybind},
		{cfg.Select.Keybind, cfg.Cancel.Keybind},
	}
}

func (m *Model) setEntries(entries []entry) {
	m.entries = entries
	m.rows = make([]*row, len(entries))
	for i, e := range entries {
		m.rows[i] = &row{line: e.line}
	}
	m.SetBuilder(func(index int) list.Item {
		if index < 0 || index >= len(m.rows) {
			return nil
		}
		return m.rows[index]
	})
	m.ScrollTop()
	if len(entries) == 0 {
		m.SetCursor(-1)
	} else {
		m.SetCursor(0)
	}
}

func (m *Model) setNotice(text string) {
	m.setEntries([]entry{{line: tview.NewLine(tview.NewSegment(text, timeStyle))}})
}

// SetLoading replaces the list with a placeholder while mentions are fetched.
func (m *Model) SetLoading() {
	m.setNotice("Loading...")
}

type mentionsParams struct {
	Limit    uint `schema:"limit"`
	Roles    bool `schema:"roles"`
	Everyone bool `schema:"everyone"`
}

// Fetch returns the messages that mentioned the user, newest first.
func Fetch(state *ningen.State) ([]discord.Message, error) {
	params := mentionsParams{Limit: fetchLimit, Roles: true, Everyone: true}
	return state.Client.RequestJSON[[]discord.Message](
		"GET", api.EndpointMe+"/mentions",
		httputil.WithSchema(state.Client, params),
	)
}

func (m *Model) SetMentions(state *ningen.State, messages []discord.Message) {
	if len(messages) == 0 {
		m.setNotice("No recent mentions")
		return
	}

	entries := make([]entry, 0, len(messages))
	for _, message := range messages {
		entries = append(entries, entry{
			line: m.itemLine(state, message),
			ref:  &SelectedMsg{ChannelID: message.ChannelID, MessageID: message.ID},
		})
	}
	m.setEntries(entries)
}

// Unread reports whether the mention is newer than what the user has read in
// a channel that still has unread mentions.
func Unread(state *ningen.State, message discord.Message) bool {
	rs := state.ReadState.ReadState(message.ChannelID)
	return rs != nil && rs.MentionCount > 0 && message.ID > rs.LastMessageID
}

func (m *Model) itemLine(state *ningen.State, message discord.Message) tview.Line {
	marker := tview.NewSegment("  ", tcell.StyleDefault)
	if Unread(state, message) {
		marker = tview.NewSegment(unreadMarker, unreadStyle)
	}

	line := tview.NewLine(
		marker,
		tview.NewSegment(message.Timestamp.Time().Local().Format("01/02 15:04")+"  ", timeStyle),
	)

	if channel, err := state.Cabinet.Channel(message.ChannelID); err == nil {
		line = append(line, tview.NewSegment(ui.ChannelToString(*channel, m.cfg.Icons, state), channelStyle))
		if channel.GuildID.IsValid() {
			if guild, err := state.Cabinet.Guild(channel.GuildID); err == nil {
				line = append(line, tview.NewSegment(" · "+guild.Name, guildStyle))
			}
		}
	} else {
		line = append(line, tview.NewSegment("#unknown", channelStyle))
	}

	return append(line,
		tview.NewSegment("  ", tcell.StyleDefault),
		tview.NewSegment(message.Author.DisplayOrUsername(), authorStyle),
		tview.NewSegment(": "+preview(message, roleNamer(state, message)), tcell.StyleDefault),
	)
}

var roleMentionRe = regexp.MustCompile(`<@&(\d+)>`)

// roleNamer looks up role names in the guild of the message's channel.
func roleNamer(state *ningen.State, message discord.Message) func(discord.RoleID) string {
	guildID := message.GuildID
	if !guildID.IsValid() {
		if channel, err := state.Cabinet.Channel(message.ChannelID); err == nil {
			guildID = channel.GuildID
		}
	}
	return func(id discord.RoleID) string {
		if !guildID.IsValid() {
			return ""
		}
		if role, err := state.Cabinet.Role(guildID, id); err == nil {
			return role.Name
		}
		return ""
	}
}

// preview flattens the message onto one line and spells out user and role
// mentions.
func preview(message discord.Message, roleName func(discord.RoleID) string) string {
	content := message.Content
	for _, user := range message.Mentions {
		name := "@" + user.DisplayOrUsername()
		id := user.ID.String()
		content = strings.ReplaceAll(content, "<@"+id+">", name)
		content = strings.ReplaceAll(content, "<@!"+id+">", name)
	}
	content = roleMentionRe.ReplaceAllStringFunc(content, func(match string) string {
		id, err := discord.ParseSnowflake(roleMentionRe.FindStringSubmatch(match)[1])
		if err != nil {
			return match
		}
		if name := roleName(discord.RoleID(id)); name != "" {
			return "@" + name
		}
		return "@role"
	})
	content = strings.Join(strings.Fields(content), " ")
	if content == "" && len(message.Attachments) > 0 {
		content = "[attachment]"
	}
	return content
}

// row draws one line of styled segments and remembers where it was drawn so
// clicks can be matched to it.
type row struct {
	line       tview.Line
	x, y, w, h int
}

func (r *row) Update(tview.Msg) tview.Cmd { return nil }

func (r *row) View(screen tcell.Screen) {
	x, maxWidth := r.x, r.w
	for _, seg := range r.line {
		if maxWidth <= 0 {
			break
		}
		_, width := tview.PrintWithStyle(screen, seg.Text, x, r.y, maxWidth, tview.AlignmentLeft, seg.Style)
		x += width
		maxWidth -= width
	}
}

func (r *row) contains(x, y int) bool {
	return y >= r.y && y < r.y+r.h && x >= r.x && x < r.x+r.w
}

func (r *row) Rect() (int, int, int, int) { return r.x, r.y, r.w, r.h }
func (r *row) SetRect(x, y, w, h int)     { r.x, r.y, r.w, r.h = x, y, w, h }
func (r *row) HasFocus() bool             { return false }
func (r *row) Height(int) int             { return 1 }
