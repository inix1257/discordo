package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ayn2op/tview/layers"

	"github.com/ayn2op/arikawa/v3/api"
	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/utils/json/option"
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/consts"
	"github.com/ayn2op/discordo/internal/markdown"
	"github.com/ayn2op/discordo/internal/ui"
	"github.com/ayn2op/discordo/internal/ui/chat/attachmentspicker"
	"github.com/ayn2op/ningen/v3/discordmd"
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/help"
	"github.com/ayn2op/tview/keybind"
	"github.com/ayn2op/tview/list"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/ncruces/zenity"
	"github.com/rivo/uniseg"
	"github.com/skratchdot/open-golang/open"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"golang.design/x/clipboard"
)

type messagesList struct {
	*list.Model
	cfg      *config.Config
	chat     *Model
	messages []discord.Message
	rows     []messagesListRow

	renderer *markdown.Renderer
	// itemByID caches rendered message TextViews.
	itemByID map[discord.MessageID]*tview.TextView

	attachmentsPicker *attachmentspicker.Model

	// olderLoading is the channel whose older-history request is in flight.
	olderLoading discord.ChannelID
	// olderExhausted is set once the start of the channel history is loaded.
	olderExhausted bool

	// firstDrawn and lastDrawn are the message indexes drawn in the last
	// frame, or -1.
	firstDrawn, lastDrawn int
	// topBar and bottomBar are the mentions the off-screen bars jump to, or -1.
	topBar, bottomBar int
}

var _ help.KeyMap = (*messagesList)(nil)

type messagesListRowKind uint8

const (
	messagesListRowMessage messagesListRowKind = iota
	messagesListRowSeparator
)

type messagesListRow struct {
	kind         messagesListRowKind
	messageIndex int
	timestamp    discord.Timestamp
}

func newMessagesList(cfg *config.Config, chat *Model) *messagesList {
	ml := &messagesList{
		Model:    list.NewModel(),
		cfg:      cfg,
		chat:     chat,
		renderer: markdown.NewRenderer(cfg),

		firstDrawn: -1, lastDrawn: -1,
		topBar: -1, bottomBar: -1,
		itemByID: make(map[discord.MessageID]*tview.TextView),
	}
	ml.attachmentsPicker = attachmentspicker.NewModel(cfg)

	ui.ConfigureBox(ml.Box, &cfg.Theme)
	ml.SetTitle("Messages")
	ml.SetBuilder(ml.buildItem)
	ml.SetTrackEnd(true)
	ml.SetCenterCursor(false)
	ml.SetSelectedStyle(cfg.Theme.MessagesList.SelectedMessageStyle.Style)
	ml.SetKeybinds(list.Keybinds{
		ScrollUp:     cfg.Keybinds.MessagesList.ScrollUp.Keybind,
		ScrollDown:   cfg.Keybinds.MessagesList.ScrollDown.Keybind,
		ScrollTop:    cfg.Keybinds.MessagesList.ScrollTop.Keybind,
		ScrollBottom: cfg.Keybinds.MessagesList.ScrollBottom.Keybind,
	})
	ml.SetScrollBarVisibility(cfg.Theme.ScrollBar.Visibility.ScrollBarVisibility)
	ml.SetScrollBar(tview.NewScrollBar().
		SetTrackStyle(cfg.Theme.ScrollBar.TrackStyle.Style).
		SetThumbStyle(cfg.Theme.ScrollBar.ThumbStyle.Style).
		SetGlyphSet(cfg.Theme.ScrollBar.GlyphSet.GlyphSet))
	return ml
}

func (ml *messagesList) reset() {
	ml.messages = nil
	ml.rows = nil
	clear(ml.itemByID)
	ml.resetOlderHistory()
	ml.
		Clear().
		SetBuilder(ml.buildItem).
		SetTitle("")
}

func (ml *messagesList) setTitle(channel discord.Channel) {
	title := ui.ChannelToString(channel, ml.cfg.Icons, ml.chat.state)
	if topic := channel.Topic; topic != "" {
		title += " - " + topic
	}

	ml.SetTitle(title)
}

func (ml *messagesList) View(screen tcell.Screen) {
	ml.firstDrawn, ml.lastDrawn = -1, -1
	ml.Model.View(screen)
	ml.drawMentionIndicators(screen)
	if ml.chat.guildsCollapsed && ml.cfg.Mouse {
		drawCollapsedGuildsToggle(screen, ml.Box, ml.cfg, ml.HasFocus())
	}
	drawMentionsButton(screen, ml.Box, ml.cfg, ml.HasFocus(), ml.chat.mentionsUnread)
}

func (ml *messagesList) setMessages(messages []discord.Message) {
	ml.messages = slices.Clone(messages)
	slices.Reverse(ml.messages)
	clear(ml.itemByID)
	ml.resetOlderHistory()
	ml.rebuildRows()
}

func (ml *messagesList) addMessage(message discord.Message) {
	ml.messages = append(ml.messages, message)
	delete(ml.itemByID, message.ID)
	ml.rebuildRows()
}

func (ml *messagesList) setMessage(index int, message discord.Message) {
	if index < 0 || index >= len(ml.messages) {
		return
	}

	ml.messages[index] = message
	delete(ml.itemByID, message.ID)
	if index+1 < len(ml.messages) {
		delete(ml.itemByID, ml.messages[index+1].ID)
	}
	ml.rebuildRows()
}

func (ml *messagesList) deleteMessage(index int) {
	if index < 0 || index >= len(ml.messages) {
		return
	}

	delete(ml.itemByID, ml.messages[index].ID)
	if index+1 < len(ml.messages) {
		delete(ml.itemByID, ml.messages[index+1].ID)
	}
	ml.messages = slices.Delete(ml.messages, index, index+1)
	ml.rebuildRows()
}

func (ml *messagesList) clearSelection() {
	ml.SetCursor(-1)
}

func (ml *messagesList) buildItem(index int) list.Item {
	if index < 0 || index >= len(ml.rows) {
		return nil
	}

	row := ml.rows[index]
	if row.kind == messagesListRowSeparator {
		return ml.buildSeparatorItem(row.timestamp)
	}

	// The list applies the selection style at draw time, so a message is
	// rendered once and reused for every cursor position. Cursor moves no
	// longer re-parse markdown or re-run syntax highlighting.
	message := ml.messages[row.messageIndex]
	item, ok := ml.itemByID[message.ID]
	if !ok {
		item = tview.NewTextView().
			SetWrap(true).
			SetWordWrap(true).
			SetLines(ml.renderMessage(message, ml.cfg.Theme.MessagesList.MessageStyle.Style))
		ml.itemByID[message.ID] = item
	}
	return &trackedItem{TextView: item, ml: ml, messageIndex: row.messageIndex}
}

