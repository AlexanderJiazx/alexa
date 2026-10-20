// Package hub fans out per-user realtime events to connected clients.
package hub

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type sub struct {
	ch      chan []byte
	visible atomic.Bool
}

type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[*sub]struct{}
}

func New() *Hub { return &Hub{subs: map[int64]map[*sub]struct{}{}} }

// Subscribe registers a client. It returns the client's events, a function
// to report whether the client is on screen (it starts out visible), and
// one to unsubscribe.
func (h *Hub) Subscribe(userID int64) (<-chan []byte, func(visible bool), func()) {
	s := &sub{ch: make(chan []byte, 256)}
	s.visible.Store(true)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[*sub]struct{}{}
	}
	h.subs[userID][s] = struct{}{}
	h.mu.Unlock()
	return s.ch, s.visible.Store, func() {
		h.mu.Lock()
		if _, ok := h.subs[userID][s]; ok {
			delete(h.subs[userID], s)
			close(s.ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(userID int64, ev Event) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[userID] {
		select {
		case s.ch <- b:
		default: // drop for slow clients; they resync on reconnect
		}
	}
}

// Online reports whether the user has Alexa open in front of them on some
// device; if not, what Alexa says reaches them as a notification.
func (h *Hub) Online(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[userID] {
		if s.visible.Load() {
			return true
		}
	}
	return false
}
