package ui

import (
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
)

func TestThreadMembershipTracksThreadListSync(t *testing.T) {
	s := newThreadMembershipState()
	s.onReady(&gateway.ReadyEvent{User: discord.User{ID: 42}})

	joinedID := discord.ChannelID(1)
	unjoinedID := discord.ChannelID(2)
	s.onThreadListSync(&gateway.ThreadListSyncEvent{
		Threads: []discord.Channel{
			{ID: joinedID, Type: discord.GuildPublicThread},
			{ID: unjoinedID, Type: discord.GuildPublicThread},
		},
		Members: []discord.ThreadMember{{ID: joinedID, UserID: 42}},
	})

	if !s.has(joinedID) {
		t.Fatal("thread listed in ThreadListSync members should be tracked")
	}
	if s.has(unjoinedID) {
		t.Fatal("synced thread without membership should not be tracked")
	}
}

func TestThreadMembershipJoinAndLeave(t *testing.T) {
	s := newThreadMembershipState()
	s.onReady(&gateway.ReadyEvent{User: discord.User{ID: 42}})

	id := discord.ChannelID(7)
	s.onThreadMembersUpdate(&gateway.ThreadMembersUpdateEvent{
		ID:           id,
		AddedMembers: []discord.ThreadMember{{ID: id, UserID: 42}},
	})
	if !s.has(id) {
		t.Fatal("joined thread should be tracked")
	}

	s.onThreadMembersUpdate(&gateway.ThreadMembersUpdateEvent{
		ID:               id,
		RemovedMemberIDs: []discord.UserID{42},
	})
	if s.has(id) {
		t.Fatal("left thread should no longer be tracked")
	}
}

func TestThreadMembershipIgnoresOtherUsers(t *testing.T) {
	s := newThreadMembershipState()
	s.onReady(&gateway.ReadyEvent{User: discord.User{ID: 42}})

	id := discord.ChannelID(8)
	s.onThreadMembersUpdate(&gateway.ThreadMembersUpdateEvent{
		ID:           id,
		AddedMembers: []discord.ThreadMember{{ID: id, UserID: 99}},
	})
	if s.has(id) {
		t.Fatal("another user joining should not mark the thread as ours")
	}
}

func TestThreadMembershipResetOnReady(t *testing.T) {
	s := newThreadMembershipState()
	s.onReady(&gateway.ReadyEvent{User: discord.User{ID: 42}})

	id := discord.ChannelID(3)
	s.add(id)
	s.onReady(&gateway.ReadyEvent{User: discord.User{ID: 42}})
	if s.has(id) {
		t.Fatal("reconnect should clear stale membership")
	}
}