func (ml *messagesList) renderMessage(message discord.Message, baseStyle tcell.Style) []tview.Line {
	builder := tview.NewLineBuilder()
	ml.writeMessage(builder, message, baseStyle)
	return builder.Finish()
}

func (ml *messagesList) buildSeparatorItem(ts discord.Timestamp) *tview.TextView {
	builder := tview.NewLineBuilder()
	ml.drawDateSeparator(builder, ts, ml.cfg.Theme.MessagesList.MessageStyle.Style)
	return tview.NewTextView().
		SetScrollable(false).
		SetWrap(false).
		SetWordWrap(false).
		SetLines(builder.Finish())
}

func (ml *messagesList) drawDateSeparator(builder *tview.LineBuilder, ts discord.Timestamp, baseStyle tcell.Style) {
	date := ts.Time().In(time.Local).Format(ml.cfg.DateSeparator.Format)
	label := " " + date + " "
	fillChar := ml.cfg.DateSeparator.Character
	dimStyle := baseStyle.Dim(true)
	_, _, width, _ := ml.InnerRect()
	if width <= 0 {
		builder.Write(strings.Repeat(fillChar, 8)+label+strings.Repeat(fillChar, 8), dimStyle)
		return
	}

	labelWidth := utf8.RuneCountInString(label)
	if width <= labelWidth {
		builder.Write(date, dimStyle)
		return
	}

	fillWidth := width - labelWidth
	left := fillWidth / 2
	right := fillWidth - left
	builder.Write(strings.Repeat(fillChar, left)+label+strings.Repeat(fillChar, right), dimStyle)
}

func (ml *messagesList) rebuildRows() {
	rows := make([]messagesListRow, 0, len(ml.messages)*2)

	for index := range ml.messages {
		// Always show a date separator before the first message, and between messages on different days.
		if ml.cfg.DateSeparator.Enabled && (index == 0 || !sameLocalDate(ml.messages[index-1].Timestamp, ml.messages[index].Timestamp)) {
			rows = append(rows, messagesListRow{
				kind:      messagesListRowSeparator,
				timestamp: ml.messages[index].Timestamp,
			})
		}

		rows = append(rows, messagesListRow{
			kind:         messagesListRowMessage,
			messageIndex: index,
		})
	}

	ml.rows = rows
	ml.SetBuilder(ml.buildItem)
}

func sameLocalDate(a discord.Timestamp, b discord.Timestamp) bool {
	ta := a.Time().In(time.Local)
	tb := b.Time().In(time.Local)
	return ta.Year() == tb.Year() && ta.YearDay() == tb.YearDay()
}

func sameLocalMinute(a discord.Timestamp, b discord.Timestamp) bool {
	ta := a.Time().In(time.Local)
	tb := b.Time().In(time.Local)
	return ta.Year() == tb.Year() && ta.YearDay() == tb.YearDay() && ta.Hour() == tb.Hour() && ta.Minute() == tb.Minute()
}

func (ml *messagesList) showTimestamp(message discord.Message) bool {
	for i, current := range ml.messages {
		if current.ID != message.ID {
			continue
		}
		if i == 0 {
			return true
		}
		prev := ml.messages[i-1]
		if prev.Author.ID == message.Author.ID {
			return false
		}
		return !sameLocalMinute(prev.Timestamp, message.Timestamp)
	}
	return true
}

// Cursor returns the selected message index, skipping separator rows.
func (ml *messagesList) Cursor() int {
	rowIndex := ml.Model.Cursor()
	if rowIndex < 0 || rowIndex >= len(ml.rows) {
		return -1
	}

	row := ml.rows[rowIndex]
	if row.kind != messagesListRowMessage {
		return -1
	}
	return row.messageIndex
}

// SetCursor selects a message index and maps it to the corresponding row.
func (ml *messagesList) SetCursor(index int) {
	ml.Model.SetCursor(ml.messageToRowIndex(index))
}

func (ml *messagesList) messageToRowIndex(messageIndex int) int {
	if messageIndex < 0 || messageIndex >= len(ml.messages) {
		return -1
	}

	for i, row := range ml.rows {
		if row.kind == messagesListRowMessage && row.messageIndex == messageIndex {
			return i
		}
	}

	return -1
}

func (ml *messagesList) onRowCursorChanged(rowIndex int) {
	if rowIndex < 0 || rowIndex >= len(ml.rows) || ml.rows[rowIndex].kind == messagesListRowMessage {
		return
	}

	target := ml.nearestMessageRowIndex(rowIndex)
	ml.Model.SetCursor(target)
}

// nearestMessageRowIndex expects rowIndex to be within bounds.
func (ml *messagesList) nearestMessageRowIndex(rowIndex int) int {
	for i := rowIndex - 1; i >= 0; i-- {
		if ml.rows[i].kind == messagesListRowMessage {
			return i
		}
	}
	for i := rowIndex + 1; i < len(ml.rows); i++ {
		if ml.rows[i].kind == messagesListRowMessage {
			return i
		}
	}
	return -1
}

func (ml *messagesList) writeMessage(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	if ml.cfg.HideBlockedUsers {
		isBlocked := ml.chat.state.UserIsBlocked(message.Author.ID)
		if isBlocked {
			builder.Write("Blocked message", baseStyle.Foreground(color.Red).Bold(true))
			return
		}
	}

	switch message.Type {
	case discord.DefaultMessage:
		if message.Reference != nil && message.Reference.Type == discord.MessageReferenceTypeForward {
			ml.drawForwardedMessage(builder, message, baseStyle)
		} else {
			ml.drawDefaultMessage(builder, message, baseStyle)
		}
	case discord.GuildMemberJoinMessage:
		ml.drawAuthor(builder, message, baseStyle)
		builder.Write("joined the server.", baseStyle)
		ml.drawOwnTimestamp(builder, message, baseStyle)
	case discord.InlinedReplyMessage:
		ml.drawReplyMessage(builder, message, baseStyle)
	case discord.ChannelPinnedMessage:
		ml.drawPinnedMessage(builder, message, baseStyle)
	default:
		ml.drawAuthor(builder, message, baseStyle)
		ml.drawOwnTimestamp(builder, message, baseStyle)
	}
	ml.drawReactions(builder, message.Reactions, baseStyle)
}

func (ml *messagesList) drawReactions(builder *tview.LineBuilder, reactions []discord.Reaction, baseStyle tcell.Style) {
	if len(reactions) == 0 {
		return
	}

	builder.NewLine()
	for i, reaction := range reactions {
		if i > 0 {
			builder.Write("  ", baseStyle)
		}

		name := reaction.Emoji.Name
		if reaction.Emoji.IsCustom() {
			name = ":" + name + ":"
		}
		style := ml.cfg.Theme.MessagesList.ReactionStyle.Style
		if reaction.Me {
			style = ml.cfg.Theme.MessagesList.OwnReactionStyle.Style
		}
		builder.Write("["+name+" "+strconv.Itoa(reaction.Count)+"]", tview.MergeStyle(baseStyle, style))
	}
}

