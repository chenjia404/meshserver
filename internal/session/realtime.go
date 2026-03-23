package session

import (
	"context"

	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
)

// IsChannelMemberForWS reports whether the user may subscribe to channel push (same as stream SUBSCRIBE).
func (m *Manager) IsChannelMemberForWS(ctx context.Context, userID uint64, channelID uint32) (bool, error) {
	return m.channels.IsUserMember(ctx, channelID, userID)
}

// RealtimeSubscriber receives MESSAGE_EVENT-sized payloads for a subscribed channel (e.g. WebSocket).
type RealtimeSubscriber interface {
	RealtimeDeliver(channelID uint32, event *sessionv1.MessageEvent)
}

// RegisterRealtimeSubscriber adds a subscriber for push fan-out (WebSocket, etc.).
func (m *Manager) RegisterRealtimeSubscriber(channelID uint32, sub RealtimeSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.realtimeSubs == nil {
		m.realtimeSubs = make(map[uint32]map[RealtimeSubscriber]struct{})
	}
	if _, ok := m.realtimeSubs[channelID]; !ok {
		m.realtimeSubs[channelID] = make(map[RealtimeSubscriber]struct{})
	}
	m.realtimeSubs[channelID][sub] = struct{}{}
}

// UnregisterRealtimeSubscriber removes one subscriber from a channel.
func (m *Manager) UnregisterRealtimeSubscriber(channelID uint32, sub RealtimeSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.realtimeSubs == nil {
		return
	}
	delete(m.realtimeSubs[channelID], sub)
	if len(m.realtimeSubs[channelID]) == 0 {
		delete(m.realtimeSubs, channelID)
	}
}

// UnregisterRealtimeSubscriberAll removes a subscriber from every channel (e.g. WebSocket disconnect).
func (m *Manager) UnregisterRealtimeSubscriberAll(sub RealtimeSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.realtimeSubs == nil {
		return
	}
	for chID, subs := range m.realtimeSubs {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(m.realtimeSubs, chID)
		}
	}
}
