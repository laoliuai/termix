// JS bridge contract — Native -> WebView (window.* globals).
export type SpecialKey =
  | "Enter" | "Tab" | "Escape"
  | "Up" | "Down" | "Left" | "Right"
  | "C-c" | "C-d" | "C-j"
  | "Backspace";

// JS bridge contract — WebView -> Native (window.TermixBridge optional global).
// ConnectionState is a discriminated union so the bridge can surface
// reconnect-supervisor metadata (attempt count, last error, give-up duration)
// without overloading a string.
export type ConnectionState =
  | { phase: "connecting" }
  | { phase: "connected" }
  | { phase: "reconnecting"; attempt: number; lastError: string }
  | { phase: "gave-up"; attemptCount: number; durationMs: number; lastError: string; attemptHistory?: Array<{ at: Date; error: string }> }
  | { phase: "disconnected" }
  | { phase: "error" };
export type ControlState = "none" | "requesting" | "granted" | "denied" | "revoked";

export interface TermixBridge {
  onConnectionState(state: ConnectionState): void;
  onControlState(state: ControlState, detail?: string): void;
  onError(code: string, message: string): void;
}

// JSON text envelope (spec §17.3 / §3b).
export interface Envelope<T = unknown> {
  type: string;
  request_id: string | null;
  payload: T;
}

// Outgoing envelope payloads.
export interface HelloAndroidPayload     { device_id: string }
// cols/rows are optional initial-grid hints. When present, the relay
// forwards them into session.snapshot.req so the daemon resizes the
// tmux pane before capture-pane runs — letting the very first snapshot
// match the viewer's actual viewport without a separate pre-watch
// client.resize (which the relay rejects when the peer is not yet a
// watcher).
export interface SessionWatchPayload     { session_id: string; cols?: number; rows?: number }
export interface SessionUnwatchPayload   { session_id: string }
export interface ControlAcquirePayload   { session_id: string }
export interface ControlRenewPayload     { session_id: string; lease_version: number }
export interface ControlReleasePayload   { session_id: string; lease_version: number }
export interface ClientResizePayload     { session_id: string; cols: number; rows: number }
export interface HeartbeatPayload        { /* empty object */ }

// Incoming envelope payloads.
export interface HelloOkPayload          { connection_id: string }
export interface SessionJoinedPayload    { session_id: string }
export interface SessionSnapshotReadyPayload {
  session_id: string;
  total_chunks?: number;
  cols?: number;        // authoritative pane width (new daemon only)
  rows?: number;        // authoritative pane height (new daemon only)
  generation?: number;  // per-session generation for the snapshot fence
}
export interface ControlGrantedPayload   { session_id: string; lease_version: number; expires_at: string; controller_device_id: string }
export interface ControlDeniedPayload    { session_id: string; reason: string }
export interface ControlRevokedPayload   { session_id: string; reason: string }
export interface ErrorPayload            { code: string; message: string }

// TMX1 binary frame (spec §17.4 / §3c).
export const FRAME_MAGIC = "TMX1";
export const FRAME_VERSION = 0x01;
export type FrameType = 1 | 2 | 3; // 1 output, 2 input, 3 snapshot

export interface OutputHeader   { session_id: string; seq: number; stream: "stdout" | "stderr" }
export interface InputHeader    { session_id: string; seq: number; encoding: "raw" }
export interface SnapshotHeader { session_id: string; seq: number; is_last: boolean }

export type FrameHeader = OutputHeader | InputHeader | SnapshotHeader;

export interface DecodedFrame {
  type: FrameType;
  header: FrameHeader;
  payload: Uint8Array;
}
