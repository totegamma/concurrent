package worker

import "context"

// LeaderState reports whether this replica currently holds cluster
// leadership.
type LeaderState interface {
	IsLeader() bool
}

// SubscriberManager is the Subscriber the rest of the application talks to.
// It delegates every call to LeaderSubscriber (which actually dials upstream
// websockets) while this replica leads, and to WorkerSubscriber (which only
// relays to the leader over HTTP) otherwise. In standalone mode elector is an
// AlwaysLeader, so every call goes to LeaderSubscriber.
type SubscriberManager struct {
	elector LeaderState
	leader  *LeaderSubscriber
	worker  *WorkerSubscriber
}

func NewSubscriberManager(elector LeaderState, leader *LeaderSubscriber, worker *WorkerSubscriber) *SubscriberManager {
	return &SubscriberManager{elector: elector, leader: leader, worker: worker}
}

func (m *SubscriberManager) CurrentSubscriptions() []string {
	if m.elector.IsLeader() {
		return m.leader.CurrentSubscriptions()
	}
	return m.worker.CurrentSubscriptions()
}

func (m *SubscriberManager) EnsureSubscriptions(ctx context.Context, prefixes []string) {
	if m.elector.IsLeader() {
		m.leader.EnsureSubscriptions(ctx, prefixes)
		return
	}
	m.worker.EnsureSubscriptions(ctx, prefixes)
}
