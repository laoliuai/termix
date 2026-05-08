import { useState } from "preact/hooks";
import { t } from "@/i18n/store";

interface Props {
  open: boolean;
  serverUrl: string;
  attempts: number;
  durationMs: number;
  lastError: string;
  onReload: () => void;
  onRetry: () => void;
}

function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}m ${s}s`;
}

export function DisconnectModal(props: Props) {
  const [showDetails, setShowDetails] = useState(false);
  if (!props.open) return null;
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
          <pre className="modal-details">
            {t("relay.modal.lastError", { error: props.lastError })}
          </pre>
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
