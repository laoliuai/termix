import { t } from "../i18n/store";

export interface HelpPageProps {
  onBack: () => void;
}

const installCommand = "curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh";

export function HelpPage({ onBack }: HelpPageProps) {
  return (
    <main class="help-page">
      <header class="help-header">
        <button class="icon help-back" type="button" aria-label={t("common.back")} onClick={onBack}>{"<"}</button>
        <div>
          <p class="help-kicker">{t("help.kicker")}</p>
          <h1>{t("help.title")}</h1>
        </div>
      </header>

      <section class="help-section">
        <h2>{t("help.download")}</h2>
        <div class="download-grid">
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Darwin_arm64.tar.gz">
            <span class="download-title">macOS Apple Silicon</span>
            <span class="download-sub">termix_Darwin_arm64.tar.gz</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Darwin_x86_64.tar.gz">
            <span class="download-title">macOS Intel</span>
            <span class="download-sub">termix_Darwin_x86_64.tar.gz</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Linux_x86_64.tar.gz">
            <span class="download-title">Ubuntu x86_64</span>
            <span class="download-sub">termix_Linux_x86_64.tar.gz</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Linux_arm64.tar.gz">
            <span class="download-title">Ubuntu arm64</span>
            <span class="download-sub">termix_Linux_arm64.tar.gz</span>
          </a>
        </div>
      </section>

      <section class="help-section">
        <h2>{t("help.oneLine")}</h2>
        <pre class="command-block"><code>{installCommand}</code></pre>
      </section>

      <section class="help-section">
        <h2>{t("help.startSession")}</h2>
        <ol class="help-steps">
          <li><code>termix login</code></li>
          <li><code>termix start codex --name main</code></li>
          <li>{t("home.cta.sessions")}</li>
        </ol>
      </section>

      <section class="help-section">
        <h2>{t("help.supportedTools")}</h2>
        <div class="tool-row">
          <span>claude</span>
          <span>codex</span>
          <span>opencode</span>
        </div>
        <p class="help-copy">{t("help.backgroundService")}</p>
      </section>

      <section class="help-section">
        <h2>{t("help.troubleshooting")}</h2>
        <ul class="help-list">
          <li><code>termix doctor</code></li>
          <li><code>~/.local/bin</code> on <code>PATH</code></li>
          <li><code>tmux</code> installed</li>
        </ul>
      </section>
    </main>
  );
}
