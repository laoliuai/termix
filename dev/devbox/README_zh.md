# Devbox

通用的 Docker 开发容器，仅包含**基础工具链**：Go 1.25、Node.js 22 LTS、Python 3.12 + `uv`、常用命令行工具（`git`、`tmux`、`ripgrep`、`jq`、`gh`、`vim` 等），以及三款 AI 编程助手 CLI（Claude Code、Codex、opencode）。镜像内不预装任何项目专用的 Go 或 Python 库——按需在容器内安装即可。

每个 AI 助手的状态目录（`~/.claude`、`~/.codex`、opencode 配置）存放在独立的 Docker 命名卷中，因此容器内运行的助手不会读取、也不会污染宿主机上已登录账号的配置。

## 首次配置

```bash
cd dev/devbox
cp .env.example .env                          # 可选；该文件主要用于覆盖 WORKSPACE_DIR
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose build
docker compose up -d
```

镜像在构建时会将宿主机的 UID/GID 写入 `dev` 用户，因此在绑定挂载的 `/workspace` 中写入的文件归属于你，而非 root。若宿主机 UID/GID 发生变化（例如将此目录复制到另一台机器），请重新运行 `docker compose build`。

## 日常使用

```bash
cd dev/devbox
export HOST_UID=$(id -u) HOST_GID=$(id -g)   # 若尚未导出
docker compose up -d                          # 幂等操作，已启动则无影响
docker compose exec devbox bash               # 进入 /workspace 下的 dev shell
```

在容器内：

```bash
cd /workspace
claude            # 首次启动会进入交互登录；凭证持久化到命名卷
codex
opencode
```

各 AI CLI 自行处理鉴权——通常通过交互式登录完成——并将凭证写入对应的命名卷。**无需**在 `.env` 里配置 `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`。如果某个工作流确实需要 API key，把它写进 `.env` 即可，`docker-compose.yml` 的 `env_file: .env` 会把它转发进容器。

## 版本验证

```bash
docker compose exec devbox bash -c '
  go version
  node -v
  python3.12 --version
  uv --version
  claude --version
  codex --version
  opencode --version
'
```

## 国内网络加速构建

Dockerfile 支持三个可选的构建参数，用于将上游源替换为国内镜像。默认保留国际源，镜像保持可移植性。

```bash
export HOST_UID=$(id -u) HOST_GID=$(id -g)
export APT_MIRROR=mirrors.tuna.tsinghua.edu.cn
export GOPROXY=https://goproxy.cn,direct
export NPM_REGISTRY=https://registry.npmmirror.com
docker compose build
docker compose up -d
```

compose 配置会将这些 shell 环境变量传递给构建参数。`GOPROXY` 同时作为运行时环境变量写入镜像，因此在容器内执行 `go install` 也会走镜像代理。

`gosu` 和 `gh` 直接从 `github.com` 的 releases 下载，curl 命令带有 `--retry`/`--speed-limit` 标志，短暂中断会自动重试，不会卡死构建。

## 跨重启暂停与恢复

```bash
docker compose stop          # 保留卷数据
docker compose start         # 恢复；助手状态、shell 历史、Go 缓存均保留
```

## 重置助手状态（不重建镜像）

```bash
docker compose down
docker volume rm \
  devbox_devbox-claude-home \
  devbox_devbox-codex-home \
  devbox_devbox-opencode-home
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose up -d
```

（Compose 会在卷名前加上项目名前缀，默认与目录同名：`devbox`。若不同，可通过 `docker volume ls` 查看实际名称。）

## 完全清理

```bash
docker compose down -v
```

此命令删除所有命名卷——助手历史、shell 历史及 Go 模块缓存将全部丢失。镜像本身保留，如需删除，执行 `docker rmi ai-devbox:latest`。

## 迁移到其他仓库

```bash
cp -r /path/to/this/repo/dev/devbox /path/to/new-repo/dev/devbox
cd /path/to/new-repo/dev/devbox
cp .env.example .env
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose up -d
```

若新仓库中 `dev/devbox/` 相对于项目根目录的层级不同，在 `.env` 中覆盖 `WORKSPACE_DIR`。默认值 `WORKSPACE_DIR=../..` 在 `dev/devbox/` 直接位于仓库根目录下时会正确解析。

## 隔离机制说明

- 宿主机的 `~/.claude` 和 `~/.codex` **永远不会**挂载到容器中。每个助手将会话状态写入 Docker 管理的命名卷（`devbox-claude-home`、`devbox-codex-home`、`devbox-opencode-home`）。
- 仓库根目录以绑定挂载方式映射到 `/workspace`，容器内的文件修改会立即反映到宿主机，反之亦然。
- 镜像中的 `dev` 用户在构建时根据 `HOST_UID`/`HOST_GID` 构建参数（来自 shell 环境变量）创建，镜像默认使用 `USER dev`，因此直接执行 `docker compose exec devbox bash` 即可进入 `dev` shell，绑定挂载的文件在宿主机上归属于你。运行时无 UID 重映射逻辑；若宿主机 UID/GID 变更，请使用新值重新运行 `docker compose build`。
- `.env`（通过 `env_file: .env` 转发）是将额外环境变量注入容器的位置；默认无任何必需项。

## 目录文件说明

| 文件 | 用途 |
|------|------|
| `Dockerfile` | 镜像构建：Ubuntu 22.04 → apt 包 → gosu → gh → Go → Node.js → Python+uv → AI 助手 CLI → dev 用户。 |
| `docker-compose.yml` | 长驻服务，绑定挂载 `/workspace`，用命名卷持久化助手状态和开发缓存。 |
| `.env.example` | `.env` 的模板文件。复制后编辑；切勿提交 `.env`。 |
| `.env`（已 gitignore） | 可选的本机覆盖项（如 `WORKSPACE_DIR`），以及任何想转发到容器的额外环境变量。 |
| `.gitignore` | 确保 `.env` 不被提交。 |
| `.dockerignore` | 将 `.env`、README 及 `.gitignore` 排除在构建上下文之外。 |
