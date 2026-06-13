package relayclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relayclient"
	"github.com/termix/termix/go/internal/relayproto"
)

func TestClientAnswersSnapshotRequest(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read hello: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected hello text frame, got %v", msgType)
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode hello: %v", err)
		}
		if env.Type != relayproto.TypeHelloDaemon {
			t.Fatalf("expected hello.daemon, got %q", env.Type)
		}

		request, err := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type:    relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{"session_id": "session-1"},
		})
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write snapshot request: %v", err)
		}

		msgType, data, err = conn.Read(ctx)
		if err != nil {
			t.Fatalf("read snapshot ready: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected snapshot ready text frame, got %v", msgType)
		}
		env, err = relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode snapshot ready: %v", err)
		}
		if env.Type != relayproto.TypeSessionSnapshotReady {
			t.Fatalf("expected snapshot ready, got %q", env.Type)
		}

		msgType, data, err = conn.Read(ctx)
		if err != nil {
			t.Fatalf("read snapshot frame: %v", err)
		}
		if msgType != websocket.MessageBinary {
			t.Fatalf("expected binary snapshot frame, got %v", msgType)
		}
		frame, err := relayproto.DecodeBinaryFrame(data)
		if err != nil {
			t.Fatalf("decode snapshot frame: %v", err)
		}
		if string(frame.Payload) != "snapshot" {
			t.Fatalf("unexpected snapshot payload: %q", frame.Payload)
		}
		close(done)
	}))
	defer server.Close()

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		return []byte("snapshot"), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for snapshot response: %v", ctx.Err())
	}
}

func TestClientSkipsResizeWhenSnapshotRequestHasNoSize(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read hello: %v", err)
		}

		// Old protocol shape: no cols/rows.
		request, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type:    relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{"session_id": "sess-1"},
		})
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write request: %v", err)
		}
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read snapshot.ready: %v", err)
		}
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read snapshot frame: %v", err)
		}
		close(done)
	}))
	defer server.Close()

	resizeCalled := false
	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetResizeHandler(func(context.Context, string, uint32, uint32) error {
		resizeCalled = true
		return nil
	})
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		return []byte("snap"), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("snapshot never delivered: %v", ctx.Err())
	}
	if resizeCalled {
		t.Fatal("resize handler should not have been called when size is absent")
	}
}

