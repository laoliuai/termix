package relay

import (
	"context"
	"sync"

	"github.com/coder/websocket"
)

type peer struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func newPeer(conn *websocket.Conn) *peer {
	return &peer{conn: conn}
}

func (p *peer) write(ctx context.Context, msgType websocket.MessageType, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.Write(ctx, msgType, data)
}

type registry struct {
	mu       sync.RWMutex
	daemons  map[string]*peer
	watchers map[string]map[*peer]struct{}
}

func newRegistry() *registry {
	return &registry{
		daemons:  make(map[string]*peer),
		watchers: make(map[string]map[*peer]struct{}),
	}
}

func (r *registry) setDaemon(sessionID string, p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.daemons[sessionID] = p
}

func (r *registry) daemon(sessionID string) *peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.daemons[sessionID]
}

func (r *registry) addWatcher(sessionID string, p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.watchers[sessionID] == nil {
		r.watchers[sessionID] = make(map[*peer]struct{})
	}
	r.watchers[sessionID][p] = struct{}{}
}

func (r *registry) watchersFor(sessionID string) []*peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	watchers := r.watchers[sessionID]
	if len(watchers) == 0 {
		return nil
	}

	result := make([]*peer, 0, len(watchers))
	for watcher := range watchers {
		result = append(result, watcher)
	}
	return result
}

func (r *registry) removePeer(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sessionID, daemon := range r.daemons {
		if daemon == p {
			delete(r.daemons, sessionID)
		}
	}
	for sessionID, watchers := range r.watchers {
		delete(watchers, p)
		if len(watchers) == 0 {
			delete(r.watchers, sessionID)
		}
	}
}
