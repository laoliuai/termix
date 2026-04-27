import { useEffect } from "preact/hooks";
import { snackbar } from "../app/store";

export function Snackbar() {
  const s = snackbar.value;

  useEffect(() => {
    if (!s) return;
    if (s.kind === "error") return; // sticky
    const t = setTimeout(() => { snackbar.value = null; }, 3000);
    return () => clearTimeout(t);
  }, [s]);

  if (!s) return null;
  return (
    <div class={`snackbar kind-${s.kind}`} role="status">
      <span>{s.msg}</span>
      {s.action ? (
        <button class="snackbar-action" onClick={s.action.cb}>{s.action.label}</button>
      ) : null}
    </div>
  );
}