// TestHandleSnapshotRequestEmitsAuthoritativeSizeAndGeneration verifies the
// snapshot.ready payload carries session_id, cols, rows, and a per-session
// generation that increments on each fresh snapshot.req — and that the viewer
// resize handler is NOT invoked even when the req payload includes cols/rows
// (Stage 2 D2: viewers never drive the pane size).
func TestHandleSnapshotRequestEmitsAuthoritativeSizeAndGeneration(t *testing.T) {
	resizeCalled := false
	gens := make(chan uint64, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if _, _, err := conn.Read(ctx); err != nil { // hello.daemon
			t.Errorf("read hello: %v", err)
			return
		}

		// Two consecutive snapshot.req for the same session; the req carries
		// cols/rows (an old-SPA hint) which must be ignored — no resize.
		for i := 0; i < 2; i++ {
			req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
				Type: relayproto.TypeSessionSnapshotReq,
				Payload: map[string]any{
					"session_id": "s1",
					"cols":       float64(200),
					"rows":       float64(50),
				},
			})
			if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
				t.Errorf("write req: %v", err)
				return
			}

			_, data, err := conn.Read(ctx) // snapshot.ready
			if err != nil {
				t.Errorf("read snapshot.ready: %v", err)
				return
			}
			env, err := relayproto.DecodeEnvelope(data)
			if err != nil {
				t.Errorf("decode snapshot.ready: %v", err)
				return
			}
			if env.Type != relayproto.TypeSessionSnapshotReady {
				t.Errorf("expected snapshot.ready, got %q", env.Type)
				return
			}
			payload := env.Payload
			if _, present := payload["cols"]; !present {
				t.Errorf("snapshot.ready missing cols key: %#v", payload)
			}
			if _, present := payload["rows"]; !present {
				t.Errorf("snapshot.ready missing rows key: %#v", payload)
			}
			g, present := payload["generation"]
			if !present {
				t.Errorf("snapshot.ready missing generation key: %#v", payload)
				return
			}
			gf, ok := g.(float64)
			if !ok {
				t.Errorf("generation not numeric: %#v", g)
				return
			}
			gens <- uint64(gf)

			if _, _, err := conn.Read(ctx); err != nil { // binary snapshot frame
				t.Errorf("read snapshot frame: %v", err)
				return
			}
		}
	}))
	defer server.Close()

	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		return []byte("snap"), nil
	})
	client.SetResizeHandler(func(context.Context, string, uint32, uint32) error {
		resizeCalled = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var got []uint64
	for i := 0; i < 2; i++ {
		select {
		case g := <-gens:
			got = append(got, g)
		case <-ctx.Done():
			t.Fatalf("timed out waiting for snapshot.ready #%d: %v", i+1, ctx.Err())
		}
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("expected generations [1 2], got %v", got)
	}
	if resizeCalled {
		t.Fatal("resizeHandler must not be called from a snapshot.req (Stage 2: viewer never resizes)")
	}
}

// TestHandleResizeRequestIsNoOp verifies a viewer client.resize parses + guards
// but never resizes the pane and never re-snapshots (Stage 2 D2).
func TestHandleResizeRequestIsNoOp(t *testing.T) {
	resizeCalled := false
	snapshotCalled := false
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, nil)
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // hello.daemon
		req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type:    relayproto.TypeClientResize,
			Payload: map[string]any{"session_id": "s1", "cols": 200, "rows": 50},
		})
		_ = conn.Write(ctx, websocket.MessageText, req)
		time.Sleep(100 * time.Millisecond) // no follow-up frame should arrive
		close(done)
	}))
	defer server.Close()

	c := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	c.SetSnapshotHandler(func(context.Context, string) ([]byte, error) { snapshotCalled = true; return []byte("x"), nil })
	c.SetResizeHandler(func(context.Context, string, uint32, uint32) error { resizeCalled = true; return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Connect(ctx)
	<-done
	if resizeCalled {
		t.Fatal("resizeHandler must not be called for client.resize")
	}
	if snapshotCalled {
		t.Fatal("snapshotHandler must not be called for client.resize")
	}
}

// TestRepushSnapshotEmitsNewSizeSameGeneration verifies the host-resize re-push:
// a fresh snapshot.req advances the generation to 1; RepushSnapshot then emits a
// snapshot.ready carrying the NEW cols/rows but the SAME generation (a host
// resize is not a new watch, so the generation does NOT increment), followed by
// the snapshot bytes as a binary frame.
func TestRepushSnapshotEmitsNewSizeSameGeneration(t *testing.T) {
	type readyMsg struct {
		cols, rows, gen uint64
	}
	readies := make(chan readyMsg, 2)
	repushDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if _, _, err := conn.Read(ctx); err != nil { // hello.daemon
			t.Errorf("read hello: %v", err)
			return
		}

		readReady := func() {
			_, data, err := conn.Read(ctx) // snapshot.ready
			if err != nil {
				t.Errorf("read snapshot.ready: %v", err)
				return
			}
			env, err := relayproto.DecodeEnvelope(data)
			if err != nil {
				t.Errorf("decode snapshot.ready: %v", err)
				return
			}
			if env.Type != relayproto.TypeSessionSnapshotReady {
				t.Errorf("expected snapshot.ready, got %q", env.Type)
				return
			}
			num := func(k string) uint64 {
				v, _ := env.Payload[k].(float64)
				return uint64(v)
			}
			readies <- readyMsg{cols: num("cols"), rows: num("rows"), gen: num("generation")}
			if _, _, err := conn.Read(ctx); err != nil { // binary snapshot frame
				t.Errorf("read snapshot frame: %v", err)
			}
		}

		// First, a normal snapshot.req: advances generation to 1.
		req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type:    relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{"session_id": "s1"},
		})
		if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
			t.Errorf("write req: %v", err)
			return
		}
		readReady()
		// Then the host-resize re-push (driven by the client below).
		readReady()
		close(repushDone)
	}))
	defer server.Close()

	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		return []byte("snap"), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Wait for the first (watch-driven) snapshot.ready (generation 1) before
	// re-pushing, so the re-push observes the advanced current generation.
	var first readyMsg
	select {
	case first = <-readies:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for initial snapshot.ready: %v", ctx.Err())
	}
	if first.gen != 1 {
		t.Fatalf("initial snapshot.ready generation = %d, want 1", first.gen)
	}

	if err := client.RepushSnapshot(ctx, "s1", []byte("snap2"), 180, 45); err != nil {
		t.Fatalf("RepushSnapshot: %v", err)
	}

	var repush readyMsg
	select {
	case repush = <-readies:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for re-push snapshot.ready: %v", ctx.Err())
	}
	if repush.cols != 180 || repush.rows != 45 {
		t.Fatalf("re-push size = (%d,%d), want (180,45)", repush.cols, repush.rows)
	}
	if repush.gen != 1 {
		t.Fatalf("re-push generation = %d, want 1 (host resize must NOT increment)", repush.gen)
	}
	<-repushDone
}

