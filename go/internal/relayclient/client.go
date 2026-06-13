package relayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/session"
)

// noProxyHTTPClient is the dialer used for the relay WSS handshake. It
// is the *only* HTTP transport in termix that explicitly bypasses the
// user's HTTPS_PROXY / HTTP_PROXY env vars, because the relay
// connection is a single long-lived stream and HTTP-style proxies
// (mihomo / clash / corporate gateways) typically idle-timeout
// long-lived tunnels — surfacing as `broken pipe` spam in the daemon
// log. Every other endpoint (login, refresh, heartbeat, doctor) uses
// the normal http.DefaultClient and honors the user's shell env, which
// is what corporate-proxy users need to reach api.termix.cloud at all.
var noProxyHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil, // explicit: never honor HTTP(S)_PROXY for this dial
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// defaultPaneRedrawDelay is the wait inserted between a tmux resize-window
// and the subsequent capture-pane (or any other readback). resize-window
// updates the pane cell array immediately, but the TUI inside only redraws
// after SIGWINCH lands and its render loop ticks. Without a small grace
// window, capture-pane returns "new size + old layout" and the SPA renders
// a misaligned snapshot until the user's next keystroke triggers a redraw.
// 120ms covers a couple of React/Ink render frames with margin. With the
// poll-until-stable capture below it is now only the *floor* (first settle)
// before re-capturing, so a slow TUI no longer ships a half-painted frame.
const defaultPaneRedrawDelay = 120 * time.Millisecond

// After the floor wait, capture-pane is repeated every defaultPaneRedrawPoll
// until two consecutive captures are byte-identical (the TUI has finished
// repainting) or defaultPaneRedrawMaxPolls attempts elapse — whichever comes
// first. This adapts to fast and slow TUIs instead of trusting one fixed delay:
// a fast repaint settles in one extra capture; a slow one keeps polling up to
// floor + maxPolls*poll (≈120ms + 6*30ms = 300ms) before giving up.
const (
	defaultPaneRedrawPoll     = 30 * time.Millisecond
	defaultPaneRedrawMaxPolls = 6
)

type Client struct {
	url                string
	accessToken        string
	deviceID           string
	conn               *websocket.Conn
	mu                 sync.Mutex
	snapshotHandler    func(context.Context, string) ([]byte, error)
	inputHandler       func(context.Context, string, []byte) error
	resizeHandler      func(context.Context, string, uint32, uint32) error
	paneRedrawDelay    time.Duration
	paneRedrawPoll     time.Duration
	paneRedrawMaxPolls int

	done        chan error
	closeOnce   sync.Once
	closeFnOnce sync.Once
	closeErr    error
}

func New(url string, accessToken string, deviceID string) *Client {
	return &Client{
		url:                url,
		accessToken:        accessToken,
		deviceID:           deviceID,
		paneRedrawDelay:    defaultPaneRedrawDelay,
		paneRedrawPoll:     defaultPaneRedrawPoll,
		paneRedrawMaxPolls: defaultPaneRedrawMaxPolls,
		done:               make(chan error, 1),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.accessToken}},
		HTTPClient: noProxyHTTPClient,
	})
	if err != nil {
		return err
	}
	c.conn = conn

	if err := c.writeEnvelope(ctx, HelloDaemonEnvelope(c.deviceID)); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "hello failed")
		return err
	}
	go c.readLoop(ctx)
	return nil
}

func (c *Client) AnnounceSession(ctx context.Context, s session.LocalSession) error {
	return c.writeEnvelope(ctx, relayproto.Envelope{
		Type: relayproto.TypeSessionOnline,
		Payload: map[string]any{
			"session_id": s.SessionID,
		},
	})
}

func (c *Client) PublishSnapshot(ctx context.Context, sessionID string, snapshot []byte) error {
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": sessionID,
			"seq":        1,
			"is_last":    true,
		},
		Payload: snapshot,
	})
	if err != nil {
		return err
	}
	return c.writeBinary(ctx, frame)
}

