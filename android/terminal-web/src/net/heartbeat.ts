export function startHeartbeat(send: () => void, intervalMs: number): () => void {
  const handle = setInterval(send, intervalMs);
  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    clearInterval(handle);
  };
}
