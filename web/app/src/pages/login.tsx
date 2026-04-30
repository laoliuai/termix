import { useSignal } from "@preact/signals";
import { login } from "../api/endpoints";
import { accessToken, accessTokenExpiresAt, userInfo } from "../auth/store";

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
}

export function LoginPage({ onSuccess, onHelp }: LoginPageProps) {
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
          res.status === 401 ? "邮箱或密码错误" :
          res.status === 429 ? "尝试过于频繁，请稍候" :
          res.status === 0   ? "无法连接服务器" :
          (res.message || "登录失败");
        busy.value = false;
        return;
      }
      accessToken.value = res.data.access_token;
      accessTokenExpiresAt.value = Date.now() + res.data.expires_in_seconds * 1000;
      userInfo.value = { user: res.data.user, device: res.data.device };
      onSuccess();
    } catch {
      error.value = "无法连接服务器";
      busy.value = false;
    }
  };

  return (
    <div class="login-screen">
      <form class="login-page" onSubmit={submit}>
        <div class="brand-block">
          <div class="brand-glyph">{">_"}</div>
          <h1>Termix</h1>
          <p class="tagline">Remote control for your tmux sessions</p>
        </div>
        <label>
          <span class="input-label">Email</span>
          <input id="login-email" name="username" class="input-field" type="email" autocomplete="username" required
                 value={email.value}
                 onInput={e => {
                   email.value = (e.currentTarget as HTMLInputElement).value;
                   saveEmail(email.value);
                 }} />
        </label>
        <label>
          <span class="input-label">Password</span>
          <input id="login-password" name="password" class="input-field" type="password" autocomplete="current-password" required
                 value={password.value}
                 onInput={e => { password.value = (e.currentTarget as HTMLInputElement).value; }} />
        </label>
        {error.value ? <div class="form-error">{error.value}</div> : null}
        <button type="submit" class="btn-primary" disabled={busy.value}
                onClick={submit}>
          {busy.value ? "Signing in…" : "Sign in"}
        </button>
        <div class="hint">
          Sessions are created from your host with <code>termix start</code>.
          <button class="link-button" type="button" onClick={onHelp}>Install Termix</button>
        </div>
      </form>
    </div>
  );
}