func (ml *messagesList) formatTimestamp(ts discord.Timestamp) string {
	return ts.Time().In(time.Local).Format(ml.cfg.Timestamps.Format)
}

func (ml *messagesList) drawTimestamps(builder *tview.LineBuilder, ts discord.Timestamp, baseStyle tcell.Style) {
	dimStyle := baseStyle.Dim(true)
	builder.Write(" "+ml.formatTimestamp(ts), dimStyle)
}

func (ml *messagesList) drawOwnTimestamp(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	if ml.showTimestamp(message) {
		ml.drawTimestamps(builder, message.Timestamp, baseStyle)
	}
}

func (ml *messagesList) drawAuthor(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	name := message.Author.DisplayOrUsername()
	foreground := tcell.ColorDefault

	if member := ml.memberForMessage(message); member != nil {
		color, ok := state.MemberColor(member, func(id discord.RoleID) *discord.Role {
			r, _ := ml.chat.state.Cabinet.Role(message.GuildID, id)
			return r
		})
		if ok {
			foreground = tcell.NewHexColor(int32(color))
		}
	}

	style := baseStyle.Foreground(foreground).Bold(true)
	builder.Write(name+" ", style)
}

func (ml *messagesList) memberForMessage(message discord.Message) *discord.Member {
	// Webhooks do not have nicknames or roles.
	if !message.GuildID.IsValid() || message.WebhookID.IsValid() {
		return nil
	}

	member, err := ml.chat.state.Cabinet.Member(message.GuildID, message.Author.ID)
	if err != nil {
		slog.Error("failed to get member from state", "guild_id", message.GuildID, "member_id", message.Author.ID, "err", err)
		return nil
	}
	return member
}

// drawContent renders the message body and returns the parsed markdown AST
// together with the source bytes it indexes into, so callers can reuse them
// instead of re-parsing the same content (see drawEmbeds). root is nil when
// markdown rendering is disabled.
func (ml *messagesList) drawContent(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) (ast.Node, []byte) {
	lines, root, source := ml.renderContentLines(message, baseStyle)
	if ml.cfg.Markdown.Enabled && builder.HasCurrentLine() {
		startsWithCodeBlock := false
		if root != nil {
			if first := root.FirstChild(); first != nil {
				_, startsWithCodeBlock = first.(*ast.FencedCodeBlock)
			}
		}

		if startsWithCodeBlock {
			// Keep code blocks visually separate from the author line.
			builder.NewLine()
			for len(lines) > 0 && len(lines[0]) == 0 {
				lines = lines[1:]
			}
		} else {
			for len(lines) > 1 && len(lines[0]) == 0 {
				lines = lines[1:]
			}
		}
	}
	builder.AppendLines(lines)
	return root, source
}

func (ml *messagesList) renderContentLines(message discord.Message, baseStyle tcell.Style) ([]tview.Line, ast.Node, []byte) {
	return ml.renderContentLinesWithMarkdown(message, baseStyle, false)
}

func (ml *messagesList) renderContentLinesWithMarkdown(message discord.Message, baseStyle tcell.Style, forceMarkdown bool) ([]tview.Line, ast.Node, []byte) {
	// Keep one rendering path for both normal messages and embed fragments so we preserve mention/link parsing behavior consistently across both.
	if forceMarkdown || ml.cfg.Markdown.Enabled {
		c := []byte(message.Content)
		root := discordmd.ParseWithMessage(c, *ml.chat.state.Cabinet, &message, false)
		return ml.renderer.RenderLines(c, root, baseStyle), root, c
	}

	b := tview.NewLineBuilder()
	b.Write(message.Content, baseStyle)
	return b.Finish(), nil, nil
}

func (ml *messagesList) drawSnapshotContent(builder *tview.LineBuilder, parent discord.Message, snapshot discord.MessageSnapshotMessage, baseStyle tcell.Style) {
	// Convert discord.MessageSnapshotMessage to discord.Message with common fields.
	message := discord.Message{
		Type:            snapshot.Type,
		Content:         snapshot.Content,
		Embeds:          snapshot.Embeds,
		Attachments:     snapshot.Attachments,
		Timestamp:       snapshot.Timestamp,
		EditedTimestamp: snapshot.EditedTimestamp,
		Flags:           snapshot.Flags,
		Mentions:        snapshot.Mentions,
		MentionRoleIDs:  snapshot.MentionRoleIDs,
		Stickers:        snapshot.Stickers,
		Components:      snapshot.Components,
		ChannelID:       parent.ChannelID,
		GuildID:         parent.GuildID,
	}
	ml.drawContent(builder, message, baseStyle)
	ml.drawStickers(builder, message, baseStyle)
}

func (ml *messagesList) drawDefaultMessage(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	ml.drawAuthor(builder, message, baseStyle)
	contentRoot, contentSource := ml.drawContent(builder, message, baseStyle)

	if message.EditedTimestamp.IsValid() {
		dimStyle := baseStyle.Dim(true)
		builder.Write(" (edited)", dimStyle)
	}
	if ml.cfg.Timestamps.Enabled {
		ml.drawOwnTimestamp(builder, message, baseStyle)
	}

	ml.drawEmbeds(builder, message, baseStyle, contentRoot, contentSource)

	attachmentStyle := tview.MergeStyle(baseStyle, ml.cfg.Theme.MessagesList.AttachmentStyle.Style)
	for _, a := range message.Attachments {
		builder.NewLine()
		style := attachmentStyle
		if ml.cfg.ShowAttachmentLinks {
			style = attachmentStyle.Url(a.URL)
		}
		builder.Write(a.Filename, style)
	}

	ml.drawStickers(builder, message, baseStyle)
}

func (ml *messagesList) drawStickers(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	if len(message.Stickers) == 0 {
		return
	}

	builder.NewLine()
	builder.Write("(sticker)", baseStyle.Foreground(color.Green))
}

