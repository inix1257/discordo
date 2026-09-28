package root

import (
	"path/filepath"
	"testing"

	"github.com/ayn2op/discordo/internal/config"
	"github.com/gdamore/tcell/v3"
)

func TestHelpBarNotShown(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Help.Enabled = true
	m := NewModel(cfg)

	if count := m.rootFlex.GetItemCount(); count != 0 {
		t.Fatalf("layout item count = %d, want 0", count)
	}

	m.Update(tcell.NewEventKey(tcell.KeyRune, ".", tcell.ModCtrl))
	m.Update(tcell.NewEventKey(tcell.KeyRune, ".", tcell.ModAlt))
	if count := m.rootFlex.GetItemCount(); count != 0 {
		t.Fatalf("layout item count = %d, want 0", count)
	}
}
