package chat

import (
	"path/filepath"
	"testing"

	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/tview"
	"github.com/rivo/uniseg"
)

func TestGuildsToggleBounds(t *testing.T) {
	box := tview.NewBox()
	box.SetRect(2, 3, 30, 12)

	x, y, w, ok := guildsToggleBounds(box, false)
	wantW := uniseg.StringWidth(guildsCollapseGlyph)
	if !ok || y != 3 || w != wantW || x != 2+30-1-w {
		t.Fatalf("expanded bounds = %d,%d,%d ok=%v", x, y, w, ok)
	}
	if !hitGuildsToggle(box, false, x, y) || hitGuildsToggle(box, false, x-1, y) {
		t.Fatal("expanded hit box")
	}

	x, y, w, ok = guildsToggleBounds(box, true)
	wantW = uniseg.StringWidth(guildsExpandGlyph)
	if !ok || x != 3 || y != 3 || w != wantW {
		t.Fatalf("collapsed bounds = %d,%d,%d ok=%v", x, y, w, ok)
	}
	if !hitGuildsToggle(box, true, x, y) || hitGuildsToggle(box, true, x+w, y) {
		t.Fatal("collapsed hit box")
	}
}

func TestToggleGuildsTreeLayout(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(cfg, "token")
	if m.guildsCollapsed || m.mainFlex.GetItemCount() != 2 || m.mainFlex.GetItem(0) != m.sidebar {
		t.Fatalf("initial collapsed=%v count=%d", m.guildsCollapsed, m.mainFlex.GetItemCount())
	}

	m.Update(toggleGuildsTreeMsg{})
	if !m.guildsCollapsed || m.mainFlex.GetItemCount() != 1 || m.mainFlex.GetItem(0) != m.rightFlex {
		t.Fatal("guilds pane still expanded")
	}

	m.Update(toggleGuildsTreeMsg{})
	if m.guildsCollapsed || m.mainFlex.GetItemCount() != 2 || m.mainFlex.GetItem(0) != m.sidebar {
		t.Fatal("guilds pane did not expand")
	}
}
