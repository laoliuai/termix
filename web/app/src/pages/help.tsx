export interface HelpPageProps {
  onBack: () => void;
}

const installCommand = "curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh";

export function HelpPage({ onBack }: HelpPageProps) {
  return (
    <main class="help-page">
      <header class="help-header">
        <button class="icon help-back" type="button" aria-label="Back" onClick={onBack}>{"<"}</button>
        <div>
          <p class="help-kicker">Host client</p>
          <h1>Install Termix</h1>
        </div>
      </header>

      <section class="help-section">
        <h2>Download</h2>
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
        <h2>One-line install</h2>
        <pre class="command-block"><code>{installCommand}</code></pre>
        <p class="help-copy">The installer detects your platform, downloads the matching GitHub release, and installs <code>termix</code> into <code>~/.local/bin</code>.</p>
      </section>

      <section class="help-section">
        <h2>Start a session</h2>
        <ol class="help-steps">
          <li><code>termix login</code></li>
          <li><code>termix start codex --name laoliu-codex-termix</code></li>
          <li>Return to this Web UI to view or control the running session.</li>
        </ol>
      </section>

      <section class="help-section">
        <h2>Supported tools</h2>
        <div class="tool-row">
          <span>claude</span>
          <span>codex</span>
          <span>opencode</span>
        </div>
        <p class="help-copy">Termix starts its local background service automatically when you create or attach a session.</p>
      </section>

      <section class="help-section">
        <h2>Troubleshooting</h2>
        <ul class="help-list">
          <li>Run <code>termix doctor</code>.</li>
          <li>Confirm <code>~/.local/bin</code> is on your <code>PATH</code>.</li>
          <li>Confirm <code>tmux</code> is installed on the host.</li>
        </ul>
      </section>
    </main>
  );
}
