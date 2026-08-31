package ui

import (
	"sync"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/ningen/v3"
)

// For user accounts the gateway only sends joined threads in READY and
// GuildCreate, which is what ningen's ThreadIsJoined tracks. Threads that
// arrive later through ThreadCreate or ThreadListSync also land in the channel
// cabinet, so membership has to be tracked to tell them apart.
//
// ThreadListSync carries its own member list for joined threads; ningen
// ignores that event, so those threads are tracked here.
type threadMembershipState struct {
	mu     sync.RWMutex
	userID discord.UserID
	joined map[discord.ChannelID]struct{}
}

var threadMembership = newThreadMembershipState()

func newThreadMembershipState() *threadMembershipState {
	return &threadMembershipState{joined: make(map[discord.ChannelID]struct{})}
}

// TrackThreadMembership records thread joins that ningen's thread state misses.
func TrackThreadMembership(state *ningen.State) {
	s := threadMembership
	state.State.AddSyncHandler(func(ev *gateway.ReadyEvent) { s.onReady(ev) })
	state.State.AddSyncHandler(func(ev *gateway.ThreadListSyncEvent) { s.onThreadListSync(ev) })
	state.State.AddSyncHandler(func(ev *gateway.ThreadMemberUpdateEvent) { s.onThreadMemberUpdate(ev) })
	state.State.AddSyncHandler(func(ev *gateway.ThreadMembersUpdateEvent) { s.onThreadMembersUpdate(ev) })
	state.State.AddSyncHandler(func(ev *gateway.ThreadDeleteEvent) { s.remove(ev.ID) })
}

// ThreadIsSubscribed reports whether the current user has joined the thread.
func ThreadIsSubscribed(state *ningen.State, id discord.ChannelID) bool {
	if state != nil && state.ThreadState.ThreadIsJoined(id) {
		return true
	}
	return threadMembership.has(id)
}

func (s *threadMembershipState) has(id discord.ChannelID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.joined[id]
	return ok
}

func (s *threadMembershipState) add(id discord.ChannelID) {
	if !id.IsValid() {
		return
	}
	s.mu.Lock()
	s.joined[id] = struct{}{}
	s.mu.Unlock()
}

func (s *threadMembershipState) remove(id discord.ChannelID) {
	if !id.IsValid() {
		return
	}
	s.mu.Lock()
	delete(s.joined, id)
	s.mu.Unlock()
}

func (s *threadMembershipState) onReady(ev *gateway.ReadyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.userID = ev.User.ID
	clear(s.joined)
}

func (s *threadMembershipState) onThreadListSync(ev *gateway.ThreadListSyncEvent) {
	for _, member := range ev.Members {
		s.add(member.ID)
	}
}

func (s *threadMembershipState) onThreadMemberUpdate(ev *gateway.ThreadMemberUpdateEvent) {
	s.mu.RLock()
	userID := s.userID
	s.mu.RUnlock()
	if ev.UserID.IsValid() && userID.IsValid() && ev.UserID != userID {
		return
	}
	s.add(ev.ID)
}

func (s *threadMembershipState) onThreadMembersUpdate(ev *gateway.ThreadMembersUpdateEvent) {
	s.mu.RLock()
	userID := s.userID
	s.mu.RUnlock()

	for _, member := range ev.AddedMembers {
		if !userID.IsValid() || member.UserID == userID {
			s.add(ev.ID)
			return
		}
	}
	for _, id := range ev.RemovedMemberIDs {
		if id == userID {
			s.remove(ev.ID)
			return
		}
	}
}