func (ml *messagesList) drawEmbeds(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style, contentRoot ast.Node, contentSource []byte) {
	if len(message.Embeds) == 0 {
		return
	}

	// Embed URLs are deduplicated against links already shown in the message
	// body. Reuse the body's parsed AST when markdown is enabled; only the
	// markdown-disabled path (no AST) has to parse the content here.
	var contentListURLs []string
	if contentRoot != nil {
		contentListURLs = urlsFromAST(contentRoot, contentSource)
	} else {
		contentListURLs = extractURLs(message.Content)
	}
	contentURLs := make(map[string]struct{}, len(contentListURLs))
	for _, u := range contentListURLs {
		contentURLs[u] = struct{}{}
	}

	lineStyles := embedLineStyles(baseStyle, ml.cfg.Theme.MessagesList.Embeds)
	defaultBarStyle := baseStyle.Dim(true)
	prefixText := "  ▎ "
	prefixWidth := uniseg.StringWidth(prefixText)
	_, _, innerWidth, _ := ml.InnerRect()
	// Wrap against the current list viewport. This keeps embed wrapping stable even when sidebars/panes are resized.
	wrapWidth := max(innerWidth-prefixWidth, 1)

	for _, embed := range message.Embeds {
		lines := embedLines(embed, contentURLs)
		if len(lines) == 0 {
			continue
		}

		embedContentLines := make([]tview.Line, 0, len(lines)*2)
		barStyle := defaultBarStyle
		if embed.Color != discord.NullColor && embed.Color != 0 {
			barStyle = barStyle.Foreground(tcell.NewHexColor(int32(embed.Color)))
		}
		prefix := tview.NewSegment(prefixText, barStyle)
		builder.NewLine()
		for _, line := range lines {
			if strings.TrimSpace(line.Text) == "" {
				continue
			}
			msg := message
			msg.Content = line.Text
			lineStyle := lineStyles[line.Kind]
			// Embed descriptions are always markdown-rendered to match Discord's rich embed semantics, even when message markdown is globally disabled.
			rendered, _, _ := ml.renderContentLinesWithMarkdown(msg, lineStyle, line.Kind == embedLineDescription)
			for _, renderedLine := range rendered {
				if line.URL != "" {
					renderedLine = lineWithURL(renderedLine, line.URL)
				}
				// Prefix must be applied after wrapping so every visual line keeps the embed bar marker ("▎"), not only the first logical line.
				for _, wrapped := range wrapStyledLine(renderedLine, wrapWidth) {
					prefixed := make(tview.Line, 0, len(wrapped)+1)
					prefixed = append(prefixed, prefix)
					prefixed = append(prefixed, wrapped...)
					embedContentLines = append(embedContentLines, prefixed)
				}
			}
		}

		if len(embedContentLines) > 0 {
			builder.AppendLines(embedContentLines)
		}
	}
}

func wrapStyledLine(line tview.Line, width int) []tview.Line {
	if width <= 0 {
		return []tview.Line{line}
	}
	if len(line) == 0 {
		return []tview.Line{line}
	}

	lines := make([]tview.Line, 0, 2)
	current := make(tview.Line, 0, len(line))
	currentWidth := 0

	pushSegment := func(text string, style tcell.Style) {
		if text == "" {
			return
		}
		if n := len(current); n > 0 && current[n-1].Style == style {
			current[n-1].Text += text
			return
		}
		current = append(current, tview.Segment{Text: text, Style: style})
	}

	flush := func() {
		lineCopy := make(tview.Line, len(current))
		copy(lineCopy, current)
		lines = append(lines, lineCopy)
		current = current[:0]
		currentWidth = 0
	}

	for _, segment := range line {
		state := -1
		rest := segment.Text
		for len(rest) > 0 {
			cluster, nextRest, boundaries, nextState := uniseg.StepString(rest, state)
			state = nextState
			rest = nextRest
			if cluster == "" {
				continue
			}

			// Use grapheme width (not rune count) so wrapping stays correct with wide glyphs, emoji, and combining characters.
			clusterWidth := graphemeClusterWidth(boundaries)
			if currentWidth > 0 && currentWidth+clusterWidth > width {
				flush()
			}
			pushSegment(cluster, segment.Style)
			currentWidth += clusterWidth

			if currentWidth >= width {
				flush()
			}
		}
	}

	if len(current) > 0 {
		flush()
	}
	if len(lines) == 0 {
		return []tview.Line{{}}
	}
	return lines
}

func graphemeClusterWidth(boundaries int) int {
	return boundaries >> uniseg.ShiftWidth
}

func lineWithURL(line tview.Line, rawURL string) tview.Line {
	out := make(tview.Line, len(line))
	for i, segment := range line {
		out[i] = segment
		out[i].Style = out[i].Style.Url(rawURL)
	}
	return out
}

type embedLine struct {
	Text string
	Kind embedLineKind
	URL  string
}

type embedLineKind uint8

const (
	// Keep this ordering stable: drawEmbeds indexes precomputed style slots by this enum.
	embedLineProvider embedLineKind = iota
	embedLineAuthor
	embedLineTitle
	embedLineDescription
	embedLineFieldName
	embedLineFieldValue
	embedLineFooter
	embedLineURL
)

func embedLineStyles(baseStyle tcell.Style, theme config.MessagesListEmbedsTheme) [8]tcell.Style {
	styles := [8]tcell.Style{}
	styles[embedLineProvider] = tview.MergeStyle(baseStyle, theme.ProviderStyle.Style)
	styles[embedLineAuthor] = tview.MergeStyle(baseStyle, theme.AuthorStyle.Style)
	styles[embedLineTitle] = tview.MergeStyle(baseStyle, theme.TitleStyle.Style)
	styles[embedLineDescription] = tview.MergeStyle(baseStyle, theme.DescriptionStyle.Style)
	styles[embedLineFieldName] = tview.MergeStyle(baseStyle, theme.FieldNameStyle.Style)
	styles[embedLineFieldValue] = tview.MergeStyle(baseStyle, theme.FieldValueStyle.Style)
	styles[embedLineFooter] = tview.MergeStyle(baseStyle, theme.FooterStyle.Style)
	styles[embedLineURL] = tview.MergeStyle(baseStyle, theme.URLStyle.Style)
	return styles
}

type embedLineDedupKey struct {
	kind embedLineKind
	text string
}

func embedLines(embed discord.Embed, contentURLs map[string]struct{}) []embedLine {
	lines := make([]embedLine, 0, 8)
	seen := make(map[embedLineDedupKey]struct{}, 8)

	appendUnique := func(s string, kind embedLineKind, rawURL string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// Deduplicate by kind+text so the same value can intentionally appear in multiple semantic slots with different styles (e.g. title vs. field).
		key := embedLineDedupKey{kind: kind, text: s}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		lines = append(lines, embedLine{
			Text: s,
			Kind: kind,
			URL:  rawURL,
		})
	}

	appendURL := func(url discord.URL) {
		u := strings.TrimSpace(url)
		if u == "" {
			return
		}
		// Avoid duplicating links that already appear in message body content.
		if _, ok := contentURLs[u]; ok {
			return
		}
		appendUnique(linkDisplayText(u), embedLineURL, u)
	}

	if embed.Provider != nil {
		appendUnique(embed.Provider.Name, embedLineProvider, "")
	}
	if embed.Author != nil {
		appendUnique(embed.Author.Name, embedLineAuthor, "")
	}
	appendUnique(embed.Title, embedLineTitle, embed.URL)
	// Some Discord embeds include markdown-escaped punctuation in raw payload text (e.g. "\."), so normalize for display.
	appendUnique(unescapeMarkdownEscapes(embed.Description), embedLineDescription, "")

	for _, field := range embed.Fields {
		switch {
		case field.Name != "" && field.Value != "":
			appendUnique(field.Name, embedLineFieldName, "")
			appendUnique(field.Value, embedLineFieldValue, "")
		case field.Name != "":
			appendUnique(field.Name, embedLineFieldName, "")
		default:
			appendUnique(field.Value, embedLineFieldValue, "")
		}
	}

	if embed.Footer != nil {
		appendUnique(embed.Footer.Text, embedLineFooter, "")
	}

	// Prefer media URLs after textual fields so previews read top-to-bottom before jumping to link targets.
	// When a title exists, embed.URL is represented by title Style.Url metadata instead of a separate URL row.
	if embed.Title == "" {
		appendURL(embed.URL)
	}
	if embed.Image != nil {
		appendURL(embed.Image.URL)
	}
	if embed.Video != nil {
		appendURL(embed.Video.URL)
	}

	return lines
}

