import { useSignal } from "@preact/signals";
import { login } from "../api/endpoints";
import { accessToken, accessTokenExpiresAt, userInfo } from "../auth/store";
import { t } from "../i18n/store";

const LAST_EMAIL_KEY = "termix.login.email";

function storedEmail(): string {
  try {
    return localStorage.getItem(LAST_EMAIL_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveEmail(value: string): void {
  try {
    if (value) localStorage.setItem(LAST_EMAIL_KEY, value);
    else localStorage.removeItem(LAST_EMAIL_KEY);
  } catch {
    // Storage can be disabled in private browsing; login should still work.
  }
}

export interface LoginPageProps {
  onSuccess: () => void;
  onHelp: () => void;
  onHome: () => void;
}

export function LoginPage({ onSuccess, onHelp, onHome }: LoginPageProps) {
  const email = useSignal(storedEmail());
  const password = useSignal("");
  const busy = useSignal(false);
  const error = useSignal<string | null>(null);

  const submit = async (e: Event) => {
    e.preventDefault();
    if (busy.value) return;
    busy.value = true;
    error.value = null;
    try {
      const res = await login({
        email: email.value,
        password: password.value,
        device_label: navigator.userAgent.slice(0, 80),
      });
      if (!res.ok) {
        error.value =
          res.status === 401 ? t("login.badCredentials") :
          res.status === 429 ? t("login.rateLimited") :
          res.status === 0   ? t("login.network") :
          (res.message || t("login.failed"));
        busy.value = false;
        return;
      }
      accessToken.value = res.data.access_token;
      accessTokenExpiresAt.value = Date.now() + res.data.expires_in_seconds * 1000;
      userInfo.value = { user: res.data.user, device: res.data.device };
      onSuccess();
    } catch {
      error.value = t("login.network");
      busy.value = false;
    }
  };

  return (
    <div class="login-screen login-shell">
      <form class="login-page" onSubmit={submit}>
        <div class="brand-block login-brand">
          <button type="button" class="brand-home" aria-label={t("home.heroAria")} onClick={onHome}>
            <img class="brand-mark" src="/icons/termix.svg?v=tmx" alt="" />
            <span>{t("brand.name")}</span>
          </button>
          <h1>{t("login.title")}</h1>
        </div>
        <label>
          <span class="input-label">{t("login.email")}</span>
          <input id="login-email" name="username" class="input-field" type="email" autocomplete="username" required
                 value={email.value}
                 onInput={e => {
                   email.value = (e.currentTarget as HTMLInputElement).value;
                   saveEmail(email.value);
                 }} />
        </label>
        <label>
          <span class="input-label">{t("login.password")}</span>
          <input id="login-password" name="password" class="input-field" type="password" autocomplete="current-password" required
                 value={password.value}
                 onInput={e => { password.value = (e.currentTarget as HTMLInputElement).value; }} />
        </label>
        {error.value ? <div class="form-error">{error.value}</div> : null}
        <button type="submit" class="btn-primary" disabled={busy.value}
                onClick={submit}>
          {busy.value ? t("login.signingIn") : t("login.submit")}
        </button>
        <div class="hint">
          {t("login.installHint")}
          <button class="link-button login-help-link" type="button" onClick={onHelp}>{t("home.cta.install")}</button>
        </div>
      </form>
    </div>
  );
}
