package chat

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/tview/tree"
)

func newTestFavorites(t *testing.T) (*favoritesTree, *guildsTree) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	gt := newGuildsTree(cfg, nil)
	ft := newFavoritesTree(cfg, gt)
	ft.path = filepath.Join(t.TempDir(), favoritesFileName)
	ft.ids = nil
	return ft, gt
}

func TestFavoritesToggleSavesAndLoads(t *testing.T) {
	ft, _ := newTestFavorites(t)

	if !ft.toggle(1) || !ft.toggle(2) {
		t.Fatal("toggle did not add")
	}
	if ft.toggle(1) {
		t.Fatal("toggle did not remove")
	}

	loaded := &favoritesTree{path: ft.path}
	loaded.load()
	if !slices.Equal(loaded.ids, []discord.ChannelID{2}) {
		t.Fatalf("loaded ids = %v", loaded.ids)
	}
}

func TestSetFavoriteMarker(t *testing.T) {
	_, gt := newTestFavorites(t)
	node := tree.NewNode("#general")
	gt.channelNodeByID[5] = node

	gt.setFavoriteMarker(5, true)
	if got := node.Line()[0].Text; got != favoriteMarker+"#general" {
		t.Fatalf("marked text = %q", got)
	}
	gt.setFavoriteMarker(5, true)
	if got := node.Line()[0].Text; got != favoriteMarker+"#general" {
		t.Fatalf("marker added twice: %q", got)
	}
	gt.setFavoriteMarker(5, false)
	if got := node.Line()[0].Text; got != "#general" {
		t.Fatalf("unmarked text = %q", got)
	}
}

func TestFavoritesHeightCapsRows(t *testing.T) {
	ft, _ := newTestFavorites(t)
	ft.cfg.Sidebar.FavoritesHeight = 2
	for i := range 5 {
		ft.Root().AddChild(tree.NewNode("").SetReference(discord.ChannelID(i + 1)))
	}
	padding := ft.cfg.Theme.Border.Padding
	want := 2 + padding[0] + padding[1]
	if ft.cfg.Theme.Border.Enabled {
		want += 2
	}
	if got := ft.height(); got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
}

func TestNodeAtRow(t *testing.T) {
	m := tree.NewModel()
	m.SetRect(0, 0, 10, 3)
	nodes := []*tree.Node{tree.NewNode("a"), tree.NewNode("b")}
	_, top, _, _ := m.InnerRect()
	if nodeAtRow(m, nodes, top+1) != nodes[1] {
		t.Fatal("wrong node at second row")
	}
	if nodeAtRow(m, nodes, top+2) != nil {
		t.Fatal("empty row returned a node")
	}
}
