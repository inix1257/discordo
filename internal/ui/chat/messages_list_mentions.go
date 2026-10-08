package chat

import (
	"slices"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/tview"
	"github.com/gdamore/tcell/v3"
)

// trackedItem reports to the list which message rows were drawn in the
// current frame, so mentions outside the viewport can be told apart.
type trackedItem struct {
	*tview.TextView
	ml           *messagesList
	messageIndex int
}

func (t *trackedItem) View(screen tcell.Screen) {
	t.TextView.View(screen)
	t.ml.noteDrawn(t.messageIndex)
}

func (ml *messagesList) noteDrawn(messageIndex int) {
	if ml.firstDrawn < 0 || messageIndex < ml.firstDrawn {
		ml.firstDrawn = messageIndex
	}
	ml.lastDrawn = max(ml.lastDrawn, messageIndex)
}

// mentionedIndexes lists the loaded messages that mention the user directly,
// through @everyone, or through one of the user's roles.
func (ml *messagesList) mentionedIndexes() []int {
	state := ml.chat.state
	me, _ := state.Cabinet.Me()
	if me == nil {
		return nil
	}
	var roles []discord.RoleID
	if channel, ok := ml.chat.SelectedChannel(); ok && channel.GuildID.IsValid() {
		if member, err := state.Cabinet.Member(channel.GuildID, me.ID); err == nil {
			roles = member.RoleIDs
		}
	}

	var indexes []int
	for i, message := range ml.messages {
		if mentionsUser(message, me.ID, roles) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func mentionsUser(message discord.Message, me discord.UserID, roles []discord.RoleID) bool {
	if message.Author.ID == me {
		return false
	}
	if message.MentionEveryone {
		return true
	}
	for _, user := range message.Mentions {
		if user.ID == me {
			return true
		}
	}
	for _, role := range message.MentionRoleIDs {
		if slices.Contains(roles, role) {
			return true
		}
	}
	return false
}

// drawMentionIndicators covers the first or last row of the list with a white
// arrow bar while a mention sits above or below the drawn messages.
func (ml *messagesList) drawMentionIndicators(screen tcell.Screen) {
	ml.topBar, ml.bottomBar = -1, -1
	if ml.firstDrawn < 0 {
		return
	}
	x, y, width, height := ml.InnerRect()
	if height < 3 || width < 3 {
		return
	}

	var above, below int
	for _, i := range ml.mentionedIndexes() {
		switch {
		case i < ml.firstDrawn:
			above++
			ml.topBar = i
		case i > ml.lastDrawn:
			below++
			if ml.bottomBar < 0 {
				ml.bottomBar = i
			}
		}
	}
	if above > 0 {
		drawMentionBar(screen, x, y, width, "▲", above)
	}
	if below > 0 {
		drawMentionBar(screen, x, y+height-1, width, "▼", below)
	}
}

// jumpToMentionBar selects the nearest off-screen mention when y is the row
// of a drawn bar.
func (ml *messagesList) jumpToMentionBar(y int) bool {
	_, innerY, _, height := ml.InnerRect()
	target := -1
	switch {
	case ml.topBar >= 0 && y == innerY:
		target = ml.topBar
	case ml.bottomBar >= 0 && y == innerY+height-1:
		target = ml.bottomBar
	}
	if target < 0 {
		return false
	}
	ml.SetCursor(target)
	return true
}