func TestClientHandlesTerminalInputFrame(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read hello: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected hello text frame, got %v", msgType)
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode hello: %v", err)
		}
		if env.Type != relayproto.TypeHelloDaemon {
			t.Fatalf("expected hello.daemon, got %q", env.Type)
		}

		frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
			FrameType: relayproto.FrameTypeTerminalInput,
			Header: map[string]any{
				"session_id": "session-1",
				"encoding":   "raw",
			},
			Payload: []byte("pwd\n"),
		})
		if err != nil {
			t.Fatalf("encode input frame: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
			t.Fatalf("write input frame: %v", err)
		}

		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for input handler: %v", ctx.Err())
		}
	}))
	defer server.Close()

	var gotSessionID string
	var gotPayload []byte

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	client.SetInputHandler(func(_ context.Context, sessionID string, payload []byte) error {
		gotSessionID = sessionID
		gotPayload = append([]byte(nil), payload...)
		close(done)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for input frame handling: %v", ctx.Err())
	}
	if gotSessionID != "session-1" {
		t.Fatalf("unexpected session id: %q", gotSessionID)
	}
	if string(gotPayload) != "pwd\n" {
		t.Fatalf("unexpected payload: %q", gotPayload)
	}
}

func TestResizeRequestPayloadCarriesDebugObservations(t *testing.T) {
	// When the SPA is in DEBUG mode it piggybacks observed viewport geometry on
	// client.resize. The daemon must decode (not silently drop) that object so
	// it can be logged for correlation with server-side behaviour.
	var p relayclient.ResizeRequestPayload
	raw := []byte(`{"session_id":"s","cols":120,"rows":40,"debug":{"vw":390,"vh":780,"dpr":3}}`)
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Debug == nil {
		t.Fatal("debug observations were dropped (missing json tag?)")
	}
	if p.Debug["vw"] != float64(390) || p.Debug["dpr"] != float64(3) {
		t.Fatalf("unexpected debug payload: %#v", p.Debug)
	}
}

func TestClientPublishOutputSendsTerminalOutputFrameWithStreamHeader(t *testing.T) {
	type captured struct {
		frame relayproto.BinaryFrame
	}
	got := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Drain the hello.daemon envelope.
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read hello: %v", err)
		}

		// Read the output binary frame.
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read output frame: %v", err)
		}
		if msgType != websocket.MessageBinary {
			t.Fatalf("expected binary output frame, got %v", msgType)
		}
		frame, err := relayproto.DecodeBinaryFrame(data)
		if err != nil {
			t.Fatalf("decode output frame: %v", err)
		}
		got <- captured{frame: frame}
	}))
	defer server.Close()

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	if err := client.PublishOutput(ctx, "session-1", []byte("hi")); err != nil {
		t.Fatalf("PublishOutput returned error: %v", err)
	}

	var cap captured
	select {
	case cap = <-got:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for output frame: %v", ctx.Err())
	}

	if cap.frame.FrameType != relayproto.FrameTypeTerminalOutput {
		t.Fatalf("expected FrameTypeTerminalOutput, got %d", cap.frame.FrameType)
	}
	if got, want := cap.frame.Header["session_id"], "session-1"; got != want {
		t.Fatalf("session_id mismatch: want %q, got %v", want, got)
	}
	if stream, _ := cap.frame.Header["stream"].(string); stream != "stdout" {
		t.Fatalf("stream header missing or wrong: want %q, got %v", "stdout", cap.frame.Header["stream"])
	}
	if string(cap.frame.Payload) != "hi" {
		t.Fatalf("payload mismatch: got %q", cap.frame.Payload)
	}
}

func TestClientDoneClosesWhenReadLoopExits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}))
	defer server.Close()

	c := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case err := <-c.Done():
		if err == nil {
			t.Fatalf("expected non-nil error from Done after server close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close within 2s of server-side close")
	}
}

func TestClientCloseTerminatesReadLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		<-ctx.Done() // hold open until the client closes
	}))
	defer server.Close()

	c := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-c.Done():
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close within 2s of explicit Close()")
	}
}

func TestClientCloseIsIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		<-ctx.Done() // hold open until the client closes
	}))
	defer server.Close()

	c := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// First Close() should return nil
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second Close() should also return nil (idempotency)
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should return nil, got %v", err)
	}
}