func linkDisplayText(raw string) string {
	if name, ok := markdown.FileLinkName(raw); ok {
		return name
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}

	path := strings.TrimSpace(parsed.EscapedPath())
	switch {
	case path == "", path == "/":
		return parsed.Host
	case len(path) > 48:
		return parsed.Host + path[:45] + "..."
	default:
		return parsed.Host + path
	}
}

func unescapeMarkdownEscapes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := range len(s) {
		if s[i] == '\\' && i+1 < len(s) && isMarkdownEscapable(s[i+1]) {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isMarkdownEscapable(c byte) bool {
	switch c {
	case '\\', '`', '*', '_', '{', '}', '[', ']', '(', ')', '#', '+', '-', '.', '!', '|', '>', '~':
		return true
	default:
		return false
	}
}

func (ml *messagesList) drawForwardedMessage(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	dimStyle := baseStyle.Dim(true)
	ml.drawAuthor(builder, message, baseStyle)
	builder.Write(ml.cfg.Theme.MessagesList.ForwardedIndicator+" ", dimStyle)
	ml.drawSnapshotContent(builder, message, message.MessageSnapshots[0].Message, baseStyle)
	builder.Write(" ("+ml.formatTimestamp(message.MessageSnapshots[0].Message.Timestamp)+")", dimStyle)
	ml.drawOwnTimestamp(builder, message, baseStyle)
}

func (ml *messagesList) drawReplyMessage(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	dimStyle := baseStyle.Dim(true)
	// indicator
	builder.Write(ml.cfg.Theme.MessagesList.ReplyIndicator+" ", dimStyle)

	if m := message.ReferencedMessage; m != nil {
		m.GuildID = message.GuildID
		ml.drawAuthor(builder, *m, dimStyle)
		ml.drawContent(builder, *m, dimStyle)
		ml.drawStickers(builder, *m, dimStyle)
	} else {
		builder.Write("Original message was deleted", dimStyle)
	}

	builder.NewLine()
	// main
	ml.drawDefaultMessage(builder, message, baseStyle)
}

func (ml *messagesList) drawPinnedMessage(builder *tview.LineBuilder, message discord.Message, baseStyle tcell.Style) {
	builder.Write(message.Author.DisplayOrUsername(), baseStyle)
	builder.Write(" pinned a message.", baseStyle)
}

func (ml *messagesList) selectedMessage() (*discord.Message, bool) {
	if len(ml.messages) == 0 {
		return nil, false
	}

	cursor := ml.Cursor()
	if cursor == -1 || cursor >= len(ml.messages) {
		return nil, false
	}

	return &ml.messages[cursor], true
}

func (ml *messagesList) Update(msg tview.Msg) tview.Cmd {
	ui.UpdateBoxFocus(ml.Box, &ml.cfg.Theme, msg)
	switch msg := msg.(type) {
	case tview.FocusMsg:
		return tview.Sequence(ml.Model.Update(msg), focused(ml))
	case tview.KeyMsg:
		switch {
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.Cancel.Keybind):
			ml.clearSelection()
			return nil
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.SelectUp.Keybind):
			return ml.selectUp()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.SelectDown.Keybind):
			ml.selectDown()
			return nil
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.SelectTop.Keybind):
			ml.selectTop()
			return nil
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.SelectBottom.Keybind):
			ml.selectBottom()
			return nil
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.SelectReply.Keybind):
			return ml.selectReply()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.YankID.Keybind):
			return ml.yankMessageID()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.YankContent.Keybind):
			return ml.yankContent()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.YankURL.Keybind):
			return ml.yankURL()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.Open.Keybind):
			return ml.open()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.OpenInBrowser.Keybind):
			return ml.openInBrowser()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.OpenWithApp.Keybind):
			return ml.openWith(ml.openAttachment)
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.Download.Keybind):
			return ml.download()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.Reply.Keybind):
			return ml.reply(false)
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.ReplyMention.Keybind):
			return ml.reply(true)
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.Edit.Keybind):
			return ml.editSelectedMessage()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.Delete.Keybind):
			return ml.deleteSelectedMessage()
		case keybind.Matches(msg, ml.cfg.Keybinds.MessagesList.DeleteConfirm.Keybind):
			return ml.confirmDelete()
		}
	case tview.MouseMsg:
		x, y := msg.Position()
		if ml.chat.guildsCollapsed && msg.Action == tview.MouseLeftClick && hitGuildsToggle(ml.Box, true, x, y) {
			return toggleGuildsTree()
		}
		if msg.Action == tview.MouseLeftClick && hitMentionsButton(ml.Box, x, y) {
			return toggleMentionsInbox()
		}
		if msg.Action == tview.MouseLeftClick && ml.InRect(x, y) && ml.jumpToMentionBar(y) {
			return tview.SetFocus(ml)
		}
		if msg.Action == tview.MouseRightClick {
			ml.Model.Update(tview.MouseMsg{EventMouse: msg.EventMouse, Action: tview.MouseLeftClick})
			ml.onRowCursorChanged(ml.Model.Cursor())
			return ml.showMessageMenu(x, y)
		}
		if msg.Action == tview.MouseLeftClick {
			cmd := ml.Model.Update(msg)
			ml.onRowCursorChanged(ml.Model.Cursor())
			if message, ok := ml.authorAt(x, y); ok {
				return tview.Sequence(cmd, ml.chat.openProfileCard(x, y, message))
			}
			if ref, ok := ml.replyPreviewAt(y); ok {
				return tview.Sequence(cmd, ml.jumpToMessage(ref))
			}
			return cmd
		}
	case olderMessagesLoadedMsg:
		return ml.onOlderMessagesLoaded(msg)
	}
	cmd := ml.Model.Update(msg)
	ml.onRowCursorChanged(ml.Model.Cursor())
	if mouse, ok := msg.(tview.MouseMsg); ok && mouse.Action == tview.MouseScrollUp {
		return tview.Batch(cmd, ml.loadOlderOnScrollTop(mouse))
	}
	return cmd
}

