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
