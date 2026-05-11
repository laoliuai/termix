package relayclient

import (
	"context"
	"encoding/json"
	"errors"
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

type Client struct {
	url             string
	accessToken     string
	deviceID        string
	conn            *websocket.Conn
	mu              sync.Mutex
	snapshotHandler func(context.Context, string) ([]byte, error)
	inputHandler    func(context.Context, string, []byte) error
	resizeHandler   func(context.Context, string, uint32, uint32) error

	done          chan error
	closeOnce     sync.Once
	closeFnOnce   sync.Once
	closeErr      error
}

func New(url string, accessToken string, deviceID string) *Client {
	return &Client{
		url:         url,
		accessToken: accessToken,
		deviceID:    deviceID,
		done:        make(chan error, 1),
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
	// by the relay into snapshot.req). Resize the tmux pane before
	// capture-pane so the snapshot reflects the viewer's actual viewport;
	// otherwise the snapshot is taken at the previous pane size and the
	// resize-driven redraw stacks on top of a wrongly-sized snapshot.
	if req.Cols > 0 && req.Rows > 0 && c.resizeHandler != nil {
		_ = c.resizeHandler(ctx, req.SessionID, req.Cols, req.Rows)
	}

	snapshot, err := c.snapshotHandler(ctx, req.SessionID)
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
	_ = c.resizeHandler(ctx, p.SessionID, p.Cols, p.Rows)
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