func (ml *messagesList) selectUp() tview.Cmd {
	messages := ml.messages
	if len(messages) == 0 {
		return nil
	}

	cursor := ml.Cursor()
	switch {
	case cursor == -1:
		cursor = len(messages) - 1
	case cursor > 0:
		cursor--
	case cursor == 0:
		return ml.fetchOlderMessages()
	}

	ml.SetCursor(cursor)
	return nil
}

func (ml *messagesList) selectDown() {
	messages := ml.messages
	if len(messages) == 0 {
		return
	}

	cursor := ml.Cursor()
	switch {
	case cursor == -1:
		cursor = len(messages) - 1
	case cursor < len(messages)-1:
		cursor++
	}

	ml.SetCursor(cursor)
}

func (ml *messagesList) selectTop() {
	if len(ml.messages) == 0 {
		return
	}
	ml.SetCursor(0)
}

func (ml *messagesList) selectBottom() {
	if len(ml.messages) == 0 {
		return
	}
	ml.SetCursor(len(ml.messages) - 1)
}

func (ml *messagesList) selectReply() tview.Cmd {
	message, ok := ml.selectedMessage()
	if !ok || message.ReferencedMessage == nil {
		return nil
	}
	return ml.jumpToMessage(*message.ReferencedMessage)
}

// replyPreviewAt returns the original message when y is on the reply preview
// of the selected message.
func (ml *messagesList) replyPreviewAt(y int) (discord.Message, bool) {
	message, ok := ml.selectedMessage()
	if !ok || message.Type != discord.InlinedReplyMessage || message.ReferencedMessage == nil {
		return discord.Message{}, false
	}
	item, ok := ml.itemByID[message.ID]
	if !ok {
		return discord.Message{}, false
	}
	_, itemY, itemW, _ := item.Rect()
	if row := y - itemY; row < 0 || row >= replyPreviewHeight(item.Lines(), itemW) {
		return discord.Message{}, false
	}
	return *message.ReferencedMessage, true
}

// jumpToMessage selects target when it is loaded, and otherwise reloads the
// channel back to it.
func (ml *messagesList) jumpToMessage(target discord.Message) tview.Cmd {
	if i := slices.IndexFunc(ml.messages, func(m discord.Message) bool { return m.ID == target.ID }); i != -1 {
		ml.SetCursor(i)
		return nil
	}
	channel, ok := ml.chat.SelectedChannel()
	if !ok || channel.ID != target.ChannelID {
		return nil
	}
	return ml.chat.guildsTree.loadChannelAt(*channel, target.ID)
}

func writeClipboardText(text string) tview.Cmd {
	if text == "" {
		return nil
	}
	return func() tview.Msg {
		if _, err := clipboard.Write(context.Background(), clipboard.FmtText, []byte(text)); err != nil {
			slog.Error("failed to write to clipboard", "err", err)
		}
		return nil
	}
}

func (ml *messagesList) yankMessageID() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}
	return writeClipboardText(selectedMessage.ID.String())
}

func (ml *messagesList) yankContent() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}
	return writeClipboardText(messageCopyText(*selectedMessage))
}

func (ml *messagesList) yankURL() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}
	return writeClipboardText(selectedMessage.URL())
}

func messageCopyText(msg discord.Message) string {
	extra := extraCopyURLs(msg)
	if msg.Content == "" {
		return strings.Join(extra, "\n")
	}
	if len(extra) == 0 {
		return msg.Content
	}
	return msg.Content + "\n" + strings.Join(extra, "\n")
}

func extraCopyURLs(msg discord.Message) []string {
	var extra []string
	seen := make(map[string]struct{})
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		if strings.Contains(msg.Content, u) {
			return
		}
		seen[u] = struct{}{}
		extra = append(extra, u)
	}
	for _, u := range messageURLs(msg) {
		add(u)
	}
	for _, a := range msg.Attachments {
		add(a.URL)
	}
	return extra
}

const (
	messageMenuCopy         = "Copy"
	messageMenuCopyID       = "Copy ID"
	messageMenuMention      = "Mention"
	messageMenuReply        = "Reply"
	messageMenuReplyMention = "Reply (mention)"
	messageMenuEdit         = "Edit"
	messageMenuDelete       = "Delete"
	messageMenuOpen         = "Open"
)

func (ml *messagesList) messageMenuItems() []string {
	items := []string{messageMenuCopy, messageMenuCopyID, messageMenuMention}
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return items
	}

	if ml.chat.isMe(selectedMessage.Author.ID) {
		items = append(items, messageMenuEdit)
	} else {
		items = append(items, messageMenuReply, messageMenuReplyMention)
	}
	if ml.canDeleteMessage(*selectedMessage) {
		items = append(items, messageMenuDelete)
	}
	if len(messageURLs(*selectedMessage)) != 0 || len(selectedMessage.Attachments) != 0 {
		items = append(items, messageMenuOpen)
	}
	return items
}

func (ml *messagesList) showMessageMenu(x, y int) tview.Cmd {
	if _, ok := ml.selectedMessage(); !ok {
		return nil
	}
	return ml.chat.openMessageMenu(x, y, ml.messageMenuItems())
}

func (ml *messagesList) applyMessageMenu(choice string) tview.Cmd {
	switch choice {
	case messageMenuCopy:
		return ml.yankContent()
	case messageMenuCopyID:
		return ml.yankMessageID()
	case messageMenuMention:
		return ml.mentionAuthor()
	case messageMenuReply:
		return ml.reply(false)
	case messageMenuReplyMention:
		return ml.reply(true)
	case messageMenuEdit:
		return ml.editSelectedMessage()
	case messageMenuDelete:
		return ml.confirmDelete()
	case messageMenuOpen:
		return ml.open()
	default:
		return nil
	}
}

func (ml *messagesList) mentionAuthor() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok || ml.chat.composer.Disabled() {
		return nil
	}

	mention := "@" + selectedMessage.Author.Username
	text := ml.chat.composer.Text()
	if text != "" && !strings.HasSuffix(text, " ") && !strings.HasSuffix(text, "\n") {
		mention = " " + mention
	}
	ml.chat.composer.SetText(text+mention+" ", true)
	return tview.SetFocus(ml.chat.composer)
}

func (ml *messagesList) open() tview.Cmd {
	return ml.openWith(func(attachment discord.Attachment) tview.Cmd {
		if strings.HasPrefix(attachment.ContentType, "image/") {
			return ml.openAttachment(attachment)
		}
		return openURL(attachment.URL)
	})
}

