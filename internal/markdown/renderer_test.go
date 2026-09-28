package markdown

import (
	"strings"
	"testing"

	"github.com/ayn2op/discordo/internal/config"
	"github.com/ayn2op/ningen/v3/discordmd"
	"github.com/gdamore/tcell/v3"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

func TestRendererShowsFileLinkName(t *testing.T) {
	raw := "https://cdn.discordapp.com/attachments/1/2/shot.png?ex=abc&hm=def"
	page := "https://example.com/posts/hello"
	for _, test := range []struct {
		source string
		want   string
		url    string
	}{
		{raw, "shot.png", raw},
		{page, page, page},
		{"see " + raw, "see shot.png", raw},
	} {
		source := []byte(test.source)
		node := parser.NewParser(
			parser.WithBlockParsers(discordmd.BlockParsers()...),
			parser.WithInlineParsers(discordmd.InlineParserWithLink()...),
		).Parse(text.NewReader(source))
		lines := NewRenderer(&config.Config{}).RenderLines(source, node, tcell.StyleDefault)

		var got strings.Builder
		var link string
		for _, line := range lines {
			for _, segment := range line {
				got.WriteString(segment.Text)
				if _, u := segment.Style.GetUrl(); u != "" {
					link = u
				}
			}
		}
		if got.String() != test.want || link != test.url {
			t.Fatalf("source %q: got %q link %q, want %q link %q", test.source, got.String(), link, test.want, test.url)
		}
	}
}

func TestRendererMasksSpoilers(t *testing.T) {
	source := []byte("before ||secret|| after")
	node := parser.NewParser(
		parser.WithBlockParsers(discordmd.BlockParsers()...),
		parser.WithInlineParsers(discordmd.InlineParserWithLink()...),
	).Parse(text.NewReader(source))

	for _, test := range []struct {
		mask bool
		want string
	}{
		{true, "before [spoiler] after"},
		{false, "before secret after"},
	} {
		lines := NewRenderer(&config.Config{Markdown: config.MarkdownConfig{MaskSpoilers: test.mask}}).RenderLines(source, node, tcell.StyleDefault)
		var got strings.Builder
		for _, line := range lines {
			for _, segment := range line {
				got.WriteString(segment.Text)
			}
		}
		if got.String() != test.want {
			t.Fatalf("mask=%t: got %q, want %q", test.mask, got.String(), test.want)
		}
	}
}
