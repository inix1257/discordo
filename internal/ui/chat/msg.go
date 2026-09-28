package chat

import (
	"context"
	"log/slog"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/ningen/v3"
	"github.com/ayn2op/tview"
)

func openState(state *ningen.State) tview.Cmd {
	return func() tview.Msg {
		if err := state.Open(context.Background()); err != nil {
			slog.Error("failed to open chat state", "err", err)
			return nil
		}
		return nil
	}
}

func closeState(state *ningen.State) tview.Cmd {
	if state == nil {
		return nil
	}
	return func() tview.Msg {
		if err := state.Close(); err != nil {
			slog.Error("failed to close the session", "err", err)
		}
		return nil
	}
}

func listen(events <-chan gateway.Event) tview.Cmd {
	return func() tview.Msg {
		return <-events
	}
}

type channelLoadedMsg struct {
	Channel  discord.Channel
	Messages []discord.Message
}

type olderMessagesLoadedMsg struct {
	ChannelID discord.ChannelID
	// Before is the oldest message ID that was loaded when the request started.
	Before discord.MessageID
	Older  []discord.Message
	// Exhausted reports that Discord has no messages older than Older.
	Exhausted bool
	// FromScroll reports that mouse-wheel scrolling triggered the request.
	FromScroll bool
}

type deleteMessageMsg discord.Message

type attachmentActionMsg struct {
	Action tview.Cmd
}

type LogoutMsg struct{}

func logout() tview.Cmd {
	return func() tview.Msg {
		return LogoutMsg{}
	}
}

type QuitMsg struct{}

type toggleGuildsTreeMsg struct{}

func toggleGuildsTree() tview.Cmd {
	return func() tview.Msg { return toggleGuildsTreeMsg{} }
}

type FocusedMsg struct{ Model tview.Model }

func focused(model tview.Model) tview.Cmd { return func() tview.Msg { return FocusedMsg{model} } }
