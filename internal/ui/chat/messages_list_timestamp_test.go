package chat

import (
	"testing"
	"time"

	"github.com/ayn2op/arikawa/v3/discord"
)

func TestShowTimestampGroupsByMinuteAndAuthor(t *testing.T) {
	base := time.Date(2026, 9, 22, 14, 4, 10, 0, time.Local)
	ml := &messagesList{messages: []discord.Message{
		{ID: 1, Author: discord.User{ID: 10}, Timestamp: discord.NewTimestamp(base)},
		{ID: 2, Author: discord.User{ID: 10}, Timestamp: discord.NewTimestamp(base.Add(40 * time.Second))},
		{ID: 3, Author: discord.User{ID: 10}, Timestamp: discord.NewTimestamp(base.Add(time.Minute))},
		{ID: 4, Author: discord.User{ID: 20}, Timestamp: discord.NewTimestamp(base.Add(time.Minute + time.Second))},
		{ID: 5, Author: discord.User{ID: 30}, Timestamp: discord.NewTimestamp(base.Add(time.Hour))},
	}}

	if !ml.showTimestamp(ml.messages[0]) || ml.showTimestamp(ml.messages[1]) || ml.showTimestamp(ml.messages[2]) {
		t.Fatal("a consecutive author should keep only the first timestamp")
	}
	if ml.showTimestamp(ml.messages[3]) {
		t.Fatal("a different author in the same minute should stay hidden")
	}
	if !ml.showTimestamp(ml.messages[4]) {
		t.Fatal("a different author in a new minute should show the timestamp")
	}
}
