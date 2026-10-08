package chat

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/ui"
	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/help"
	"github.com/ayn2op/tview/keybind"
	"github.com/ayn2op/tview/tree"
)

const (
	favoritesFileName = "favorites.json"
	// favoriteMarker goes in front of favorite channels in the guilds tree.
	favoriteMarker = "★ "

	channelMenuFavorite   = "Favorite channel"
	channelMenuUnfavorite = "Unfavorite channel"
)

// toggleFavoriteMsg adds the channel to favorites or removes it.
type toggleFavoriteMsg struct{ ChannelID discord.ChannelID }

// channelMenuMsg asks for the channel context menu at the mouse position.
type channelMenuMsg struct {
	Owner     tview.Model
	ChannelID discord.ChannelID
	X, Y      int
}

type favoritesFile struct {
	ChannelIDs []discord.ChannelID `json:"channel_ids"`
}

// favoritesTree is the pane of favorite channels pinned above the guilds
// tree. It scrolls on its own and shares unread styles with the guilds tree.
type favoritesTree struct {
	*tree.Model

	cfg *config.Config
	gt  *guildsTree

	path     string
	ids      []discord.ChannelID
	nodeByID map[discord.ChannelID]*tree.Node
}

func newFavoritesTree(cfg *config.Config, gt *guildsTree) *favoritesTree {
	ft := &favoritesTree{
		Model:    tree.NewModel(),
		cfg:      cfg,
		gt:       gt,
		path:     filepath.Join(config.Dir(), favoritesFileName),
		nodeByID: make(map[discord.ChannelID]*tree.Node),
	}
	ui.ConfigureBox(ft.Box, &cfg.Theme)
	ft.
		SetRoot(tree.NewNode("")).
		SetTopLevel(1).
		SetMarkers(tree.Markers{Leaf: cfg.Sidebar.Markers.Leaf}).
		SetGraphics(false).
		SetCenterCursor(false).
		SetTitle("Favorites")
	ft.SetKeybinds(tree.Keybinds{
		Up:     cfg.Keybinds.GuildsTree.SelectUp.Keybind,
		Down:   cfg.Keybinds.GuildsTree.SelectDown.Keybind,
		Top:    cfg.Keybinds.GuildsTree.SelectTop.Keybind,
		Bottom: cfg.Keybinds.GuildsTree.SelectBottom.Keybind,
		Select: cfg.Keybinds.GuildsTree.SelectCurrent.Keybind,
	})
	gt.favorites = ft
	ft.load()
	return ft
}

func (ft *favoritesTree) load() {
	data, err := os.ReadFile(ft.path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		slog.Error("failed to read favorites", "err", err, "path", ft.path)
		return
	}
	var file favoritesFile
	if err := json.Unmarshal(data, &file); err != nil {
		slog.Error("failed to parse favorites", "err", err, "path", ft.path)
		return
	}
	ft.ids = file.ChannelIDs
}

