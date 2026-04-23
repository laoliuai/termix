package relay

import (
	"context"
	"errors"
	"net/http"

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
	default:
		return nil
	}
}

func payloadString(env relayproto.Envelope, key string) (string, error) {
	value, _ := env.Payload[key].(string)
	if value == "" {
		return "", errors.New("missing " + key)
	}
	return value, nil
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
