import { t } from "@/i18n/store";

interface Props {
  phase: string;
  attempt: number;
}

export function ReconnectBanner({ phase, attempt }: Props) {
  if (phase !== "reconnecting") return null;
  return (
    <div role="status" className="reconnect-banner">
      {t("relay.banner.reconnecting", { attempt: String(attempt) })}
    </div>
  );
}
