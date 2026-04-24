package relay

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/termix/termix/go/internal/relayproto"
)

type Server struct {
	auth SessionAuthorizer
	reg  *registry
}

func NewServer(auth SessionAuthorizer) *Server {
	return &Server{
		auth: auth,
		reg:  newRegistry(),
	}
}

func (s *Server) Handler() http.Handler {
	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		conn, err := websocket.Accept(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		p := newPeer(conn)
		go s.serveConn(context.WithoutCancel(c.Request.Context()), p, bearerToken(c.GetHeader("Authorization")))
	})
	return router
}

func (s *Server) serveConn(ctx context.Context, p *peer, accessToken string) {
	defer func() {
		s.reg.removePeer(p)
		_ = p.conn.Close(websocket.StatusNormalClosure, "done")
	}()

	for {
		msgType, data, err := p.conn.Read(ctx)
		if err != nil {
			return
		}
		if msgType == websocket.MessageBinary {
			s.forwardBinary(ctx, p, data)
			continue
		}
		if msgType != websocket.MessageText {
			continue
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			return
		}
		if err := s.handleEnvelope(ctx, p, accessToken, env); err != nil {
			_ = s.writeError(ctx, p, err)
			return
		}
	}
}

func (s *Server) handleEnvelope(ctx context.Context, p *peer, accessToken string, env relayproto.Envelope) error {
	switch env.Type {
	case relayproto.TypeHelloDaemon, relayproto.TypeHelloViewer:
		return nil
	case relayproto.TypeSessionOnline:
		sessionID, err := payloadString(env, "session_id")
		if err != nil {
			return err
		}
		s.reg.setDaemon(sessionID, p)
		return nil
	case relayproto.TypeSessionSnapshotReady:
		sessionID, err := payloadString(env, "session_id")
		if err != nil {
			return err
		}
		s.forwardEnvelope(ctx, sessionID, env)
		return nil
	case relayproto.TypeSessionWatch:
		sessionID, err := payloadString(env, "session_id")
		if err != nil {
			return err
		}
		if s.auth != nil {
			if err := s.auth.AuthorizeWatch(ctx, accessToken, sessionID); err != nil {
				return err
			}
		}
		daemon := s.reg.daemon(sessionID)
		if daemon == nil {
			return errors.New("session daemon is offline")
		}
		s.reg.addWatcher(sessionID, p)
		if err := writeEnvelope(ctx, p, relayproto.Envelope{
			Type:    relayproto.TypeSessionJoined,
			Payload: map[string]any{"session_id": sessionID},
		}); err != nil {
			return err
		}
		return writeEnvelope(ctx, daemon, relayproto.Envelope{
			Type:    relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{"session_id": sessionID},
		})
	case relayproto.TypeControlAcquire:
		return s.handleControlAcquire(ctx, p, accessToken, env)
	case relayproto.TypeControlRenew:
		return s.handleControlRenew(ctx, p, accessToken, env)
	case relayproto.TypeControlRelease:
		return s.handleControlRelease(ctx, p, accessToken, env)
	default:
		return nil
	}
}

func (s *Server) forwardEnvelope(ctx context.Context, sessionID string, env relayproto.Envelope) {
	for _, watcher := range s.reg.watchersFor(sessionID) {
		_ = writeEnvelope(ctx, watcher, env)
	}
}

func (s *Server) forwardBinary(ctx context.Context, sender *peer, data []byte) {
	frame, err := relayproto.DecodeBinaryFrame(data)
	if err != nil {
		return
	}
	sessionID := relayproto.HeaderString(frame.Header, "session_id")
	if sessionID == "" {
		return
	}
	switch frame.FrameType {
	case relayproto.FrameTypeTerminalInput:
		s.forwardInput(ctx, sender, sessionID, frame, data)
	case relayproto.FrameTypeTerminalOutput, relayproto.FrameTypeSnapshotChunk:
		for _, watcher := range s.reg.watchersFor(sessionID) {
			_ = watcher.write(ctx, websocket.MessageBinary, data)
		}
	}
}

func payloadString(env relayproto.Envelope, key string) (string, error) {
	value, _ := env.Payload[key].(string)
	if value == "" {
		return "", errors.New("missing " + key)
	}
	return value, nil
}

func payloadInt64(env relayproto.Envelope, key string) (int64, error) {
	value, ok := env.Payload[key]
	if !ok {
		return 0, errors.New("missing " + key)
	}
	switch value.(type) {
	case int, int64, float64:
	default:
		return 0, errors.New("invalid " + key)
	}
	n := relayproto.HeaderInt64(map[string]any{key: value}, key)
	if n == 0 {
		return 0, errors.New("invalid " + key)
	}
	return n, nil
}

func writeEnvelope(ctx context.Context, p *peer, env relayproto.Envelope) error {
	data, err := relayproto.EncodeEnvelope(env)
	if err != nil {
		return err
	}
	return p.write(ctx, websocket.MessageText, data)
}

func (s *Server) writeError(ctx context.Context, p *peer, err error) error {
	return writeEnvelope(ctx, p, relayproto.Envelope{
		Type:    relayproto.TypeError,
		Payload: map[string]any{"message": err.Error()},
	})
}

