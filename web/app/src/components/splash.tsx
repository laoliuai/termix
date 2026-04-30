import { t } from "../i18n/store";

export function Splash() {
  return (
    <div class="splash">
      <div class="brand-glyph">{">_"}</div>
      <div class="splash-spinner" aria-label={t("common.loading")}></div>
    </div>
  );
}
