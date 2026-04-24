package relay

import (
	"context"
	"sync"
	"time"

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
	mu          sync.RWMutex
	daemons     map[string]*peer
	watchers    map[string]map[*peer]struct{}
	controllers map[string]controllerState
	watching    map[*peer]map[string]struct{}
}

type controllerState struct {
	peer         *peer
	sessionID    string
	leaseVersion int64
	expiresAt    time.Time
}

func newRegistry() *registry {
	return &registry{
		daemons:     make(map[string]*peer),
		watchers:    make(map[string]map[*peer]struct{}),
		controllers: make(map[string]controllerState),
		watching:    make(map[*peer]map[string]struct{}),
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
	if r.watching[p] == nil {
		r.watching[p] = make(map[string]struct{})
	}
	r.watching[p][sessionID] = struct{}{}
}

func (r *registry) isWatching(sessionID string, p *peer) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.watching[p][sessionID]
	return ok
}

func (r *registry) setController(sessionID string, p *peer, grant ControlGrant) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controllers[sessionID] = controllerState{
		peer:         p,
		sessionID:    sessionID,
		leaseVersion: grant.LeaseVersion,
		expiresAt:    grant.ExpiresAt,
	}
}

func (r *registry) clearController(sessionID string, p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.controllers[sessionID]
	if !ok || state.peer != p {
		return
	}
	delete(r.controllers, sessionID)
}

func (r *registry) controller(sessionID string) (controllerState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.controllers[sessionID]
	return state, ok
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
	for sessionID := range r.watching[p] {
		watchers := r.watchers[sessionID]
		delete(watchers, p)
		if len(watchers) == 0 {
			delete(r.watchers, sessionID)
		}
	}
	delete(r.watching, p)
	for sessionID, state := range r.controllers {
		if state.peer == p {
			delete(r.controllers, sessionID)
		}
	}
}
