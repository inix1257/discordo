//go:build live

package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/discordo/internal/keyring"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

// TestFavoritesLive drives the real chat UI with the saved account on a
// virtual terminal and writes screen dumps. It never opens a channel, so no
// channel is marked as read.
//
//	go test -tags live -run TestFavoritesLive -v ./internal/ui/chat
func TestFavoritesLive(t *testing.T) {
	token, err := keyring.GetToken()
	if err != nil {
		t.Skip("no saved token:", err)
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mouse = true

	const width, height = 110, 40
	screen, err := tcell.NewTerminfoScreenFromTty(vt.NewMockTerm(vt.MockOptSize{X: width, Y: height}))
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.EnableMouse()

	m := NewModel(cfg, token)
	m.favorites.path = filepath.Join(t.TempDir(), favoritesFileName)
	m.favorites.ids = nil
	tview.Styles = tview.Theme{}
	app := tview.NewApplication(m, tview.WithScreen(screen))
	go app.Run()
	defer func() { _ = closeState(m.state)() }()

	outDir := os.Getenv("DUMP_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}
	step := 0
	dump := func(name string) string {
		time.Sleep(700 * time.Millisecond)
		var b strings.Builder
		for y := range height {
			for x := 0; x < width; {
				str, style, w := screen.Get(x, y)
				if str == "" {
					str = " "
				}
				// Mark rows drawn black on white (mentions, menus) with a gutter.
				_ = style
				b.WriteString(str)
				x += max(w, 1)
			}
			b.WriteString("\n")
		}
		step++
		text := b.String()
		path := filepath.Join(outDir, strings.ReplaceAll(name, " ", "_")+".txt")
		_ = os.WriteFile(path, []byte(text), 0o644)
		t.Logf("--- %d. %s ---\n%s", step, name, text)
		return text
	}
	key := func(k tcell.Key, s string) {
		screen.EventQ() <- tcell.NewEventKey(k, s, tcell.ModNone)
		time.Sleep(150 * time.Millisecond)
	}
	rightClick := func(x, y int) {
		screen.EventQ() <- tcell.NewEventMouse(x, y, tcell.ButtonSecondary, tcell.ModNone)
		time.Sleep(50 * time.Millisecond)
		screen.EventQ() <- tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)
		time.Sleep(300 * time.Millisecond)
	}
	// rowOf finds the first screen row in the sidebar whose text contains s.
	rowOf := func(text, s string, from int) (int, int) {
		for y, line := range strings.Split(text, "\n") {
			if y < from {
				continue
			}
			runes := []rune(line)
			side := string(runes[:min(len(runes), width*cfg.Sidebar.WidthPercent/100)])
			if i := strings.Index(side, s); i >= 0 {
				return len([]rune(side[:i])) + 1, y
			}
		}
		return -1, -1
	}

	deadline := time.Now().Add(30 * time.Second)
	var text string
	for {
		text = dump("ready")
		if strings.Contains(text, "Direct Messages") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("READY did not arrive")
		}
	}

	// Expand the first guild below Direct Messages with the keyboard.
	key(tcell.KeyRune, "j")
	key(tcell.KeyEnter, "")
	text = dump("guild expanded")

	x, y := rowOf(text, cfg.Icons.GuildText, 2)
	if y < 0 {
		t.Fatal("no text channel row found")
	}
	rightClick(x, y)
	text = dump("menu on channel")
	if !strings.Contains(text, channelMenuFavorite) {
		t.Fatal("favorite menu did not open")
	}

	key(tcell.KeyEnter, "")
	text = dump("after favorite")
	if !strings.Contains(text, "Favorites") || !strings.Contains(text, strings.TrimSpace(favoriteMarker)) {
		t.Fatal("favorites pane or star missing")
	}

	// Unfavorite from the favorites pane itself.
	x, y = rowOf(text, cfg.Icons.GuildText, 1)
	rightClick(x, y)
	text = dump("menu on favorite")
	if !strings.Contains(text, channelMenuUnfavorite) {
		t.Fatal("unfavorite menu did not open")
	}
	key(tcell.KeyEnter, "")
	text = dump("after unfavorite")
	if strings.Contains(text, "Favorites") {
		t.Fatal("favorites pane still shown")
	}
}
