package tests

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/termix/termix/go/internal/relayclient"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/tmux"
)

// skipIfNoTmuxStage2 self-skips the Stage 2 integration tests when tmux is not
// installed. When tmux is present these tests drive a real pane so the
// assertions cover the authoritative cols/rows that flow from the daemon-side
// sizeHandler (tmux.PaneSize) onto snapshot.ready.
func skipIfNoTmuxStage2(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// newStage2Pane creates a detached tmux session at a known fixed pane size and
// returns its name. window-size is pinned to manual so the pane never changes
// size on its own (there are no attached clients in the test), which lets the
// test assert that viewer snapshot requests leave the pane untouched. The
// session is NOT named with the production `termix_` prefix so it can never
// collide with a real session.
func newStage2Pane(t *testing.T, cols, rows int) string {
	t.Helper()
	name := "t11_test_" + uuid.NewString()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "main",
		"-x", itoa(cols), "-y", itoa(rows), "sleep", "30").Run(); err != nil {
		t.Fatalf("new-session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	// The -x/-y at create time only take effect under window-size manual; the
	// default (latest) snaps the pane to the controlling terminal. Pin manual,
	// then resize-window to the requested dimensions so the pane is held at a
	// known, stable size for the duration of the test.
	if err := exec.Command("tmux", "set-option", "-t", name, "window-size", "manual").Run(); err != nil {
		t.Fatalf("set-option window-size manual: %v", err)
	}
	if err := exec.Command("tmux", "resize-window", "-t", name, "-x", itoa(cols), "-y", itoa(rows)).Run(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	return name
}

func itoa(n int) string {
	// small dependency-free int->string for the tmux -x/-y args
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// readyResult captures the authoritative-size fields the daemon publishes on a
// snapshot.ready envelope.
type readyResult struct {
	cols int
	rows int
	gen  uint64
}

// TestTwoDifferentSizeViewersDoNotChangePane drives two simulated viewers
// (two consecutive session.snapshot.request envelopes, each carrying a DIFFERENT
// cols/rows hint as an old SPA would) against a real relayclient.Client wired to
// a real tmux pane. Stage 2 invariants asserted:
//   - the pane size is unchanged after both viewers (viewers never resize),
//   - neither viewer hint triggered the resize handler,
//   - each snapshot.ready carries the SAME authoritative cols/rows (the pane
//     size, from the daemon-side sizeHandler -> tmux.PaneSize),
//   - the per-session generation increments per watch (viewer1 -> 1, viewer2 -> 2).
func TestTwoDifferentSizeViewersDoNotChangePane(t *testing.T) {
	skipIfNoTmuxStage2(t)

	const paneCols, paneRows = 140, 35
	name := newStage2Pane(t, paneCols, paneRows)

	beforeCols, beforeRows, err := tmux.PaneSize(context.Background(), name)
	if err != nil {
		t.Fatalf("PaneSize (before): %v", err)
	}
	if int(beforeCols) != paneCols || int(beforeRows) != paneRows {
		t.Fatalf("pane born at (%d,%d), want (%d,%d)", beforeCols, beforeRows, paneCols, paneRows)
	}

	// Two distinct viewer-supplied size hints; the daemon must ignore both.
	viewerHints := [][2]int{{200, 50}, {80, 24}}
	results := make(chan readyResult, len(viewerHints))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, acceptErr := websocket.Accept(w, r, nil)
		if acceptErr != nil {
			t.Errorf("Accept: %v", acceptErr)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if _, _, herr := conn.Read(ctx); herr != nil { // hello.daemon
			t.Errorf("read hello: %v", herr)
			return
		}

		for _, hint := range viewerHints {
			req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
				Type: relayproto.TypeSessionSnapshotReq,
				Payload: map[string]any{
					"session_id": name,
					"cols":       float64(hint[0]),
					"rows":       float64(hint[1]),
				},
			})
			if werr := conn.Write(ctx, websocket.MessageText, req); werr != nil {
				t.Errorf("write snapshot.req: %v", werr)
				return
			}

			_, data, rerr := conn.Read(ctx) // snapshot.ready
			if rerr != nil {
				t.Errorf("read snapshot.ready: %v", rerr)
				return
			}
			env, derr := relayproto.DecodeEnvelope(data)
			if derr != nil {
				t.Errorf("decode snapshot.ready: %v", derr)
				return
			}
			if env.Type != relayproto.TypeSessionSnapshotReady {
				t.Errorf("expected snapshot.ready, got %q", env.Type)
				return
			}
			cols, colsOK := env.Payload["cols"].(float64)
			rows, rowsOK := env.Payload["rows"].(float64)
			gen, genOK := env.Payload["generation"].(float64)
			if !colsOK || !rowsOK || !genOK {
				t.Errorf("snapshot.ready missing cols/rows/generation: %#v", env.Payload)
				return
			}
			results <- readyResult{cols: int(cols), rows: int(rows), gen: uint64(gen)}

			if _, _, ferr := conn.Read(ctx); ferr != nil { // binary snapshot frame
				t.Errorf("read snapshot frame: %v", ferr)
				return
			}
		}
	}))
	defer server.Close()

	resizeCalled := false
	client := relayclient.New("ws"+server.URL[len("http"):], "tok", "dev")
	// snapshotHandler returns the real captured pane bytes (with cursor restore).
	client.SetSnapshotHandler(func(ctx context.Context, sessionID string) ([]byte, error) {
		return tmux.CaptureSnapshotWithCursor(ctx, sessionID)
	})
	// resizeHandler must never fire — a viewer snapshot.req carries a size hint
	// in this test and Stage 2 forbids it from resizing the pane.
	client.SetResizeHandler(func(context.Context, string, uint32, uint32) error {
		resizeCalled = true
		return nil
	})
	// sizeHandler mirrors the daemon wiring (session id -> tmux name -> PaneSize).
	// Here the session id IS the tmux name, so it resolves directly.
	client.SetSizeHandler(func(ctx context.Context, sessionID string) (uint32, uint32, error) {
		return tmux.PaneSize(ctx, sessionID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var got []readyResult
	for i := 0; i < len(viewerHints); i++ {
		select {
		case r := <-results:
			got = append(got, r)
		case <-ctx.Done():
			t.Fatalf("timed out waiting for snapshot.ready #%d: %v", i+1, ctx.Err())
		}
	}

	if resizeCalled {
		t.Fatal("resizeHandler must not be called from a viewer snapshot.req (Stage 2: viewer never resizes the pane)")
	}

	// Both viewers must observe the SAME authoritative size — the real pane size,
	// not their own differing hints.
	for i, r := range got {
		if r.cols != paneCols || r.rows != paneRows {
			t.Fatalf("viewer %d snapshot.ready size = (%d,%d), want pane (%d,%d) — viewer hints must be ignored",
				i+1, r.cols, r.rows, paneCols, paneRows)
		}
	}

	// Generation increments per fresh watch.
	if len(got) != 2 || got[0].gen != 1 || got[1].gen != 2 {
		t.Fatalf("generations = [%d %d], want [1 2]", got[0].gen, got[1].gen)
	}

	// The pane size is unchanged after both viewers connected.
	afterCols, afterRows, err := tmux.PaneSize(context.Background(), name)
	if err != nil {
		t.Fatalf("PaneSize (after): %v", err)
	}
	if afterCols != beforeCols || afterRows != beforeRows {
		t.Fatalf("pane changed size: before (%d,%d) after (%d,%d) — viewers must not resize the pane",
			beforeCols, beforeRows, afterCols, afterRows)
	}
}

// cupTail matches a trailing CUP (cursor-position) escape \x1b[{row};{col}H,
// optionally followed by a hide-cursor \x1b[?25l. BuildSnapshot always appends
// the CUP last, so CaptureSnapshotWithCursor output must end with this.
var cupTail = regexp.MustCompile(`\x1b\[[0-9]+;[0-9]+H(\x1b\[\?25l)?$`)

// TestSnapshotContainsCursorRestore writes multi-line content into a real pane,
// captures it via tmux.CaptureSnapshotWithCursor, and asserts the result is a
// self-clearing snapshot (reset prefix) whose tail is a cursor-restore CUP
// escape. This proves the cursor-restore sequence is present end-to-end through
// the same capture path the daemon uses.
func TestSnapshotContainsCursorRestore(t *testing.T) {
	skipIfNoTmuxStage2(t)

	name := "t11_test_" + uuid.NewString()
	// Print two lines, then sit idle so the cursor settles at a known-ish spot.
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "main",
		"-x", "80", "-y", "24", "sh", "-c", "printf 'line1\\nline2\\n'; sleep 30").Run(); err != nil {
		t.Fatalf("new-session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	time.Sleep(150 * time.Millisecond)

	snap, err := tmux.CaptureSnapshotWithCursor(context.Background(), name)
	if err != nil {
		t.Fatalf("CaptureSnapshotWithCursor: %v", err)
	}

	const resetPrefix = "\x1b[3J\x1b[2J\x1b[H"
	if !bytes.HasPrefix(snap, []byte(resetPrefix)) {
		tail := snap
		if len(tail) > 24 {
			tail = tail[:24]
		}
		t.Fatalf("snapshot missing self-clear reset prefix; got prefix %q", tail)
	}

	if !cupTail.Match(snap) {
		tail := snap
		if len(tail) > 16 {
			tail = tail[len(tail)-16:]
		}
		t.Fatalf("snapshot missing trailing cursor-restore CUP; got tail %q", tail)
	}
}