func (c *Client) PublishOutput(ctx context.Context, sessionID string, payload []byte) error {
	// stream is required by spec §17.4 and the terminal-web decoder rejects
	// frames without it. tmux pipe-pane gives us the merged pane display, so
	// "stdout" is the appropriate default.
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalOutput,
		Header: map[string]any{
			"session_id": sessionID,
			"seq":        1,
			"stream":     "stdout",
		},
		Payload: payload,
	})
	if err != nil {
		return err
	}
	return c.writeBinary(ctx, frame)
}

func (c *Client) SetSnapshotHandler(fn func(context.Context, string) ([]byte, error)) {
	c.snapshotHandler = fn
}

func (c *Client) SetInputHandler(fn func(context.Context, string, []byte) error) {
	c.inputHandler = fn
}

func (c *Client) SetResizeHandler(fn func(context.Context, string, uint32, uint32) error) {
	c.resizeHandler = fn
}

// SetPaneRedrawDelay overrides the floor wait inserted between a tmux resize
// and the first follow-up capture-pane. Tests use 0 to keep runs quick;
// production keeps the default 120ms.
func (c *Client) SetPaneRedrawDelay(d time.Duration) {
	c.paneRedrawDelay = d
}

// SetPaneRedrawPolling overrides the post-floor re-capture interval and the max
// number of re-capture attempts used by captureStable. Tests pass interval 0 to
// avoid real sleeps; production keeps the 30ms / 6-attempt defaults.
func (c *Client) SetPaneRedrawPolling(interval time.Duration, maxPolls int) {
	c.paneRedrawPoll = interval
	c.paneRedrawMaxPolls = maxPolls
}