func (ml *messagesList) openInBrowser() tview.Cmd {
	return ml.openWith(func(attachment discord.Attachment) tview.Cmd { return openURL(attachment.URL) })
}

func (ml *messagesList) openWith(openAttachment func(discord.Attachment) tview.Cmd) tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}

	urls := messageURLs(*selectedMessage)
	switch total := len(urls) + len(selectedMessage.Attachments); {
	case total == 0:
		return nil
	case total > 1:
		return ml.showAttachmentsList(urls, selectedMessage.Attachments, openAttachment)
	case len(urls) == 1:
		return openURL(urls[0])
	}

	return openAttachment(selectedMessage.Attachments[0])
}

func (ml *messagesList) download() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok || len(selectedMessage.Attachments) == 0 {
		return nil
	}
	if len(selectedMessage.Attachments) == 1 {
		attachment := selectedMessage.Attachments[0]
		return ml.confirmAttachment(attachment, saveAttachment(attachment))
	}

	items := make([]attachmentspicker.Item, len(selectedMessage.Attachments))
	for i, attachment := range selectedMessage.Attachments {
		items[i] = attachmentspicker.Item{Label: attachment.Filename, Action: ml.confirmAttachment(attachment, saveAttachment(attachment))}
	}
	return ml.showAttachmentsPicker(items)
}

func extractURLs(content string) []string {
	src := []byte(content)
	node := parser.NewParser(
		parser.WithBlockParsers(discordmd.BlockParsers()...),
		parser.WithInlineParsers(discordmd.InlineParserWithLink()...),
	).Parse(text.NewReader(src))
	return urlsFromAST(node, src)
}

// urlsFromAST collects link destinations from an already-parsed markdown AST.
// src must be the byte slice the node was parsed from (AutoLink resolves its
// URL against it).
func urlsFromAST(node ast.Node, src []byte) []string {
	var urls []string
	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch n := n.(type) {
			case *ast.AutoLink:
				urls = append(urls, string(n.URL(src)))
			case *ast.Link:
				urls = append(urls, string(n.Destination))
			}
		}

		return ast.WalkContinue, nil
	})
	return urls
}

func extractEmbedURLs(embeds []discord.Embed) []string {
	urls := make([]string, 0, len(embeds)*3)
	for _, embed := range embeds {
		if embed.URL != "" {
			urls = append(urls, embed.URL)
		}
		if embed.Image != nil && embed.Image.URL != "" {
			urls = append(urls, embed.Image.URL)
		}
		if embed.Video != nil && embed.Video.URL != "" {
			urls = append(urls, embed.Video.URL)
		}
	}
	return urls
}

func messageURLs(msg discord.Message) []string {
	combined := slices.Concat(extractURLs(msg.Content), extractEmbedURLs(msg.Embeds))

	urls := make([]string, 0, len(combined))
	seen := make(map[string]struct{}, len(combined))
	for _, u := range combined {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	return urls
}

func (ml *messagesList) showAttachmentsList(urls []string, attachments []discord.Attachment, openAttachment func(discord.Attachment) tview.Cmd) tview.Cmd {
	var items []attachmentspicker.Item
	for _, attachment := range attachments {
		items = append(items, attachmentspicker.Item{Label: attachment.Filename, Action: openAttachment(attachment)})
	}
	for _, url := range urls {
		items = append(items, attachmentspicker.Item{Label: url, Action: openURL(url)})
	}
	return ml.showAttachmentsPicker(items)
}

func (ml *messagesList) showAttachmentsPicker(items []attachmentspicker.Item) tview.Cmd {
	ml.attachmentsPicker.SetItems(items)

	ml.chat.
		AddLayer(
			ui.Centered(ml.attachmentsPicker, ml.cfg.Picker.Width, ml.cfg.Picker.Height),
			layers.WithName(attachmentsPickerLayerName),
			layers.WithResize(true),
			layers.WithVisible(true),
			layers.WithOverlay(),
		).
		SendToFront(attachmentsPickerLayerName)
	return tview.SetFocus(ml.attachmentsPicker)
}

func (ml *messagesList) openAttachment(attachment discord.Attachment) tview.Cmd {
	return ml.confirmAttachment(attachment, openDownloadedAttachment(attachment))
}

func (ml *messagesList) confirmAttachment(attachment discord.Attachment, action tview.Cmd) tview.Cmd {
	if !ml.cfg.AllowedMIMETypes.Has(attachment.ContentType) {
		return ui.ShowModal(
			"This attachment type is not allowed and may be unsafe. Continue anyway?",
			ui.ModalButton{Label: "No"},
			ui.ModalButton{Label: "Yes", Result: attachmentActionMsg{action}},
		)
	}
	return action
}

func openDownloadedAttachment(attachment discord.Attachment) tview.Cmd {
	return func() tview.Msg {
		extension := filepath.Ext(attachment.Filename)
		if extension == "" {
			mediaType, _, _ := mime.ParseMediaType(attachment.ContentType)
			if extensions, _ := mime.ExtensionsByType(mediaType); len(extensions) != 0 {
				extension = extensions[0]
			}
		}

		dir := filepath.Join(consts.CacheDir(), "attachments")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return attachmentErr("create attachments directory", err)
		}

		file, err := os.CreateTemp(dir, "attachment-*"+extension)
		if err != nil {
			return attachmentErr("create attachment file", err)
		}
		defer file.Close()

		path := file.Name()
		if err := downloadAttachment(attachment, path); err != nil {
			os.Remove(path)
			return attachmentErr("download attachment", err)
		} else if err := open.Start(path); err != nil {
			return attachmentErr("open attachment file", err)
		}
		return nil
	}
}

func attachmentErr(what string, err error) tview.Msg {
	slog.Error("failed to "+what, "err", err)
	return ui.ModalMsg{Text: "Failed to " + what + ": " + err.Error(), Buttons: []ui.ModalButton{{Label: "OK"}}}
}

func saveAttachment(attachment discord.Attachment) tview.Cmd {
	return func() tview.Msg {
		destination, err := zenity.SelectFileSave(zenity.Filename(filepath.Base(attachment.Filename)), zenity.ConfirmOverwrite())
		if errors.Is(err, zenity.ErrCanceled) {
			return nil
		}
		if err != nil {
			return attachmentErr("select attachment destination", err)
		}

		if err := downloadAttachment(attachment, destination); err != nil {
			return attachmentErr("download attachment", err)
		}
		return nil
	}
}

