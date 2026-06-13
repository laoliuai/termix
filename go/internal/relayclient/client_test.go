package relayclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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

func TestClientResizesPaneBeforeAnsweringSnapshotRequestWithSize(t *testing.T) {
	// serverDone is closed by the server goroutine after it has consumed
	// both the snapshot.ready envelope and the binary snapshot frame, so
	// the test body never returns before the handler is finished (avoids
	// t.Fatalf-after-test races when the handler is still mid-read).
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		defer close(serverDone)

		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		request, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type: relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{
				"session_id": "sess-1",
				"cols":       float64(117),
				"rows":       float64(38),
			},
		})
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Errorf("write request: %v", err)
			return
		}
		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read snapshot.ready: %v", err)
			return
		}
		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read snapshot frame: %v", err)
			return
		}
	}))
	defer server.Close()

	var mu sync.Mutex
	var order []string
	var gotCols, gotRows uint32

	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetPaneRedrawDelay(0)
	client.SetPaneRedrawPolling(0, 6)
	client.SetResizeHandler(func(_ context.Context, sessionID string, cols, rows uint32) error {
		mu.Lock()
		order = append(order, "resize")
		gotCols, gotRows = cols, rows
		mu.Unlock()
		return nil
	})
	client.SetSnapshotHandler(func(_ context.Context, sessionID string) ([]byte, error) {
		mu.Lock()
		order = append(order, "snapshot")
		mu.Unlock()
		return []byte("snap"), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatalf("server never observed snapshot completion: %v", ctx.Err())
	}

	mu.Lock()
	defer mu.Unlock()
	// poll-until-stable captures more than once; assert resize came first and
	// every following step is a snapshot capture.
	if len(order) < 2 || order[0] != "resize" {
		t.Fatalf("expected resize before snapshot(s), got %v", order)
	}
	for _, step := range order[1:] {
		if step != "snapshot" {
			t.Fatalf("expected only snapshot steps after resize, got %v", order)
		}
	}
	if gotCols != 117 || gotRows != 38 {
		t.Fatalf("expected resize 117x38, got %dx%d", gotCols, gotRows)
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

func TestClientWaitsForPaneRedrawBetweenResizeAndSnapshot(t *testing.T) {
	// tmux resize-window updates the pane's cell array immediately but
	// SIGWINCH→TUI redraw is async. capture-pane right after resize sees
	// "new size + old layout" and ships a stale snapshot to the SPA. The
	// client must wait between resize and capture so the TUI has time to
	// repaint at the new size first.
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		defer close(serverDone)

		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type: relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{
				"session_id": "sess-1",
				"cols":       float64(120),
				"rows":       float64(40),
			},
		})
		if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
			t.Errorf("write req: %v", err)
			return
		}
		// Drain snapshot.ready + binary so the server-side handler doesn't
		// block on backpressure while the wait happens.
		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read snapshot.ready: %v", err)
			return
		}
		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read snapshot frame: %v", err)
			return
		}
	}))
	defer server.Close()

	var mu sync.Mutex
	var resizeAt, snapshotAt time.Time

	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetPaneRedrawDelay(80 * time.Millisecond)
	client.SetResizeHandler(func(context.Context, string, uint32, uint32) error {
		mu.Lock()
		resizeAt = time.Now()
		mu.Unlock()
		return nil
	})
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		mu.Lock()
		snapshotAt = time.Now()
		mu.Unlock()
		return []byte("snap"), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatalf("server never drained snapshot reply: %v", ctx.Err())
	}

	mu.Lock()
	defer mu.Unlock()
	if resizeAt.IsZero() || snapshotAt.IsZero() {
		t.Fatalf("missing callbacks: resize=%v snapshot=%v", resizeAt, snapshotAt)
	}
	elapsed := snapshotAt.Sub(resizeAt)
	if elapsed < 75*time.Millisecond {
		t.Fatalf("expected ≥75ms wait between resize and snapshot, got %v", elapsed)
	}
}

