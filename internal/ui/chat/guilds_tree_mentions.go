package chat

import (
	"strconv"
	"strings"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/ningen/v3"
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/tree"
	"github.com/gdamore/tcell/v3"
)

// visibleNodes lists the nodes the tree draws, top to bottom.
func (gt *guildsTree) visibleNodes() []*tree.Node {
	var nodes []*tree.Node
	var walk func(*tree.Node)
	walk = func(node *tree.Node) {
		for _, child := range node.Children() {
			nodes = append(nodes, child)
			if child.Expanded() {
				walk(child)
			}
		}
	}
	walk(gt.Root())
	return nodes
}

// nodeMentioned reports whether the node is a channel with a mention, or a
// collapsed node that hides one.
func (gt *guildsTree) nodeMentioned(node *tree.Node) bool {
	switch ref := node.Reference().(type) {
	case discord.ChannelID:
		if gt.state.ChannelIsUnread(ref, ningen.UnreadOpts{IncludeMutedCategories: true}) == ningen.ChannelMentioned {
			return true
		}
	case discord.GuildID:
		if !node.Expanded() {
			return gt.state.GuildIsUnread(ref, ningen.GuildUnreadOpts{IncludeMutedCategories: true}) == ningen.ChannelMentioned
		}
	}
	if node.Expanded() {
		return false
	}
	for _, child := range node.Children() {
		if gt.nodeMentioned(child) {
			return true
		}
	}
	return false
}

// offscreenMentions counts mentioned rows above and below the window
// [offset, offset+height) and returns the nearest one on each side, or -1.
func offscreenMentions(mentioned []bool, offset, height int) (above, below, nearestAbove, nearestBelow int) {
	nearestAbove, nearestBelow = -1, -1
	for i, m := range mentioned {
		switch {
		case !m:
		case i < offset:
			above++
			nearestAbove = i
		case i >= offset+height:
			below++
			if nearestBelow < 0 {
				nearestBelow = i
			}
		}
	}
	return
}

// mentionBar is an arrow bar drawn over a tree row. Clicking it jumps to target.
type mentionBar struct {
	y      int
	target *tree.Node
}

// jumpToMentionBar selects the target of the bar drawn at row y, centering it.
func (gt *guildsTree) jumpToMentionBar(y int) bool {
	for _, bar := range []mentionBar{gt.topBar, gt.bottomBar} {
		if bar.target != nil && bar.y == y {
			gt.SetCenterCursor(true)
			gt.SetCurrentNode(bar.target)
			gt.SetCenterCursor(false)
			return true
		}
	}
	return false
}

// drawMentionIndicators covers the first or last visible row with a white
// arrow bar when a mention sits outside the scrolled area.
func (gt *guildsTree) drawMentionIndicators(screen tcell.Screen) {
	gt.topBar, gt.bottomBar = mentionBar{}, mentionBar{}
	if gt.state == nil {
		return
	}
	nodes := gt.visibleNodes()
	x, y, width, height := gt.InnerRect()
	if height < 3 || width < 3 || len(nodes) <= height {
		return
	}
	mentioned := make([]bool, len(nodes))
	for i, node := range nodes {
		mentioned[i] = gt.nodeMentioned(node)
	}
	offset := min(max(gt.GetScrollOffset(), 0), len(nodes)-height)
	above, below, nearestAbove, nearestBelow := offscreenMentions(mentioned, offset, height)

	current := gt.CurrentNode()
	if above > 0 && nodes[offset] != current {
		drawMentionBar(screen, x, y, width, "▲", above)
		gt.topBar = mentionBar{y: y, target: nodes[nearestAbove]}
	}
	if below > 0 && nodes[offset+height-1] != current {
		drawMentionBar(screen, x, y+height-1, width, "▼", below)
		gt.bottomBar = mentionBar{y: y + height - 1, target: nodes[nearestBelow]}
	}
}

func drawMentionBar(screen tcell.Screen, x, y, width int, arrow string, count int) {
	style := tcell.StyleDefault.Background(tcell.ColorWhite).Foreground(tcell.ColorBlack).Bold(true)
	label := " " + arrow + " "
	if count > 1 {
		label += strconv.Itoa(count) + " "
	}
	for i := range width {
		screen.Put(x+i, y, " ", style)
	}
	col := x + max((width-len([]rune(label)))/2, 0)
	for _, r := range strings.Split(label, "") {
		if col >= x+width {
			break
		}
		screen.Put(col, y, r, style)
		col++
	}
}

// mentionCount sums the mentions in a node and everything under it.
func (gt *guildsTree) mentionCount(node *tree.Node) int {
	count := 0
	switch ref := node.Reference().(type) {
	case discord.GuildID:
		// Channels of a guild are loaded lazily, so ask the state instead.
		channels, err := gt.state.Cabinet.Channels(ref)
		if err != nil {
			return 0
		}
		for _, channel := range channels {
			count += gt.channelMentionCount(channel.ID)
		}
		return count
	case discord.ChannelID:
		count = gt.channelMentionCount(ref)
	}
	for _, child := range node.Children() {
		count += gt.mentionCount(child)
	}
	return count
}

func (gt *guildsTree) channelMentionCount(id discord.ChannelID) int {
	if readState := gt.state.ReadState.ReadState(id); readState != nil {
		return int(readState.MentionCount)
	}
	return 0
}

// refreshMentionBadges puts an "N mentions" label in front of the name of
// every collapsed node that hides mentions, and removes stale ones.
func (gt *guildsTree) refreshMentionBadges() {
	if gt.state == nil {
		return
	}
	for _, node := range gt.visibleNodes() {
		text := ""
		if (node.Expandable() || len(node.Children()) > 0) && !node.Expanded() {
			text = mentionBadgeText(gt.mentionCount(node))
		}
		gt.setMentionBadge(node, text)
	}
}

func mentionBadgeText(count int) string {
	switch {
	case count <= 0:
		return ""
	case count == 1:
		return "1 mention "
	default:
		return strconv.Itoa(count) + " mentions "
	}
}

// setMentionBadge shows text as a first segment of the node's line, replaces
// it, or removes it when text is empty.
func (gt *guildsTree) setMentionBadge(node *tree.Node, text string) {
	old, has := gt.badges[node]
	line := node.Line()
	if has && (len(line) == 0 || line[0].Text != old) {
		// The line was replaced since the badge was added.
		delete(gt.badges, node)
		has = false
	}
	switch {
	case has && text == "":
		delete(gt.badges, node)
		node.SetLine(line[1:])
	case has && text != old:
		line[0].Text = text
		gt.badges[node] = text
		node.SetLine(line)
	case !has && text != "" && len(line) > 0:
		badge := line[0]
		badge.Text = text
		gt.badges[node] = text
		node.SetLine(append(tview.Line{badge}, line...))
	}
}