func (ft *favoritesTree) save() {
	data, err := json.MarshalIndent(favoritesFile{ChannelIDs: ft.ids}, "", "  ")
	if err != nil {
		slog.Error("failed to encode favorites", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(ft.path), os.ModePerm); err != nil {
		slog.Error("failed to create favorites dir", "err", err, "path", ft.path)
		return
	}
	if err := os.WriteFile(ft.path, data, 0o644); err != nil {
		slog.Error("failed to write favorites", "err", err, "path", ft.path)
	}
}

func (ft *favoritesTree) has(id discord.ChannelID) bool {
	return slices.Contains(ft.ids, id)
}

// toggle adds or removes the channel and reports whether it is now a favorite.
func (ft *favoritesTree) toggle(id discord.ChannelID) bool {
	if i := slices.Index(ft.ids, id); i >= 0 {
		ft.ids = slices.Delete(ft.ids, i, i+1)
	} else {
		ft.ids = append(ft.ids, id)
	}
	ft.save()
	ft.rebuild()
	return ft.has(id)
}

// rebuild recreates the rows from the saved IDs. Channels missing from the
// state (another account, left guilds) stay saved but are not shown.
func (ft *favoritesTree) rebuild() {
	current := ft.CurrentNode()
	var currentID discord.ChannelID
	if current != nil {
		currentID, _ = current.Reference().(discord.ChannelID)
	}

	clear(ft.nodeByID)
	root := ft.Root().ClearChildren()
	if ft.gt.state == nil {
		return
	}
	for _, id := range ft.ids {
		channel, err := ft.gt.state.Cabinet.Channel(id)
		if err != nil {
			continue
		}
		node := tree.NewNode(ft.label(*channel)).SetReference(id).SetIndent(0)
		ft.gt.setNodeLineStyle(node, ft.gt.channelNodeStyle(*channel))
		root.AddChild(node)
		ft.nodeByID[id] = node
	}

	if node := ft.nodeByID[currentID]; node != nil {
		ft.SetCurrentNode(node)
	} else if children := root.Children(); len(children) > 0 {
		ft.SetCurrentNode(children[0])
	} else {
		ft.SetCurrentNode(nil)
	}
}

func (ft *favoritesTree) label(channel discord.Channel) string {
	label := ui.ChannelToString(channel, ft.cfg.Icons, ft.gt.state)
	if channel.GuildID.IsValid() {
		if guild, err := ft.gt.state.Cabinet.Guild(channel.GuildID); err == nil {
			label += " (" + guild.Name + ")"
		}
	}
	return label
}

// refreshStyle mirrors the unread state of the channel in the guilds tree.
func (ft *favoritesTree) refreshStyle(id discord.ChannelID) {
	node := ft.nodeByID[id]
	if node == nil {
		return
	}
	channel, err := ft.gt.state.Cabinet.Channel(id)
	if err != nil {
		return
	}
	ft.gt.setNodeLineStyle(node, ft.gt.channelNodeStyle(*channel))
}

func (ft *favoritesTree) empty() bool {
	return len(ft.nodeByID) == 0
}

// height is the pane height that fits every row up to the configured limit.
func (ft *favoritesTree) height() int {
	rows := min(len(ft.Root().Children()), ft.cfg.Sidebar.FavoritesHeight)
	padding := ft.cfg.Theme.Border.Padding
	height := rows + padding[0] + padding[1]
	if ft.cfg.Theme.Border.Enabled {
		height += 2
	}
	return height
}

func (ft *favoritesTree) Update(msg tview.Msg) tview.Cmd {
	ui.UpdateBoxFocus(ft.Box, &ft.cfg.Theme, msg)
	switch msg := msg.(type) {
	case tview.FocusMsg:
		return tview.Sequence(ft.Model.Update(msg), focused(ft))
	case tree.SelectedMsg:
		id, ok := msg.Node.Reference().(discord.ChannelID)
		if !ok {
			return nil
		}
		channel, err := ft.gt.state.Cabinet.Channel(id)
		if err != nil {
			slog.Error("failed to get channel from state", "err", err, "channel_id", id)
			return nil
		}
		return ft.gt.loadChannel(*channel)
	case tview.MouseMsg:
		x, y := msg.Position()
		if msg.Action == tview.MouseRightClick && ft.InRect(x, y) {
			return channelMenuAt(ft, ft.Model, ft.Root().Children(), x, y)
		}
	case tview.KeyMsg:
		switch {
		case keybind.Matches(msg, ft.cfg.Keybinds.GuildsTree.ToggleFavorite.Keybind):
			return toggleCurrentFavorite(ft.CurrentNode())
		case keybind.Matches(msg, ft.cfg.Keybinds.GuildsTree.YankID.Keybind):
			return ft.gt.yankNodeID(ft.CurrentNode())
		}
	}
	return ft.Model.Update(msg)
}

var _ help.KeyMap = (*favoritesTree)(nil)

func (ft *favoritesTree) ShortHelp() []keybind.Keybind {
	cfg := ft.cfg.Keybinds.GuildsTree
	return []keybind.Keybind{cfg.SelectUp.Keybind, cfg.SelectDown.Keybind, cfg.SelectCurrent.Keybind, cfg.ToggleFavorite.Keybind}
}

func (ft *favoritesTree) FullHelp() [][]keybind.Keybind {
	cfg := ft.cfg.Keybinds.GuildsTree
	return [][]keybind.Keybind{
		{cfg.SelectUp.Keybind, cfg.SelectDown.Keybind, cfg.SelectTop.Keybind, cfg.SelectBottom.Keybind},
		{cfg.SelectCurrent.Keybind, cfg.ToggleFavorite.Keybind, cfg.YankID.Keybind},
	}
}

// favoritable reports whether the channel holds messages and so can be
// opened from the favorites pane.
func favoritable(channel discord.Channel) bool {
	return channel.Type != discord.GuildCategory && channel.Type != discord.GuildForum
}

// nodeAtRow returns the node drawn at screen row y of a tree whose drawn
// rows are nodes.
func nodeAtRow(t *tree.Model, nodes []*tree.Node, y int) *tree.Node {
	_, top, _, height := t.InnerRect()
	offset := min(max(t.GetScrollOffset(), 0), max(len(nodes)-height, 0))
	if index := offset + y - top; index >= 0 && index < len(nodes) {
		return nodes[index]
	}
	return nil
}

// channelMenuAt selects the channel row under the mouse and asks for the
// channel context menu.
func channelMenuAt(owner tview.Model, t *tree.Model, nodes []*tree.Node, x, y int) tview.Cmd {
	node := nodeAtRow(t, nodes, y)
	if node == nil {
		return nil
	}
	id, ok := node.Reference().(discord.ChannelID)
	if !ok {
		return nil
	}
	t.SetCurrentNode(node)
	return func() tview.Msg { return channelMenuMsg{Owner: owner, ChannelID: id, X: x, Y: y} }
}

func toggleCurrentFavorite(node *tree.Node) tview.Cmd {
	if node == nil {
		return nil
	}
	id, ok := node.Reference().(discord.ChannelID)
	if !ok {
		return nil
	}
	return func() tview.Msg { return toggleFavoriteMsg{ChannelID: id} }
}

// setFavoriteMarker adds or removes the star in front of the channel name.
func (gt *guildsTree) setFavoriteMarker(id discord.ChannelID, favorite bool) {
	node := gt.channelNodeByID[id]
	if node == nil {
		return
	}
	line := node.Line()
	index := 0
	if _, ok := gt.badges[node]; ok {
		index = 1
	}
	if index >= len(line) {
		return
	}
	text := strings.TrimPrefix(line[index].Text, favoriteMarker)
	if favorite {
		text = favoriteMarker + text
	}
	line[index].Text = text
	node.SetLine(line)
}

func (gt *guildsTree) isFavorite(id discord.ChannelID) bool {
	return gt.favorites != nil && gt.favorites.has(id)
}
