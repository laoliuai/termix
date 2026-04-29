# Single Binary Install Help Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the first user-facing install/onboarding flow where users install one `termix` binary, run `termix login`, then run `termix start <tool> --name <name>` without seeing `termixd`.

**Architecture:** Keep the daemon architecture, but move it behind an internal `termix __daemon` mode. Add a root install script that downloads GitHub release artifacts into `~/.local/bin`, then add a Web help page that explains the install and session-start flow.

**Tech Stack:** Go CLI/daemon, POSIX shell install script, Preact/Vite Web UI, Vitest, Go tests, Makefile release packaging.

**Execution Rule:** Implement this plan with subagent-driven development in a project-local git worktree under `.worktrees/`. Do not implement directly in the main checkout.

---

## File Structure

- Create: `go/internal/hostdaemon/daemon.go`
  - Owns the daemon runtime currently embedded in `go/cmd/termixd/main.go`.
  - Exposes `Run(ctx context.Context, paths config.HostPaths) error`.
- Modify: `go/cmd/termixd/main.go`
  - Keeps a developer/back-compat daemon binary that delegates to `hostdaemon.Run`.
- Modify: `go/cmd/termix/main.go`
  - Adds `termix --version`, `termix version`, and hidden `termix __daemon`.
  - Makes `termix start` launch the current executable with `__daemon` instead of launching `termixd`.
  - Adds preflight login checks and supported-tool validation.
- Modify: `go/cmd/termix/main_test.go`
  - Adds tests for hidden daemon mode, version output, daemon launch command, login preflight, and supported-tool validation.
- Create: `install.sh`
  - POSIX shell installer. Detects OS/arch, downloads GitHub release artifact, installs `termix`.
- Create: `scripts/test-install.sh`
  - Shell test harness for `install.sh` helper functions.
- Modify: `Makefile`
  - Adds `test-install` and `package-client-release` targets.
- Create: `web/app/src/pages/help.tsx`
  - Help/download page.
- Create: `web/app/src/pages/help.test.tsx`
  - Tests for install command, platform options, and CLI flow.
- Modify: `web/app/src/routes/Router.tsx`
  - Adds `/help` route and navigation callbacks.
- Modify: `web/app/src/pages/login.tsx`
  - Adds a clear link to `/help`.
- Modify: `web/app/src/components/header.tsx`
  - Adds a Help menu item for authenticated screens.
- Modify: `web/app/src/components/header.test.tsx`
  - Adds menu test for Help callback.
- Modify: `web/app/src/theme/styles.css`
  - Styles help page using the existing restrained product UI.
- Modify: `README.md`
  - Documents the one-line install and the normal host flow.
- Modify: `docs/PROGRESS.md`
  - Marks tasks as in progress/completed before reporting completion.

## Scope Check

This spec touches CLI packaging, install script, and Web onboarding. They are planned together because each piece is required for the single product flow to work end to end, but every task below is independently testable and commit-sized.

---

### Task 1: Extract Host Daemon Runtime

**Files:**
- Create: `go/internal/hostdaemon/daemon.go`
- Modify: `go/cmd/termixd/main.go`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Single-binary install/help implementation: extract host daemon runtime into an internal package.
```

- [ ] **Step 2: Create the daemon package**

Create `go/internal/hostdaemon/daemon.go` with this content. This is the current `termixd` runtime converted from `log.Fatal` exits to returned errors.

```go
package hostdaemon

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/diagnostics"
	"github.com/termix/termix/go/internal/relayclient"
	"github.com/termix/termix/go/internal/session"
	"github.com/termix/termix/go/internal/tmux"
)