func (s *Server) handleControlAcquire(ctx context.Context, p *peer, accessToken string, env relayproto.Envelope) error {
	sessionID, err := payloadString(env, "session_id")
	if err != nil {
		return s.writeControlDenied(ctx, p, env.RequestID, "", "invalid_request", err.Error())
	}
	if !s.reg.isWatching(sessionID, p) {
		return s.writeControlDenied(ctx, p, env.RequestID, sessionID, "invalid_request", "session.watch is required before control.acquire")
	}
	if s.reg.daemon(sessionID) == nil {
		return s.writeControlDenied(ctx, p, env.RequestID, sessionID, "relay_no_daemon", "session daemon is offline")
	}
	if s.auth == nil {
		return errors.New("control authorizer is unavailable")
	}
	grant, err := s.auth.AcquireControl(ctx, accessToken, sessionID)
	if err != nil {
		return s.writeDeniedError(ctx, p, env.RequestID, sessionID, err)
	}
	s.reg.setController(sessionID, p, grant)
	return writeEnvelope(ctx, p, relayproto.Envelope{
		Type:      relayproto.TypeControlGranted,
		RequestID: env.RequestID,
		Payload: map[string]any{
			"session_id":          grant.SessionID,
			"lease_version":       grant.LeaseVersion,
			"expires_at":          grant.ExpiresAt.Format(time.RFC3339),
			"renew_after_seconds": grant.RenewAfterSeconds,
		},
	})
}

func (s *Server) handleControlRenew(ctx context.Context, p *peer, accessToken string, env relayproto.Envelope) error {
	sessionID, err := payloadString(env, "session_id")
	if err != nil {
		return s.writeControlDenied(ctx, p, env.RequestID, "", "invalid_request", err.Error())
	}
	leaseVersion, err := payloadInt64(env, "lease_version")
	if err != nil {
		return s.writeControlDenied(ctx, p, env.RequestID, sessionID, "invalid_request", err.Error())
	}

	state, ok := s.reg.controller(sessionID)
	if !ok || state.peer != p || state.leaseVersion != leaseVersion {
		return s.writeControlDenied(ctx, p, env.RequestID, sessionID, "stale_lease", "stale control lease")
	}
	if s.auth == nil {
		return errors.New("control authorizer is unavailable")
	}
	grant, err := s.auth.RenewControl(ctx, accessToken, sessionID, leaseVersion)
	if err != nil {
		return s.writeDeniedError(ctx, p, env.RequestID, sessionID, err)
	}
	s.reg.setController(sessionID, p, grant)
	return writeEnvelope(ctx, p, relayproto.Envelope{
		Type:      relayproto.TypeControlGranted,
		RequestID: env.RequestID,
		Payload: map[string]any{
			"session_id":          grant.SessionID,
			"lease_version":       grant.LeaseVersion,
			"expires_at":          grant.ExpiresAt.Format(time.RFC3339),
			"renew_after_seconds": grant.RenewAfterSeconds,
		},
	})
}

func (s *Server) handleControlRelease(ctx context.Context, p *peer, accessToken string, env relayproto.Envelope) error {
	sessionID, err := payloadString(env, "session_id")
	if err != nil {
		return s.writeControlDenied(ctx, p, env.RequestID, "", "invalid_request", err.Error())
	}
	leaseVersion, err := payloadInt64(env, "lease_version")
	if err != nil {
		return s.writeControlDenied(ctx, p, env.RequestID, sessionID, "invalid_request", err.Error())
	}

	state, ok := s.reg.controller(sessionID)
	if !ok || state.peer != p || state.leaseVersion != leaseVersion {
		return s.writeControlDenied(ctx, p, env.RequestID, sessionID, "stale_lease", "stale control lease")
	}
	if s.auth == nil {
		return errors.New("control authorizer is unavailable")
	}
	if err := s.auth.ReleaseControl(ctx, accessToken, sessionID, leaseVersion); err != nil {
		return s.writeDeniedError(ctx, p, env.RequestID, sessionID, err)
	}
	s.reg.clearController(sessionID, p)
	return writeEnvelope(ctx, p, relayproto.Envelope{
		Type:      relayproto.TypeControlRevoked,
		RequestID: env.RequestID,
		Payload: map[string]any{
			"session_id":    sessionID,
			"lease_version": leaseVersion,
			"reason":        "released",
		},
	})
}

func (s *Server) forwardInput(ctx context.Context, sender *peer, sessionID string, frame relayproto.BinaryFrame, data []byte) {
	state, ok := s.reg.controller(sessionID)
	if !ok || state.peer != sender {
		_ = s.writeError(ctx, sender, errors.New("control lease is required for terminal input"))
		return
	}
	if time.Now().After(state.expiresAt) {
		s.reg.clearController(sessionID, state.peer)
		_ = s.writeError(ctx, sender, errors.New("control lease expired"))
		return
	}
	if relayproto.HeaderString(frame.Header, "encoding") != "raw" {
		_ = s.writeError(ctx, sender, errors.New("unsupported terminal input encoding"))
		return
	}
	leaseVersion := relayproto.HeaderInt64(frame.Header, "lease_version")
	if leaseVersion != 0 && leaseVersion != state.leaseVersion {
		_ = s.writeError(ctx, sender, errors.New("stale control lease"))
		return
	}
	daemon := s.reg.daemon(sessionID)
	if daemon == nil {
		_ = s.writeError(ctx, sender, errors.New("session daemon is offline"))
		return
	}
	_ = daemon.write(ctx, websocket.MessageBinary, data)
}

func (s *Server) writeDeniedError(ctx context.Context, p *peer, requestID string, sessionID string, err error) error {
	var denied ErrControlDenied
	if errors.As(err, &denied) {
		return s.writeControlDenied(ctx, p, requestID, sessionID, denied.Reason, denied.Error())
	}
	return err
}

func (s *Server) writeControlDenied(ctx context.Context, p *peer, requestID string, sessionID string, reason string, message string) error {
	return writeEnvelope(ctx, p, relayproto.Envelope{
		Type:      relayproto.TypeControlDenied,
		RequestID: requestID,
		Payload: map[string]any{
			"session_id": sessionID,
			"reason":     reason,
			"message":    message,
		},
	})
}
