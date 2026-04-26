import type {
  ControlDeniedPayload,
  ControlGrantedPayload,
  ControlRevokedPayload,
  Envelope,
  ErrorPayload,
  SessionJoinedPayload,
  SessionSnapshotReadyPayload,
} from "./types";

export function encodeEnvelope<T>(type: string, payload: T, requestId: string | null = null): string {
  const env: Envelope<T> = { type, request_id: requestId, payload };
  return JSON.stringify(env);
}

export function decodeEnvelope(text: string): Envelope {
  const raw = JSON.parse(text) as unknown;
  if (typeof raw !== "object" || raw === null) {
    throw new Error("envelope must be an object");
  }
  const obj = raw as Record<string, unknown>;
  if (typeof obj.type !== "string") {
    throw new Error("envelope.type must be a string");
  }
  if (obj.request_id !== null && typeof obj.request_id !== "string") {
    throw new Error("envelope.request_id must be string or null");
  }
  if (typeof obj.payload !== "object" || obj.payload === null) {
    throw new Error("envelope.payload must be an object");
  }
  return obj as unknown as Envelope;
}

export function isSessionJoined(env: Envelope): env is Envelope<SessionJoinedPayload> {
  return env.type === "session.joined";
}

export function isSessionSnapshotReady(env: Envelope): env is Envelope<SessionSnapshotReadyPayload> {
  return env.type === "session.snapshot.ready";
}

export function isControlGranted(env: Envelope): env is Envelope<ControlGrantedPayload> {
  return env.type === "control.granted";
}

export function isControlDenied(env: Envelope): env is Envelope<ControlDeniedPayload> {
  return env.type === "control.denied";
}

export function isControlRevoked(env: Envelope): env is Envelope<ControlRevokedPayload> {
  return env.type === "control.revoked";
}

export function isErrorEnvelope(env: Envelope): env is Envelope<ErrorPayload> {
  return env.type === "error";
}