// sleepFor blocks for d or until ctx is cancelled. Safe with a zero/negative d
// (returns immediately).
func (c *Client) sleepFor(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// captureStable resizes-then-captures with a poll-until-stable loop: it waits
// the floor (paneRedrawDelay), captures, then re-captures every paneRedrawPoll
// until two consecutive captures are byte-identical (the TUI's SIGWINCH repaint
// has settled) or paneRedrawMaxPolls attempts elapse. This replaces trusting a
// single fixed delay — a slow TUI no longer ships a half-painted frame, and a
// fast one settles immediately. Best-effort: a capture error returns the last
// good frame rather than failing the whole snapshot.
func (c *Client) captureStable(ctx context.Context, sessionID string) ([]byte, error) {
	c.sleepFor(ctx, c.paneRedrawDelay)
	prev, err := c.snapshotHandler(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for i := 0; i < c.paneRedrawMaxPolls; i++ {
		if ctx.Err() != nil {
			return prev, nil
		}
		c.sleepFor(ctx, c.paneRedrawPoll)
		cur, err := c.snapshotHandler(ctx, sessionID)
		if err != nil {
			return prev, nil
		}
		if bytes.Equal(cur, prev) {
			return cur, nil
		}
		prev = cur
	}
	return prev, nil
}

func (c *Client) readLoop(ctx context.Context) {
	var loopErr error
	defer func() {
		c.closeOnce.Do(func() {
			if loopErr != nil {
				c.done <- loopErr
			}
			close(c.done)
		})
	}()

	for {
		msgType, data, err := c.conn.Read(ctx)
		if err != nil {
			loopErr = err
			return
		}
		if msgType == websocket.MessageBinary {
			c.handleInputFrame(ctx, data)
			continue
		}
		if msgType != websocket.MessageText {
			continue
		}

		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			loopErr = err
			return
		}
		switch env.Type {
		case relayproto.TypeSessionSnapshotReq:
			c.handleSnapshotRequest(ctx, env)
		case relayproto.TypeClientResize:
			c.handleResizeRequest(ctx, env)
		}
	}
}

func (c *Client) handleInputFrame(ctx context.Context, data []byte) {
	if c.inputHandler == nil {
		return
	}

	frame, err := relayproto.DecodeBinaryFrame(data)
	if err != nil {
		return
	}
	if frame.FrameType != relayproto.FrameTypeTerminalInput {
		return
	}
	if relayproto.HeaderString(frame.Header, "encoding") != "raw" {
		return
	}

	sessionID := relayproto.HeaderString(frame.Header, "session_id")
	if sessionID == "" {
		return
	}
	_ = c.inputHandler(ctx, sessionID, frame.Payload)
}

func (c *Client) handleSnapshotRequest(ctx context.Context, env relayproto.Envelope) {
	if c.snapshotHandler == nil {
		return
	}

	raw, err := json.Marshal(env.Payload)
	if err != nil {
		return
	}
	var req SnapshotRequestPayload
	if err := json.Unmarshal(raw, &req); err != nil {
		return
	}
	if req.SessionID == "" {
		return
	}

	// The viewer can attach an initial grid hint to session.watch (forwarded
	// by the relay into snapshot.req). Resize the tmux pane before capture-pane
	// so the snapshot reflects the viewer's actual viewport; otherwise the
	// snapshot is taken at the previous pane size and the resize-driven redraw
	// stacks on top of a wrongly-sized snapshot. `resize-window` only updates
	// the cell array — the TUI's repaint runs asynchronously after SIGWINCH, so
	// captureStable polls capture-pane until the redraw settles before publishing.
	var snapshot []byte
	if req.Cols > 0 && req.Rows > 0 && c.resizeHandler != nil {
		_ = c.resizeHandler(ctx, req.SessionID, req.Cols, req.Rows)
		snapshot, err = c.captureStable(ctx, req.SessionID)
	} else {
		// No resize was requested → the pane is already settled, so a single
		// capture is correct (and avoids the poll-until-stable latency).
		snapshot, err = c.snapshotHandler(ctx, req.SessionID)
	}
	if err != nil {
		return
	}
	_ = c.writeEnvelope(ctx, relayproto.Envelope{
		Type:    relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{"session_id": req.SessionID},
	})
	_ = c.PublishSnapshot(ctx, req.SessionID, snapshot)
}

func (c *Client) handleResizeRequest(ctx context.Context, env relayproto.Envelope) {
	if c.resizeHandler == nil {
		return
	}
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		return
	}
	var p ResizeRequestPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	if p.SessionID == "" || p.Cols == 0 || p.Rows == 0 {
		return
	}
	if err := c.resizeHandler(ctx, p.SessionID, p.Cols, p.Rows); err != nil {
		return
	}
	// DEBUG mode only: the SPA piggybacks its observed viewport geometry so we
	// can correlate the client's view with the pane size we just applied. Nil
	// (no log) on normal resizes — zero effect when DEBUG is off.
	if p.Debug != nil {
		log.Printf("client.resize debug: session=%s applied=%dx%d client=%v", p.SessionID, p.Cols, p.Rows, p.Debug)
	}

	// A SPA-driven resize (composer-dock toggle, browser window change)
	// changes the tmux pane size. Without a follow-up snapshot the SPA's
	// xterm keeps the pre-resize layout stacked under the live redraw,
	// leaving the cursor at the wrong row. Mirror the snapshot.req tail
	// (wait → capture → ready → publish) so the SPA's snapshot.ready
	// handler resets xterm and writes the new state.
	if c.snapshotHandler == nil {
		return
	}
	snapshot, err := c.captureStable(ctx, p.SessionID)
	if err != nil {
		return
	}
	_ = c.writeEnvelope(ctx, relayproto.Envelope{
		Type:    relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{"session_id": p.SessionID},
	})
	_ = c.PublishSnapshot(ctx, p.SessionID, snapshot)
}

func (c *Client) writeEnvelope(ctx context.Context, env relayproto.Envelope) error {
	data, err := relayproto.EncodeEnvelope(env)
	if err != nil {
		return err
	}
	return c.writeText(ctx, data)
}

func (c *Client) writeText(ctx context.Context, data []byte) error {
	if c.conn == nil {
		return errors.New("relay client is not connected")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) writeBinary(ctx context.Context, data []byte) error {
	if c.conn == nil {
		return errors.New("relay client is not connected")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageBinary, data)
}

// Done returns a channel that is closed when the client's read loop exits.
// A non-nil error is delivered before the close if the exit was caused by
// a read or decode error; a nil delivery indicates a normal close (e.g.
// after Close() was called).
func (c *Client) Done() <-chan error {
	return c.done
}

// Close gracefully terminates the WSS connection and the read loop.
// Idempotent; the second call returns nil without error.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	c.closeFnOnce.Do(func() {
		c.closeErr = c.conn.Close(websocket.StatusNormalClosure, "client closing")
	})
	return c.closeErr
}
