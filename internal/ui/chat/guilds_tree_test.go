package chat

import (
	"path/filepath"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/tree"
	"github.com/gdamore/tcell/v3"
)

func TestClickingVisibleChannelKeepsGuildsScroll(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}

	gt := newGuildsTree(cfg, nil)
	gt.SetRect(0, 0, 30, 12)
	for range 30 {
		gt.Root().AddChild(tree.NewNode("channel"))
	}

	click := tview.MouseMsg{
		EventMouse: tcell.NewEventMouse(2, 1, tcell.ButtonNone, tcell.ModNone),
		Action:     tview.MouseScrollDown,
	}
	for range 5 {
		gt.Update(click)
	}
	offset := gt.GetScrollOffset()
	if offset == 0 {
		t.Fatal("tree did not scroll before the click")
	}

	click.Action = tview.MouseLeftClick
	gt.Update(click)
	if gt.GetScrollOffset() != offset {
		t.Fatalf("scroll offset = %d, want %d", gt.GetScrollOffset(), offset)
	}
}

func TestMoveDMToFront(t *testing.T) {
	root := tree.NewNode("dms")
	a := tree.NewNode("a")
	b := tree.NewNode("b")
	c := tree.NewNode("c")
	root.AddChild(a)
	root.AddChild(b)
	root.AddChild(c)

	id := discord.ChannelID(3)
	gt := &guildsTree{
		dmRootNode:      root,
		channelNodeByID: map[discord.ChannelID]*tree.Node{id: c},
	}
	gt.moveDMToFront(id)

	children := root.Children()
	if len(children) != 3 || children[0] != c || children[1] != a || children[2] != b {
		t.Fatalf("order = %v", children)
	}
}

func TestOffscreenMentions(t *testing.T) {
	mentioned := []bool{true, false, true, false, false, true, false, true}
	above, below, nearAbove, nearBelow := offscreenMentions(mentioned, 3, 3)
	if above != 2 || below != 1 || nearAbove != 2 || nearBelow != 7 {
		t.Fatalf("got %d, %d, %d, %d; want 2, 1, 2, 7", above, below, nearAbove, nearBelow)
	}
	above, below, nearAbove, nearBelow = offscreenMentions(mentioned, 0, 8)
	if above != 0 || below != 0 || nearAbove != -1 || nearBelow != -1 {
		t.Fatalf("all visible: got %d, %d, %d, %d; want 0, 0, -1, -1", above, below, nearAbove, nearBelow)
	}
}

func TestJumpToMentionBar(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	gt := newGuildsTree(cfg, nil)
	gt.SetRect(0, 0, 30, 12)
	var nodes []*tree.Node
	for range 40 {
		node := tree.NewNode("channel")
		gt.Root().AddChild(node)
		nodes = append(nodes, node)
	}
	gt.topBar = mentionBar{y: 1, target: nodes[20]}
	if gt.jumpToMentionBar(5) {
		t.Fatal("click off the bar jumped")
	}
	if !gt.jumpToMentionBar(1) || gt.CurrentNode() != nodes[20] {
		t.Fatal("click on the bar did not select the target")
	}
	if off := gt.GetScrollOffset(); off > 20 || off+10 <= 20 {
		t.Fatalf("target not visible after jump: offset %d", off)
	}
}

func TestMentionBadge(t *testing.T) {
	gt := &guildsTree{badges: make(map[*tree.Node]string)}
	node := tree.NewNode("Guild")
	name := func() string {
		var s string
		for _, seg := range node.Line() {
			s += seg.Text
		}
		return s
	}

	gt.setMentionBadge(node, mentionBadgeText(3))
	if got := name(); got != "3 mentions Guild" {
		t.Fatalf("line = %q", got)
	}
	gt.setMentionBadge(node, mentionBadgeText(1))
	if got := name(); got != "1 mention Guild" {
		t.Fatalf("line = %q", got)
	}
	gt.setMentionBadge(node, "")
	if got := name(); got != "Guild" {
		t.Fatalf("line = %q", got)
	}
	gt.setMentionBadge(node, "")
	if got := name(); got != "Guild" {
		t.Fatalf("removing twice changed the line: %q", got)
	}
}