func downloadAttachment(attachment discord.Attachment, destination string) error {
	resp, err := http.Get(attachment.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func openURL(url string) tview.Cmd {
	return func() tview.Msg {
		if err := open.Start(url); err != nil {
			return attachmentErr("open URL", err)
		}
		return nil
	}
}

func (ml *messagesList) reply(mention bool) tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}

	name := selectedMessage.Author.DisplayOrUsername()

	data := ml.chat.composer.sendMessageData
	data.Reference = &discord.MessageReference{MessageID: selectedMessage.ID}
	data.AllowedMentions = &api.AllowedMentions{RepliedUser: option.Some(false)}

	title := "Replying to "
	if mention {
		data.AllowedMentions.RepliedUser = option.Some(true)
		title = "[@] " + title
	}

	ml.chat.composer.sendMessageData = data
	hint := ""
	if keys := ml.cfg.Keybinds.Composer.ToggleReplyMention.Keys(); len(keys) > 0 {
		hint = " (" + keys[0] + " to toggle @)"
	}
	ml.chat.composer.SetTitle(title + name + hint)
	return tview.SetFocus(ml.chat.composer)
}

func (ml *messagesList) editSelectedMessage() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}

	if !ml.chat.isMe(selectedMessage.Author.ID) {
		slog.Error("failed to edit message; not the author", "channel_id", selectedMessage.ChannelID, "message_id", selectedMessage.ID)
		return nil
	}

	ml.chat.composer.SetTitle("Editing")
	ml.chat.composer.edit = true
	ml.chat.composer.SetText(selectedMessage.Content, true)
	return tview.SetFocus(ml.chat.composer)
}

func (ml *messagesList) confirmDelete() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok || !ml.canDeleteMessage(*selectedMessage) {
		return nil
	}
	message := *selectedMessage
	return ui.ShowModal(
		"Are you sure you want to delete this message?",
		ui.ModalButton{Label: "Yes", Result: deleteMessageMsg(message)},
		ui.ModalButton{Label: "No"},
	)
}

func (ml *messagesList) deleteSelectedMessage() tview.Cmd {
	selectedMessage, ok := ml.selectedMessage()
	if !ok {
		return nil
	}
	return ml.deleteMessageRequest(*selectedMessage)
}

func (ml *messagesList) deleteMessageRequest(message discord.Message) tview.Cmd {
	return func() tview.Msg {
		if !ml.canDeleteMessage(message) {
			slog.Error("failed to delete message; missing relevant permissions", "channel_id", message.ChannelID, "message_id", message.ID)
			return nil
		}

		if err := ml.chat.state.DeleteMessage(message.ChannelID, message.ID, ""); err != nil {
			slog.Error("failed to delete message", "channel_id", message.ChannelID, "message_id", message.ID, "err", err)
			return nil
		}

		if err := ml.chat.state.MessageRemove(message.ChannelID, message.ID); err != nil {
			slog.Error("failed to delete message", "channel_id", message.ChannelID, "message_id", message.ID, "err", err)
			return nil
		}
		return nil
	}
}

func (ml *messagesList) canDeleteMessage(message discord.Message) bool {
	return ml.chat.isMe(message.Author.ID) ||
		(message.GuildID.IsValid() && ml.chat.state.HasPermissions(message.ChannelID, discord.PermissionManageMessages))
}

func (ml *messagesList) requestGuildMembers(guildID discord.GuildID, messages []discord.Message) tview.Cmd {
	usersToFetch := make([]discord.UserID, 0, len(messages))
	seen := make(map[discord.UserID]struct{}, len(messages))

	for _, message := range messages {
		// Do not fetch member for a webhook message.
		if message.WebhookID.IsValid() {
			continue
		}

		if member, _ := ml.chat.state.Cabinet.Member(guildID, message.Author.ID); member == nil {
			userID := message.Author.ID
			if _, ok := seen[userID]; !ok {
				seen[userID] = struct{}{}
				usersToFetch = append(usersToFetch, userID)
			}
		}
	}

	if len(usersToFetch) == 0 {
		return nil
	}

	return func() tview.Msg {
		if err := ml.chat.state.SendGateway(context.Background(), &gateway.RequestGuildMembersCommand{
			GuildIDs: []discord.GuildID{guildID},
			UserIDs:  usersToFetch,
		}); err != nil {
			slog.Error("failed to request guild members", "guild_id", guildID, "err", err)
		}
		return nil
	}
}

func (ml *messagesList) invalidateRenderedMessages() {
	clear(ml.itemByID)
	ml.SetBuilder(ml.buildItem)
}

func (ml *messagesList) ShortHelp() []keybind.Keybind {
	cfg := ml.cfg.Keybinds.MessagesList
	help := []keybind.Keybind{
		cfg.SelectUp.Keybind,
		cfg.SelectDown.Keybind,
		cfg.Cancel.Keybind,
	}

	if selectedMessage, ok := ml.selectedMessage(); ok {
		if !ml.chat.isMe(selectedMessage.Author.ID) {
			help = append(help, cfg.Reply.Keybind)
		}
	}

	return help
}

func (ml *messagesList) FullHelp() [][]keybind.Keybind {
	cfg := ml.cfg.Keybinds.MessagesList

	canSelectReply := false
	canReply := false
	canEdit := false
	canDelete := false
	canOpen := false
	canDownload := false
	if selectedMessage, ok := ml.selectedMessage(); ok {
		canSelectReply = selectedMessage.ReferencedMessage != nil
		canOpen = len(messageURLs(*selectedMessage)) != 0 || len(selectedMessage.Attachments) != 0
		canDownload = len(selectedMessage.Attachments) != 0

		canEdit = ml.chat.isMe(selectedMessage.Author.ID)
		canReply = !canEdit
		canDelete = ml.canDeleteMessage(*selectedMessage)
	}

	actions := make([]keybind.Keybind, 0, 4)
	if canReply {
		actions = append(actions, cfg.Reply.Keybind, cfg.ReplyMention.Keybind)
	}
	if canSelectReply {
		actions = append(actions, cfg.SelectReply.Keybind)
	}
	actions = append(actions, cfg.Cancel.Keybind)

	manage := make([]keybind.Keybind, 0, 4)
	if canEdit {
		manage = append(manage, cfg.Edit.Keybind)
	}
	if canDelete {
		manage = append(manage, cfg.DeleteConfirm.Keybind, cfg.Delete.Keybind)
	}
	if canOpen {
		manage = append(manage, cfg.Open.Keybind, cfg.OpenInBrowser.Keybind)
	}
	if canDownload {
		manage = append(manage, cfg.OpenWithApp.Keybind, cfg.Download.Keybind)
	}

	return [][]keybind.Keybind{
		{cfg.SelectUp.Keybind, cfg.SelectDown.Keybind, cfg.SelectTop.Keybind, cfg.SelectBottom.Keybind},
		{cfg.ScrollUp.Keybind, cfg.ScrollDown.Keybind, cfg.ScrollTop.Keybind, cfg.ScrollBottom.Keybind},
		actions,
		manage,
		{cfg.YankContent.Keybind, cfg.YankURL.Keybind, cfg.YankID.Keybind},
	}
}
