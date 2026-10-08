package chat

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/rivo/uniseg"
)

const profileDateFormat = "2006-01-02"

// authorSpan is where an author name sits inside a rendered message item,
// relative to the item's top-left cell.
type authorSpan struct {
	row, col, width int
	message         discord.Message
}

// authorSpans lists the clickable author names of a rendered message. width
// is the item's render width, needed to find rows that come after wrapping.
func (ml *messagesList) authorSpans(message discord.Message, lines []tview.Line, width int) []authorSpan {
	if ml.cfg.HideBlockedUsers && ml.chat.state.UserIsBlocked(message.Author.ID) {
		return nil
	}

	nameWidth := func(m discord.Message) int { return uniseg.StringWidth(m.Author.DisplayOrUsername()) }
	switch message.Type {
	case discord.ChannelPinnedMessage:
		return nil
	case discord.InlinedReplyMessage:
		var spans []authorSpan
		if ref := message.ReferencedMessage; ref != nil {
			ref := *ref
			ref.GuildID = message.GuildID
			col := uniseg.StringWidth(ml.cfg.Theme.MessagesList.ReplyIndicator + " ")
			spans = append(spans, authorSpan{row: 0, col: col, width: nameWidth(ref), message: ref})
		}
		return append(spans, authorSpan{row: replyPreviewHeight(lines, width), col: 0, width: nameWidth(message), message: message})
	default:
		return []authorSpan{{row: 0, col: 0, width: nameWidth(message), message: message}}
	}
}

// replyPreviewHeight returns how many rows the reply preview takes. The
// preview is the first line; the reply itself starts after it wraps.
func replyPreviewHeight(lines []tview.Line, width int) int {
	if len(lines) == 0 {
		return 1
	}
	return tview.NewTextView().SetWrap(true).SetWordWrap(true).SetLines(lines[:1]).Height(width)
}

// authorAt returns the message whose author name is drawn at (x, y), if any.
func (ml *messagesList) authorAt(x, y int) (discord.Message, bool) {
	message, ok := ml.selectedMessage()
	if !ok {
		return discord.Message{}, false
	}
	item, ok := ml.itemByID[message.ID]
	if !ok {
		return discord.Message{}, false
	}

	itemX, itemY, itemW, _ := item.Rect()
	row, col := y-itemY, x-itemX
	for _, span := range ml.authorSpans(*message, item.Lines(), itemW) {
		if row == span.row && col >= span.col && col < span.col+span.width {
			return span.message, true
		}
	}
	return discord.Message{}, false
}

func (m *Model) openProfileCard(x, y int, message discord.Message) tview.Cmd {
	m.profileCard.SetLines(m.profileLines(message))
	w, h := m.profileCard.PreferredSize()
	_, _, maxW, maxH := m.InnerRect()
	// Open just below the clicked name, like the message menu opens at the cursor.
	y++
	if x+w > maxW {
		x = max(maxW-w, 0)
	}
	if y+h > maxH {
		y = max(maxH-h, 0)
	}
	m.profileCard.SetRect(x, y, w, h)
	m.SetLayerEnabled(profileCardLayerName, true)
	m.ShowLayer(profileCardLayerName).SendToFront(profileCardLayerName)
	return tview.SetFocus(m.profileCard)
}

func (m *Model) closeProfileCard() tview.Cmd {
	m.HideLayer(profileCardLayerName)
	m.SetLayerEnabled(profileCardLayerName, false)
	return tview.SetFocus(m.messagesList)
}