// Run starts the local Termix daemon and blocks serving the local daemon IPC.
func Run(ctx context.Context, paths config.HostPaths) error {
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		return err
	}

	socketPath := daemonipc.SocketPath(paths)
	listener, err := daemonipc.Listen(socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()

	doctor := diagnostics.NewRunner(paths)
	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		return err
	}
	creds, err := credentials.Load(paths.CredentialsFile)
	if err != nil {
		return err
	}

	controlClient, err := controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
	if err != nil {
		return err
	}
	refresher := credentials.NewRefresher(
		paths.CredentialsFile,
		&controlRefreshAdapter{client: controlClient},
		nil,
	)

	freshCreds, err := refresher.EnsureFresh(ctx)
	if err != nil {
		return err
	}

	relayClient := relayclient.New(cfg.RelayWSURL, freshCreds.AccessToken, freshCreds.DeviceID)
	if err := relayClient.Connect(ctx); err != nil {
		return err
	}

	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(paths.StateDir),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return refresher.EnsureFresh(context.Background())
		},
		RefreshCredentials: refresher.RefreshNow,
		IsAuthError:        isControlAuthError,
		Control:            controlClient,
		NewControl: func(creds credentials.StoredCredentials) (session.ControlClient, error) {
			return controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
		},
		Tmux:  tmux.NewRunner(),
		Relay: relayClient,
		Snapshot: func(ctx context.Context, sessionName string) ([]byte, error) {
			return tmux.CaptureSnapshot(ctx, sessionName)
		},
		Input: func(ctx context.Context, sessionName string, payload []byte) error {
			return tmux.InjectInput(ctx, sessionName, payload)
		},
		Now:      time.Now,
		Hostname: os.Hostname,
		DoctorChecks: func(ctx context.Context) ([]string, error) {
			return doctor.Checks(ctx)
		},
		OutputFifoDir: filepath.Join(paths.RunDir, "output-fifos"),
	})

	go func() {
		if err := manager.Reap(context.Background()); err != nil {
			log.Printf("reap: initial sweep failed: %v", err)
		}
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			if err := manager.Reap(context.Background()); err != nil {
				log.Printf("reap: periodic sweep failed: %v", err)
			}
		}
	}()

	server := daemonipc.NewServer(manager)
	return server.Serve(listener)
}

type controlRefreshAdapter struct {
	client *controlapi.Client
}

func (a *controlRefreshAdapter) RefreshAccessToken(ctx context.Context, refreshToken string) (*credentials.RefreshResult, error) {
	res, err := a.client.RefreshAccessToken(ctx, refreshToken)
	if err != nil {
		if isControlAuthError(err) {
			return nil, credentials.ErrReLoginRequired
		}
		return nil, err
	}
	return &credentials.RefreshResult{
		AccessToken:      res.AccessToken,
		ExpiresInSeconds: res.ExpiresInSeconds,
	}, nil
}

func isControlAuthError(err error) bool {
	var ae *controlapi.APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusUnauthorized
}
```

- [ ] **Step 3: Replace `termixd` main with a thin wrapper**

Replace `go/cmd/termixd/main.go` with:

```go
package main

import (
	"context"
	"log"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/hostdaemon"
)

