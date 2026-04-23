package tests

import (
	"bytes"
	"testing"

	"github.com/termix/termix/go/internal/relayproto"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	data, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:      relayproto.TypeSessionWatch,
		RequestID: "req-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	if err != nil {
		t.Fatalf("EncodeEnvelope returned error: %v", err)
	}

	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope returned error: %v", err)
	}
	if env.Type != relayproto.TypeSessionWatch {
		t.Fatalf("expected session.watch, got %q", env.Type)
	}
	if env.RequestID != "req-1" {
		t.Fatalf("expected request id req-1, got %q", env.RequestID)
	}
}

func TestBinaryFrameRoundTrip(t *testing.T) {
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        1,
			"is_last":    true,
		},
		Payload: []byte("snapshot-data"),
	})
	if err != nil {
		t.Fatalf("EncodeBinaryFrame returned error: %v", err)
	}

	decoded, err := relayproto.DecodeBinaryFrame(frame)
	if err != nil {
		t.Fatalf("DecodeBinaryFrame returned error: %v", err)
	}
	if decoded.FrameType != relayproto.FrameTypeSnapshotChunk {
		t.Fatalf("unexpected frame type: %d", decoded.FrameType)
	}
	if !bytes.Equal(decoded.Payload, []byte("snapshot-data")) {
		t.Fatalf("unexpected payload: %q", decoded.Payload)
	}
}