func (m *Model) profileLines(message discord.Message) []tview.Line {
	user := message.Author
	guildID := message.GuildID
	var member *discord.Member
	// Webhooks share IDs with nothing in the member list.
	if guildID.IsValid() && !message.WebhookID.IsValid() {
		member, _ = m.state.Cabinet.Member(guildID, user.ID)
	}
	if member != nil && member.User.ID.IsValid() {
		user = member.User
	}

	dim := tcell.StyleDefault.Dim(true)
	nameStyle := tcell.StyleDefault.Bold(true)
	if member != nil {
		if c, ok := state.MemberColor(member, func(id discord.RoleID) *discord.Role {
			r, _ := m.state.Cabinet.Role(guildID, id)
			return r
		}); ok {
			nameStyle = nameStyle.Foreground(tcell.NewHexColor(int32(c)))
		}
	}

	builder := tview.NewLineBuilder()
	builder.Write(user.DisplayOrUsername(), nameStyle)
	if user.Bot || message.WebhookID.IsValid() {
		builder.Write(" BOT", tcell.StyleDefault.Reverse(true).Bold(true))
	}

	builder.NewLine()
	builder.Write("@"+user.Username, dim)

	if !message.WebhookID.IsValid() {
		m.writePresence(builder, guildID, user.ID)
	}
	if user.Pronouns != "" {
		builder.NewLine()
		builder.Write(user.Pronouns, dim)
	}
	if user.Bio != "" {
		builder.NewLine()
		builder.NewLine()
		builder.Write(user.Bio, tcell.StyleDefault)
	}

	if member != nil {
		if roles := m.memberRoles(guildID, member); len(roles) > 0 {
			builder.NewLine()
			builder.NewLine()
			builder.Write("Roles", dim)
			builder.NewLine()
			for i, role := range roles {
				if i > 0 {
					builder.Write(", ", dim)
				}
				style := tcell.StyleDefault
				if role.Color != 0 {
					style = style.Foreground(tcell.NewHexColor(int32(role.Color)))
				}
				builder.Write(role.Name, style)
			}
		}
	}

	builder.NewLine()
	builder.NewLine()
	if member != nil && member.Nick != "" {
		writeProfileField(builder, "Nickname", member.Nick)
		builder.NewLine()
	}
	if member != nil && member.Joined.IsValid() {
		writeProfileField(builder, "Member since", member.Joined.Time().In(time.Local).Format(profileDateFormat))
		builder.NewLine()
	}
	writeProfileField(builder, "Joined Discord", user.ID.Time().In(time.Local).Format(profileDateFormat))
	builder.NewLine()
	writeProfileField(builder, "ID", user.ID.String())
	return builder.Finish()
}

func writeProfileField(builder *tview.LineBuilder, label, value string) {
	builder.Write(label+" ", tcell.StyleDefault.Dim(true))
	builder.Write(value, tcell.StyleDefault)
}

func (m *Model) writePresence(builder *tview.LineBuilder, guildID discord.GuildID, userID discord.UserID) {
	presence, err := m.state.Cabinet.Presence(guildID, userID)
	if err != nil && guildID.IsValid() {
		presence, err = m.state.Cabinet.Presence(discord.NullGuildID, userID)
	}
	if err != nil {
		return
	}

	label, c := "Offline", color.Gray
	switch presence.Status {
	case discord.OnlineStatus:
		label, c = "Online", color.Green
	case discord.IdleStatus:
		label, c = "Idle", color.Yellow
	case discord.DoNotDisturbStatus:
		label, c = "Do Not Disturb", color.Red
	}
	builder.NewLine()
	builder.Write("● ", tcell.StyleDefault.Foreground(c))
	builder.Write(label, tcell.StyleDefault)

	for _, activity := range presence.Activities {
		var text string
		switch activity.Type {
		case discord.CustomActivity:
			text = activity.State
			if activity.Emoji != nil && activity.Emoji.Name != "" && !activity.Emoji.IsCustom() {
				text = strings.TrimSpace(activity.Emoji.Name + " " + text)
			}
		case discord.GameActivity:
			text = "Playing " + activity.Name
		case discord.StreamingActivity:
			text = "Streaming " + activity.Name
		case discord.ListeningActivity:
			text = "Listening to " + activity.Name
		case discord.WatchingActivity:
			text = "Watching " + activity.Name
		case discord.CompetingActivity:
			text = "Competing in " + activity.Name
		}
		if text != "" {
			builder.NewLine()
			builder.Write(text, tcell.StyleDefault.Italic(true))
		}
	}
}

// memberRoles returns the member's roles, highest first.
func (m *Model) memberRoles(guildID discord.GuildID, member *discord.Member) []discord.Role {
	roles := make([]discord.Role, 0, len(member.RoleIDs))
	for _, id := range member.RoleIDs {
		if role, err := m.state.Cabinet.Role(guildID, id); err == nil {
			roles = append(roles, *role)
		}
	}
	slices.SortFunc(roles, func(a, b discord.Role) int { return cmp.Compare(b.Position, a.Position) })
	return roles
}
