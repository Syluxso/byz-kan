package main

import (
	"sync"
	"time"
)

// Event is one board mutation broadcast to live subscribers.
// Payload carries type-specific detail (e.g. the target stateId on ticket.moved).
type Event struct {
	Type     string         `json:"type"`
	BoardID  string         `json:"boardId"`
	TicketID string         `json:"ticketId,omitempty"`
	ActorID  string         `json:"actorId,omitempty"`
	At       time.Time      `json:"at"`
	Payload  map[string]any `json:"payload,omitempty"`
}

// subKey scopes a subscription. Tenant is part of the key, so a board id alone
// can never deliver events across a tenant boundary.
type subKey struct {
	orgID    string
	tenantID string
	boardID  string
}

// subscriber is one connected SSE client.
type subscriber struct {
	ch chan Event
}

// Hub fans board events out to connected SSE clients.
//
// In-process only. Correct for the current single-process deploy; if byz-kan is
// ever scaled horizontally each instance would see only its own writes, and this
// must move to Postgres LISTEN/NOTIFY. See CW-13.
type Hub struct {
	mu   sync.RWMutex
	subs map[subKey]map[*subscriber]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[subKey]map[*subscriber]struct{})}
}

// subBuffer is how many events a subscriber may fall behind before it is
// considered slow and its events are dropped.
const subBuffer = 32

// Subscribe registers a client for one board's events. The returned cancel
// function unregisters it and closes the channel; callers must always call it.
func (h *Hub) Subscribe(orgID, tenantID, boardID string) (<-chan Event, func()) {
	k := subKey{orgID: orgID, tenantID: tenantID, boardID: boardID}
	sub := &subscriber{ch: make(chan Event, subBuffer)}

	h.mu.Lock()
	if h.subs[k] == nil {
		h.subs[k] = make(map[*subscriber]struct{})
	}
	h.subs[k][sub] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if m, ok := h.subs[k]; ok {
				delete(m, sub)
				if len(m) == 0 {
					delete(h.subs, k)
				}
			}
			h.mu.Unlock()
			close(sub.ch)
		})
	}
	return sub.ch, cancel
}

// Publish delivers ev to every subscriber of that board within the tenant.
//
// Never blocks: a subscriber whose buffer is full has this event dropped rather
// than stalling the caller, because Publish runs on the request path of a DB
// mutation. A wedged browser tab must not be able to hold up a write.
func (h *Hub) Publish(orgID, tenantID string, ev Event) {
	if h == nil || ev.BoardID == "" {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	k := subKey{orgID: orgID, tenantID: tenantID, boardID: ev.BoardID}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs[k] {
		select {
		case sub.ch <- ev:
		default: // slow consumer; drop
		}
	}
}

// subscriberCount reports live subscribers for a board. Test/diagnostic helper.
func (h *Hub) subscriberCount(orgID, tenantID, boardID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[subKey{orgID: orgID, tenantID: tenantID, boardID: boardID}])
}