func main() {
	if err := hostdaemon.Run(context.Background(), config.DefaultHostPaths()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Run focused Go tests**

Run:

```bash
cd go && go test ./cmd/termixd ./internal/session ./tests -run 'TestManager|TestDaemon' -count=1
```

Expected: packages compile; matching tests pass or report "no tests to run" for packages without focused tests.

- [ ] **Step 5: Commit**

```bash
git add go/internal/hostdaemon/daemon.go go/cmd/termixd/main.go docs/PROGRESS.md
git commit -m "Extract host daemon runtime"
```

---

### Task 2: Add Hidden Daemon Mode and Single-Binary Launch

**Files:**
- Modify: `go/cmd/termix/main.go`
- Modify: `go/cmd/termix/main_test.go`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Single-binary install/help implementation: wire `termix __daemon` and same-binary daemon launch.
```

- [ ] **Step 2: Write failing CLI tests**

Add these tests to `go/cmd/termix/main_test.go` after `TestRunDoctorPrintsChecks`:

```go
func TestRunVersionPrintsVersion(t *testing.T) {
	oldVersion := version
	version = "v9.8.7-test"
	defer func() { version = oldVersion }()

	deps := testDeps(testPaths(t))
	var stdout bytes.Buffer
	deps.stdout = &stdout

	code := run(context.Background(), []string{"termix", "--version"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if strings.TrimSpace(stdout.String()) != "termix v9.8.7-test" {
		t.Fatalf("unexpected version output %q", stdout.String())
	}
}

func TestRunHiddenDaemonUsesInternalRunner(t *testing.T) {
	paths := testPaths(t)
	deps := testDeps(paths)
	called := false
	deps.runDaemon = func(_ context.Context, got config.HostPaths) error {
		called = true
		if got.RunDir != paths.RunDir {
			t.Fatalf("expected daemon paths to be passed through, got %q", got.RunDir)
		}
		return nil
	}

	code := run(context.Background(), []string{"termix", "__daemon"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !called {
		t.Fatal("expected hidden daemon runner to be called")
	}
}

func TestDaemonCommandUsesCurrentExecutableHiddenMode(t *testing.T) {
	cmd := daemonCommand(context.Background(), "/opt/termix/bin/termix")
	if cmd.Path != "/opt/termix/bin/termix" {
		t.Fatalf("expected command path to be termix executable, got %q", cmd.Path)
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "__daemon" {
		t.Fatalf("expected hidden daemon args, got %#v", cmd.Args)
	}
}

func TestRunStartRequiresLoginBeforeLaunchingDaemon(t *testing.T) {
	deps := testDeps(testPaths(t))
	deps.launchDaemon = func(context.Context, config.HostPaths) error {
		t.Fatal("launchDaemon should not be called when login files are missing")
		return nil
	}
	var stderr bytes.Buffer
	deps.stderr = &stderr

	code := run(context.Background(), []string{"termix", "start", "codex", "--name", "demo"}, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Not logged in. Run: termix login") {
		t.Fatalf("expected login hint, got %q", stderr.String())
	}
}

func TestRunStartRejectsUnsupportedTool(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	deps := testDeps(paths)
	var stderr bytes.Buffer
	deps.stderr = &stderr

	code := run(context.Background(), []string{"termix", "start", "vim"}, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), `unsupported tool "vim"; expected claude, codex, or opencode`) {
		t.Fatalf("expected unsupported tool message, got %q", stderr.String())
	}
}
```

Add this helper near `testPaths`:

```go
func writeLoggedInHostFiles(t *testing.T, paths config.HostPaths) {
	t.Helper()
	if err := credentials.Save(paths.CredentialsFile, credentials.StoredCredentials{
		ServerBaseURL: "https://termix.example.com",
		UserID:        "user-1",
		DeviceID:      "device-1",
		AccessToken:   "access-token",
		RefreshToken:  "refresh-token",
		ExpiresAt:     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("Save credentials returned error: %v", err)
	}
	cfg, err := config.DeriveHostConfig("https://termix.example.com")
	if err != nil {
		t.Fatalf("DeriveHostConfig returned error: %v", err)
	}
	if err := config.SaveHostConfig(paths.HostConfigFile, cfg); err != nil {
		t.Fatalf("SaveHostConfig returned error: %v", err)
	}
}
```

Add this field to `fakeDaemonClient` interface coverage if the compiler asks for it:

```go
func (f *fakeDaemonClient) ListSessions(context.Context, *daemonv1.ListSessionsRequest, ...grpc.CallOption) (*daemonv1.ListSessionsResponse, error) {
	return &daemonv1.ListSessionsResponse{}, nil
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
cd go && go test ./cmd/termix -run 'TestRunVersionPrintsVersion|TestRunHiddenDaemonUsesInternalRunner|TestDaemonCommandUsesCurrentExecutableHiddenMode|TestRunStartRequiresLoginBeforeLaunchingDaemon|TestRunStartRejectsUnsupportedTool' -count=1
```

Expected: FAIL because `version`, `runDaemon`, `daemonCommand`, login preflight, or tool validation is missing.

- [ ] **Step 4: Implement hidden daemon mode and version output**

Modify `go/cmd/termix/main.go`:

Add the host daemon import:

```go
	"github.com/termix/termix/go/internal/hostdaemon"
```

Add a package variable after imports:

```go
var version = "dev"
```

Add this field to `cliDeps`:

```go
	runDaemon        func(ctx context.Context, paths config.HostPaths) error
```

Set it in `defaultDeps()`:

```go
		runDaemon: hostdaemon.Run,
```

Update `run` switch:

```go
	case "--version", "version":
		err = runVersion(deps)
	case "__daemon":
		err = runDaemon(ctx, deps)
```

Add these functions near `runDoctor`:

```go
func runVersion(deps cliDeps) error {
	fmt.Fprintf(deps.stdout, "termix %s\n", version)
	return nil
}

func runDaemon(ctx context.Context, deps cliDeps) error {
	if deps.runDaemon == nil {
		return errors.New("daemon runner is not available")
	}
	return deps.runDaemon(ctx, deps.paths)
}
```

Keep the usage text user-facing:

```go
fmt.Fprintln(deps.stderr, "usage: termix <login|start|sessions|doctor|version>")
```

- [ ] **Step 5: Implement same-binary daemon launch**

Replace `launchDaemonProcess` and add `daemonCommand`:

```go
func launchDaemonProcess(ctx context.Context, paths config.HostPaths) error {
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := daemonCommand(ctx, executable)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func daemonCommand(ctx context.Context, executable string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, "__daemon")
}
```

- [ ] **Step 6: Implement login preflight and supported-tool validation**

At the top of `runStart`, after parsing args and before `ensureDaemon`, add:

```go
	if err := ensureLoggedIn(deps); err != nil {
		return err
	}
```

Add:

```go
func ensureLoggedIn(deps cliDeps) error {
	if _, err := credentials.Load(deps.paths.CredentialsFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Not logged in. Run: termix login")
		}
		return err
	}
	if _, err := config.LoadHostConfig(deps.paths.HostConfigFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Not logged in. Run: termix login")
		}
		return err
	}
	return nil
}

func isSupportedTool(tool string) bool {
	switch tool {
	case "claude", "codex", "opencode":
		return true
	default:
		return false
	}
}
```

In `parseStartArgs`, after `tool := args[0]`, add:

```go
	if !isSupportedTool(tool) {
		return "", "", fmt.Errorf("unsupported tool %q; expected claude, codex, or opencode", tool)
	}
```

Update existing `TestRunStartLaunchesDaemonAndAttachesSession` to call:

```go
	writeLoggedInHostFiles(t, paths)
```

immediately after `paths := testPaths(t)`.

Update `testDeps` to include:

```go
		runDaemon: func(context.Context, config.HostPaths) error {
			return nil
		},
```

- [ ] **Step 7: Run focused and full CLI tests**

Run:

```bash
cd go && go test ./cmd/termix -count=1
```

Expected: PASS.

Run:

```bash
cd go && go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add go/cmd/termix/main.go go/cmd/termix/main_test.go docs/PROGRESS.md
git commit -m "Run host daemon from termix binary"
```

---

### Task 3: Add Release Packaging for Client Artifacts

**Files:**
- Modify: `Makefile`
- Create: `scripts/package-termix-release.sh`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Single-binary install/help implementation: add client release artifact packaging.
```

- [ ] **Step 2: Create packaging script**

Create `scripts/package-termix-release.sh`:

```sh
#!/usr/bin/env sh
set -eu

VERSION="${VERSION:-dev}"
OUT_DIR="${OUT_DIR:-dist/release}"
ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

mkdir -p "$ROOT_DIR/$OUT_DIR"

package_one() {
  os="$1"
  arch="$2"
  artifact_arch="$3"
  work_dir="$ROOT_DIR/$OUT_DIR/termix_${os}_${artifact_arch}"
  archive="$ROOT_DIR/$OUT_DIR/termix_${os}_${artifact_arch}.tar.gz"

  rm -rf "$work_dir"
  mkdir -p "$work_dir"

  GOOS="$(printf '%s' "$os" | tr '[:upper:]' '[:lower:]')" \
  GOARCH="$arch" \
  go build -ldflags "-X main.version=$VERSION" -o "$work_dir/termix" ./cmd/termix

  cp "$ROOT_DIR/README.md" "$work_dir/README.md"
  if [ -f "$ROOT_DIR/LICENSE" ]; then
    cp "$ROOT_DIR/LICENSE" "$work_dir/LICENSE"
  fi

  tar -C "$work_dir" -czf "$archive" .
  rm -rf "$work_dir"
  printf 'wrote %s\n' "$archive"
}

cd "$ROOT_DIR/go"
package_one Darwin amd64 x86_64
package_one Darwin arm64 arm64
package_one Linux amd64 x86_64
package_one Linux arm64 arm64
```

- [ ] **Step 3: Make script executable**

Run:

```bash
chmod +x scripts/package-termix-release.sh
```

- [ ] **Step 4: Add Makefile target**

Append to `.PHONY`:

```make
package-client-release
```

Add target:

```make
package-client-release:
	./scripts/package-termix-release.sh
```

- [ ] **Step 5: Run packaging target**

Run:

```bash
VERSION=v0.0.0-test make package-client-release
```

Expected:

```text
wrote .../dist/release/termix_Darwin_x86_64.tar.gz
wrote .../dist/release/termix_Darwin_arm64.tar.gz
wrote .../dist/release/termix_Linux_x86_64.tar.gz
wrote .../dist/release/termix_Linux_arm64.tar.gz
```

- [ ] **Step 6: Inspect one archive**

Run:

```bash
tar -tzf dist/release/termix_Linux_x86_64.tar.gz
```

Expected output includes:

```text
./termix
./README.md
```

If the repository has a `LICENSE` file at execution time, expected output also includes:

```text
./LICENSE
```

- [ ] **Step 7: Commit**

```bash
git add Makefile scripts/package-termix-release.sh docs/PROGRESS.md
git commit -m "Package single termix client artifacts"
```

---

### Task 4: Add Root Install Script and Shell Tests

**Files:**
- Create: `install.sh`
- Create: `scripts/test-install.sh`
- Modify: `Makefile`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Single-binary install/help implementation: add one-line GitHub release installer.
```

- [ ] **Step 2: Create install script**

Create `install.sh`:

```sh
#!/usr/bin/env sh
set -eu

TERMIX_REPO="${TERMIX_REPO:-termix/termix}"
TERMIX_VERSION="${TERMIX_VERSION:-latest}"
TERMIX_INSTALL_DIR="${TERMIX_INSTALL_DIR:-$HOME/.local/bin}"

normalize_os() {
  case "$1" in
    Darwin|darwin) printf 'Darwin' ;;
    Linux|linux) printf 'Linux' ;;
    *) printf 'unsupported' ;;
  esac
}

normalize_arch() {
  case "$1" in
    x86_64|amd64) printf 'x86_64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) printf 'unsupported' ;;
  esac
}

asset_name() {
  os="$(normalize_os "$1")"
  arch="$(normalize_arch "$2")"
  if [ "$os" = "unsupported" ] || [ "$arch" = "unsupported" ]; then
    printf 'unsupported'
    return
  fi
  printf 'termix_%s_%s.tar.gz' "$os" "$arch"
}

download_url() {
  repo="$1"
  version="$2"
  asset="$3"
  if [ "$version" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download/%s' "$repo" "$asset"
  else
    printf 'https://github.com/%s/releases/download/%s/%s' "$repo" "$version" "$asset"
  fi
}

path_contains_dir() {
  dir="$1"
  old_ifs="$IFS"
  IFS=:
  for item in $PATH; do
    if [ "$item" = "$dir" ]; then
      IFS="$old_ifs"
      return 0
    fi
  done
  IFS="$old_ifs"
  return 1
}

main() {
  os="$(normalize_os "$(uname -s)")"
  arch="$(normalize_arch "$(uname -m)")"
  if [ "$os" = "unsupported" ]; then
    echo "unsupported operating system: $(uname -s)" >&2
    exit 1
  fi
  if [ "$arch" = "unsupported" ]; then
    echo "unsupported CPU architecture: $(uname -m)" >&2
    exit 1
  fi

  asset="$(asset_name "$os" "$arch")"
  url="$(download_url "$TERMIX_REPO" "$TERMIX_VERSION" "$asset")"
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT INT TERM

  echo "Downloading $url"
  curl -fsSL "$url" -o "$tmp_dir/$asset"
  tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
  if [ ! -f "$tmp_dir/termix" ]; then
    echo "downloaded archive does not contain termix" >&2
    exit 1
  fi

  mkdir -p "$TERMIX_INSTALL_DIR"
  cp "$tmp_dir/termix" "$TERMIX_INSTALL_DIR/termix"
  chmod +x "$TERMIX_INSTALL_DIR/termix"

  echo "Installed termix to $TERMIX_INSTALL_DIR/termix"
  "$TERMIX_INSTALL_DIR/termix" --version

  if ! path_contains_dir "$TERMIX_INSTALL_DIR"; then
    echo ""
    echo "Add Termix to your PATH:"
    echo "  export PATH=\"$TERMIX_INSTALL_DIR:\$PATH\""
  fi
}

if [ "${TERMIX_INSTALL_TEST:-0}" != "1" ]; then
  main "$@"
fi
```

- [ ] **Step 3: Create shell tests**

Create `scripts/test-install.sh`:

```sh
#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TERMIX_INSTALL_TEST=1 . "$ROOT_DIR/install.sh"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

assert_eq() {
  actual="$1"
  expected="$2"
  label="$3"
  if [ "$actual" != "$expected" ]; then
    fail "$label: expected '$expected', got '$actual'"
  fi
}

assert_eq "$(normalize_os Darwin)" "Darwin" "darwin os"
assert_eq "$(normalize_os Linux)" "Linux" "linux os"
assert_eq "$(normalize_os FreeBSD)" "unsupported" "unsupported os"

assert_eq "$(normalize_arch x86_64)" "x86_64" "x86_64 arch"
assert_eq "$(normalize_arch amd64)" "x86_64" "amd64 arch"
assert_eq "$(normalize_arch arm64)" "arm64" "arm64 arch"
assert_eq "$(normalize_arch aarch64)" "arm64" "aarch64 arch"
assert_eq "$(normalize_arch riscv64)" "unsupported" "unsupported arch"

assert_eq "$(asset_name Darwin x86_64)" "termix_Darwin_x86_64.tar.gz" "darwin x86 asset"
assert_eq "$(asset_name Linux aarch64)" "termix_Linux_arm64.tar.gz" "linux arm asset"
assert_eq "$(asset_name Plan9 x86_64)" "unsupported" "unsupported asset"

assert_eq "$(download_url termix/termix latest termix_Linux_x86_64.tar.gz)" \
  "https://github.com/termix/termix/releases/latest/download/termix_Linux_x86_64.tar.gz" \
  "latest url"

assert_eq "$(download_url termix/termix v0.1.0 termix_Linux_x86_64.tar.gz)" \
  "https://github.com/termix/termix/releases/download/v0.1.0/termix_Linux_x86_64.tar.gz" \
  "version url"

old_path="$PATH"
PATH="/usr/bin:/bin"
if path_contains_dir "$HOME/.local/bin"; then
  fail "path_contains_dir returned true for absent dir"
fi
PATH="$HOME/.local/bin:/usr/bin:/bin"
if ! path_contains_dir "$HOME/.local/bin"; then
  fail "path_contains_dir returned false for present dir"
fi
PATH="$old_path"

echo "install.sh helper tests passed"
```

- [ ] **Step 4: Make scripts executable**

Run:

```bash
chmod +x install.sh scripts/test-install.sh
```

- [ ] **Step 5: Add Makefile target**

Append to `.PHONY`:

```make
test-install
```

Add target:

```make
test-install:
	./scripts/test-install.sh
```

- [ ] **Step 6: Run shell tests**

Run:

```bash
make test-install
```

Expected:

```text
install.sh helper tests passed
```

- [ ] **Step 7: Commit**

```bash
git add install.sh scripts/test-install.sh Makefile docs/PROGRESS.md
git commit -m "Add one-line client installer"
```

---

### Task 5: Add Web Help and Download Page

**Files:**
- Create: `web/app/src/pages/help.tsx`
- Create: `web/app/src/pages/help.test.tsx`
- Modify: `web/app/src/routes/Router.tsx`
- Modify: `web/app/src/pages/login.tsx`
- Modify: `web/app/src/components/header.tsx`
- Modify: `web/app/src/components/header.test.tsx`
- Modify: `web/app/src/theme/styles.css`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Single-binary install/help implementation: add Web help/download page.
```

- [ ] **Step 2: Write failing Help page tests**

Create `web/app/src/pages/help.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/preact";

import { HelpPage } from "./help";

describe("HelpPage", () => {
  it("shows install command and platform downloads", () => {
    render(<HelpPage onBack={() => {}} />);

    expect(screen.getByRole("heading", { name: "Install Termix" })).toBeTruthy();
    expect(screen.getByText("macOS")).toBeTruthy();
    expect(screen.getByText("Ubuntu")).toBeTruthy();
    expect(screen.getByText(/curl -fsSL https:\/\/raw\.githubusercontent\.com\/termix\/termix\/main\/install\.sh \| sh/)).toBeTruthy();
  });

  it("shows the host workflow without mentioning termixd", () => {
    const { container } = render(<HelpPage onBack={() => {}} />);

    expect(screen.getByText("termix login")).toBeTruthy();
    expect(screen.getByText("termix start codex --name laoliu-codex-termix")).toBeTruthy();
    expect(screen.getByText("claude")).toBeTruthy();
    expect(screen.getByText("codex")).toBeTruthy();
    expect(screen.getByText("opencode")).toBeTruthy();
    expect(container.textContent).not.toContain("termixd");
  });

  it("calls onBack from the back button", () => {
    const onBack = vi.fn();
    render(<HelpPage onBack={onBack} />);

    fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(onBack).toHaveBeenCalledOnce();
  });
});
```

Add this test to `web/app/src/components/header.test.tsx`:

```tsx
import { fireEvent, screen } from "@testing-library/preact";
```

If the file already imports from `@testing-library/preact`, merge the import into one line:

```tsx
import { render, fireEvent, screen } from "@testing-library/preact";
```

Then add:

```tsx
  it("opens Help from the menu", () => {
    const onHelp = vi.fn();
    render(<Header onHelp={onHelp} />);

    fireEvent.click(screen.getByRole("button", { name: "menu" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Help" }));

    expect(onHelp).toHaveBeenCalledOnce();
  });
```

Add this import at the top of `header.test.tsx`:

```tsx
import { vi } from "vitest";
```

If the `vitest` import already exists, merge `vi` into it.

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
cd web/app && npm test -- --run src/pages/help.test.tsx src/components/header.test.tsx
```

Expected: FAIL because `HelpPage` and `Header.onHelp` do not exist.

- [ ] **Step 4: Create Help page component**

Create `web/app/src/pages/help.tsx`:

```tsx
export interface HelpPageProps {
  onBack: () => void;
}

const installCommand = "curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh";

export function HelpPage({ onBack }: HelpPageProps) {
  return (
    <main class="help-page">
      <header class="help-header">
        <button class="icon help-back" type="button" aria-label="Back" onClick={onBack}>‹</button>
        <div>
          <p class="help-kicker">Host client</p>
          <h1>Install Termix</h1>
        </div>
      </header>

      <section class="help-section">
        <h2>Download</h2>
        <div class="download-grid">
          <a class="download-option" href="https://github.com/termix/termix/releases/latest">
            <span class="download-title">macOS</span>
            <span class="download-sub">Apple Silicon and Intel</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest">
            <span class="download-title">Ubuntu</span>
            <span class="download-sub">Linux x86_64 and arm64</span>
          </a>
        </div>
      </section>

      <section class="help-section">
        <h2>One-line install</h2>
        <pre class="command-block"><code>{installCommand}</code></pre>
        <p class="help-copy">The installer detects your platform, downloads the matching release from GitHub, and installs <code>termix</code> into <code>~/.local/bin</code>.</p>
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
```

- [ ] **Step 5: Wire route and navigation**

Modify `web/app/src/routes/Router.tsx`:

```tsx
import { HelpPage } from "../pages/help";
```

Update `LoginRoute`:

```tsx
const LoginRoute = (_props: { path?: string }) => (
  <LoginPage
    onSuccess={() => route("/sessions", true)}
    onHelp={() => route("/help")}
  />
);
```

Update `SessionsPage` usage:

```tsx
      onHelp={() => route("/help")}
```

Add:

```tsx
const HelpRoute = (_props: { path?: string }) => (
  <HelpPage onBack={() => route(accessToken.value ? "/sessions" : "/", true)} />
);
```

Add `accessToken` import:

```tsx
import { accessToken, clearAuth } from "../auth/store";
```

Replace the existing `clearAuth` import line with the combined import above.

Add route:

```tsx
      <HelpRoute path="/help" />
```

- [ ] **Step 6: Add Login help link**

Modify `LoginPageProps` in `web/app/src/pages/login.tsx`:

```tsx
export interface LoginPageProps {
  onSuccess: () => void;
  onHelp: () => void;
}
```

Modify function signature:

```tsx
export function LoginPage({ onSuccess, onHelp }: LoginPageProps) {
```

Replace the hint:

```tsx
        <div class="hint">
          Sessions are created from your host with <code>termix start</code>.
          <button class="link-button" type="button" onClick={onHelp}>Install Termix</button>
        </div>
```

Update every `render(<LoginPage onSuccess={...} />)` in `web/app/src/pages/login.test.tsx` to pass:

```tsx
onHelp={() => {}}
```

- [ ] **Step 7: Add Header help menu item**

Modify `HeaderProps` in `web/app/src/components/header.tsx`:

```tsx
  onHelp?: () => void;
```

Modify function signature:

```tsx
export function Header({ onLogout, onRefresh, onHelp, refreshing = false, refreshDone = false }: HeaderProps) {
```

Replace the menu rendering block:

```tsx
        {menuOpen.value && (onLogout || onHelp) ? (
          <div class="menu" role="menu">
            {onHelp ? <button role="menuitem" onClick={() => { menuOpen.value = false; onHelp(); }}>Help</button> : null}
            {onLogout ? <button role="menuitem" onClick={() => { menuOpen.value = false; onLogout(); }}>Logout</button> : null}
          </div>
        ) : null}
```

Modify `SessionsPageProps` in `web/app/src/pages/sessions.tsx`:

```tsx
  onHelp: () => void;
```

Modify function signature:

```tsx
export function SessionsPage({ onOpen, onLogout, onHelp }: SessionsPageProps) {
```

Pass the prop to `Header`:

```tsx
        onHelp={onHelp}
```

Update `web/app/src/pages/sessions.test.tsx` render helpers to pass `onHelp={() => {}}`.

- [ ] **Step 8: Add styles**

Append to `web/app/src/theme/styles.css`:

```css
.link-button {
  color: var(--accent);
  background: transparent;
  border: none;
  padding: 0;
  margin-left: 6px;
  font: inherit;
  font-weight: 700;
}

.help-page {
  min-height: 100%;
  max-width: 760px;
  margin: 0 auto;
  padding: 16px 16px calc(40px + env(safe-area-inset-bottom));
}

.help-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0 18px;
}

.help-back {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--card);
  color: var(--fg);
  font-size: 24px;
}

.help-kicker {
  color: var(--muted);
  font-size: 12px;
  text-transform: uppercase;
  margin: 0 0 3px;
  font-weight: 700;
}

.help-header h1 {
  margin: 0;
  font-size: 26px;
}

.help-section {
  border-top: 1px solid var(--border);
  padding: 18px 0;
}

.help-section h2 {
  margin: 0 0 12px;
  font-size: 17px;
}

.download-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 10px;
}

.download-option {
  display: flex;
  flex-direction: column;
  gap: 3px;
  text-decoration: none;
  color: var(--fg);
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 14px;
}

.download-title {
  font-weight: 700;
}

.download-sub,
.help-copy,
.help-list {
  color: var(--muted);
  font-size: 13px;
}

.command-block {
  overflow-x: auto;
  background: #111;
  color: #f2f2f2;
  padding: 12px;
  border-radius: 8px;
  font-size: 13px;
}

.help-steps,
.help-list {
  margin: 0;
  padding-left: 20px;
}

.help-steps li,
.help-list li {
  margin: 8px 0;
}

.tool-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tool-row span {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 8px 10px;
  font-family: ui-monospace, monospace;
  font-size: 13px;
}

@media (min-width: 700px) {
  .download-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
```

- [ ] **Step 9: Run Web checks**

Run:

```bash
cd web/app && npm test -- --run src/pages/help.test.tsx src/components/header.test.tsx src/pages/login.test.tsx src/pages/sessions.test.tsx
```

Expected: PASS.

Run:

```bash
cd web/app && npm run typecheck && npm test -- --run && npm run build
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add web/app/src/pages/help.tsx web/app/src/pages/help.test.tsx web/app/src/routes/Router.tsx web/app/src/pages/login.tsx web/app/src/pages/login.test.tsx web/app/src/components/header.tsx web/app/src/components/header.test.tsx web/app/src/pages/sessions.tsx web/app/src/pages/sessions.test.tsx web/app/src/theme/styles.css docs/PROGRESS.md
git commit -m "Add Web client install help page"
```

---

### Task 6: Update Docs, Embedded Assets, and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/PROGRESS.md`
- Modify: `go/internal/controlapi/web_dist/**`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Single-binary install/help implementation: update docs, embedded assets, and final verification.
```

- [ ] **Step 2: Update README**

Replace `README.md` with:

```markdown
## termix

**Anywhere, Anytime, Your PC Terminal, Reimagined.**

Termix lets you view and control AI coding CLI sessions from a mobile or desktop browser while the real session keeps running on your Mac or Ubuntu host.

### Install the host client

```bash
curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh
```

The installer detects macOS or Ubuntu/Linux, downloads the matching GitHub release, and installs the single `termix` binary into `~/.local/bin`.

If `~/.local/bin` is not on your `PATH`, add:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Start a session

Log in once:

```bash
termix login
```

Start an AI coding session from a project directory:

```bash
termix start codex --name laoliu-codex-termix
```

Supported tools:

- `claude`
- `codex`
- `opencode`

Termix starts its local background service automatically. You do not need to run a separate daemon command.

### Diagnose host setup

```bash
termix doctor
```
```

- [ ] **Step 3: Regenerate embedded Web assets**

Run:

```bash
make build-web
make check-web-dist
```

Expected: both commands pass and `go/internal/controlapi/web_dist` is updated with the `/help` route bundle.

- [ ] **Step 4: Run final verification**

Run:

```bash
make test-install
```

Expected:

```text
install.sh helper tests passed
```

Run:

```bash
cd go && go test ./... -count=1
```

Expected: PASS.

Run:

```bash
cd go && go vet ./...
```

Expected: PASS.

Run:

```bash
cd web/app && npm run typecheck && npm test -- --run && npm run build
```

Expected: PASS.

Run:

```bash
VERSION=v0.0.0-test make package-client-release
```

Expected: the four release archives are written under `dist/release/`.

Run:

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 5: Update progress ledger**

Move the three pending Product UX items to Completed or replace them with one completed entry:

```markdown
- [x] Implement single-binary install/help product flow: `termix` now owns hidden daemon mode, install script defaults to `~/.local/bin`, release packaging produces macOS/Ubuntu artifacts, and Web help page documents install/login/start.
```

Leave follow-up production deployment work visible if release automation is not created in this slice:

```markdown
- [ ] Release automation: publish `dist/release/termix_*` artifacts to GitHub Releases from CI when tags are created.
```

- [ ] **Step 6: Commit**

```bash
git add README.md docs/PROGRESS.md go/internal/controlapi/web_dist
git commit -m "Document single-binary install flow"
```

---

## Final Review Checklist

- [ ] `termix --version` works.
- [ ] `termix start codex --name demo` launches `termix __daemon` when local IPC is unavailable.
- [ ] Normal usage/help output does not mention `__daemon` or `termixd`.
- [ ] `termix start vim` returns `unsupported tool "vim"; expected claude, codex, or opencode`.
- [ ] Missing login files return `Not logged in. Run: termix login`.
- [ ] `install.sh` defaults to `~/.local/bin`.
- [ ] `TERMIX_VERSION` and `TERMIX_INSTALL_DIR` are supported by the install script.
- [ ] `/help` is reachable while logged out and while logged in.
- [ ] `/help` does not mention `termixd`.
- [ ] Embedded Web assets are regenerated and pass `make check-web-dist`.