func TestClientReSnapshotsAfterClientResize(t *testing.T) {
	// When the viewer sends client.resize (e.g. composer-dock appears and
	// shrinks the xterm grid), the daemon resizes the tmux pane. Without a
	// fresh snapshot the SPA's xterm keeps showing the pre-resize layout
	// stacked under the live redraw, leaving the cursor at the wrong row.
	// After resize, the client must wait for the TUI to repaint, then
	// publish a fresh snapshot.ready + binary frame so the SPA's
	// snapshot.ready handler clears xterm and writes the new state.
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		defer close(serverDone)

		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type: relayproto.TypeClientResize,
			Payload: map[string]any{
				"session_id": "sess-1",
				"cols":       float64(100),
				"rows":       float64(30),
			},
		})
		if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
			t.Errorf("write resize: %v", err)
			return
		}

		// Expect snapshot.ready envelope first.
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read snapshot.ready: %v", err)
			return
		}
		if msgType != websocket.MessageText {
			t.Errorf("expected text snapshot.ready, got %v", msgType)
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

		// Then the binary snapshot frame with the fresh capture.
		msgType, data, err = conn.Read(ctx)
		if err != nil {
			t.Errorf("read snapshot frame: %v", err)
			return
		}
		if msgType != websocket.MessageBinary {
			t.Errorf("expected binary snapshot frame, got %v", msgType)
			return
		}
		frame, err := relayproto.DecodeBinaryFrame(data)
		if err != nil {
			t.Errorf("decode snapshot frame: %v", err)
			return
		}
		if string(frame.Payload) != "fresh" {
			t.Errorf("expected snapshot payload 'fresh', got %q", frame.Payload)
		}
	}))
	defer server.Close()

	var mu sync.Mutex
	var order []string
	var gotCols, gotRows uint32

	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetPaneRedrawDelay(0)
	client.SetPaneRedrawPolling(0, 6)
	client.SetResizeHandler(func(_ context.Context, _ string, cols, rows uint32) error {
		mu.Lock()
		order = append(order, "resize")
		gotCols, gotRows = cols, rows
		mu.Unlock()
		return nil
	})
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		mu.Lock()
		order = append(order, "snapshot")
		mu.Unlock()
		return []byte("fresh"), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatalf("server never observed re-snapshot: %v", ctx.Err())
	}

	mu.Lock()
	defer mu.Unlock()
	// poll-until-stable captures more than once; assert resize came first and
	// every following step is a snapshot capture.
	if len(order) < 2 || order[0] != "resize" {
		t.Fatalf("expected resize→snapshot, got %v", order)
	}
	for _, step := range order[1:] {
		if step != "snapshot" {
			t.Fatalf("expected only snapshot steps after resize, got %v", order)
		}
	}
	if gotCols != 100 || gotRows != 30 {
		t.Fatalf("expected resize 100×30, got %d×%d", gotCols, gotRows)
	}
}

func TestClientPollsUntilPaneRedrawStabilizes(t *testing.T) {
	// After a resize the TUI repaints asynchronously, so the first capture-pane
	// can still return the pre-resize layout. Rather than trust a single fixed
	// delay, the client re-captures until two consecutive captures match (the
	// redraw has settled) and publishes that stable frame.
	serverDone := make(chan struct{})
	var pubMu sync.Mutex
	var published string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		defer close(serverDone)

		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type: relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{
				"session_id": "sess-1",
				"cols":       float64(120),
				"rows":       float64(40),
			},
		})
		if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
			t.Errorf("write req: %v", err)
			return
		}
		if _, _, err := conn.Read(ctx); err != nil { // snapshot.ready
			t.Errorf("read snapshot.ready: %v", err)
			return
		}
		_, data, err := conn.Read(ctx) // binary snapshot
		if err != nil {
			t.Errorf("read snapshot frame: %v", err)
			return
		}
		frame, err := relayproto.DecodeBinaryFrame(data)
		if err != nil {
			t.Errorf("decode snapshot frame: %v", err)
			return
		}
		pubMu.Lock()
		published = string(frame.Payload)
		pubMu.Unlock()
	}))
	defer server.Close()

	var mu sync.Mutex
	var n int
	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	client.SetPaneRedrawDelay(0)
	client.SetPaneRedrawPolling(0, 6)
	client.SetResizeHandler(func(context.Context, string, uint32, uint32) error { return nil })
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		mu.Lock()
		n++
		cur := n
		mu.Unlock()
		switch cur {
		case 1:
			return []byte("frame-A"), nil // pre-resize layout
		case 2:
			return []byte("frame-B"), nil // mid-repaint
		default:
			return []byte("stable"), nil // settled
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatalf("server never received snapshot: %v", ctx.Err())
	}

	pubMu.Lock()
	defer pubMu.Unlock()
	if published != "stable" {
		t.Fatalf("expected the stabilized capture 'stable' to be published, got %q", published)
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

func TestClientHandlesResizeEnvelope(t *testing.T) {
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
			Type: relayproto.TypeClientResize,
			Payload: map[string]any{
				"session_id": "sid-1",
				"cols":       float64(80),
				"rows":       float64(24),
			},
		})
		if err != nil {
			t.Fatalf("encode resize request: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write resize request: %v", err)
		}

		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for resize handler: %v", ctx.Err())
		}
	}))
	defer server.Close()

	var gotSessionID string
	var gotCols, gotRows uint32

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	client.SetResizeHandler(func(_ context.Context, sessionID string, cols, rows uint32) error {
		gotSessionID = sessionID
		gotCols = cols
		gotRows = rows
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
		t.Fatalf("timed out waiting for resize envelope handling: %v", ctx.Err())
	}
	if gotSessionID != "sid-1" {
		t.Fatalf("unexpected session id: %q", gotSessionID)
	}
	if gotCols != 80 {
		t.Fatalf("unexpected cols: %d", gotCols)
	}
	if gotRows != 24 {
		t.Fatalf("unexpected rows: %d", gotRows)
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
