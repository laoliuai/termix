import { accessToken } from "../auth/store";
import { locale, setLocale, t } from "../i18n/store";
import { SiteFooter } from "../components/site-footer";
import { CommandBlock } from "../components/command-block";

export interface HomePageProps {
  onLogin: () => void;
  onHelp: () => void;
  onSessions: () => void;
}

const installCommand = "curl -fsSL https://termix.cloud/install.sh | sh";

export function HomePage({ onLogin, onHelp, onSessions }: HomePageProps) {
  const authed = accessToken.value !== null;
  const primaryAction = authed ? onSessions : onLogin;
  const primaryLabel = authed ? t("home.cta.sessions") : t("nav.signIn");
  const nextLocale = locale.value === "en" ? "zh-CN" : "en";

  return (
    <main class="home-page">
      <header class="home-nav">
        <div class="brand">
          <img class="brand-mark" src="/icons/termix.svg?v=tmx" alt="" />
          <span>{t("brand.name")}</span>
        </div>
        <div class="home-nav-actions">
          <button type="button" class="nav-link" onClick={onHelp}>{t("nav.help")}</button>
          <button type="button" class="nav-link" aria-label={t("nav.language")} onClick={() => setLocale(nextLocale)}>
            {locale.value === "en" ? "中文" : "EN"}
          </button>
          <button type="button" class="nav-primary" onClick={primaryAction}>{primaryLabel}</button>
        </div>
      </header>

      <section class="home-hero">
        <div class="home-hero-copy">
          <h1>{t("home.hero.title")}</h1>
          <p>{t("home.hero.subtitle")}</p>
          <div class="home-hero-actions">
            {authed ? (
              <button type="button" class="home-cta" onClick={onSessions}>{t("home.cta.sessions")}</button>
            ) : (
              <button type="button" class="home-cta" onClick={onHelp}>{t("home.cta.install")}</button>
            )}
            <button type="button" class="btn-secondary" onClick={onHelp}>{t("home.cta.help")}</button>
          </div>
          <CommandBlock className="home-command" command={installCommand} />
        </div>

        <div class="home-visual" aria-hidden="true">
          <div class="home-terminal-window">
            <div class="terminal-dots"><span></span><span></span><span></span></div>
            <div class="terminal-row dim">$ termix start codex --name main</div>
            <div class="terminal-row ok">session main connected</div>
            <div class="terminal-row">codex&gt; running repo analysis...</div>
            <div class="terminal-row dim">browser control lease: active</div>
          </div>
        </div>
      </section>

      <section class="home-values" aria-label={t("home.valuesAria")}>
        <article>
          <h2>{t("home.value.browser.title")}</h2>
          <p>{t("home.value.browser.body")}</p>
        </article>
        <article>
          <h2>{t("home.value.tmux.title")}</h2>
          <p>{t("home.value.tmux.body")}</p>
        </article>
        <article>
          <h2>{t("home.value.install.title")}</h2>
          <p>{t("home.value.install.body")}</p>
        </article>
      </section>

      <section class="home-install">
        <h2>{t("home.cta.install")}</h2>
        <CommandBlock className="home-command" command={installCommand} />
        <ol>
          <li><code>termix login</code></li>
          <li><code>termix start codex --name main</code></li>
          <li>{t("home.cta.sessions")}</li>
        </ol>
      </section>

      <SiteFooter />
    </main>
  );
}
