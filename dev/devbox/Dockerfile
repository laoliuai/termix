FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC

# Optional apt mirror override for faster builds in regions where the default
# archive.ubuntu.com / security.ubuntu.com are slow. Default keeps the upstream
# mirrors so the image is portable; override at build time with e.g.
#   --build-arg APT_MIRROR=mirrors.tuna.tsinghua.edu.cn
ARG APT_MIRROR=archive.ubuntu.com
RUN if [ "$APT_MIRROR" != "archive.ubuntu.com" ]; then \
      sed -i "s|archive.ubuntu.com|${APT_MIRROR}|g; s|security.ubuntu.com|${APT_MIRROR}|g" \
        /etc/apt/sources.list; \
    fi

# Base apt packages
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gnupg lsb-release \
      git \
      build-essential pkg-config \
      tmux openssh-client \
      ripgrep jq vim less unzip xz-utils \
      postgresql-client \
      sudo \
      protobuf-compiler \
      software-properties-common \
  && rm -rf /var/lib/apt/lists/*

# Common curl flags for downloading large release artifacts. We add retry +
# stall detection because some egress paths to github releases can hang.
ENV DL_CURL_OPTS="-fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 30 --speed-limit 10240 --speed-time 30"

# gosu for the entrypoint privilege drop
ARG GOSU_VERSION=1.17
RUN ARCH="$(dpkg --print-architecture)" \
  && curl $DL_CURL_OPTS -o /usr/local/bin/gosu \
       "https://github.com/tianon/gosu/releases/download/${GOSU_VERSION}/gosu-${ARCH}" \
  && chmod +x /usr/local/bin/gosu \
  && gosu --version

# GitHub CLI installed from a release .deb. The official cli.github.com apt repo
# is unreliably slow from some regions; the github.com releases CDN tends to be
# more consistent. Pin a specific gh version to keep builds reproducible.
ARG GH_VERSION=2.91.0
RUN ARCH="$(dpkg --print-architecture)" \
  && curl $DL_CURL_OPTS -o /tmp/gh.deb \
       "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${ARCH}.deb" \
  && dpkg -i /tmp/gh.deb \
  && rm /tmp/gh.deb \
  && gh --version

# Go 1.25.x
ARG GO_VERSION=1.25.0
RUN ARCH_GO="$(dpkg --print-architecture)" \
  && case "$ARCH_GO" in \
       amd64) GO_ARCH=amd64 ;; \
       arm64) GO_ARCH=arm64 ;; \
       *) echo "unsupported architecture: $ARCH_GO" >&2 && exit 1 ;; \
     esac \
  && curl $DL_CURL_OPTS "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tgz \
  && tar -C /usr/local -xzf /tmp/go.tgz \
  && rm /tmp/go.tgz
ENV PATH=/usr/local/go/bin:$PATH

# Node.js 22 LTS via NodeSource
RUN curl $DL_CURL_OPTS https://deb.nodesource.com/setup_22.x | bash - \
  && apt-get install -y --no-install-recommends nodejs \
  && rm -rf /var/lib/apt/lists/*

# Python 3.12 via deadsnakes PPA (Ubuntu 22.04 archive only ships 3.10)
RUN add-apt-repository -y ppa:deadsnakes/ppa \
  && apt-get update && apt-get install -y --no-install-recommends \
       python3.12 python3.12-venv python3.12-dev python3-pip \
  && rm -rf /var/lib/apt/lists/* \
  && ln -sf /usr/bin/python3.12 /usr/local/bin/python3 \
  && ln -sf /usr/bin/python3.12 /usr/local/bin/python

# uv from Astral
RUN curl $DL_CURL_OPTS https://astral.sh/uv/install.sh \
      | env UV_INSTALL_DIR=/usr/local/bin sh

# Go developer tools, installed system-wide so the volume-mounted /home/dev/go
# at runtime does not shadow them.
# GOPROXY is parameterized so contributors in regions where proxy.golang.org
# is slow can override at build time, e.g. --build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
ENV GOTOOLS=/usr/local/go-tools
ENV GOBIN=$GOTOOLS/bin
ENV PATH=$GOBIN:$PATH
RUN mkdir -p $GOBIN \
  && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
  && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
  && go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest \
  && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
  && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest \
  && go install github.com/bufbuild/buf/cmd/buf@latest \
  && rm -rf /root/.cache/go-build /root/go/pkg

# AI coding agent CLIs (npm-distributed). NPM_REGISTRY is parameterized so
# contributors in regions where registry.npmjs.org is slow can override at
# build time, e.g. --build-arg NPM_REGISTRY=https://registry.npmmirror.com
ARG NPM_REGISTRY=https://registry.npmjs.org/
RUN npm config set registry $NPM_REGISTRY \
  && npm install -g \
       @anthropic-ai/claude-code \
       @openai/codex \
       opencode-ai \
  && npm cache clean --force

WORKDIR /workspace
