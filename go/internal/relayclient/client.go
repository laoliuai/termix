package relayclient

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/session"
)

type Client struct {
	url             string
	accessToken     string
	deviceID        string
	conn            *websocket.Conn
	mu              sync.Mutex
	snapshotHandler func(context.Context, string) ([]byte, error)
}

func New(url string, accessToken string, deviceID string) *Client {
	return &Client{
		url:         url,
		accessToken: accessToken,
		deviceID:    deviceID,
	}
}

func (c *Client) Connect(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.accessToken}},
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
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalOutput,
		Header: map[string]any{
			"session_id": sessionID,
			"seq":        1,
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

func (c *Client) readLoop(ctx context.Context) {
	for {
		msgType, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageText {
			continue
		}

		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			return
		}
		if env.Type == relayproto.TypeSessionSnapshotReq {
			c.handleSnapshotRequest(ctx, env)
		}
	}
}

func (c *Client) handleSnapshotRequest(ctx context.Context, env relayproto.Envelope) {
	if c.snapshotHandler == nil {
		return
	}

	sessionID, _ := env.Payload["session_id"].(string)
	if sessionID == "" {
		return
	}

	snapshot, err := c.snapshotHandler(ctx, sessionID)
	if err != nil {
		return
	}
	_ = c.writeEnvelope(ctx, relayproto.Envelope{
		Type:    relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{"session_id": sessionID},
	})
	_ = c.PublishSnapshot(ctx, sessionID, snapshot)
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
