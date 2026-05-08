import { useState } from "preact/hooks";
import { t } from "@/i18n/store";

interface Props {
  open: boolean;
  serverUrl: string;
  attempts: number;
  durationMs: number;
  lastError: string;
  attemptHistory?: Array<{ at: Date; error: string }>;
  onReload: () => void;
  onRetry: () => void;
}

function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}m ${s}s`;
}

function formatTime(d: Date): string {
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

export function DisconnectModal(props: Props) {
  const [showDetails, setShowDetails] = useState(false);
  if (!props.open) return null;

  const history = props.attemptHistory ?? [];

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true">
      <div className="modal-card">
        <h2 className="modal-title">{t("relay.modal.title")}</h2>
        <p className="modal-body">
          {t("relay.modal.body", {
            server: props.serverUrl,
            attempts: String(props.attempts),
            duration: formatDuration(props.durationMs),
          })}
        </p>
        <button className="modal-details-toggle" onClick={() => setShowDetails((v) => !v)}>
          {t("relay.modal.details")}
        </button>
        {showDetails && (
          <div className="modal-details">
            {history.length > 0 ? (
              <ul className="modal-attempt-history" aria-label="attempt history">
                {history.map((entry, i) => (
                  <li key={i}>
                    <span className="attempt-time">{formatTime(entry.at)}</span>
                    {" "}
                    <span className="attempt-error">{entry.error}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <pre>{t("relay.modal.lastError", { error: props.lastError })}</pre>
            )}
          </div>
        )}
        <div className="modal-actions">
          <button className="btn btn-primary" onClick={props.onReload}>
            {t("relay.modal.reload")}
          </button>
          <button className="btn btn-secondary" onClick={props.onRetry}>
            {t("relay.modal.retry")}
          </button>
        </div>
      </div>
    </div>
  );
}
